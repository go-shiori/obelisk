package main

import (
	"io"
	nurl "net/url"
	"os"
	fp "path/filepath"
	"strings"
)

// saveToFile saves an input into specified path
func saveToFile(input io.Reader, dstPath string) error {
	err := os.MkdirAll(fp.Dir(dstPath), os.ModePerm)
	if err != nil {
		return err
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, input)
	if err != nil {
		return err
	}

	return f.Sync()
}

// createAbsoluteURL convert url to absolute path based on base.
// However, if url is prefixed with hash (#), the url won't be changed.
func createAbsoluteURL(url string, base *nurl.URL) string {
	if url == "" || base == nil {
		return ""
	}

	// If it is hash tag, return as it is
	if strings.HasPrefix(url, "#") {
		return url
	}

	// If it is already an absolute URL, clean the URL then return it
	tmp, err := nurl.ParseRequestURI(url)
	if err == nil && tmp.Scheme != "" && tmp.Hostname() != "" {
		cleanURL(tmp)
		return tmp.String()
	}

	// Otherwise, resolve against base URL.
	tmp, err = nurl.Parse(url)
	if err != nil {
		return url
	}

	cleanURL(tmp)
	return base.ResolveReference(tmp).String()
}

// cleanURL removes fragment (#fragment) and UTM queries from URL
func cleanURL(url *nurl.URL) {
	queries := url.Query()

	for key := range queries {
		if strings.HasPrefix(key, "utm_") {
			queries.Del(key)
		}
	}

	url.Fragment = ""
	url.RawQuery = queries.Encode()
}
