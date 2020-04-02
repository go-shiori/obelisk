package main

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
	rxImageSrcAttr    = regexp.MustCompile(`(?i)^\s*\S+\.(jpg|jpeg|png|webp)\S*\s*$`)
	rxImageSrcsetAttr = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|webp)\s+\d`)
	rxImageSrcsetURL  = regexp.MustCompile(`(?i)(\S+)(\s+\d+[xw])?,?`)
)

func (arc *Archiver) processHTML(ctx context.Context, input io.Reader, baseURL *nurl.URL) (string, error) {
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

func (arc *Archiver) processStyleAttr(ctx context.Context, node *html.Node, baseURL *nurl.URL) error {
	style := dom.GetAttribute(node, "style")
	newStyle, err := arc.processCSS(ctx, strings.NewReader(style), baseURL)
	if err == nil {
		dom.SetAttribute(node, "style", newStyle)
	}

	return err
}

func (arc *Archiver) processStyleNode(ctx context.Context, node *html.Node, baseURL *nurl.URL) error {
	style := dom.TextContent(node)
	newStyle, err := arc.processCSS(ctx, strings.NewReader(style), baseURL)
	if err == nil {
		dom.SetTextContent(node, newStyle)
	}

	return err
}

func (arc *Archiver) processURLNode(ctx context.Context, node *html.Node, attrName string) error {
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

func (arc *Archiver) processMediaNode(ctx context.Context, node *html.Node) error {
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
	for _, parts := range rxImageSrcsetURL.FindAllStringSubmatch(srcset, -1) {
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

// replaceLazyImage find all <noscript> that located after <img> node,
// and contains exactly single <img> element. Once it found, this method
// will replace the previous <img> with <img> inside <noscript>, then finally
// remove the <noscript> tag. This is done because in some website (e.g. Medium),
// they use lazy load method like this.
// This is ADDITIONAL and doesn't exist yet in readability.js.
func (arc *Archiver) replaceLazyImage(doc *html.Node) {
	// First, find div which only contains single img element, then put it out.
	for _, div := range dom.GetElementsByTagName(doc, "div") {
		if children := dom.Children(div); len(children) == 1 && dom.TagName(children[0]) == "img" {
			dom.ReplaceChild(div.Parent, children[0], div)
		}
	}

	// Next find img without source, and remove it. This is done to
	// prevent a placeholder img is replaced by img from noscript in next step.
	for _, img := range dom.GetElementsByTagName(doc, "img") {
		src := dom.GetAttribute(img, "src")
		srcset := dom.GetAttribute(img, "srcset")
		dataSrc := dom.GetAttribute(img, "data-src")
		dataSrcset := dom.GetAttribute(img, "data-srcset")

		if src == "" && srcset == "" && dataSrc == "" && dataSrcset == "" {
			img.Parent.RemoveChild(img)
		}
	}

	// Next find noscript and try to extract its image
	for _, noscript := range dom.GetElementsByTagName(doc, "noscript") {
		// Make sure prev sibling is exist and it's image
		prevElement := dom.PreviousElementSibling(noscript)
		if dom.TagName(prevElement) != "img" {
			continue
		}

		// In Go content of noscript is treated as string, so here we parse it.
		noscriptContent := dom.TextContent(noscript)
		tmpDoc, err := html.Parse(strings.NewReader(noscriptContent))
		if err != nil {
			continue
		}

		// Make sure noscript only has one children, and it's <img> element
		tmpBody := dom.GetElementsByTagName(tmpDoc, "body")[0]
		children := dom.Children(tmpBody)
		if len(children) != 1 || dom.TagName(children[0]) != "img" {
			continue
		}

		// At this point, just replace the previous img with img from noscript
		dom.ReplaceChild(noscript.Parent, children[0], prevElement)
	}
}

// convertLazyImageAttrs convert attributes data-src and data-srcset
// which often found in lazy-loaded images and pictures, into basic attribute
// src and srcset, so images that can be loaded without JS.
func (arc *Archiver) convertLazyImageAttrs(doc *html.Node) {
	for _, elem := range dom.GetAllNodesWithTag(doc, "img", "picture", "figure") {
		src := dom.GetAttribute(elem, "src")
		srcset := dom.GetAttribute(elem, "srcset")
		nodeTag := dom.TagName(elem)
		nodeClass := dom.ClassName(elem)

		// In some sites (e.g. Kotaku), they put 1px square image as data uri in
		// the src attribute. So, here we check if the data uri is too short,
		// just might as well remove it.
		if src != "" && strings.HasPrefix(src, "data:") {
			// I don't have any source but I guess if image is less
			// than 100 bytes it will be too small, therefore it might
			// be placeholder image. With that said, I will use 100B
			// as threshold (or 133B after encoded to base64).
			b64starts := strings.Index(src, "base64,") + 7
			b64length := len(src) - b64starts
			if b64length < 133 {
				src = ""
				dom.RemoveAttribute(elem, "src")
			}
		}

		// Some websites store their resource for lazy-loaded image in data- attributes
		// (e.g. websites that uses LazyLoad library), so here we try to move it.
		if dataSrc := dom.GetAttribute(elem, "data-src"); dataSrc != "" {
			src = dataSrc
			dom.SetAttribute(elem, "src", dataSrc)
			dom.RemoveAttribute(elem, "data-src")
		}

		if dataSrcset := dom.GetAttribute(elem, "data-srcset"); dataSrcset != "" {
			srcset = dataSrcset
			dom.SetAttribute(elem, "srcset", dataSrcset)
			dom.RemoveAttribute(elem, "data-srcset")
		}

		if (src != "" || srcset != "") && !strings.Contains(strings.ToLower(nodeClass), "lazy") {
			continue
		}

		for _, attr := range elem.Attr {
			if attr.Key == "src" || attr.Key == "srcset" {
				continue
			}

			copyTo := ""
			if rxImageSrcsetAttr.MatchString(attr.Val) {
				copyTo = "srcset"
			} else if rxImageSrcAttr.MatchString(attr.Val) {
				copyTo = "src"
			}

			if copyTo == "" {
				continue
			}

			if nodeTag == "img" || nodeTag == "picture" {
				// if this is an img or picture, set the attribute directly
				dom.SetAttribute(elem, copyTo, attr.Val)
			} else if nodeTag == "figure" {
				// if the item is a <figure> that does not contain an image or picture,
				// create one and place it inside the figure
				if len(dom.GetAllNodesWithTag(elem, "img", "picture")) == 0 {
					img := dom.CreateElement("img")
					dom.SetAttribute(img, copyTo, attr.Val)
					dom.AppendChild(elem, img)
				}
			}
		}
	}
}

// convertRelativeURLs converts all relative URL in document into absolute URL.
// We do this for a, img, picture, figure, video, audio, source, link,
// embed, iframe and object.
func (arc *Archiver) convertRelativeURLs(doc *html.Node, baseURL *nurl.URL) {
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
		if !dom.HasAttribute(media, "srcset") {
			continue
		}

		var newSets []string
		srcset := dom.GetAttribute(media, "srcset")
		for _, parts := range rxImageSrcsetURL.FindAllStringSubmatch(srcset, -1) {
			oldURL := parts[1]
			targetWidth := parts[2]

			newSet := createAbsoluteURL(oldURL, baseURL)
			newSet += targetWidth
			newSets = append(newSets, newSet)
		}

		newSrcset := strings.Join(newSets, ",")
		dom.SetAttribute(media, "srcset", newSrcset)
	}
}

// removeScripts removes script and noscript tags from the document.
func (arc *Archiver) removeScripts(doc *html.Node) {
	scripts := dom.GetAllNodesWithTag(doc, "script", "noscript")
	dom.RemoveNodes(scripts, nil)
}

// removeLinkIntegrityAttrs removes integrity attributes from link tags.
func (arc *Archiver) removeLinkIntegrityAttr(doc *html.Node) {
	for _, link := range dom.GetElementsByTagName(doc, "link") {
		dom.RemoveAttribute(link, "integrity")
	}
}

// removeComments find all comments in document then remove it.
func (arc *Archiver) removeComments(doc *html.Node) {
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
