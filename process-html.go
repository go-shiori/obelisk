package obelisk

import (
	"context"
	"fmt"
	"io"
	nurl "net/url"
	"regexp"
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

var (
	rxLazyImageSrc    = regexp.MustCompile(`(?i)^\s*\S+\.(jpg|jpeg|png|webp)\S*\s*$`)
	rxLazyImageSrcset = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|webp)\s+\d`)
	rxImgExtensions   = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|webp)`)
	rxSrcsetURL       = regexp.MustCompile(`(?i)(\S+)(\s+[\d.]+[xw])?(\s*(?:,|$))`)
	rxB64DataURL      = regexp.MustCompile(`(?i)^data:\s*([^\s;,]+)\s*;\s*base64\s*`)
)

func (arc *archiver) processHTML(ctx context.Context, input io.Reader, baseURL *nurl.URL) (string, error) {
	// Parse input into HTML document
	doc, err := html.Parse(input)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Prepare documents by doing these steps :
	// - Replace lazy loaded image with image from its noscript counterpart
	// - Convert data-src and data-srcset attribute in lazy image to src and srcset
	// - Convert relative URL into absolute URL
	// - Remove all script and noscript tags
	// - Remove subresources integrity attribute from links
	// - Remove all comments in documents
	arc.replaceLazyImage(doc)
	arc.convertLazyImageAttrs(doc)
	arc.convertRelativeURLs(doc, baseURL)
	arc.removeScripts(doc)
	arc.removeLinkIntegrityAttr(doc)
	arc.removeComments(doc)

	// Find all nodes which might has subresource.
	// A node might has subresource if it fulfills one of these criterias :
	// - It has inline style;
	// - It's link for icon or stylesheets;
	// - It's tag name is either style, img, picture, figure, video, audio, source, iframe or object;
	resourceNodes := make(map[*html.Node]struct{})
	for _, node := range dom.GetElementsByTagName(doc, "*") {
		if style := dom.GetAttribute(node, "style"); strings.TrimSpace(style) != "" {
			resourceNodes[node] = struct{}{}
			continue
		}

		switch dom.TagName(node) {
		case "link":
			rel := dom.GetAttribute(node, "rel")
			if strings.Contains(rel, "icon") || strings.Contains(rel, "stylesheet") {
				resourceNodes[node] = struct{}{}
			}

		case "iframe", "embed", "object", "style",
			"img", "picture", "figure", "video", "audio", "source":
			resourceNodes[node] = struct{}{}
		}
	}

	// Process each node concurrently
	g, ctx := errgroup.WithContext(ctx)
	for node := range resourceNodes {
		node := node
		g.Go(func() error {
			// Update style attribute
			if dom.HasAttribute(node, "style") {
				err := arc.processStyleAttr(ctx, node, baseURL)
				if err != nil {
					return err
				}
			}

			// Update node depending on its tag name
			switch dom.TagName(node) {
			case "link":
				return arc.processURLNode(ctx, node, "href")
			case "object":
				return arc.processURLNode(ctx, node, "data")
			case "embed", "iframe":
				return arc.processURLNode(ctx, node, "src")
			case "style":
				return arc.processStyleNode(ctx, node, baseURL)
			case "img", "picture", "figure", "video", "audio", "source":
				return arc.processMediaNode(ctx, node)
			default:
				return nil
			}
		})
	}

	// Wait until all resources processed
	if err = g.Wait(); err != nil {
		return "", err
	}

	// Convert document back to string
	docHTML := dom.OuterHTML(doc)
	return docHTML, nil
}

func (arc *archiver) processStyleAttr(ctx context.Context, node *html.Node, baseURL *nurl.URL) error {
	style := dom.GetAttribute(node, "style")
	newStyle, err := arc.processCSS(ctx, strings.NewReader(style), baseURL)
	if err == nil {
		dom.SetAttribute(node, "style", newStyle)
	}

	return err
}

func (arc *archiver) processStyleNode(ctx context.Context, node *html.Node, baseURL *nurl.URL) error {
	style := dom.TextContent(node)
	newStyle, err := arc.processCSS(ctx, strings.NewReader(style), baseURL)
	if err == nil {
		dom.SetTextContent(node, newStyle)
	}

	return err
}

func (arc *archiver) processURLNode(ctx context.Context, node *html.Node, attrName string) error {
	if !dom.HasAttribute(node, attrName) {
		return nil
	}

	url := dom.GetAttribute(node, attrName)
	newURL, err := arc.processURL(ctx, url)
	if err == nil {
		dom.SetAttribute(node, attrName, newURL)
	}

	return err
}

