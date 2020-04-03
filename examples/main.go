// +build ignore

package main

import (
	"context"

	"github.com/go-shiori/obelisk"
)

func main() {
	req := obelisk.Request{
		URL:     "https://kotaku.com/the-spectacular-story-of-metroid-one-of-gamings-riche-1284029577",
		DstPath: "kotaku.html",
	}

	cfg := obelisk.DefaultConfig
	cfg.EnableLog = true

	err := obelisk.Archive(context.Background(), req, cfg)
	if err != nil {
		panic(err)
	}
}
