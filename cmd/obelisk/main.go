package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	fp "path/filepath"
	"strconv"
	"strings"
	"time"

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
	userAgent, _ := cmd.Flags().GetString("user-agent")
	outputPath, _ := cmd.Flags().GetString("output")
	useGzip, _ := cmd.Flags().GetBool("gzip")
	disableLog, _ := cmd.Flags().GetBool("quiet")
	useVerboseLog, _ := cmd.Flags().GetBool("verbose")

	disableJS, _ := cmd.Flags().GetBool("no-js")
	disableCSS, _ := cmd.Flags().GetBool("no-css")
	disableEmbeds, _ := cmd.Flags().GetBool("no-embeds")
	disableMedias, _ := cmd.Flags().GetBool("no-medias")

	maxConcurrentDownload, _ := cmd.Flags().GetInt64("max-concurrent-download")
	cookiesFilePath, _ := cmd.Flags().GetString("load-cookies")

	// Validate URL
	url, err := nurl.ParseRequestURI(args[0])
	if err != nil || url.Scheme == "" || url.Hostname() == "" {
		return fmt.Errorf("url is not valid")
	}

	// Read cookies file
	var cookiesMap map[string][]*http.Cookie
	if cookiesFilePath != "" {
		cookiesMap, err = parseCookiesFile(cookiesFilePath)
		if err != nil {
			return err
		}
	}

	// Create archiver config
	cfg := obelisk.Config{
		UserAgent:    userAgent,
		EnableLog:    !disableLog,
		LogParentURL: !disableLog && useVerboseLog,

		DisableJS:     disableJS,
		DisableCSS:    disableCSS,
		DisableEmbeds: disableEmbeds,
		DisableMedias: disableMedias,

		MaxConcurrentDownload: maxConcurrentDownload,
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

func parseCookiesFile(path string) (map[string][]*http.Cookie, error) {
	// Open file
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Create map to store cookies
	cookiesMap := make(map[string][]*http.Cookie)

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

		cookiesMap[domainName] = append(cookiesMap[domainName], &http.Cookie{
			Name:     parts[5],
			Value:    parts[6],
			Path:     parts[2],
			Domain:   domainName,
			Expires:  time.Unix(unixTime, 0),
			Secure:   parts[3] == "TRUE",
			HttpOnly: httpOnly,
		})
	}

	return cookiesMap, nil
}

func getDomainName(hostname string) string {
	parts := strings.Split(hostname, ".")
	if len(parts) <= 2 {
		return hostname
	}

	return strings.Join(parts[len(parts)-2:], ".")
}
