package obelisk

import (
	"context"
	nurl "net/url"
	"strings"
	"testing"

	"github.com/go-shiori/dom"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/html"
)

func TestProcessHTML(t *testing.T) {
	arc := &Archiver{}

	// Create a mock

	t.Run("is fragment off", func(t *testing.T) {
		input := strings.NewReader("<html><body><h1>Hello, World!</h1></body></html>")
		baseURL, _ := nurl.Parse("https://example.com")
		result, err := arc.processHTML(context.Background(), input, baseURL, false)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Check if the result is correct
		expectedResult := "<html><head><meta charset=\"utf-8\"/><meta property=\"source:url\" content=\"https://example.com\"/><meta http-equiv=\"Content-Security-Policy\" content=\"default-src &#39;unsafe-inline&#39; &#39;self&#39; data:;\"/><meta http-equiv=\"Content-Security-Policy\" content=\"connect-src &#39;none&#39;;\"/></head><body><h1>Hello, World!</h1></body></html>"
		assert.Equal(t, expectedResult, result)
	})

	t.Run("is fragment on", func(t *testing.T) {
		input := strings.NewReader("<html><body><h1>Hello, World!</h1></body></html>")
		baseURL, _ := nurl.Parse("https://example.com")
		result, err := arc.processHTML(context.Background(), input, baseURL, true)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Check if the result is correct
		expectedResult := "<h1>Hello, World!</h1>"
		assert.Equal(t, expectedResult, result)
	})
}

func TestConvertNoScriptToDiv(t *testing.T) {
	// Create a sample HTML document
	htmlContent := `
	<html>
		<head></head>
		<body>
			<noscript>
				<p>This is first noscript element.</p>
			</noscript>
			<noscript>
				<p>This second noscript element.</p>
			</noscript>
			<div change=false>
				<p>This second noscript element.</p>
			</div>
		</body>
	</html>
	`

	// Parse the HTML document
	doc, err := html.Parse(strings.NewReader(htmlContent))
	assert.NoError(t, err)

	// Create an instance of the Archiver struct
	arc := &Archiver{}

	// Call the convertNoScriptToDiv function
	arc.convertNoScriptToDiv(doc, true)

	// Assert that the noscript element has been replaced with a div element
	divs := dom.GetElementsByTagName(doc, "div")

	assert.Equal(t, 3, len(divs))

	// Assert that the div element has the correct attribute
	div1 := divs[0]
	div2 := divs[1]
	div3 := divs[2]
	attr1 := dom.GetAttribute(div1, "data-obelisk-noscript")
	attr2 := dom.GetAttribute(div2, "data-obelisk-noscript")
	attr3 := dom.GetAttribute(div3, "change")
	assert.Equal(t, "true", attr1)
	assert.Equal(t, "true", attr2)
	// other div attr should not affect
	assert.Equal(t, "false", attr3)
}
