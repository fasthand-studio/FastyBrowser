package main

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

func label(text string, classes string, ell pango.EllipsizeMode) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetEllipsize(ell)
	for _, c := range strings.Fields(classes) {
		l.AddCSSClass(c)
	}
	return l
}

func fromUnix(ts float64) time.Time { return time.Unix(int64(ts), 0) }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func when(ts float64) string {
	d := fromUnix(ts)
	n := time.Now()
	switch {
	case sameDay(d, n):
		return d.Format("15:04")
	case sameDay(d, n.AddDate(0, 0, -1)):
		return "Yesterday " + d.Format("15:04")
	}
	return d.Format("02 Jan 2006")
}

func day(ts float64) string {
	d := fromUnix(ts)
	n := time.Now()
	switch {
	case sameDay(d, n):
		return "Today"
	case sameDay(d, n.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	return d.Format("Monday, 02 January 2006")
}

type rowButton struct {
	icon, tip string
	fn        func()
}

// ListPanel is a searchable list in a sheet. Return opens the first row; Esc
// closes.
type ListPanel struct {
	*adw.Dialog
	b        *Browser
	extra    *gtk.Box
	search   *gtk.SearchEntry
	list     *gtk.ListBox
	empty    *adw.StatusPage
	stack    *gtk.Stack
	data     []any
	groups   []string
	populate func(q string) int
	activate func(data any)
}

func newListPanel(b *Browser, title, ph string) *ListPanel {
	p := &ListPanel{Dialog: adw.NewDialog(), b: b}
	p.SetTitle(title)
	p.SetContentWidth(620)
	p.SetContentHeight(560)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	p.extra = gtk.NewBox(gtk.OrientationHorizontal, 6)
	hb.PackEnd(p.extra)
	tv.AddTopBar(hb)
	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)
	box.SetMarginBottom(12)
	p.search = gtk.NewSearchEntry()
	p.search.SetPlaceholderText(ph)
	p.search.ConnectSearchChanged(p.fill)
	p.search.ConnectActivate(func() {
		r := p.list.SelectedRow()
		if r == nil {
			r = p.list.RowAtIndex(0)
		}
		if r != nil {
			p.activateRow(r)
		}
	})
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(kv, _ uint, _ gdk.ModifierType) bool {
		if kv == gdk.KEY_Down {
			if r := p.list.RowAtIndex(0); r != nil {
				p.list.SelectRow(r)
				r.GrabFocus()
			}
			return true
		}
		return false
	})
	p.search.AddController(keys)
	box.Append(p.search)
	sw := gtk.NewScrolledWindow()
	sw.SetVExpand(true)
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	p.list = gtk.NewListBox()
	p.list.SetSelectionMode(gtk.SelectionSingle)
	p.list.AddCSSClass("panel-list")
	p.list.ConnectRowActivated(p.activateRow)
	p.list.SetHeaderFunc(func(row, before *gtk.ListBoxRow) {
		i := row.Index()
		g := ""
		if i >= 0 && i < len(p.groups) {
			g = p.groups[i]
		}
		prev := ""
		if before != nil {
			if j := before.Index(); j >= 0 && j < len(p.groups) {
				prev = p.groups[j]
			}
		}
		if g != "" && (before == nil || prev != g) {
			row.SetHeader(label(g, "panel-group heading", pango.EllipsizeEnd))
		} else {
			row.SetHeader(nil)
		}
	})
	sw.SetChild(p.list)
	p.empty = adw.NewStatusPage()
	p.empty.SetIconName("edit-find-symbolic")
	p.empty.SetTitle("Nothing here")
	p.empty.AddCSSClass("compact")
	p.stack = gtk.NewStack()
	p.stack.AddNamed(sw, "list")
	p.stack.AddNamed(p.empty, "empty")
	box.Append(p.stack)
	tv.SetContent(box)
	toasts := adw.NewToastOverlay()
	toasts.SetChild(tv)
	p.SetChild(toasts)
	releaseToasts := b.toastInto(toasts.AddToast)
	p.ConnectClosed(releaseToasts)
	p.SetFocus(p.search)
	return p
}

func (p *ListPanel) activateRow(r *gtk.ListBoxRow) {
	i := r.Index()
	if p.activate != nil && i >= 0 && i < len(p.data) {
		p.activate(p.data[i])
	}
}

