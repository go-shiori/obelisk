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
	"golang.org/x/sync/semaphore"
)

var (
	rxImageDataSrcset = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|webp)\s+\d`)
	rxImageDataSrc    = regexp.MustCompile(`(?i)^\s*\S+\.(jpg|jpeg|png|webp)\S*\s*$`)
	rxImageMeta       = regexp.MustCompile(`(?i)image|thumbnail`)
)

func processHTML(ctx context.Context, sem *semaphore.Weighted, input io.Reader, baseURL *nurl.URL) error {
	// Parse input into HTML document
	doc, err := html.Parse(input)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Prepare documents by doing these steps :
	// - Replace lazy loaded image with image from its noscript counterpart
	// - Convert data-src and data-srcset attribute in lazy image to src and srcset
	// - Convert relative URL into absolute URL
	// - Remove all script and noscript tags
	// - Remove all comments in documents
	replaceLazyImage(doc)
	convertLazyImageAttrs(doc)
	convertRelativeURLs(doc, baseURL)
	removeScripts(doc)
	removeComments(doc)

	// Find all nodes which might has subresource.
	// A node might has subresource if it fulfills one of these criterias :
	// - It has inline style;
	// - It's tag name is meta, and it contains link to image for social media.
	// - It's tag name is either style, img, picture, figure, video, audio, source,
	//   link, iframe or object;
	mapNodes := make(map[*html.Node]struct{})
	for _, node := range dom.GetElementsByTagName(doc, "*") {
		if style := dom.GetAttribute(node, "style"); strings.TrimSpace(style) != "" {
			mapNodes[node] = struct{}{}
			continue
		}

		switch dom.TagName(node) {
		case "meta":
			name := dom.GetAttribute(node, "name")
			property := dom.GetAttribute(node, "property")
			if rxImageMeta.MatchString(name + " " + property) {
				mapNodes[node] = struct{}{}
			}

		case "style", "img", "picture", "figure", "video", "audio", "source", "link", "iframe", "object":
			mapNodes[node] = struct{}{}
		}
	}

	// Process each node with resources concurrently.
	g, ctx := errgroup.WithContext(ctx)
	for node := range mapNodes {
		// Try to acquire semaphore
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}

		// Process sub resource in background
		node := node
		g.Go(func() error {
			defer sem.Release(1)

			if style := dom.GetAttribute(node, "style"); strings.TrimSpace(style) != "" {
				newStyle, err := processCSS(ctx, sem, strings.NewReader(style), baseURL)
				if err != nil {
					return err
				}
				dom.SetAttribute(node, "style", newStyle)
			}

			return nil
		})
	}

	g.Wait()
	return nil
}

// replaceLazyImage find all <noscript> that located after <img> node,
// and contains exactly single <img> element. Once it found, this method
// will replace the previous <img> with <img> inside <noscript>, then finally
// remove the <noscript> tag. This is done because in some website (e.g. Medium),
// they use lazy load method like this.
// This is ADDITIONAL and doesn't exist yet in readability.js.
func replaceLazyImage(doc *html.Node) {
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
		tmp, err := parseHTMLString(dom.TextContent(noscript))
		if err != nil {
			continue
		}

		// Make sure noscript only has one children, and it's <img> element
		children := dom.Children(tmp)
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
func convertLazyImageAttrs(doc *html.Node) {
	for _, elem := range dom.GetAllNodesWithTag(doc, "img", "picture", "figure") {
		src := dom.GetAttribute(elem, "src")
		srcset := dom.GetAttribute(elem, "srcset")
		nodeTag := dom.TagName(elem)
		nodeClass := dom.ClassName(elem)

		if (src != "" || srcset != "") && !strings.Contains(strings.ToLower(nodeClass), "lazy") {
			continue
		}

		for _, attr := range elem.Attr {
			if attr.Key == "src" || attr.Key == "srcset" {
				continue
			}

			copyTo := ""
			if rxImageDataSrcset.MatchString(attr.Val) {
				copyTo = "srcset"
			} else if rxImageDataSrc.MatchString(attr.Val) {
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

// convertRelativeURLs converts each <a> and <img> uri in the given element
// to an absolute URL, ignoring #ref URLs.
func convertRelativeURLs(doc *html.Node, baseURL *nurl.URL) {
	for _, link := range dom.GetElementsByTagName(doc, "a") {
		href := dom.GetAttribute(link, "href")
		if href == "" {
			continue
		}

		// Remove links with javascript: URIs, since they won't
		// work after scripts have been removed from the page.
		if strings.HasPrefix(href, "javascript:") {
			linkChilds := dom.ChildNodes(link)

			if len(linkChilds) == 1 && linkChilds[0].Type == html.TextNode {
				// If the link only contains simple text content,
				// it can be converted to a text node
				text := dom.CreateTextNode(dom.TextContent(link))
				dom.ReplaceChild(link.Parent, text, link)
			} else {
				// If the link has multiple children, they should
				// all be preserved
				container := dom.CreateElement("span")
				for _, child := range linkChilds {
					container.AppendChild(dom.CloneNode(child))
				}
				dom.ReplaceChild(link.Parent, container, link)
			}
		} else {
			newHref := createAbsoluteURL(href, baseURL)
			if newHref == "" {
				dom.RemoveAttribute(link, "href")
			} else {
				dom.SetAttribute(link, "href", newHref)
			}
		}
	}

	for _, img := range dom.GetElementsByTagName(doc, "img") {
		src := dom.GetAttribute(img, "src")
		if src == "" {
			continue
		}

		newSrc := createAbsoluteURL(src, baseURL)
		if newSrc == "" {
			dom.RemoveAttribute(img, "src")
		} else {
			dom.SetAttribute(img, "src", newSrc)
		}
	}
}

// removeScripts removes script and noscript tags from the document.
func removeScripts(doc *html.Node) {
	scripts := dom.GetAllNodesWithTag(doc, "script", "noscript")
	dom.RemoveNodes(scripts, nil)
}

// removeComments find all comments in document then remove it.
func removeComments(doc *html.Node) {
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

func parseHTMLString(str string) (*html.Node, error) {
	doc, err := html.Parse(strings.NewReader(str))
	if err != nil {
		return nil, err
	}

	body := dom.GetElementsByTagName(doc, "body")[0]
	return body, nil
}
