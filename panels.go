package main

// The panels: each searchable, each one keystroke away.

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
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
	p.SetChild(tv)
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

// siteCard is "about this site": is the connection private, is the blocker
// on here, what is hidden here.
func siteCard(b *Browser, anchor gtk.Widgetter) {
	t := b.current
	pop := gtk.NewPopover()
	pop.SetParent(anchor)
	box := gtk.NewBox(gtk.OrientationVertical, 10)
	box.AddCSSClass("site-card")
	if t == nil || t.url == "" {
		box.Append(label("No page open", "", pango.EllipsizeNone))
	} else {
		host := hostOf(t.url)
		if host == "" {
			host = t.url
		}
		box.Append(label(host, "title-4", pango.EllipsizeEnd))
		var secure string
		switch {
		case t.private:
			secure = "Private tab: nothing is kept after it closes."
		case strings.HasPrefix(t.url, "https://"):
			secure = "Connection is encrypted."
		case strings.HasPrefix(t.url, "http://"):
			secure = "Connection is not encrypted. Don't type passwords here."
		default:
			secure = scheme(t.url) + " page"
		}
		box.Append(label(secure, "dim-label", pango.EllipsizeNone))
		if t.view != nil && strings.HasPrefix(t.url, "https://") {
			var cert gio.TLSCertificater
			var ok bool
			try(func() { cert, _, ok = t.view.TLSInfo() })
			if ok && cert != nil {
				c := gio.BaseTLSCertificate(cert)
				issuer := c.IssuerName()
				txt := issuer
				if i := strings.LastIndex(issuer, "O="); i >= 0 {
					txt = strings.SplitN(issuer[i+2:], ",", 2)[0]
				}
				txt = "Certificate from " + txt
				if exp := c.NotValidAfter(); exp != nil {
					txt += "\nValid until " + exp.Format("%d %b %Y")
				}
				box.Append(label(txt, "dim-label caption", pango.EllipsizeNone))
			}
		}
		if b.app.settings.Shield && host != "" {
			row := gtk.NewBox(gtk.OrientationHorizontal, 12)
			row.Append(label("Block ads and trackers", "", pango.EllipsizeNone))
			sw := gtk.NewSwitch()
			sw.SetActive(!b.app.shield.Paused(host))
			sw.SetHExpand(true)
			sw.SetHAlign(gtk.AlignEnd)
			sw.NotifyProperty("active", func() {
				b.app.shield.Pause(host, !sw.Active())
				b.retuneShield()
				b.reload(t, false)
			})
			row.Append(sw)
			box.Append(row)
		}
		veils := b.app.curtain.Veils(host)
		text := "Hide something on this page…"
		if len(veils) > 0 {
			text = fmt.Sprintf("%d hidden on this site…", len(veils))
		}
		hid := gtk.NewButtonWithLabel(text)
		hid.AddCSSClass("flat")
		hid.ConnectClicked(func() {
			pop.Popdown()
			if len(veils) > 0 {
				b.showHidden()
			} else {
				b.startPicking()
			}
		})
		box.Append(hid)
		if t.view != nil {
			if z := t.view.ZoomLevel(); z < 0.99 || z > 1.01 {
				zb := gtk.NewButtonWithLabel(fmt.Sprintf("Zoom %d%% — reset", int(z*100+0.5)))
				zb.AddCSSClass("flat")
				zb.ConnectClicked(func() { b.zoom(0); pop.Popdown() })
				box.Append(zb)
			}
		}
		if !t.private {
			clear := gtk.NewButtonWithLabel("Clear cookies and data for this site")
			clear.AddCSSClass("flat")
			clear.AddCSSClass("error")
			clear.ConnectClicked(func() { b.clearSiteData(host, false); pop.Popdown() })
			box.Append(clear)
		}
	}
	pop.SetChild(box)
	pop.ConnectClosed(func() { coreglib.IdleAdd(func() { pop.Unparent() }) })
	pop.Popup()
}

