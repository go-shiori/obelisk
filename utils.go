package obelisk

import (
	"encoding/base64"
	"fmt"
	nurl "net/url"
	"regexp"
	"strings"
)

var (
	rxStyleURL = regexp.MustCompile(`(?i)^url\((.+)\)$`)
)

// isValidURL checks if URL is valid.
func isValidURL(s string) bool {
	_, err := nurl.ParseRequestURI(s)
	return err == nil
}

// createAbsoluteURL convert url to absolute path based on base.
func createAbsoluteURL(url string, base *nurl.URL) string {
	url = strings.TrimSpace(url)
	if url == "" || base == nil {
		return ""
	}

	// If it is data url, return as it is
	if strings.HasPrefix(url, "data:") {
		return url
	}

	// If it is fragment path, return as it is
	if strings.HasPrefix(url, "#") {
		return url
	}

	// If it is already an absolute URL, clean the URL then return it
	tmp, err := nurl.ParseRequestURI(url)
	if err == nil && tmp.Scheme != "" && tmp.Hostname() != "" {
		cleanURL(tmp)
		return tmp.String()
	}

	// Otherwise, resolve against base URL.
	tmp, err = nurl.Parse(url)
	if err != nil {
		return url
	}

	cleanURL(tmp)
	return base.ResolveReference(tmp).String()
}

// cleanURL removes fragment (#fragment) and UTM queries from URL
func cleanURL(url *nurl.URL) {
	queries := url.Query()

	for key := range queries {
		if strings.HasPrefix(key, "utm_") {
			queries.Del(key)
		}
	}

	url.Fragment = ""
	url.RawQuery = queries.Encode()
}

// sanitizeStyleURL sanitizes the URL in CSS by removing `url()`,
// quotation mark and trailing slash
func sanitizeStyleURL(url string) string {
	cssURL := rxStyleURL.ReplaceAllString(url, "$1")
	cssURL = strings.TrimSpace(cssURL)

	if strings.HasPrefix(cssURL, `"`) {
		return strings.Trim(cssURL, `"`)
	}

	if strings.HasPrefix(cssURL, `'`) {
		return strings.Trim(cssURL, `'`)
	}

	return cssURL
}

// createDataURL returns base64 encoded data URL
func createDataURL(content []byte, contentType string) string {
	b64encoded := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf("data:%s;base64,%s", contentType, b64encoded)
}
