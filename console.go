package main

import (
	"fmt"
	"io"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

const (
	colorReset = "\033[0m"
	colorGreen = "\033[1;32m"
	colorCyan  = "\033[1;36m"
)

func paint(color, s string) string {
	return color + s + colorReset
}

func ansiWriter() io.Writer {
	return ansi.NewAnsiStdout()
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func newBar(w io.Writer, total int) *progressbar.ProgressBar {
	return progressbar.NewOptions(
		total,
		progressbar.OptionSetWriter(w),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionSetDescription("downloading"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}
