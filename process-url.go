package obelisk

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	nurl "net/url"
	"strings"
)

func (arc *archiver) processURL(ctx context.Context, url string, parentURL string, embedded ...bool) (string, error) {
	// Parse embedded value
	isEmbedded := len(embedded) != 0 && embedded[0]

	// Make sure this URL is not empty, data or hash
	url = strings.TrimSpace(url)
	if url == "" || strings.HasPrefix(url, "data:") || strings.HasPrefix(url, "#") {
		return url, nil
	}

	// Parse URL to make sure it's valid request URL
	// If not, there might be some error while preparing document, so
	// just return the URL as it is
	parsedURL, err := nurl.ParseRequestURI(url)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Hostname() == "" {
		return url, nil
	}

	// Check in cache to see if this URL already processed
	arc.RLock()
	cache, exist := arc.cache[url]
	arc.RUnlock()

	if exist {
		arc.logf("(CACHE) %s\n\tfrom %s\n", url, parentURL)
		return cache, nil
	}

	// Download the resource, use semaphore to limit concurrent downloads
	arc.logf("%s\n\tfrom %s\n", url, parentURL)
	err = arc.dlSemaphore.Acquire(ctx, 1)
	if err != nil {
		return url, nil
	}

	resp, err := arc.downloadFile(url)
	arc.dlSemaphore.Release(1)
	if err != nil {
		return url, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	// Get content type
	contentType := resp.Header.Get("Content-Type")
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "text/plain"
	}

	// Read content of response body. If the downloaded file is HTML
	// or CSS it need to be processed again
	var bodyContent []byte

	switch {
	case contentType == "text/html" && isEmbedded:
		newHTML, err := arc.processHTML(ctx, resp.Body, parsedURL)
		if err == nil {
			bodyContent = []byte(newHTML)
		} else {
			return url, err
		}

	case contentType == "text/css":
		newCSS, err := arc.processCSS(ctx, resp.Body, parsedURL)
		if err == nil {
			bodyContent = []byte(newCSS)
		} else {
			return url, err
		}

	default:
		bodyContent, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return url, err
		}
	}

	// Create data URL
	b64encoded := base64.StdEncoding.EncodeToString(bodyContent)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, b64encoded)

	// Save data URL to cache
	arc.Lock()
	arc.cache[url] = dataURL
	arc.Unlock()

	return dataURL, nil
}