func (arc *archiver) processMediaNode(ctx context.Context, node *html.Node) error {
	err := arc.processURLNode(ctx, node, "src")
	if err != nil {
		return err
	}

	err = arc.processURLNode(ctx, node, "poster")
	if err != nil {
		return err
	}

	if !dom.HasAttribute(node, "srcset") {
		return nil
	}

	var newSets []string
	srcset := dom.GetAttribute(node, "srcset")
	for _, parts := range rxSrcsetURL.FindAllStringSubmatch(srcset, -1) {
		oldURL := parts[1]
		targetWidth := parts[2]
		newSet, err := arc.processURL(ctx, oldURL)
		if err != nil {
			return err
		}

		newSet += targetWidth
		newSets = append(newSets, newSet)
	}

	newSrcset := strings.Join(newSets, ",")
	dom.SetAttribute(node, "srcset", newSrcset)
	return nil
}

// replaceLazyImage finds all <noscript> that are located after <img> nodes,
// and which contain only one <img> element. Replace the first image with
// the image from inside the <noscript> tag, and remove the <noscript> tag.
// This improves the quality of the images we use on some sites (e.g. Medium).
func (arc *archiver) replaceLazyImage(doc *html.Node) {
	// Find img without source or attributes that might contains image, and
	// remove it. This is done to prevent a placeholder img is replaced by
	// img from noscript in next step.
	imgs := dom.GetElementsByTagName(doc, "img")
	dom.ForEachNode(imgs, func(img *html.Node, _ int) {
		for _, attr := range img.Attr {
			switch attr.Key {
			case "src", "data-src", "srcset", "data-srcset":
				return
			}

			if rxImgExtensions.MatchString(attr.Val) {
				return
			}
		}

		img.Parent.RemoveChild(img)
	})

	// Next find noscript and try to extract its image
	noscripts := dom.GetElementsByTagName(doc, "noscript")
	dom.ForEachNode(noscripts, func(noscript *html.Node, _ int) {
		// Parse content of noscript and make sure it only contains image
		noscriptContent := dom.TextContent(noscript)
		tmpDoc, err := html.Parse(strings.NewReader(noscriptContent))
		if err != nil {
			return
		}

		tmpBody := dom.GetElementsByTagName(tmpDoc, "body")[0]
		if !arc.isSingleImage(tmpBody) {
			return
		}

		// If noscript has previous sibling and it only contains image,
		// replace it with noscript content. However we also keep old
		// attributes that might contains image.
		prevElement := dom.PreviousElementSibling(noscript)
		if prevElement != nil && arc.isSingleImage(prevElement) {
			prevImg := prevElement
			if dom.TagName(prevImg) != "img" {
				prevImg = dom.GetElementsByTagName(prevElement, "img")[0]
			}

			newImg := dom.GetElementsByTagName(tmpBody, "img")[0]
			for _, attr := range prevImg.Attr {
				if attr.Val == "" {
					continue
				}

				if attr.Key == "src" || attr.Key == "srcset" || rxImgExtensions.MatchString(attr.Val) {
					if dom.GetAttribute(newImg, attr.Key) == attr.Val {
						continue
					}

					attrName := attr.Key
					if dom.HasAttribute(newImg, attrName) {
						attrName = "data-old-" + attrName
					}

					dom.SetAttribute(newImg, attrName, attr.Val)
				}
			}

			dom.ReplaceChild(noscript.Parent, dom.FirstElementChild(tmpBody), prevElement)
		}
	})
}

// convertLazyImageAttrs convert attributes data-src and data-srcset
// which often found in lazy-loaded images and pictures, into basic attribute
// src and srcset, so images that can be loaded without JS.
func (arc *archiver) convertLazyImageAttrs(doc *html.Node) {
	imageNodes := dom.GetAllNodesWithTag(doc, "img", "picture", "figure")
	dom.ForEachNode(imageNodes, func(elem *html.Node, _ int) {
		src := dom.GetAttribute(elem, "src")
		srcset := dom.GetAttribute(elem, "srcset")
		nodeTag := dom.TagName(elem)
		nodeClass := dom.ClassName(elem)

		// In some sites (e.g. Kotaku), they put 1px square image as data uri in
		// the src attribute. So, here we check if the data uri is too short,
		// just might as well remove it.
		if src != "" && rxB64DataURL.MatchString(src) {
			// Make sure it's not SVG, because SVG can have a meaningful image
			// in under 133 bytes.
			parts := rxB64DataURL.FindStringSubmatch(src)
			if parts[1] == "image/svg+xml" {
				return
			}

			// Make sure this element has other attributes which contains
			// image. If it doesn't, then this src is important and
			// shouldn't be removed.
			srcCouldBeRemoved := false
			for _, attr := range elem.Attr {
				if attr.Key == "src" {
					continue
				}

				if rxImgExtensions.MatchString(attr.Val) {
					srcCouldBeRemoved = true
					break
				}
			}

			// Here we assume if image is less than 100 bytes (or 133B
			// after encoded to base64) it will be too small, therefore
			// it might be placeholder image.
			if srcCouldBeRemoved {
				b64starts := strings.Index(src, "base64") + 7
				b64length := len(src) - b64starts
				if b64length < 133 {
					src = ""
					dom.RemoveAttribute(elem, "src")
				}
			}
		}

		if (src != "" || srcset != "") && !strings.Contains(strings.ToLower(nodeClass), "lazy") {
			return
		}

		for i := 0; i < len(elem.Attr); i++ {
			attr := elem.Attr[i]
			if attr.Key == "src" || attr.Key == "srcset" {
				continue
			}

			copyTo := ""
			if rxLazyImageSrcset.MatchString(attr.Val) {
				copyTo = "srcset"
			} else if rxLazyImageSrc.MatchString(attr.Val) {
				copyTo = "src"
			}

			if copyTo == "" {
				continue
			}

			if nodeTag == "img" || nodeTag == "picture" {
				// if this is an img or picture, set the attribute directly
				dom.SetAttribute(elem, copyTo, attr.Val)
			} else if nodeTag == "figure" && len(dom.GetAllNodesWithTag(elem, "img", "picture")) == 0 {
				// if the item is a <figure> that does not contain an image or picture,
				// create one and place it inside the figure see the nytimes-3
				// testcase for an example
				img := dom.CreateElement("img")
				dom.SetAttribute(img, copyTo, attr.Val)
				dom.AppendChild(elem, img)
			}
		}
	})
}

