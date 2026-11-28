//go:build windows

package main

import (
	"errors"
	"io"
	"log"
	"os"
	"runtime"
)

// copyLimited copies at most max bytes and fails if the source is larger.
func copyLimited(dst io.Writer, src io.Reader, max int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, max+1))
	if err == nil && n > max {
		return n, errors.New("download too large")
	}
	return n, err
}

func main() {
	// WebView2 is a COM component: it must be driven from one OS thread for the whole run.
	runtime.LockOSThread()

	var urls []string
	selftest := false
	for _, a := range os.Args[1:] {
		if a == "--selftest" {
			selftest = true
		} else if len(a) > 0 && a[0] != '-' {
			urls = append(urls, a)
		}
	}
	base := ""
	if selftest {
		dir, err := os.MkdirTemp("", "fasty-selftest-")
		if err != nil {
			log.Fatal(err)
		}
		os.Setenv("FASTY_PROFILE", dir)
		if base, err = startSelfTestServer(); err != nil {
			log.Fatal(err)
		}
		urls = []string{base + "/a"}
	}
	if err := run(urls, base); err != nil {
		log.Fatal(err)
	}
}
