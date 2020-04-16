package main

import (
	"bufio"
	"fmt"
	"mime"
	"net/http"
	nurl "net/url"
	"os"
	pth "path"
	"strconv"
	"strings"
	"time"
)

func parseInputFile(path string) ([]string, error) {
	// Open file
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Fetch each line from file
	urls := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		text := scanner.Text()
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		urls = append(urls, text)
	}

	return urls, nil
}

func parseCookiesFile(path string) (map[string][]*http.Cookie, error) {
	// Open file
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Create map to store cookies
	mapCookies := make(map[string][]*http.Cookie)

	// Create scanner
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 7 {
			continue
		}

		httpOnly := strings.HasPrefix(parts[0], "#HttpOnly_")
		unixTime, _ := strconv.ParseInt(parts[4], 10, 64)
		domainName := parts[0]

		if httpOnly {
			domainName = strings.TrimPrefix(domainName, "#HttpOnly_")
		}

		mapCookies[domainName] = append(mapCookies[domainName], &http.Cookie{
			Name:     parts[5],
			Value:    parts[6],
			Path:     parts[2],
			Domain:   domainName,
			Expires:  time.Unix(unixTime, 0),
			Secure:   parts[3] == "TRUE",
			HttpOnly: httpOnly,
		})
	}

	return mapCookies, nil
}

func createFileName(url *nurl.URL, contentType string) string {
	// Prepare current time and domain name
	now := time.Now().Format("2006-01-01-150405")
	domainName := strings.TrimPrefix(url.Hostname(), "www.")
	domainName = strings.ReplaceAll(domainName, ".", "-")

	// Get file extension
	extension := ""
	if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
		extension = exts[0]
	}

	// If URL doesn't have any path just return time and domain
	if url.Path == "" || url.Path == "/" {
		return fmt.Sprintf("%s-%s%s", now, domainName, extension)
	}

	baseName := pth.Base(url.Path)
	if parts := strings.Split(baseName, "-"); len(parts) > 4 {
		baseName = strings.Join(parts[:4], "-")
	}

	return fmt.Sprintf("%s-%s-%s%s", now, domainName, baseName, extension)
}

func isDirectory(path string) bool {
	f, err := os.Stat(path)
	if err != nil {
		return false
	}

	return f.IsDir()
}
