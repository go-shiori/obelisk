package main

import (
	"context"
	"io"
	nurl "net/url"

	"golang.org/x/sync/semaphore"
)

func processCSS(ctx context.Context, sem *semaphore.Weighted, input io.Reader, baseURL *nurl.URL) (string, error) {
	return "", nil
}
