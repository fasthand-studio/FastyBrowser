// Bookmarks, downloads, site data and the clipboard helpers.
package main

import (
	"context"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// --- bookmarks, downloads, data ---

func (b *Browser) toggleBookmark() {
	t := b.current
	if t == nil || t.url == "" {
		return
	}
	if b.app.bookmarks.Toggle(t.url, t.displayTitle()) {
		b.toast("Bookmarked", 1)
	} else {
		b.toast("Bookmark removed", 1)
	}
	b.omnibox.secureIcon(t)
}

func (b *Browser) downloadAdded(it *DownloadItem) {
	it.onChange = func() {
		if it.name != "" && !it.told {
			it.told = true
			t := adw.NewToast("Downloading “" + it.name + "”")
			t.SetTimeout(3)
			t.SetButtonLabel("Show")
			t.ConnectButtonClicked(func() { showDownloadsPanel(b) })
			b.toasts.AddToast(t)
		}
		if it.state == "done" && !it.doneTold {
			it.doneTold = true
			t := adw.NewToast("Downloaded “" + it.name + "”")
			t.SetTimeout(4)
			t.SetButtonLabel("Open")
			p := it.path
			t.ConnectButtonClicked(func() { launchPath(b, p, false) })
			b.toasts.AddToast(t)
			// A page that was only a download leaves nothing to look at.
			for _, tab := range append([]*Tab(nil), b.tabs...) {
				if tab.view != nil && tab.view.URI() == "" && tab.title == "" {
					b.closeTab(tab)
				}
			}
		}
	}
	b.downloadsBadge()
}

func (b *Browser) downloadsBadge() { setClass(b.dlButton, "busy", b.app.downloads.active() > 0) }

func (b *Browser) clearSiteData(host string, everything bool) {
	mgr := b.app.engine.session.WebsiteDataManager()
	types := webkit.WebsiteDataAll
	if everything {
		mgr.Clear(context.Background(), types, 0, nil)
		b.app.engine.favicons.Clear()
		b.toast("Cleared cookies, site data and cache", 2)
		return
	}
	mgr.Fetch(context.Background(), types, func(res gio.AsyncResulter) {
		items, err := mgr.FetchFinish(res)
		if err != nil {
			return
		}
		var mine []*webkit.WebsiteData
		for _, d := range items {
			n := d.Name()
			if n == host || strings.HasSuffix(n, "."+host) || strings.HasSuffix(host, "."+n) || n == "www."+host {
				mine = append(mine, d)
			}
		}
		if len(mine) == 0 {
			b.toast(host+" has nothing stored", 2)
			return
		}
		mgr.Remove(context.Background(), types, mine, func(gio.AsyncResulter) { b.toast("Cleared data for "+host, 2) })
	})
}

func (b *Browser) confirmClearAll() {
	d := adw.NewAlertDialog("Clear all browsing data?",
		"History, cookies, site data and the cache go. Bookmarks and open tabs stay.")
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("clear", "Clear")
	d.SetResponseAppearance("clear", adw.ResponseDestructive)
	d.ConnectResponse(func(r string) {
		if r == "clear" {
			b.app.history.Clear(0)
			b.clearSiteData("", true)
		}
	})
	d.Present(b)
}

var bookmarkRe = regexp.MustCompile(`(?is)<A[^>]*HREF="([^"]+)"[^>]*>(.*?)</A>`)

var tagRe = regexp.MustCompile(`<[^>]+>`)

func (b *Browser) importBookmarks() {
	fd := gtk.NewFileDialog()
	fd.SetTitle("Import Bookmarks")
	f := gtk.NewFileFilter()
	f.SetName("Bookmarks HTML")
	f.AddPattern("*.html")
	f.AddPattern("*.htm")
	filters := gio.NewListStore(gtk.GTypeFileFilter)
	filters.Append(f.Object)
	fd.SetFilters(filters)
	fd.Open(context.Background(), &b.Window, func(res gio.AsyncResulter) {
		var gf gio.Filer
		var err error
		try(func() { gf, err = fd.OpenFinish(res) })
		if err != nil || gf == nil {
			return
		}
		data, err := os.ReadFile(gf.Path())
		if err != nil {
			return
		}
		n := 0
		for _, m := range bookmarkRe.FindAllStringSubmatch(string(data), -1) {
			u := m[1]
			title := strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(m[2], "")))
			if (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) && !b.app.bookmarks.Has(u) {
				b.app.bookmarks.Add(u, title)
				n++
			}
		}
		s := "s"
		if n == 1 {
			s = ""
		}
		b.toast(fmt.Sprintf("Imported %d bookmark%s", n, s), 2)
	})
}

func (b *Browser) copyText(text string) {
	gdk.DisplayGetDefault().Clipboard().SetText(text)
	b.toast("Copied", 1)
}

func (b *Browser) pasteAndGo() {
	cb := gdk.DisplayGetDefault().Clipboard()
	cb.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
		text, err := cb.ReadTextFinish(res)
		text = strings.TrimSpace(text)
		if err != nil || text == "" {
			return
		}
		s := b.app.settings
		u := resolve(strings.SplitN(text, "\n", 2)[0], s.Engine, s.CustomEngine)
		if b.current != nil {
			b.current.load(u)
		} else {
			b.openTab(u, true, nil, false, "", false)
		}
	})
}
