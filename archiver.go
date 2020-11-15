package obelisk

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	nurl "net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

var (
	// DefaultUserAgent is the default user agent to use, which is Chrome's.
	DefaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0"
)

// Request is data of archival request.
type Request struct {
	Input   io.Reader
	URL     string
	Cookies []*http.Cookie
}

// Asset is asset that used in a web page.
type Asset struct {
	Data        []byte
	ContentType string
}

// Archiver is the core of obelisk, which used to download a
// web page then embeds its assets.
type Archiver struct {
	sync.RWMutex

	Cache map[string]Asset

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
	DialContext           func(ctx context.Context, network, addr string) (net.Conn, error)

	isValidated bool
	cookies     []*http.Cookie
	httpClient  *http.Client
	dlSemaphore *semaphore.Weighted
}

// Validate prepares Archiver to make sure its configurations
// are valid and ready to use. Must be run at least once before
// archival started.
func (arc *Archiver) Validate() {
	if arc.Cache == nil {
		arc.Cache = make(map[string]Asset)
	}

	if arc.UserAgent == "" {
		arc.UserAgent = DefaultUserAgent
	}

	if arc.MaxConcurrentDownload <= 0 {
		arc.MaxConcurrentDownload = 10
	}

	arc.isValidated = true
	arc.dlSemaphore = semaphore.NewWeighted(arc.MaxConcurrentDownload)
	arc.httpClient = &http.Client{
		Timeout: arc.RequestTimeout,
		Transport: &http.Transport{
			DialContext: arc.DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: arc.SkipTLSVerification,
			},
		},
	}
}

// Archive starts archival process for the specified request.
// Returns the archival result, content type and error if there are any.
func (arc *Archiver) Archive(ctx context.Context, req Request) ([]byte, string, error) {
	// Make sure archiver has been validated
	if !arc.isValidated {
		return nil, "", fmt.Errorf("archiver hasn't been validated")
	}

	// Validate request
	if req.URL == "" {
		return nil, "", fmt.Errorf("request url is not specified")
	}

	url, err := nurl.ParseRequestURI(req.URL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return nil, "", fmt.Errorf("url \"%s\" is not valid", req.URL)
	}

	// If needed download page from source URL
	contentType := "text/html"
	if req.Input == nil {
		resp, err := arc.downloadFile(url.String(), "")
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

func (arc *Archiver) downloadFile(url string, parentURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", arc.UserAgent)
	if parentURL != "" {
		req.Header.Set("Referer", parentURL)
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