func permissionKind(req webkit.PermissionRequester) (string, string) {
	switch r := req.(type) {
	case *webkit.UserMediaPermissionRequest:
		cam := webkit.UserMediaPermissionIsForVideoDevice(r)
		mic := webkit.UserMediaPermissionIsForAudioDevice(r)
		switch {
		case webkit.UserMediaPermissionIsForDisplayDevice(r):
			return "UserMediaPermissionRequest", "see your screen"
		case cam && mic:
			return "UserMediaPermissionRequest", "use your camera and microphone"
		case cam:
			return "UserMediaPermissionRequest", "use your camera"
		}
		return "UserMediaPermissionRequest", "use your microphone"
	case *webkit.GeolocationPermissionRequest:
		return "GeolocationPermissionRequest", "know your location"
	case *webkit.NotificationPermissionRequest:
		return "NotificationPermissionRequest", "show notifications"
	case *webkit.DeviceInfoPermissionRequest:
		return "DeviceInfoPermissionRequest", "see which cameras and microphones you have"
	case *webkit.MediaKeySystemPermissionRequest:
		return "MediaKeySystemPermissionRequest", "play protected content"
	case *webkit.WebsiteDataAccessPermissionRequest:
		return "WebsiteDataAccessPermissionRequest", "use its cookies on this site"
	}
	return "PermissionRequest", "do something that needs your permission"
}

func askPermission(b *Browser, t *Tab, req webkit.PermissionRequester) {
	host := hostOf(t.url)
	if host == "" {
		host = "This page"
	}
	kind, what := permissionKind(req)
	key := host + "|" + kind
	if allow, ok := b.app.settings.Permissions[key]; ok {
		if allow {
			req.Allow()
		} else {
			req.Deny()
		}
		return
	}
	d := adw.NewAlertDialog(host+" wants to "+what, "")
	d.AddResponse("deny", "Don't Allow")
	d.AddResponse("allow", "Allow")
	d.SetResponseAppearance("allow", adw.ResponseSuggested)
	d.SetDefaultResponse("deny")
	d.SetCloseResponse("deny")
	remember := gtk.NewCheckButtonWithLabel("Remember for this site")
	d.SetExtraChild(remember)
	d.ConnectResponse(func(r string) {
		allow := r == "allow"
		if remember.Active() {
			b.app.settings.Set("permissions", func(s *SettingsData) { s.Permissions[key] = allow })
		}
		if allow {
			req.Allow()
		} else {
			req.Deny()
		}
	})
	d.Present(b)
}

func switchRow(b *Browser, title, key string, get func(*SettingsData) *bool, restart bool) *adw.SwitchRow {
	r := adw.NewSwitchRow()
	r.SetTitle(title)
	r.SetActive(*get(&b.app.settings.SettingsData))
	if restart {
		r.SetSubtitle("Takes effect next launch")
	}
	r.NotifyProperty("active", func() {
		b.app.settings.Set(key, func(s *SettingsData) { *get(s) = r.Active() })
	})
	return r
}

func comboRow(title, subtitle string, names []string, selected int, onPick func(i int)) *adw.ComboRow {
	r := adw.NewComboRow()
	r.SetTitle(title)
	if subtitle != "" {
		r.SetSubtitle(subtitle)
	}
	r.SetModel(gtk.NewStringList(names))
	if selected >= 0 {
		r.SetSelected(uint(selected))
	}
	r.NotifyProperty("selected", func() { onPick(int(r.Selected())) })
	return r
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return 0
}

func suffixButton(row *adw.ActionRow, text string, classes string, fn func()) *gtk.Button {
	btn := gtk.NewButtonWithLabel(text)
	btn.SetVAlign(gtk.AlignCenter)
	for _, c := range strings.Fields(classes) {
		btn.AddCSSClass(c)
	}
	btn.ConnectClicked(fn)
	row.AddSuffix(btn)
	return btn
}

