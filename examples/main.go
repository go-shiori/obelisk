// +build ignore

package main

import (
	"context"

	"github.com/go-shiori/obelisk"
)

func main() {
	req := obelisk.Request{
		URL:     "https://www.washingtonpost.com/graphics/2020/world/corona-simulator/",
		DstPath: "wapo.html",
	}

	cfg := obelisk.DefaultConfig
	cfg.EnableLog = true

	err := obelisk.Archive(context.Background(), req, cfg)
	if err != nil {
		panic(err)
	}
}
