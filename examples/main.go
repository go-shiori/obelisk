// +build ignore

package main

import (
	"context"

	"github.com/go-shiori/obelisk"
)

func main() {
	req := obelisk.Request{
		URL:     "https://www.nytimes.com/interactive/2019/07/06/us/migrants-border-patrol-clint.html",
		DstPath: "nytimes.html",
	}

	cfg := obelisk.DefaultConfig
	cfg.EnableLog = true

	err := obelisk.Archive(context.Background(), req, cfg)
	if err != nil {
		panic(err)
	}
}
