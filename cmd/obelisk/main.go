package main

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	fp "path/filepath"

	"github.com/go-shiori/obelisk"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func main() {
	// Prepare cmd
	cmd := &cobra.Command{
		Use:   "obelisk url",
		Args:  cobra.ExactArgs(1),
		Short: "CLI tool for saving web page as single HTML file",
		RunE:  cmdHandler,
	}

	cmd.Flags().StringP("user-agent", "u", "", "set custom user agent")
	cmd.Flags().StringP("output", "o", "", "path to save archival result")
	cmd.Flags().BoolP("quiet", "q", false, "disable logging")
	cmd.Flags().BoolP("gzip", "z", false, "gzip archival result")
	cmd.Flags().Bool("no-js", false, "disable JavaScript")
	cmd.Flags().Bool("no-css", false, "disable CSS styling")
	cmd.Flags().Bool("no-embeds", false, "remove embedded elements (e.g iframe)")
	cmd.Flags().Bool("no-medias", false, "remove media elements (e.g img, audio)")
	cmd.Flags().Bool("verbose", false, "more verbose logging")
	cmd.Flags().Int64("max-concurrent-download", 10, "max concurrent download at a time")

	// Execute
	err := cmd.Execute()
	if err != nil {
		logrus.Fatalln(err)
	}
}

func cmdHandler(cmd *cobra.Command, args []string) error {
	// Parse args and flags
	url := args[0]
	userAgent, _ := cmd.Flags().GetString("user-agent")
	outputPath, _ := cmd.Flags().GetString("output")
	useGzip, _ := cmd.Flags().GetBool("gzip")
	disableLog, _ := cmd.Flags().GetBool("quiet")
	disableJS, _ := cmd.Flags().GetBool("no-js")
	disableCSS, _ := cmd.Flags().GetBool("no-css")
	disableEmbeds, _ := cmd.Flags().GetBool("no-embeds")
	disableMedias, _ := cmd.Flags().GetBool("no-medias")
	useVerboseLog, _ := cmd.Flags().GetBool("verbose")
	maxConcurrentDownload, _ := cmd.Flags().GetInt64("max-concurrent-download")

	// Run archiver
	cfg := obelisk.Config{
		UserAgent:             userAgent,
		EnableLog:             !disableLog,
		LogParentURL:          !disableLog && useVerboseLog,
		DisableJS:             disableJS,
		DisableCSS:            disableCSS,
		DisableEmbeds:         disableEmbeds,
		DisableMedias:         disableMedias,
		MaxConcurrentDownload: maxConcurrentDownload,
	}

	req := obelisk.Request{
		URL: url,
	}

	result, err := obelisk.Archive(context.Background(), req, cfg)
	if err != nil {
		return err
	}

	// Create output file. However, if output path is not specified
	// just dump it to stdout
	var output io.Writer = os.Stdout
	if outputPath != "" {
		err = os.MkdirAll(fp.Dir(outputPath), os.ModePerm)
		if err != nil {
			return err
		}

		f, err := os.Create(outputPath)
		if err != nil {
			return err
		}
		defer f.Close()

		output = f
	}

	// Create gzip if needed
	if useGzip {
		gz := gzip.NewWriter(output)
		defer gz.Close()
		output = gz
	}

	_, err = output.Write(result)
	return err
}