func (p *ListPanel) row(title, sub, right string, data any, group string, icon gtk.Widgetter, buttons ...rowButton) *gtk.ListBoxRow {
	box := gtk.NewBox(gtk.OrientationHorizontal, 10)
	box.AddCSSClass("panel-row")
	if icon != nil && !reflect.ValueOf(icon).IsNil() {
		box.Append(icon)
	}
	col := gtk.NewBox(gtk.OrientationVertical, 0)
	col.SetHExpand(true)
	col.Append(label(title, "", pango.EllipsizeEnd))
	if sub != "" {
		col.Append(label(sub, "dim-label caption", pango.EllipsizeMiddle))
	}
	box.Append(col)
	if right != "" {
		box.Append(label(right, "dim-label caption", pango.EllipsizeEnd))
	}
	for _, bt := range buttons {
		btn := gtk.NewButtonFromIconName(bt.icon)
		btn.SetVAlign(gtk.AlignCenter)
		btn.SetTooltipText(bt.tip)
		btn.AddCSSClass("flat")
		fn := bt.fn
		btn.ConnectClicked(fn)
		box.Append(btn)
	}
	r := gtk.NewListBoxRow()
	r.SetChild(box)
	p.data = append(p.data, data)
	p.groups = append(p.groups, group)
	p.list.Append(r)
	return r
}

func (p *ListPanel) fill() {
	p.list.RemoveAll()
	p.data, p.groups = nil, nil
	n := 0
	if p.populate != nil {
		n = p.populate(p.search.Text())
	}
	if n > 0 {
		p.stack.SetVisibleChildName("list")
	} else {
		p.stack.SetVisibleChildName("empty")
	}
}

func (p *ListPanel) show() {
	p.fill()
	p.Present(p.b)
}

func showHistoryPanel(b *Browser) {
	p := newListPanel(b, "History", "Search history")
	clear := gtk.NewMenuButton()
	clear.SetIconName("user-trash-symbolic")
	clear.SetTooltipText("Clear History")
	menu := gio.NewMenu()
	menu.Append("The last hour", "panel.clear('hour')")
	menu.Append("Today", "panel.clear('today')")
	menu.Append("Everything", "panel.clear('all')")
	clear.SetMenuModel(menu)
	grp := gio.NewSimpleActionGroup()
	a := gio.NewSimpleAction("clear", glib.NewVariantType("s"))
	a.ConnectActivate(func(param *glib.Variant) {
		h := b.app.history
		n := time.Now()
		switch param.String() {
		case "hour":
			h.Clear(float64(n.Unix() - 3600))
		case "today":
			y, m, d := n.Date()
			h.Clear(float64(time.Date(y, m, d, 0, 0, 0, 0, n.Location()).Unix()))
		default:
			h.Clear(0)
			b.clearSiteData("", true)
		}
		p.fill()
	})
	grp.AddAction(a)
	p.InsertActionGroup("panel", grp)
	p.extra.Append(clear)
	p.populate = func(q string) int {
		n := 0
		for _, e := range b.app.history.Search(q, 400) {
			title := e.Title
			if title == "" {
				title = pretty(e.URL)
			}
			right := ""
			if d := day(e.Last); d == "Today" || d == "Yesterday" {
				w := when(e.Last)
				right = w[len(w)-5:]
			}
			u := e.URL
			p.row(title, e.URL, right, u, day(e.Last), nil,
				rowButton{"window-close-symbolic", "Remove from History", func() { b.app.history.Remove(u); p.fill() }})
			n++
		}
		return n
	}
	p.activate = func(d any) {
		b.openTab(d.(string), true, nil, false, "", false)
		p.Close()
	}
	p.show()
}

func showBookmarksPanel(b *Browser) {
	p := newListPanel(b, "Bookmarks", "Search bookmarks")
	add := gtk.NewButtonFromIconName("bookmark-new-symbolic")
	add.SetTooltipText("Bookmark This Page")
	add.ConnectClicked(func() { b.toggleBookmark(); p.fill() })
	p.extra.Append(add)
	p.empty.SetDescription("Ctrl+Shift+B bookmarks the page you are on.")
	p.populate = func(q string) int {
		n := 0
		for _, bm := range b.app.bookmarks.Search(q) {
			u := bm.URL
			p.row(bm.Title, bm.URL, "", u, "", nil,
				rowButton{"user-trash-symbolic", "Delete Bookmark", func() {
					b.app.bookmarks.Remove(u)
					p.fill()
					b.omnibox.secureIcon(b.current)
				}})
			n++
		}
		return n
	}
	p.activate = func(d any) {
		b.openTab(d.(string), true, nil, false, "", false)
		p.Close()
	}
	p.show()
}

func launchPath(b *Browser, path string, containing bool) {
	l := gtk.NewFileLauncher(gio.NewFileForPath(path))
	if containing {
		l.OpenContainingFolder(context.Background(), &b.Window, nil)
	} else {
		l.Launch(context.Background(), &b.Window, nil)
	}
}

