package main

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	fp "path/filepath"
	"strings"
	"time"

	"github.com/go-shiori/obelisk"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type archiveRequest struct {
	URL      string
	FileName string
}

func main() {
	// Prepare cmd
	cmd := &cobra.Command{
		Use:   "obelisk [url1] [url2] ... [urlN]",
		Short: "CLI tool for saving web page as single HTML file",
		RunE:  cmdHandler,
	}

	cmd.Flags().StringP("input", "i", "", "path to file which contains URLs")
	cmd.Flags().StringP("output", "o", "", "path to save archival result")
	cmd.Flags().StringP("load-cookies", "c", "", "path to Netscape cookie file")

	cmd.Flags().StringP("user-agent", "u", "", "set custom user agent")
	cmd.Flags().BoolP("gzip", "z", false, "gzip archival result")
	cmd.Flags().BoolP("quiet", "q", false, "disable logging")
	cmd.Flags().Bool("verbose", false, "more verbose logging")

	cmd.Flags().Bool("no-js", false, "disable JavaScript")
	cmd.Flags().Bool("no-css", false, "disable CSS styling")
	cmd.Flags().Bool("no-embeds", false, "remove embedded elements (e.g iframe)")
	cmd.Flags().Bool("no-medias", false, "remove media elements (e.g img, audio)")

	cmd.Flags().IntP("retries", "r", 3, "maximum number of retries for single request")
	cmd.Flags().IntP("timeout", "t", 60, "maximum time (in second) before request timeout")
	cmd.Flags().Bool("insecure", false, "skip X.509 (TLS) certificate verification")
	cmd.Flags().Int64("max-concurrent-download", 10, "max concurrent download at a time")
	cmd.Flags().Bool("skip-resource-url-error", false, "skip process resource url error")

	// Execute
	err := cmd.Execute()
	if err != nil {
		logrus.Fatalln(err)
	}
}

// nolint:gocyclo
func cmdHandler(cmd *cobra.Command, args []string) error {
	// Parse flags
	inputPath, _ := cmd.Flags().GetString("input")
	outputPath, _ := cmd.Flags().GetString("output")
	cookiesFilePath, _ := cmd.Flags().GetString("load-cookies")

	userAgent, _ := cmd.Flags().GetString("user-agent")
	useGzip, _ := cmd.Flags().GetBool("gzip")
	disableLog, _ := cmd.Flags().GetBool("quiet")
	useVerboseLog, _ := cmd.Flags().GetBool("verbose")

	disableJS, _ := cmd.Flags().GetBool("no-js")
	disableCSS, _ := cmd.Flags().GetBool("no-css")
	disableEmbeds, _ := cmd.Flags().GetBool("no-embeds")
	disableMedias, _ := cmd.Flags().GetBool("no-medias")

	retries, _ := cmd.Flags().GetInt("retries")
	timeout, _ := cmd.Flags().GetInt("timeout")
	skipTLSVerification, _ := cmd.Flags().GetBool("insecure")
	maxConcurrentDownload, _ := cmd.Flags().GetInt64("max-concurrent-download")
	skipResourceURLError, _ := cmd.Flags().GetBool("skip-resource-url-error")

	// Prepare output target
	outputDir := ""
	outputFileName := ""
	useStdout := outputPath == "-"

	if outputPath != "" && !useStdout {
		if isDirectory(outputPath) {
			outputDir = outputPath
		} else {
			outputDir = fp.Dir(outputPath)
			outputFileName = fp.Base(outputPath)
		}

		// Make sure output dir exists
		_ = os.MkdirAll(outputDir, os.ModePerm)
	}

	// Create initial list of archival request
	var err error
	requests := []archiveRequest{}
	for _, arg := range args {
		requests = append(requests, archiveRequest{URL: arg})
	}

	// Parse input file
	if inputPath != "" {
		requestsFromFile, err := parseInputFile(inputPath)
		if err != nil {
			return err
		}
		requests = append(requests, requestsFromFile...)
	}

	// Depending of requests count, there are some thing to do
	switch len(requests) {
	case 0:
		return fmt.Errorf("no url to process")
	case 1:
		if requests[0].FileName == "" && outputFileName != "" {
			requests[0].FileName = outputFileName
		}
	default:
		useStdout = false
	}

	// Read cookies file
	cookiesMap := make(map[string][]*http.Cookie)
	if cookiesFilePath != "" {
		cookiesMap, err = parseCookiesFile(cookiesFilePath)
		if err != nil {
			return err
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if skipTLSVerification {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: skipTLSVerification, //nolint:gosec
		}
	}

	// Create archiver
	archiver := obelisk.Archiver{
		Cache: make(map[string]obelisk.Asset),

		UserAgent:        userAgent,
		EnableLog:        !disableLog,
		EnableVerboseLog: !disableLog && useVerboseLog,

		DisableJS:     disableJS,
		DisableCSS:    disableCSS,
		DisableEmbeds: disableEmbeds,
		DisableMedias: disableMedias,

		Transport:             transport,
		MaxRetries:            retries,
		RequestTimeout:        time.Duration(timeout) * time.Second,
		MaxConcurrentDownload: maxConcurrentDownload,
		SkipResourceURLError:  skipResourceURLError,
	}
	archiver.Validate()

	// Process each url
	finishedURLs := make(map[string]struct{})

	for _, request := range requests {
		err = func() error {
			// Make sure this URL hasn't been processed
			if _, finished := finishedURLs[request.URL]; finished {
				return nil
			}

			// Validate URL
			url, err := nurl.ParseRequestURI(request.URL)
			if err != nil || url.Scheme == "" || url.Hostname() == "" {
				logrus.Warnf("%s is not valid URL\n", request.URL)
				return nil
			}

			// Create request
			var reqCookies []*http.Cookie
			if len(cookiesMap) != 0 {
				parts := strings.Split(url.Hostname(), ".")
				for i := 0; i < len(parts)-1; i++ {
					domainName := strings.Join(parts[i:], ".")
					reqCookies = append(reqCookies, cookiesMap[domainName]...)
					reqCookies = append(reqCookies, cookiesMap["."+domainName]...)
				}
			}

			req := obelisk.Request{
				URL:     url.String(),
				Cookies: reqCookies,
			}

			// Start archival
			if !disableLog || len(requests) > 1 {
				logrus.Printf("archival started for %s\n", request.URL)
			}

			result, contentType, err := archiver.WithCookies(reqCookies).Archive(context.Background(), req)
			if err != nil {
				return err
			}

			// Prepare output
			var output io.Writer
			if useStdout {
				output = os.Stdout
			} else {
				fileName := request.FileName
				if fileName == "" {
					fileName = createFileName(url, contentType)
					if useGzip {
						fileName += ".gz"
					}
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

			if !disableLog || len(requests) > 1 {
				logrus.Printf("archival finished for %s\n", request.URL)
			}

			finishedURLs[request.URL] = struct{}{}
			return nil
		}()

		if err != nil {
			logrus.Warnln(err)
		}

		// Create blank space separator to make it easier to see logs
		if !disableLog {
			fmt.Println()
		}
	}

	return nil
}
