package main

// Downloads: into the Downloads folder, under the name the site gave, never
// over a file that is already there.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

type DownloadItem struct {
	dl       *webkit.Download
	path     string
	name     string
	progress float64
	state    string // running | done | failed | cancelled
	err      string
	told     bool
	doneTold bool
	onChange func()
}

type Downloads struct {
	settings  *Settings
	items     []*DownloadItem
	onAdded   []func(*DownloadItem)
	onChanged []func()
}

func (d *Downloads) folder() string {
	f := d.settings.DownloadsDir
	if f == "" {
		f = glib.GetUserSpecialDir(glib.UserDirectoryDownload)
	}
	if f == "" {
		home, _ := os.UserHomeDir()
		f = filepath.Join(home, "Downloads")
	}
	os.MkdirAll(f, 0o755)
	return f
}

func (d *Downloads) watch(s *webkit.NetworkSession) {
	s.ConnectDownloadStarted(func(dl *webkit.Download) { d.started(dl) })
}

func (d *Downloads) tell() {
	for _, f := range d.onChanged {
		f()
	}
}

func (d *Downloads) started(dl *webkit.Download) {
	it := &DownloadItem{dl: dl, state: "running"}
	changed := func() {
		if it.onChange != nil {
			it.onChange()
		}
		d.tell()
	}
	dl.ConnectDecideDestination(func(suggested string) bool {
		folder := d.folder()
		name := filepath.Base(suggested)
		if name == "" || name == "." || name == "/" {
			name = "download"
		}
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		p := filepath.Join(folder, name)
		for n := 2; ; n++ {
			if _, err := os.Stat(p); os.IsNotExist(err) {
				break
			}
			p = filepath.Join(folder, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		it.path, it.name = p, filepath.Base(p)
		dl.SetAllowOverwrite(false)
		dl.SetDestination(p)
		changed()
		return true
	})
	dl.NotifyProperty("estimated-progress", func() {
		it.progress = dl.EstimatedProgress()
		changed()
	})
	dl.ConnectFinished(func() {
		if it.state == "running" {
			it.state = "done"
			it.progress = 1
		}
		changed()
	})
	dl.ConnectFailed(func(err error) {
		if it.state == "cancelled" || strings.Contains(strings.ToLower(err.Error()), "cancel") {
			it.state = "cancelled"
		} else {
			it.state = "failed"
			it.err = err.Error()
		}
		changed()
	})
	d.items = append([]*DownloadItem{it}, d.items...)
	for _, f := range d.onAdded {
		f(it)
	}
}

func (it *DownloadItem) cancel() {
	if it.state == "running" {
		it.state = "cancelled"
		it.dl.Cancel()
	}
}

func sizeText(n float64) string {
	for _, unit := range []string{"bytes", "KB", "MB", "GB"} {
		if n < 1000 || unit == "GB" {
			if unit == "bytes" {
				return fmt.Sprintf("%.0f %s", n, unit)
			}
			return fmt.Sprintf("%.1f %s", n, unit)
		}
		n /= 1000
	}
	return ""
}

func (it *DownloadItem) sizeText() string {
	got := float64(it.dl.ReceivedDataLength())
	var total float64
	try(func() {
		if r := it.dl.Response(); r != nil {
			total = float64(r.ContentLength())
		}
	})
	if total > 0 {
		return sizeText(got) + " of " + sizeText(total)
	}
	return sizeText(got)
}

func (d *Downloads) active() int {
	n := 0
	for _, it := range d.items {
		if it.state == "running" {
			n++
		}
	}
	return n
}

func (d *Downloads) clear() {
	var keep []*DownloadItem
	for _, it := range d.items {
		if it.state == "running" {
			keep = append(keep, it)
		}
	}
	d.items = keep
	d.tell()
}
