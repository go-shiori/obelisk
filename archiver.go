package main

import (
	"context"
	"fmt"
	"log"
	nurl "net/url"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
)

// Archiver is the core of Obelisk.
// It's purpose is to download a web page then embeds its assets.
type Archiver struct {
	sync.RWMutex

	ctx         context.Context
	cache       map[string]string
	dlSemaphore *semaphore.Weighted
	userAgent   string
}

// NewArchiver returns a new Archiver
func NewArchiver() *Archiver {
	return &Archiver{
		cache:       make(map[string]string),
		dlSemaphore: semaphore.NewWeighted(10),
		userAgent:   "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0",
	}
}

// Start starts archival process for web page.
// It returns a single HTML file which embed its assets as base64.
func (arc *Archiver) Start(ctx context.Context, sourceURL string, dstPath string) error {
	log.Println("Obelisk started")

	// Clear archiver cache
	arc.cache = make(map[string]string)

	// Make sure source URL valid
	url, err := nurl.ParseRequestURI(sourceURL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return fmt.Errorf("url \"%s\" is not valid", sourceURL)
	}

	// Download page from source URL
	resp, err := downloadFile(url.String(), arc.userAgent)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	// Check the type of the downloaded file.
	// If it's not HTML, just save it as it is to storage.
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		err = saveToFile(resp.Body, dstPath)
		return err
	}

	// If it's HTML process it
	result, err := arc.processHTML(ctx, resp.Body, url)
	if err != nil {
		return err
	}

	err = saveToFile(strings.NewReader(result), dstPath)
	if err != nil {
		return err
	}

	log.Println("Obelisk finished")
	return nil
}
