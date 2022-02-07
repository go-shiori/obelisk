package obelisk

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
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

	Transport             http.RoundTripper
	RequestTimeout        time.Duration
	MaxConcurrentDownload int64
	SkipResourceURLError  bool
	ResTempDir            string // directory to stores resources

	isValidated bool
	cookies     []*http.Cookie
	httpClient  *http.Client
	dlSemaphore *semaphore.Weighted

	SingleFile bool
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

	return s2b(result), contentType, nil
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

	var resp *http.Response
	op := func() error {
		var err error
		resp, err = arc.httpClient.Do(req) //nolint:bodyclose,goimports
		if err == nil && resp != nil && resp.StatusCode > 200 {
			err = fmt.Errorf("failed to fetch with status code: %d", resp.StatusCode)
		}
		return err
	}
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = 5 * time.Minute
	bo := backoff.WithMaxRetries(exp, 10)
	err = backoff.Retry(op, bo)

	return resp, err
}

func (arc *Archiver) transform(uri string, content []byte) string {
	path, name, err := arc.store(uri)
	if err != nil {
		name = sanitize.BaseName(uri)
		path = filepath.Join(arc.ResTempDir, name)
	}

	if arc.SingleFile {
		return createDataURL(content, http.DetectContentType(content))
	}

	if err := ioutil.WriteFile(path, content, 0600); err != nil {
		// Fallback to creating data URL
		return createDataURL(content, http.DetectContentType(content))
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
	dir := filepath.Join(arc.ResTempDir, filepath.Dir(rel))
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