func showSettings(b *Browser) {
	s := b.app.settings
	d := adw.NewPreferencesDialog()
	d.SetTitle("Settings")
	d.SetSearchEnabled(true)

	// General
	page := adw.NewPreferencesPage()
	page.SetTitle("General")
	page.SetIconName("preferences-system-symbolic")
	g := adw.NewPreferencesGroup()
	g.SetTitle("Searching")
	var keys, names []string
	for _, e := range engines {
		keys = append(keys, e.key)
		names = append(names, e.name)
	}
	custom := adw.NewEntryRow()
	custom.SetTitle("Custom search address (%s for the words)")
	custom.SetText(s.CustomEngine)
	custom.SetVisible(s.Engine == "custom")
	custom.ConnectChanged(func() { s.Set("custom_engine", func(d *SettingsData) { d.CustomEngine = custom.Text() }) })
	g.Add(comboRow("Search engine", "", names, indexOf(keys, s.Engine), func(i int) {
		s.Set("engine", func(d *SettingsData) { d.Engine = keys[i] })
		custom.SetVisible(keys[i] == "custom")
	}))
	g.Add(custom)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Appearance")
	looks := []string{"system", "light", "dark"}
	g.Add(comboRow("Light, dark, or the system's own", "", []string{"System", "Light", "Dark"},
		indexOf(looks, s.Appearance), func(i int) { s.Set("appearance", func(d *SettingsData) { d.Appearance = looks[i] }) }))
	sides := []string{"left", "top"}
	g.Add(comboRow("Tabs", "Ctrl+Shift+S switches", []string{"Down the left", "Across the top"},
		indexOf(sides, s.TabsSide), func(i int) { s.Set("tabs_side", func(d *SettingsData) { d.TabsSide = sides[i] }) }))
	zoom := adw.NewSpinRowWithRange(30, 300, 10)
	zoom.SetTitle("Page zoom (%)")
	zoom.SetValue(float64(int(s.Zoom*100 + 0.5)))
	zoom.NotifyProperty("value", func() { b.setDefaultZoom(zoom.Value() / 100) })
	g.Add(zoom)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Starting up and speed")
	g.Add(switchRow(b, "Reopen last session's tabs", "restore_session", func(d *SettingsData) *bool { return &d.RestoreSession }, false))
	sleep := adw.NewSpinRowWithRange(0, 240, 5)
	sleep.SetTitle("Put unused tabs to sleep after (minutes)")
	sleep.SetSubtitle("Frees their memory; 0 never does")
	sleep.SetValue(float64(s.SleepAfterMinutes))
	sleep.NotifyProperty("value", func() {
		s.Set("sleep_after_minutes", func(d *SettingsData) { d.SleepAfterMinutes = int(sleep.Value()) })
	})
	g.Add(sleep)
	g.Add(switchRow(b, "Check spelling while typing", "spellcheck", func(d *SettingsData) *bool { return &d.Spellcheck }, true))
	g.Add(switchRow(b, "Smooth scrolling", "smooth_scrolling", func(d *SettingsData) *bool { return &d.SmoothScrolling }, true))
	g.Add(switchRow(b, "Hardware acceleration", "hardware_acceleration", func(d *SettingsData) *bool { return &d.HardwareAcceleration }, true))
	def := adw.NewActionRow()
	def.SetTitle("Default browser")
	def.SetSubtitle("Open links from other apps in Fasty")
	suffixButton(def, "Make Default", "", b.makeDefault)
	g.Add(def)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Downloads")
	dl := adw.NewActionRow()
	dl.SetTitle("Save files to")
	dl.SetSubtitle(b.app.downloads.folder())
	suffixButton(dl, "Choose…", "", func() {
		fd := gtk.NewFileDialog()
		fd.SetTitle("Save downloads to")
		fd.SelectFolder(context.Background(), &b.Window, func(res gio.AsyncResulter) {
			var f gio.Filer
			var err error
			try(func() { f, err = fd.SelectFolderFinish(res) })
			if err != nil || f == nil {
				return
			}
			p := f.Path()
			s.Set("downloads_dir", func(d *SettingsData) { d.DownloadsDir = p })
			dl.SetSubtitle(p)
		})
	})
	g.Add(dl)
	page.Add(g)
	d.Add(page)

	// Extensions
	page.Add(extensionSettingsGroup(b))

	// Privacy
	page = adw.NewPreferencesPage()
	page.SetTitle("Privacy")
	page.SetIconName("security-high-symbolic")
	g = adw.NewPreferencesGroup()
	g.SetTitle("Blocking")
	g.SetDescription("EasyList, EasyPrivacy and StevenBlack hosts, compiled with Brave's adblock-rust, stop ads and trackers at the network level, before the page asks for them.")
	g.Add(switchRow(b, "Block ads and trackers", "shield", func(d *SettingsData) *bool { return &d.Shield }, false))
	g.Add(switchRow(b, "Remove tracking parameters from links", "clean_links", func(d *SettingsData) *bool { return &d.CleanLinks }, false))
	g.Add(switchRow(b, "Prevent cross-site tracking", "tracking_prevention", func(d *SettingsData) *bool { return &d.TrackingPrevention }, true))
	page.Add(g)
	if len(s.ShieldPaused) > 0 {
		g := adw.NewPreferencesGroup()
		g.SetTitle("Blocker is off on")
		for _, host := range s.ShieldPaused {
			host := host
			r := adw.NewActionRow()
			r.SetTitle(host)
			btn := gtk.NewButtonFromIconName("user-trash-symbolic")
			btn.SetVAlign(gtk.AlignCenter)
			btn.AddCSSClass("flat")
			btn.ConnectClicked(func() {
				b.app.shield.Pause(host, false)
				b.retuneShield()
				g.Remove(r)
			})
			r.AddSuffix(btn)
			g.Add(r)
		}
		page.Add(g)
	}
	if hosts := b.app.curtain.Hosts(); len(hosts) > 0 {
		g := adw.NewPreferencesGroup()
		g.SetTitle("Hidden elements")
		for _, host := range hosts {
			host := host
			r := adw.NewActionRow()
			r.SetTitle(host)
			r.SetSubtitle(fmt.Sprintf("%d hidden", len(b.app.curtain.Veils(host))))
			suffixButton(r, "Show All", "flat", func() { b.app.curtain.RestoreAll(host); g.Remove(r) })
			g.Add(r)
		}
		page.Add(g)
	}
	g = adw.NewPreferencesGroup()
	g.SetTitle("Browsing data")
	r := adw.NewActionRow()
	r.SetTitle("Clear everything")
	r.SetSubtitle("History, cookies, site data and cache. Bookmarks stay.")
	suffixButton(r, "Clear…", "destructive-action", b.confirmClearAll)
	g.Add(r)
	r = adw.NewActionRow()
	r.SetTitle("Import bookmarks")
	r.SetSubtitle("From an HTML bookmarks file (Chrome, Firefox…)")
	suffixButton(r, "Import…", "", b.importBookmarks)
	g.Add(r)
	page.Add(g)
	d.Add(page)

	// Open source credits: each row names the project, its licence and a link
	// to the upstream licence or project page.
	page = adw.NewPreferencesPage()
	page.SetTitle("Open Source")
	page.SetIconName("help-about-symbolic")
	g = adw.NewPreferencesGroup()
	g.SetTitle("Open source licenses")
	g.SetDescription("Fasty is built on the work of these projects.")
	type credit struct {
		name, license, url string
	}
	credits := []credit{
		{"Fasty Browser", "MIT", "https://github.com/fasthand-studio/Fasty-Browser/blob/main/LICENSE"},
		{"Go", "BSD-3-Clause", "https://go.dev/LICENSE"},
		{"GTK 4", "LGPL-2.1-or-later", "https://www.gtk.org/"},
		{"libadwaita", "LGPL-2.1-or-later", "https://gitlab.gnome.org/GNOME/libadwaita"},
		{"WebKitGTK 6.0", "LGPL-2.1-or-later / BSD-3-Clause", "https://webkit.org/licensing/"},
		{"GStreamer", "LGPL-2.1-or-later", "https://gstreamer.freedesktop.org/"},
		{"gotk4 bindings", "MPL-2.0", "https://github.com/diamondburned/gotk4"},
		{"adblock-rust", "MPL-2.0", "https://github.com/brave/adblock-rust/blob/master/LICENSE"},
		{"serde_json", "MIT OR Apache-2.0", "https://github.com/serde-rs/json"},
		{"EasyList / EasyPrivacy", "GPL-3.0-or-later OR CC BY-SA 3.0-or-later", "https://easylist.to/pages/licence.html"},
		{"StevenBlack hosts", "MIT", "https://github.com/StevenBlack/hosts/blob/master/license.txt"},
	}
	for _, c := range credits {
		c := c
		r := adw.NewActionRow()
		r.SetTitle(c.name)
		r.SetSubtitle(c.license)
		suffixButton(r, "View", "flat", func() {
			b.openTab(c.url, true, nil, false, "", false)
		})
		g.Add(r)
	}
	page.Add(g)
	d.Add(page)
	d.Present(b)
}
