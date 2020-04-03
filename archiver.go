package obelisk

import (
	"context"
	"fmt"
	"io"
	nurl "net/url"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
)

// DefaultUserAgent is the default user agent to use, which is Chrome's.
var DefaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0"

// DefaultConfig is the default configuration for archiver.
var DefaultConfig = Config{
	MaxNDownload: 10,
	EnableLog:    false,
	UserAgent:    DefaultUserAgent,
	Cookie:       "",
}

// Config is configuration for archival process.
type Config struct {
	MaxNDownload int64
	EnableLog    bool
	UserAgent    string
	Cookie       string
}

// Request is data of archival request.
type Request struct {
	Input   io.Reader
	URL     string
	DstPath string
}

// archiver is the core of obelisk, which used to download a
// web page then embeds its assets.
type archiver struct {
	sync.RWMutex

	ctx         context.Context
	cache       map[string]string
	dlSemaphore *semaphore.Weighted
	logEnabled  bool
	userAgent   string
}

// Archive starts archival process for the specified request.
func Archive(ctx context.Context, req Request, cfg Config) error {
	// Validate config
	if cfg.MaxNDownload <= 0 {
		cfg.MaxNDownload = 10
	}

	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}

	// Validate request
	if req.URL == "" {
		return fmt.Errorf("request url is not specified")
	}

	if req.DstPath == "" {
		return fmt.Errorf("destination path is not specified")
	}

	// Create archiver
	arc := &archiver{
		ctx:         ctx,
		cache:       make(map[string]string),
		dlSemaphore: semaphore.NewWeighted(cfg.MaxNDownload),
		logEnabled:  cfg.EnableLog,
		userAgent:   cfg.UserAgent,
	}

	arc.log("Obelisk started")

	// Make sure request URL valid
	url, err := nurl.ParseRequestURI(req.URL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return fmt.Errorf("url \"%s\" is not valid", req.URL)
	}

	// If needed download page from source URL
	contentType := "text/html"
	if req.Input == nil {
		resp, err := downloadFile(url.String(), arc.userAgent)
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
		defer resp.Body.Close()

		req.Input = resp.Body
		contentType = resp.Header.Get("Content-Type")
	}

	// Check the type of the downloaded file.
	// If it's not HTML, just save it as it is to storage.
	if !strings.HasPrefix(contentType, "text/html") {
		err = saveToFile(req.Input, req.DstPath)
		return err
	}

	// If it's HTML process it
	result, err := arc.processHTML(ctx, req.Input, url)
	if err != nil {
		return err
	}

	err = saveToFile(strings.NewReader(result), req.DstPath)
	if err != nil {
		return err
	}

	arc.log("Obelisk finished")
	return nil
}
