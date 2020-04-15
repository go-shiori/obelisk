package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	fp "path/filepath"

	"github.com/go-shiori/obelisk"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func main() {
	// Prepare cmd
	cmd := &cobra.Command{
		Use:   "obelisk [url1] [url2] ... [urlN]",
		Short: "CLI tool for saving web page as single HTML file",
		RunE:  cmdHandler,
	}

	cmd.Flags().StringP("input", "i", "", "path to file which contains URLs")
	cmd.Flags().StringP("output", "o", "", "path to save archival result")
	cmd.Flags().StringP("user-agent", "u", "", "set custom user agent")
	cmd.Flags().BoolP("gzip", "z", false, "gzip archival result")
	cmd.Flags().BoolP("quiet", "q", false, "disable logging")
	cmd.Flags().Bool("verbose", false, "more verbose logging")

	cmd.Flags().Bool("no-js", false, "disable JavaScript")
	cmd.Flags().Bool("no-css", false, "disable CSS styling")
	cmd.Flags().Bool("no-embeds", false, "remove embedded elements (e.g iframe)")
	cmd.Flags().Bool("no-medias", false, "remove media elements (e.g img, audio)")

	cmd.Flags().Int64("max-concurrent-download", 10, "max concurrent download at a time")
	cmd.Flags().StringP("load-cookies", "c", "", "path to Netscape cookie file")

	// Execute
	err := cmd.Execute()
	if err != nil {
		logrus.Fatalln(err)
	}
}

func cmdHandler(cmd *cobra.Command, args []string) error {
	// Parse flags
	inputPath, _ := cmd.Flags().GetString("input")
	outputPath, _ := cmd.Flags().GetString("output")
	outputSpecified := cmd.Flags().Changed("output")
	cookiesFilePath, _ := cmd.Flags().GetString("load-cookies")

	userAgent, _ := cmd.Flags().GetString("user-agent")
	useGzip, _ := cmd.Flags().GetBool("gzip")
	disableLog, _ := cmd.Flags().GetBool("quiet")
	useVerboseLog, _ := cmd.Flags().GetBool("verbose")

	disableJS, _ := cmd.Flags().GetBool("no-js")
	disableCSS, _ := cmd.Flags().GetBool("no-css")
	disableEmbeds, _ := cmd.Flags().GetBool("no-embeds")
	disableMedias, _ := cmd.Flags().GetBool("no-medias")

	maxConcurrentDownload, _ := cmd.Flags().GetInt64("max-concurrent-download")

	// Create list of URLs
	urls, err := args, error(nil)
	if inputPath != "" {
		urls, err = parseInputFile(inputPath)
		if err != nil {
			return err
		}
	}

	if len(urls) == 0 {
		return fmt.Errorf("no url to process")
	}

	// Prepare output name and dir
	outputDir := ""
	outputFileName := ""
	if outputSpecified {
		if isDirectory(outputPath) {
			outputDir = outputPath
		} else {
			outputDir = fp.Dir(outputPath)
			if len(urls) == 1 {
				outputFileName = fp.Base(outputPath)
			}
		}

		// Make sure output dir exists
		os.MkdirAll(outputDir, os.ModePerm)
	}

	// Read cookies file
	cookiesMap := make(map[string][]*http.Cookie)
	if cookiesFilePath != "" {
		cookiesMap, err = parseCookiesFile(cookiesFilePath)
		if err != nil {
			return err
		}
	}

	// Create archiver config
	cfg := obelisk.Config{
		UserAgent:        userAgent,
		EnableLog:        !disableLog,
		EnableVerboseLog: !disableLog && useVerboseLog,

		DisableJS:     disableJS,
		DisableCSS:    disableCSS,
		DisableEmbeds: disableEmbeds,
		DisableMedias: disableMedias,

		MaxConcurrentDownload: maxConcurrentDownload,
	}

	// Process each url
	finishedURLs := make(map[string]struct{})

	for i, strURL := range urls {
		err = func() error {
			// Make sure this URL hasn't been processed
			if _, finished := finishedURLs[strURL]; finished {
				return nil
			}

			// Validate URL
			url, err := nurl.ParseRequestURI(strURL)
			if err != nil || url.Scheme == "" || url.Hostname() == "" {
				logrus.Warnf("%s is not valid URL\n", strURL)
				return nil
			}

			// Create request
			var reqCookies []*http.Cookie
			if len(cookiesMap) != 0 {
				hostName := url.Hostname()
				domainName := getDomainName(hostName)
				reqCookies = append(reqCookies, cookiesMap[hostName]...)
				reqCookies = append(reqCookies, cookiesMap["."+hostName]...)
				reqCookies = append(reqCookies, cookiesMap["."+domainName]...)
			}

			req := obelisk.Request{
				URL:     url.String(),
				Cookies: reqCookies,
			}

			// Start archival
			logrus.Printf("archival started for %s\n", strURL)
			result, err := obelisk.Archive(context.Background(), req, cfg)
			if err != nil {
				return err
			}

			// Prepare output
			var output io.Writer
			switch {
			case len(urls) == 1 && !outputSpecified:
				output = os.Stdout

			default:
				fileName := outputFileName
				if fileName == "" {
					fileName = createFileName(url)
				}

				f, err := os.Create(fp.Join(outputDir, fileName))
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
			if err != nil {
				return err
			}

			logrus.Printf("archival finished for %s\n", strURL)
			finishedURLs[strURL] = struct{}{}
			return nil
		}()

		if err != nil {
			return err
		}

		// Create blank space separator to make it easier to see logs
		if i < len(urls)-1 && !disableLog {
			fmt.Println()
		}
	}

	return nil
}
