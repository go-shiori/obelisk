package obelisk

import (
	"context"
	"fmt"
	"io/ioutil"
	nurl "net/url"
	"strings"

	"github.com/pkg/errors"
)

var errSkippedURL = errors.New("skip processing url")

func (arc *Archiver) processURL(ctx context.Context, url string, parentURL string, embedded ...bool) ([]byte, string, error) {
	// Parse embedded value
	isEmbedded := len(embedded) != 0 && embedded[0]

	// Make sure this URL is not empty, data or hash. If yes, just skip it.
	url = strings.TrimSpace(url)
	if url == "" || strings.HasPrefix(url, "data:") || strings.HasPrefix(url, "#") {
		return nil, "", errSkippedURL
	}

	// Parse URL to make sure it's valid request URL. If not, there might be
	// some error while preparing document, so just skip this URL
	parsedURL, err := nurl.ParseRequestURI(url)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Hostname() == "" {
		return nil, "", errSkippedURL
	}

	// Check in cache to see if this URL already processed
	arc.RLock()
	cache, cacheExist := arc.Cache[url]
	arc.RUnlock()

	if cacheExist {
		arc.logURL(url, parentURL, true)
		return cache.Data, cache.ContentType, nil
	}

	// Download the resource, use semaphore to limit concurrent downloads
	arc.logURL(url, parentURL, false)
	err = arc.dlSemaphore.Acquire(ctx, 1)
	if err != nil {
		return nil, "", nil
	}

	resp, err := arc.downloadFile(url, parentURL)
	arc.dlSemaphore.Release(1)
	if err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
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
			return nil, "", err
		}

	case contentType == "text/css":
		newCSS, err := arc.processCSS(ctx, resp.Body, parsedURL)
		if err == nil {
			bodyContent = []byte(newCSS)
		} else {
			return nil, "", err
		}

	default:
		bodyContent, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}
	}

	// Save data URL to cache
	arc.Lock()
	arc.Cache[url] = Asset{
		Data:        bodyContent,
		ContentType: contentType,
	}
	arc.Unlock()

	return bodyContent, contentType, nil
}
