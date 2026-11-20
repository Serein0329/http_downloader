package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"
)

const (
	partSuffix  = ".parts"
	copyBufSize = 32 * 1024
)

var errRangeUnsupported = errors.New("server does not support range requests")

type part struct {
	index int
	start int
	end   int
}

func (p part) size() int { return p.end - p.start + 1 }

// Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/158.0.0.0 Safari/537.36
type Downloader struct {
	threads int
	client  *http.Client

	out io.Writer
	bar *progressbar.ProgressBar
}

func NewDownloader(threads int, proxy *url.URL) *Downloader {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &Downloader{
		threads: threads,
		client:  &http.Client{Transport: transport},
		out:     ansiWriter(),
	}
}

func (d *Downloader) Download(rawURL, outPath string) error {
	resp, err := d.client.Head(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if outPath == "" {
		outPath = GetFilename(rawURL, resp.Header)
	}

	size := resp.ContentLength

	if resp.StatusCode == http.StatusOK && size > 0 && resp.Header.Get("Accept-Ranges") == "bytes" {
		fmt.Fprintf(d.out, "%s %s (%s, %d threads)\n",
			paint(colorCyan, "Multi_Downloading"), paint(colorGreen, outPath), formatBytes(size), d.threads)
		return d.downloadMulti(rawURL, outPath, int(size))
	}
	fmt.Fprintf(d.out, "%s %s (%d threads)\n",
		paint(colorCyan, "Single_Downloading"), paint(colorGreen, outPath), d.threads)
	return d.downloadSingle(rawURL, outPath)
}

func (d *Downloader) downloadMulti(rawURL, filename string, size int) error {
	parts := splitRanges(size, d.threads)
	dir := partDir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create part dir: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.bar = newBar(d.out, size)

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		firstErr    error
		unsupported atomic.Bool
	)
	record := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	for _, p := range parts {
		wg.Go(func() {
			err := d.downloadPart(ctx, rawURL, filename, p)
			if err == nil {
				return
			}
			if errors.Is(err, errRangeUnsupported) {
				unsupported.Store(true)
				cancel()
			}
			record(err)
		})
	}
	wg.Wait()

	if unsupported.Load() {
		fmt.Fprintln(d.out)
		_ = os.RemoveAll(dir)
		fmt.Fprintf(d.out, "%s range requests are not supported, retrying in single mode\n",
			paint(colorCyan, "Fallback:"))
		return d.downloadSingle(rawURL, filename)
	}

	if firstErr != nil {
		fmt.Fprintln(d.out)
		return firstErr
	}

	if err := d.merge(filename, parts); err != nil {
		fmt.Fprintln(d.out)
		return err
	}

	_ = d.bar.Finish()
	fmt.Fprintln(d.out)
	_ = os.RemoveAll(dir)
	fmt.Fprintf(d.out, "%s Saved to %s\n", paint(colorGreen, "Done."), filename)
	return nil
}

func (d *Downloader) downloadPart(ctx context.Context, rawURL, filename string, p part) error {
	path := partPath(filename, p.index)

	offset := 0
	if info, err := os.Stat(path); err == nil {
		offset = int(info.Size())
	}
	if offset > 0 {
		if err := d.bar.Add(offset); err != nil {
			return err
		}
	}
	if offset == p.size() {
		return nil
	}
	if offset > p.size() {
		return fmt.Errorf("part %s has %d bytes, expected at most %d: remove it and retry",
			path, offset, p.size())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", p.start+offset, p.end))

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return fmt.Errorf("part %d: %w", p.index, errRangeUnsupported)
	}
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("part %d: unexpected status %s", p.index, resp.Status)
	}
	if want := int64(p.end - p.start - offset + 1); resp.ContentLength >= 0 && resp.ContentLength != want {
		return fmt.Errorf("part %d: got %d bytes, want %d", p.index, resp.ContentLength, want)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, copyBufSize)
	if _, err := io.CopyBuffer(io.MultiWriter(f, d.bar), resp.Body, buf); err != nil {
		return fmt.Errorf("part %d: %w", p.index, err)
	}
	return nil
}

func (d *Downloader) merge(filename string, parts []part) error {
	dest, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dest.Close()

	for _, p := range parts {
		src, err := os.Open(partPath(filename, p.index))
		if err != nil {
			_ = os.Remove(filename)
			return fmt.Errorf("open part %d: %w", p.index, err)
		}
		_, err = io.Copy(dest, src)
		_ = src.Close()
		if err != nil {
			_ = os.Remove(filename)
			return fmt.Errorf("merge part %d: %w", p.index, err)
		}
	}
	return nil
}

func (d *Downloader) downloadSingle(rawURL, filename string) error {
	resp, err := d.client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}

	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}

	d.bar = newBar(d.out, int(resp.ContentLength))

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, copyBufSize)
	if _, err := io.CopyBuffer(io.MultiWriter(f, d.bar), resp.Body, buf); err != nil {
		return err
	}

	_ = d.bar.Finish()
	fmt.Fprintln(d.out)
	fmt.Fprintf(d.out, "%s Saved to %s\n", paint(colorGreen, "Done."), filename)
	return nil
}

func splitRanges(size, threads int) []part {
	partSize := size / threads
	parts := make([]part, 0, threads)
	for i := range threads {
		start := i * partSize
		end := start + partSize - 1
		if i == threads-1 {
			end = size - 1
		}
		if start > end {
			continue
		}
		parts = append(parts, part{index: i, start: start, end: end})
	}
	return parts
}

func partDir(filename string) string { return filename + partSuffix }

func partPath(filename string, i int) string {
	return filepath.Join(partDir(filename), fmt.Sprintf("%s.%d", filename, i))
}

func GetFilename(rawURL string, resp http.Header) string {
	if cd := resp.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := filepath.Base(params["filename"]); name != "" && name != "." && name != ".." {
				return name
			}
		}
	}

	if u, err := url.Parse(rawURL); err == nil {
		if name := filepath.Base(strings.TrimSuffix(u.Path, "/")); name != "" && name != "." && name != ".." {
			return name
		}
	}

	return "download_" + time.Now().Format("200601021504")
}
