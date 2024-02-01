package obelisk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/kennygrant/sanitize"
	"golang.org/x/sync/semaphore"
)

var (
	defaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0"
	maxElapsedTime   = 30 * time.Second
)

// Request is data of archival request.
type Request struct {
	Input io.Reader
	URL   string

	// Deprecated: Use `Archiver.WithCookies` instead.
	Cookies []*http.Cookie

	origin *nurl.URL // The original URL request was based from the input. If there are no redirects, it should be the same as `URL`.
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

	Transport             http.RoundTripper
	RequestTimeout        time.Duration
	MaxRetries            int
	MaxConcurrentDownload int64
	SkipResourceURLError  bool
	WrapDirectory         string // directory to stores resources

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
		arc.UserAgent = defaultUserAgent
	}

	if arc.MaxConcurrentDownload <= 0 {
		arc.MaxConcurrentDownload = 10
	}

	arc.isValidated = true
	arc.dlSemaphore = semaphore.NewWeighted(arc.MaxConcurrentDownload)

	if arc.Transport == nil {
		arc.Transport = http.DefaultTransport
	}

	arc.httpClient = &http.Client{
		Timeout:   arc.RequestTimeout,
		Transport: arc.Transport,
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

	url, err := nurl.Parse(req.URL)
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return nil, "", fmt.Errorf("url \"%s\" is not valid", req.URL)
	}
	// Set the original url
	req.origin = url
	ctx = withOrigin(ctx, req.origin)
	url = arc.finalURI(url)

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
		content, err := io.ReadAll(req.Input)
		return content, contentType, err
	}

	// If it's HTML process it
	result, err := arc.processHTML(ctx, req.Input, url, false)
	if err != nil {
		return nil, "", err
	}

	return s2b(result), contentType, nil
}

// WithCookies attach request cookies to `Archiver`.
func (arc *Archiver) WithCookies(cookies []*http.Cookie) *Archiver {
	arc.cookies = cookies
	return arc
}

// finalURI returns the final URL that has been redirected to another URL.
func (arc *Archiver) finalURI(u *nurl.URL) *nurl.URL {
	req, err := http.NewRequest(http.MethodHead, u.String(), nil)
	if err != nil {
		return u
	}
	req.Header.Set("User-Agent", arc.UserAgent)

	resp, err := arc.httpClient.Do(req)
	if err != nil {
		return u
	}
	defer resp.Body.Close()

	return resp.Request.URL
}

func (arc *Archiver) downloadFile(url string, parentURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	var resp *http.Response
	op := func() error {
		var err error
		resp, err = arc.httpClient.Do(req) //nolint:bodyclose,goimports
		if err == nil && (resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests) {
			err = fmt.Errorf("failed to fetch with status code: %d", resp.StatusCode)
		}
		return err
	}
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = maxElapsedTime
	bo := backoff.WithMaxRetries(exp, uint64(arc.MaxRetries))
	err = backoff.Retry(op, bo)

	return resp, err
}

func (arc *Archiver) transform(uri string, content []byte, contentType string) string {
	// If no directory to store files is specified, save as a single file.
	if arc.WrapDirectory == "" {
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		return createDataURL(content, contentType)
	}

	path, name, err := arc.store(uri)
	if err != nil {
		name = sanitize.BaseName(uri)
		path = filepath.Join(arc.WrapDirectory, name)
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		// Fallback to creating data URL
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		return createDataURL(content, contentType)
	}

	return filepath.Join(".", name)
}

func (arc *Archiver) store(uri string) (path string, rel string, err error) {
	if uri == "" {
		return "", "", nil
	}
	u, err := nurl.ParseRequestURI(uri)
	if err != nil {
		return "", "", err
	}
	// e.g. /statics/css/foo.css
	rel, err = filepath.Abs(u.Path)
	if err != nil {
		return "", "", err
	}
	// e.g. /tmp/some/statics/css/
	dir := filepath.Join(arc.WrapDirectory, filepath.Dir(rel))
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	// e.g. /tmp/some/statics/css/foo.css
	path = filepath.Join(dir, filepath.Base(rel))
	return path, rel, nil
}