func showDownloadsPanel(b *Browser) {
	p := newListPanel(b, "Downloads", "Search downloads")
	d := b.app.downloads
	folder := gtk.NewButtonFromIconName("folder-download-symbolic")
	folder.SetTooltipText("Open Downloads Folder")
	folder.ConnectClicked(func() { launchPath(b, d.folder(), false) })
	p.extra.Append(folder)
	clear := gtk.NewButtonFromIconName("edit-clear-all-symbolic")
	clear.SetTooltipText("Clear Finished")
	clear.ConnectClicked(func() { d.clear(); p.fill() })
	p.extra.Append(clear)
	p.empty.SetIconName("folder-download-symbolic")
	p.empty.SetTitle("No downloads")
	var pending coreglib.SourceHandle
	open := true
	d.onChanged = append(d.onChanged, func() {
		if !open || pending != 0 {
			return
		}
		pending = coreglib.TimeoutAdd(250, func() bool {
			pending = 0
			if open {
				p.fill()
			}
			return false
		})
	})
	p.ConnectClosed(func() { open = false })
	p.populate = func(q string) int {
		n := 0
		for _, it := range d.items {
			name := it.name
			if name == "" {
				name = "Download"
			}
			if q != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(q)) {
				continue
			}
			var sub string
			var btns []rowButton
			it := it
			switch it.state {
			case "running":
				sub = fmt.Sprintf("%d%% · %s", int(it.progress*100), it.sizeText())
				btns = []rowButton{{"process-stop-symbolic", "Cancel", it.cancel}}
			case "done":
				sub = it.sizeText()
				btns = []rowButton{{"folder-open-symbolic", "Show in Folder", func() { launchPath(b, it.path, true) }}}
			case "cancelled":
				sub = "Cancelled"
			default:
				sub = "Failed: " + it.err
			}
			_, ct := gio.ContentTypeGuess(name, nil)
			icon := gtk.NewImageFromGIcon(gio.ContentTypeGetSymbolicIcon(ct))
			p.row(name, sub, "", it, "", icon, btns...)
			n++
		}
		return n
	}
	p.activate = func(x any) {
		it := x.(*DownloadItem)
		if _, err := os.Stat(it.path); it.state == "done" && err == nil {
			launchPath(b, it.path, false)
		}
	}
	p.show()
}

// showTabSwitcher is Ctrl+K: your open tabs by name.
func showTabSwitcher(b *Browser) {
	p := newListPanel(b, "Tabs", "Switch to tab")
	p.SetContentHeight(440)
	p.populate = func(q string) int {
		words := strings.Fields(strings.ToLower(q))
		n := 0
		for _, t := range b.tabs {
			hay := strings.ToLower(t.displayTitle() + " " + t.url)
			ok := true
			for _, w := range words {
				if !strings.Contains(hay, w) {
					ok = false
				}
			}
			if !ok {
				continue
			}
			right := ""
			if t == b.current {
				right = "current"
			}
			r := p.row(t.displayTitle(), pretty(t.url), right, t, "", faviconWidget(t, 16))
			n++
			if n == 1 {
				p.list.SelectRow(r)
			}
		}
		return n
	}
	p.activate = func(d any) {
		b.selectTab(d.(*Tab))
		p.Close()
		b.focusPage()
	}
	p.show()
}

// showHiddenPanel lists what is hidden on this site, and the way to put each
// one back.
func showHiddenPanel(b *Browser, host string) {
	p := newListPanel(b, "Hidden on "+host, "Search hidden elements")
	all := gtk.NewButtonWithLabel("Show All")
	all.ConnectClicked(func() { b.app.curtain.RestoreAll(host); p.fill() })
	p.extra.Append(all)
	p.empty.SetIconName("view-reveal-symbolic")
	p.empty.SetTitle("Nothing hidden here")
	p.empty.SetDescription("Ctrl+Shift+H, then click what you want gone.")
	p.populate = func(q string) int {
		n := 0
		vs := b.app.curtain.Veils(host)
		for i := len(vs) - 1; i >= 0; i-- {
			v := vs[i]
			if q != "" && !strings.Contains(strings.ToLower(v.Label+v.Selector), strings.ToLower(q)) {
				continue
			}
			sub := v.Note
			if sub == "" {
				sub = v.Selector
			}
			sel := v.Selector
			p.row(v.Label, sub, when(v.Date), v, "", nil,
				rowButton{"view-reveal-symbolic", "Show Again", func() { b.app.curtain.Restore(host, sel); p.fill() }})
			n++
		}
		return n
	}
	p.show()
}
