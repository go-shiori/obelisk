package obelisk

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	nurl "net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

var (
	// DefaultConfig is the default configuration for archiver.
	DefaultConfig = Config{}

	// DefaultUserAgent is the default user agent to use, which is Chrome's.
	DefaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0"
)

// Config is configuration for archival process.
type Config struct {
	UserAgent        string
	EnableLog        bool
	EnableVerboseLog bool

	DisableJS     bool
	DisableCSS    bool
	DisableEmbeds bool
	DisableMedias bool

	RequestTimeout        time.Duration
	SkipTLSVerification   bool
	MaxConcurrentDownload int64
}

// Request is data of archival request.
type Request struct {
	Input   io.Reader
	URL     string
	Cookies []*http.Cookie
}

// archiver is the core of obelisk, which used to download a
// web page then embeds its assets.
type archiver struct {
	sync.RWMutex

	ctx          context.Context
	cache        map[string][]byte
	contentTypes map[string]string
	dlSemaphore  *semaphore.Weighted

	config     Config
	rootURL    string
	cookies    []*http.Cookie
	httpClient *http.Client
}

// Archive starts archival process for the specified request.
// Returns the archival result, content type and error if there are any.
func Archive(ctx context.Context, req Request, cfg Config) ([]byte, string, error) {
	// Validate config
	if cfg.MaxConcurrentDownload <= 0 {
		cfg.MaxConcurrentDownload = 10
	}

	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}

	// Validate request
	if req.URL == "" {
		return nil, "", fmt.Errorf("request url is not specified")
	}

	// Make sure request URL valid
	url, err := nurl.ParseRequestURI(req.URL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return nil, "", fmt.Errorf("url \"%s\" is not valid", req.URL)
	}

	// Create archiver
	rootURL := *url
	rootURL.Fragment = ""
	for key := range rootURL.Query() {
		rootURL.Query().Del(key)
	}

	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.SkipTLSVerification,
			},
		},
		Jar: nil,
	}

	arc := &archiver{
		ctx:          ctx,
		cache:        make(map[string][]byte),
		contentTypes: make(map[string]string),
		dlSemaphore:  semaphore.NewWeighted(cfg.MaxConcurrentDownload),

		config:     cfg,
		rootURL:    rootURL.String(),
		cookies:    req.Cookies,
		httpClient: httpClient,
	}

	// If needed download page from source URL
	contentType := "text/html"
	if req.Input == nil {
		resp, err := arc.downloadFile(url.String())
		if err != nil {
			return nil, "", fmt.Errorf("download failed: %w", err)
		}
		defer resp.Body.Close()

		req.Input = resp.Body
		contentType = resp.Header.Get("Content-Type")
	}

	// Check the type of the downloaded file.
	// If it's not HTML, just return it as it is.
	if !strings.HasPrefix(contentType, "text/html") {
		content, err := ioutil.ReadAll(req.Input)
		return content, contentType, err
	}

	// If it's HTML process it
	result, err := arc.processHTML(ctx, req.Input, url)
	if err != nil {
		return nil, "", err
	}

	return []byte(result), contentType, nil
}

func (arc *archiver) downloadFile(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", arc.config.UserAgent)
	if url != arc.rootURL {
		req.Header.Set("Referer", arc.rootURL)
	}

	for _, cookie := range arc.cookies {
		req.AddCookie(cookie)
	}

	resp, err := arc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
