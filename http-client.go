package obelisk

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"time"
)

var httpClient *http.Client

func init() {
	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{
		Timeout: time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Jar: jar,
	}
}

func (arc *archiver) downloadFile(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", arc.userAgent)
	for _, cookie := range arc.cookies {
		req.AddCookie(cookie)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
