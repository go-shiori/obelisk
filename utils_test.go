package obelisk

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValitdURL(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEklEQVQIW2P8z8AARAwMjDAGACwBA/+8RVWvAAAAAElFTkSuQmCC"
	rawURL := "https://google.com/page#fragment?utm_source=google&utm_medium=cpc&utm_campaign=summer_sale"
	expected := []byte("TextforTest")
	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		fmt.Println("Failed to parse URL:", err)
		return
	}
	contentType := "text/plain"
	expectedResult := "data:text/plain;base64,VGV4dGZvclRlc3Q="

	t.Run("Test isvalidURL", func(t *testing.T) {
		result := isValidURL("https://github.com/go-shiori/obelisk")
		result2 := isValidURL("itIsNotAURL")
		assert.True(t, result)
		assert.False(t, result2)
	})

	t.Run("Test Create Absolute URL", func(t *testing.T) {

		resultdataURL := createAbsoluteURL(dataURL, parsedURL)
		resultRelativePath := createAbsoluteURL("/it/is/relarivepath", parsedURL)
		resulacualturl := createAbsoluteURL("https://bing.com", parsedURL)
		resulAcualtURLWithfragment := createAbsoluteURL(rawURL, parsedURL)
		resulWithoutURL := createAbsoluteURL("", parsedURL)
		resulWithfragment := createAbsoluteURL("#bar", parsedURL)

		assert.Equal(t, dataURL, resultdataURL)
		assert.Equal(t, "https://google.com/it/is/relarivepath", resultRelativePath)
		assert.Equal(t, "https://bing.com", resulacualturl)
		assert.Equal(t, "https://google.com/page%23fragment", resulAcualtURLWithfragment)
		assert.Equal(t, "", resulWithoutURL)
		assert.Equal(t, "#bar", resulWithfragment)

	})
	t.Run("Test create dataURL", func(t *testing.T) {
		result := createDataURL(expected, contentType)
		assert.Equal(t, expectedResult, result)
	})
	t.Run("s2b", func(t *testing.T) {
		result := s2b("TextforTest")
		assert.Equal(t, expected, result)
	})
	t.Run("b2s", func(t *testing.T) {
		result := b2s(expected)
		assert.Equal(t, "TextforTest", result)
	})
}
