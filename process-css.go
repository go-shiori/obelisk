package obelisk

import (
	"bytes"
	"context"
	"io"
	nurl "net/url"
	"strings"
	"sync"

	"github.com/tdewolff/parse/v2/css"
	"golang.org/x/sync/errgroup"
)

func (arc *Archiver) processCSS(ctx context.Context, input io.Reader, baseURL *nurl.URL) (string, error) {
	// Prepare buffer to store content from input
	buffer := bytes.NewBuffer(nil)

	// Scan CSS and find all URLs
	urls := make(map[string]struct{})
	lexer := css.NewLexer(input)

	for {
		token, bt := lexer.Next()

		// Check for error or EOF
		if token == css.ErrorToken {
			break
		}

		// If it's URL save it
		if token == css.URLToken {
			urls[string(bt)] = struct{}{}
		}

		buffer.Write(bt)
	}

	// Process each url concurrently
	mutex := sync.RWMutex{}
	processedURLs := make(map[string]string)

	g, ctx := errgroup.WithContext(ctx)
	for url := range urls {
		url := url
		g.Go(func() error {
			cssURL := sanitizeStyleURL(url)
			cssURL = createAbsoluteURL(cssURL, baseURL)
			content, contentType, err := arc.processURL(ctx, cssURL, baseURL.String())
			if err != nil && err != errSkippedURL {
				return err
			}

			var result string
			if err == errSkippedURL {
				result = `url("` + cssURL + `")`
			} else {
				result = `url("` + createDataURL(content, contentType) + `")`
			}

			mutex.Lock()
			processedURLs[url] = result
			mutex.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return buffer.String(), err
	}

	// Convert all url into the processed URL
	cssRules := buffer.String()
	for url, processedURL := range processedURLs {
		cssRules = strings.ReplaceAll(cssRules, url, processedURL)
	}

	return cssRules, nil
}
