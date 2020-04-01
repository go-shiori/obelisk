package main

import (
	"net/http"
)

// DefaultUserAgent is the default user agents which mimic Chrome.
const DefaultUserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:73.0) Gecko/20100101 Firefox/73.0"

func downloadFile(url string, userAgent string) (*http.Response, error) {
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
