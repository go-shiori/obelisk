package main

import "context"

const sourceURL = "https://kotaku.com/the-spectacular-story-of-metroid-one-of-gamings-riche-1284029577"

func main() {
	arc := NewArchiver()
	err := arc.Start(context.Background(), sourceURL, "kotaku.html")
	if err != nil {
		panic(err)
	}
}
