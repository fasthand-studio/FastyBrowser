// The browser's tab list: opening, closing, moving, pinning, muting and putting tabs to sleep.
package main

import (
	"log"
	"sort"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// --- tabs ---

func (b *Browser) index(t *Tab) int {
	for i, x := range b.tabs {
		if x == t {
			return i
		}
	}
	return -1
}

func (b *Browser) insert(t *Tab, at int) {
	if at < 0 || at > len(b.tabs) {
		b.tabs = append(b.tabs, t)
		return
	}
	b.tabs = append(b.tabs, nil)
	copy(b.tabs[at+1:], b.tabs[at:])
	b.tabs[at] = t
}

func (b *Browser) removeFromList(t *Tab) {
	if i := b.index(t); i >= 0 {
		b.tabs = append(b.tabs[:i], b.tabs[i+1:]...)
	}
}

// place is where a tab opened from after goes: after the run of tabs that
// one already opened.
func (b *Browser) place(after *Tab) int {
	i := b.index(after)
	if after == nil || i < 0 {
		return -1
	}
	i++
	for i < len(b.tabs) && b.tabs[i].opener == after {
		i++
	}
	return i
}

func (b *Browser) openTab(u string, focus bool, after *Tab, private bool, title string, pinned bool) *Tab {
	t := newTab(b, u, title, pinned, private)
	t.opener = after
	b.insert(t, b.place(after))
	if x := b.app.extensions; x != nil && x.anyBackground() {
		x.fire("tabs.onCreated", x.tabInfo(t))
	}
	b.strip.rebuild()
	if focus || b.current == nil {
		b.selectTab(t)
	}
	b.saveSession()
	return t
}

// popup opens a window.open / target=_blank page as a tab of this window.
// The view is still a related view, so window.opener and postMessage between
// the two pages keep working, and window.close() closes the tab.
func (b *Browser) popup(opener *Tab, view *webkit.WebView, uri string) {
	t := adoptTab(b, view, uri, opener.private)
	t.opener = opener
	b.insert(t, b.place(opener))
	if x := b.app.extensions; x != nil && x.anyBackground() {
		x.fire("tabs.onCreated", x.tabInfo(t))
	}
	b.strip.rebuild()
	// The view must be in the widget tree before onCreate returns.
	b.selectTab(t)
	b.saveSession()
	log.Printf("fasty: popup opened as tab: %s", hostOf(uri))
}

// newTab has no start page: the field, waiting for where to go.
// newTab opens the search field in the middle of the window; the tab itself
// is made when you go somewhere.
func (b *Browser) newTab(private bool) {
	b.spotlight.open(private)
}

func (b *Browser) selectTab(t *Tab) {
	if t == nil || b.index(t) < 0 {
		return
	}
	if b.current == t && t.view != nil && hasParent(t.view) {
		return
	}
	if b.current != nil && b.current.picking {
		b.stopPicking()
	}
	if prev := b.current; prev != nil && prev != t {
		b.parkView(prev) // its video may be playing in the pop-up
	}
	b.unparkView(t)
	b.current = t
	if x := b.app.extensions; x != nil && x.anyBackground() {
		x.fire("tabs.onActivated", map[string]any{"tabId": t.num, "windowId": 1})
	}
	if t.pip != nil && t.pip.placeholder != nil {
		// its page is in the floating window; the tab says so
		b.stack.SetVisibleChild(t.pip.placeholder)
		t.lastSeen = now()
		b.syncChrome()
		b.strip.refresh(nil)
		b.saveSession()
		return
	}
	view := t.build()
	if !hasParent(view) {
		b.stack.AddNamed(view, t.id)
	}
	b.stack.SetVisibleChild(view)
	t.lastSeen = now()
	b.syncChrome()
	b.strip.refresh(nil)
	b.closeFind(true)
	// The page takes the keyboard, unless you are typing in the field.
	if !b.omnibox.editing || strings.TrimSpace(b.omnibox.entry.Text()) == "" {
		coreglib.IdleAdd(func() { view.GrabFocus() })
	}
	b.saveSession()
}

func (b *Browser) tabChanged(t *Tab) {
	b.strip.refresh(t)
	if t.pip != nil {
		t.pip.title.SetText(t.displayTitle())
	}
	if t == b.current {
		b.syncChrome()
	}
	b.saveSession()
}

func (b *Browser) syncChrome() {
	t := b.current
	if t == nil {
		b.SetTitle("Fasty")
		b.omnibox.showTab(nil)
		b.progress.SetVisible(false)
		b.back.SetSensitive(false)
		b.fwd.SetSensitive(false)
		return
	}
	title := t.displayTitle()
	if t.private {
		title += " — Private"
	}
	b.SetTitle(title)
	b.omnibox.showTab(t)
	v := t.view
	b.back.SetSensitive(v != nil && v.CanGoBack())
	b.fwd.SetSensitive(v != nil && v.CanGoForward())
	if t.loading {
		b.reloadBtn.SetIconName("process-stop-symbolic")
	} else {
		b.reloadBtn.SetIconName("view-refresh-symbolic")
	}
	b.progress.SetVisible(t.loading && t.progress < 1)
	f := t.progress
	if f < 0.08 {
		f = 0.08
	}
	b.progress.SetFraction(f)
}

func (b *Browser) closeTab(t *Tab) {
	if t == nil {
		t = b.current
	}
	if t == nil || t.view == nil {
		return
	}
	b.app.extensions.authTabClosed(t)
	if x := b.app.extensions; x != nil && x.anyBackground() {
		x.fire("tabs.onRemoved", t.num, map[string]any{"windowId": 1, "isWindowClosing": false})
	}
	// A popup lives in its own window and not in the tab strip. Closing it
	// (from window.close in the page or from the window's close button)
	// tears down the view and the window together.
	if t.popupWin != nil {
		win := t.popupWin
		t.popupWin = nil
		for i, p := range b.popups {
			if p == t {
				b.popups = append(b.popups[:i], b.popups[i+1:]...)
				break
			}
		}
		t.destroy()
		b.saveSession()
		log.Printf("fasty: popup closed: %s (total %d)", t.url, len(b.popups))
		win.Close()
		return
	}
	i := b.index(t)
	if i < 0 {
		return
	}
	if !t.private && t.url != "" {
		b.closed = append(b.closed, closedTab{t.snapshot(), i})
		if len(b.closed) > 30 {
			b.closed = b.closed[len(b.closed)-30:]
		}
	}
	b.removeFromList(t)
	b.dropPiP(t)
	if t.picking {
		b.banner.SetRevealChild(false)
	}
	if t.view != nil && hasParent(t.view) {
		b.stack.Remove(t.view)
	}
	t.destroy()
	if b.current == t {
		b.current = nil
		// The opener, if it is still here; otherwise the neighbour below,
		// then above.
		var next *Tab
		if t.opener != nil && b.index(t.opener) >= 0 {
			next = t.opener
		} else if len(b.tabs) > 0 {
			if i >= len(b.tabs) {
				i = len(b.tabs) - 1
			}
			next = b.tabs[i]
		}
		b.strip.rebuild()
		if next != nil {
			b.selectTab(next)
		} else {
			b.stack.SetVisibleChild(b.blank)
			b.syncChrome()
			b.omnibox.focus(false, false)
		}
	} else {
		b.strip.rebuild()
	}
	b.saveSession()
}

func (b *Browser) closeOthers(keep *Tab) {
	for _, t := range append([]*Tab(nil), b.tabs...) {
		if t != keep && !t.pinned {
			b.closeTab(t)
		}
	}
	b.selectTab(keep)
}

func (b *Browser) closeBelow(t *Tab) {
	i := b.index(t)
	if i < 0 {
		return
	}
	for _, x := range append([]*Tab(nil), b.tabs[i+1:]...) {
		if !x.pinned {
			b.closeTab(x)
		}
	}
}

func (b *Browser) reopenClosed() {
	if len(b.closed) == 0 {
		return
	}
	c := b.closed[len(b.closed)-1]
	b.closed = b.closed[:len(b.closed)-1]
	t := newTab(b, c.snap.URL, c.snap.Title, c.snap.Pinned, false)
	at := c.index
	if at > len(b.tabs) {
		at = len(b.tabs)
	}
	b.insert(t, at)
	b.strip.rebuild()
	b.selectTab(t)
}

func (b *Browser) duplicate(t *Tab) {
	if t == nil {
		t = b.current
	}
	if t != nil && t.url != "" {
		b.openTab(t.url, true, t, t.private, t.title, false)
	}
}

func (b *Browser) pinnedCount() int {
	n := 0
	for _, t := range b.tabs {
		if t.pinned {
			n++
		}
	}
	return n
}

func (b *Browser) setPinned(t *Tab, pinned bool) {
	b.removeFromList(t)
	t.pinned = pinned
	b.insert(t, b.pinnedCount())
	b.strip.rebuild()
	b.saveSession()
}

func (b *Browser) moveTab(id string, before *Tab, pinned bool) {
	var t *Tab
	for _, x := range b.tabs {
		if x.id == id {
			t = x
		}
	}
	if t == nil || t == before {
		return
	}
	if t.private {
		pinned = false
	}
	b.removeFromList(t)
	t.pinned = pinned
	switch {
	case before != nil && b.index(before) >= 0:
		b.insert(t, b.index(before))
	case pinned:
		b.insert(t, b.pinnedCount())
	default:
		b.tabs = append(b.tabs, t)
	}
	// Pinned ones stay together at the front.
	sort.SliceStable(b.tabs, func(i, j int) bool { return b.tabs[i].pinned && !b.tabs[j].pinned })
	b.strip.rebuild()
	b.saveSession()
}

func (b *Browser) sleepTab(t *Tab) {
	if t == b.current || t.view == nil || t.pip != nil {
		return
	}
	if v := t.unload(); v != nil {
		if hasParent(v) {
			b.stack.Remove(v)
		}
		v.TerminateWebProcess()
	}
}

// sleepIdle puts to sleep the tabs you haven't looked at for a while, unless
// they are playing sound: each one is a whole web process.
func (b *Browser) sleepIdle() {
	mins := b.app.settings.SleepAfterMinutes
	if mins <= 0 {
		return
	}
	cut := now() - float64(mins*60)
	for _, t := range b.tabs {
		if t != b.current && t.built() && !t.private && !t.audio && !t.loading && t.lastSeen < cut {
			b.sleepTab(t)
		}
	}
}

// sleepForMemory answers the system's low-memory warning: at the first level,
// background tabs unseen for five minutes sleep; past it, every background
// tab does. Tabs playing sound are left alone.
func (b *Browser) sleepForMemory(level gio.MemoryMonitorWarningLevel) {
	cut := now() - 5*60
	if level > gio.MemoryMonitorWarningLevelLow {
		cut = now()
	}
	for _, t := range b.tabs {
		if t != b.current && t.built() && !t.private && !t.audio && t.lastSeen <= cut {
			b.sleepTab(t)
		}
	}
}

func (b *Browser) stepTab(delta int) {
	if len(b.tabs) == 0 {
		return
	}
	i := b.index(b.current)
	if i < 0 {
		i = 0
	}
	n := len(b.tabs)
	b.selectTab(b.tabs[((i+delta)%n+n)%n])
}

func (b *Browser) jump(n int) {
	if len(b.tabs) == 0 {
		return
	}
	if n == 9 || n > len(b.tabs) {
		b.selectTab(b.tabs[len(b.tabs)-1])
		return
	}
	b.selectTab(b.tabs[n-1])
}

func (b *Browser) toggleMute(t *Tab) {
	if t.view == nil {
		return
	}
	t.muted = !t.muted
	t.view.SetIsMuted(t.muted)
	t.changed()
}
