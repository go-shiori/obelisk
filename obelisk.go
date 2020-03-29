package main

import "io"

// StartObelisk starts archival process for HTML page.
// It returns a single HTML file which embed its assets as base64.
func StartObelisk(r io.Reader) {
	// Parse input into HTML document

	// Prepare documents by doing following steps :
	// - Replace lazy loaded image with image from its <noscript> counterpart.
	// - Replace `data-src` and `data-srcset` attribute to `src` and `srcset`.
	// - Convert relative URL into absolute URL.
	// - Remove all <script> and <noscript>. We don't want to use any JS.
	// - Remove all comments from document.
	// All these steps is available in go-readability, so we can reuse them here.

	// Find all resources from nodes attribute or inline CSS, then embed it.
}
