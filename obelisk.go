package main

import (
	"fmt"
	nurl "net/url"
	"strings"
)

// Start starts archival process for web page.
// It returns a single HTML file which embed its assets as base64.
func Start(sourceURL string, dstPath string) error {
	// Make sure source URL valid
	url, err := nurl.ParseRequestURI(sourceURL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return fmt.Errorf("url \"%s\" is not valid", sourceURL)
	}

	// Download page from source URL
	resp, err := downloadFile(url.String(), "")
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	// Check the type of the downloaded file.
	// If it's not HTML, just save it as it is to storage.
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		err = saveToFile(resp.Body, dstPath)
		if err != nil {
			return fmt.Errorf("failed to save file: %w", err)
		}

		return nil
	}

	return nil
}