// convertRelativeURLs converts all relative URL in document into absolute URL.
// We do this for a, img, picture, figure, video, audio, source, link,
// embed, iframe and object.
func (arc *archiver) convertRelativeURLs(doc *html.Node, baseURL *nurl.URL) {
	// Prepare nodes and methods
	as := dom.GetElementsByTagName(doc, "a")
	links := dom.GetElementsByTagName(doc, "link")
	embeds := dom.GetElementsByTagName(doc, "embed")
	iframes := dom.GetElementsByTagName(doc, "iframe")
	objects := dom.GetElementsByTagName(doc, "object")
	medias := dom.GetAllNodesWithTag(doc, "img", "picture", "figure", "video", "audio", "source")

	convertNode := func(node *html.Node, attrName string) {
		if dom.HasAttribute(node, attrName) {
			val := dom.GetAttribute(node, attrName)
			newVal := createAbsoluteURL(val, baseURL)
			dom.SetAttribute(node, attrName, newVal)
		}
	}

	convertNodes := func(nodes []*html.Node, attrName string) {
		for _, node := range nodes {
			convertNode(node, attrName)
		}
	}

	// Convert all relative URLs
	convertNodes(as, "href")
	convertNodes(links, "href")
	convertNodes(embeds, "src")
	convertNodes(iframes, "src")
	convertNodes(objects, "data")

	for _, media := range medias {
		convertNode(media, "src")
		convertNode(media, "poster")

		if srcset := dom.GetAttribute(media, "srcset"); srcset != "" {
			newSrcset := rxSrcsetURL.ReplaceAllStringFunc(srcset, func(s string) string {
				p := rxSrcsetURL.FindStringSubmatch(s)
				return createAbsoluteURL(p[1], baseURL) + p[2] + p[3]
			})
			dom.SetAttribute(media, "srcset", newSrcset)
		}
	}
}

// removeScripts removes script and noscript tags from the document.
func (arc *archiver) removeScripts(doc *html.Node) {
	scripts := dom.GetAllNodesWithTag(doc, "script", "noscript")
	dom.RemoveNodes(scripts, nil)
}

// removeLinkIntegrityAttrs removes integrity attributes from link tags.
func (arc *archiver) removeLinkIntegrityAttr(doc *html.Node) {
	for _, link := range dom.GetElementsByTagName(doc, "link") {
		dom.RemoveAttribute(link, "integrity")
	}
}

// removeComments find all comments in document then remove it.
func (arc *archiver) removeComments(doc *html.Node) {
	// Find all comments
	var comments []*html.Node
	var finder func(*html.Node)

	finder = func(node *html.Node) {
		if node.Type == html.CommentNode {
			comments = append(comments, node)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			finder(child)
		}
	}

	for child := doc.FirstChild; child != nil; child = child.NextSibling {
		finder(child)
	}

	// Remove it
	dom.RemoveNodes(comments, nil)
}

// isSingleImage checks if node is image, or if node contains exactly
// only one image whether as a direct child or as its descendants.
func (arc *archiver) isSingleImage(node *html.Node) bool {
	if dom.TagName(node) == "img" {
		return true
	}

	children := dom.Children(node)
	textContent := dom.TextContent(node)
	if len(children) != 1 || strings.TrimSpace(textContent) != "" {
		return false
	}

	return arc.isSingleImage(children[0])
}
