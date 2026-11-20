package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var (
	outPath string
	fileURL string
	threads int
	proxy   string
)

func parseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL: %q", raw)
	}
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %q", u.Scheme)
	}
	return u, nil
}

func main() {
	cmd := &cobra.Command{
		Use:           "downloader",
		Short:         "File concurrency downloader",
		Version:       "1.0.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fileURL == "" {
				return errors.New("-u/--url missing required flag")
			}
			if threads < 1 {
				return errors.New("-t/--thread must be at least 1")
			}
			proxyURL, err := parseProxy(proxy)
			if err != nil {
				return err
			}
			return NewDownloader(threads, proxyURL).Download(fileURL, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出文件位置")
	cmd.Flags().StringVarP(&fileURL, "url", "u", "", "下载链接")
	cmd.Flags().IntVarP(&threads, "thread", "t", runtime.NumCPU(), "线程数")
	cmd.Flags().StringVarP(&proxy, "proxy", "p", "", "代理链接")

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
