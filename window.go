package main

// The one window: a row of tabs and the page (Browser.swift, Side.swift,
// TabBar.swift, Stage.swift).

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

type closedTab struct {
	snap  tabSnap
	index int
}

type binding struct {
	mods gdk.ModifierType
	key  uint
}

type Browser struct {
	*adw.ApplicationWindow
	app         *App
	tabs        []*Tab
	popups      []*Tab // popups live in their own windows, not in tabs
	current     *Tab
	closed      []closedTab
	fullPage    bool
	peeking     bool
	strip       *TabStrip
	omnibox     *Omnibox
	stack       *gtk.Stack
	blank       *gtk.Box
	overlay     *gtk.Overlay
	spotlight   *Spotlight
	progress    *gtk.ProgressBar
	status      *gtk.Label
	findbar     *gtk.Revealer
	findEntry   *gtk.SearchEntry
	findCount   *gtk.Label
	banner      *gtk.Revealer
	bannerLabel *gtk.Label
	toasts      *adw.ToastOverlay
	card        *gtk.Box
	side        *gtk.Box
	sideHead    *gtk.WindowHandle
	headBox     *gtk.Box
	sideField   *gtk.Box
	sideFoot    *gtk.Box
	grip        *gtk.Box
	edge        *gtk.Box
	top         *gtk.WindowHandle
	topNav      *gtk.Box
	topMenu     *gtk.Box
	topField    *gtk.Box
	body        *gtk.Box
	root        *gtk.Box
	nav         *gtk.Box
	back        *gtk.Button
	fwd         *gtk.Button
	reloadBtn   *gtk.Button
	menu        *gtk.MenuButton
	dlButton    *gtk.Button
	bindings    map[binding]func()
	saving      coreglib.SourceHandle
	w0          int
	extButtons  []*gtk.Button
}

func newBrowser(app *App) *Browser {
	b := &Browser{ApplicationWindow: adw.NewApplicationWindow(&app.Application.Application), app: app}
	s := app.settings
	b.SetTitle("Fasty")
	b.AddCSSClass("fasty-window")
	b.SetDefaultSize(s.Window.W, s.Window.H)
	if s.Window.Max {
		b.Maximize()
	}
	b.strip = newTabStrip(b)
	b.omnibox = newOmnibox(b)
	b.aliasPoll()

	// --- the page ---
	b.stack = gtk.NewStack()
	b.stack.SetTransitionType(gtk.StackTransitionTypeNone)
	b.stack.SetHExpand(true)
	b.stack.SetVExpand(true)
	b.blank = b.blankPage()
	b.stack.AddNamed(b.blank, "blank")
	b.overlay = gtk.NewOverlay()
	b.overlay.SetChild(b.stack)
	b.progress = gtk.NewProgressBar()
	b.progress.SetVAlign(gtk.AlignStart)
	b.progress.SetCanTarget(false)
	b.progress.AddCSSClass("osd")
	b.progress.AddCSSClass("page-progress")
	b.overlay.AddOverlay(b.progress)
	b.status = gtk.NewLabel("")
	b.status.SetHAlign(gtk.AlignStart)
	b.status.SetVAlign(gtk.AlignEnd)
	b.status.SetCanTarget(false)
	b.status.SetEllipsize(pango.EllipsizeMiddle)
	b.status.SetMaxWidthChars(90)
	b.status.AddCSSClass("status-line")
	b.status.SetVisible(false)
	b.overlay.AddOverlay(b.status)
	b.findbar = b.findBar()
	b.overlay.AddOverlay(b.findbar)
	b.banner = b.hideBanner()
	b.overlay.AddOverlay(b.banner)
	b.spotlight = newSpotlight(b)
	b.overlay.AddOverlay(b.spotlight)
	b.toasts = adw.NewToastOverlay()
	b.toasts.SetChild(b.overlay)
	b.card = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.card.SetHExpand(true)
	b.card.SetVExpand(true)
	b.card.AddCSSClass("page-card")
	b.card.SetOverflow(gtk.OverflowHidden)
	b.card.Append(b.toasts)

	// --- the column ---
	b.side = gtk.NewBox(gtk.OrientationVertical, 0)
	b.side.AddCSSClass("side")
	b.side.SetSizeRequest(s.SidebarWidth, -1)
	b.side.SetHExpand(false)
	b.sideHead = gtk.NewWindowHandle()
	b.headBox = gtk.NewBox(gtk.OrientationHorizontal, 2)
	b.headBox.AddCSSClass("side-head")
	b.headBox.Append(gtk.NewWindowControls(gtk.PackStart))
	b.nav = b.navButtons()
	b.headBox.Append(b.nav)
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	b.headBox.Append(spacer)
	b.headBox.Append(gtk.NewWindowControls(gtk.PackEnd))
	b.sideHead.SetChild(b.headBox)
	b.side.Append(b.sideHead)
	b.sideField = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.sideField.AddCSSClass("side-field")
	b.side.Append(b.sideField)
	b.side.Append(b.strip.pins)
	sw := gtk.NewScrolledWindow()
	sw.SetVExpand(true)
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.AddCSSClass("side-scroll")
	wrap := gtk.NewBox(gtk.OrientationVertical, 0)
	wrap.Append(b.strip.rows)
	newtab := gtk.NewButton()
	nb := gtk.NewBox(gtk.OrientationHorizontal, 8)
	nb.Append(gtk.NewImageFromIconName("list-add-symbolic"))
	nl := gtk.NewLabel("New Tab")
	nl.SetXAlign(0)
	nb.Append(nl)
	newtab.SetChild(nb)
	newtab.AddCSSClass("flat")
	newtab.AddCSSClass("new-tab")
	newtab.ConnectClicked(func() { b.newTab(false) })
	wrap.Append(newtab)
	// Double-click the empty column for a new tab, as in a tab bar.
	empty := gtk.NewBox(gtk.OrientationVertical, 0)
	empty.SetVExpand(true)
	dbl := gtk.NewGestureClick()
	dbl.ConnectPressed(func(n int, x, y float64) {
		if n == 2 {
			b.newTab(false)
		}
	})
	empty.AddController(dbl)
	wrap.Append(empty)
	sw.SetChild(wrap)
	b.side.Append(sw)
	b.sideFoot = gtk.NewBox(gtk.OrientationHorizontal, 2)
	b.sideFoot.AddCSSClass("side-foot")
	b.menu = b.menuButton()
	b.sideFoot.Append(b.menu)
	sp2 := gtk.NewBox(gtk.OrientationHorizontal, 0)
	sp2.SetHExpand(true)
	b.sideFoot.Append(sp2)
	extBtn := gtk.NewButtonFromIconName("application-x-addon-symbolic")
	extBtn.SetTooltipText("Extensions (Ctrl+Shift+E)")
	extBtn.AddCSSClass("flat")
	extBtn.ConnectClicked(func() { showExtensionsPanel(b) })
	b.sideFoot.Append(extBtn)
	b.dlButton = gtk.NewButtonFromIconName("folder-download-symbolic")
	b.dlButton.SetTooltipText("Downloads (Ctrl+Shift+J)")
	b.dlButton.AddCSSClass("flat")
	b.dlButton.ConnectClicked(func() { showDownloadsPanel(b) })
	b.sideFoot.Append(b.dlButton)
	b.side.Append(b.sideFoot)

	// Resizing the column by its edge.
	b.grip = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.grip.AddCSSClass("side-grip")
	b.grip.SetSizeRequest(6, -1)
	b.grip.SetCursor(gdk.NewCursorFromName("col-resize", nil))
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(func(x, y float64) { b.w0 = b.side.Width() })
	drag.ConnectDragUpdate(func(dx, dy float64) {
		w := b.w0 + int(dx)
		if w < 180 {
			w = 180
		}
		if w > 480 {
			w = 480
		}
		b.side.SetSizeRequest(w, -1)
	})
	drag.ConnectDragEnd(func(x, y float64) {
		w := b.side.Width()
		s.Set("sidebar_width", func(d *SettingsData) { d.SidebarWidth = w })
	})
	b.grip.AddController(drag)

	// Folded away (Ctrl+S): the column comes back while the pointer is at
	// the window's left edge.
	b.edge = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.edge.SetSizeRequest(8, -1)
	m := gtk.NewEventControllerMotion()
	m.ConnectEnter(func(x, y float64) { b.peek(true) })
	b.edge.AddController(m)
	m2 := gtk.NewEventControllerMotion()
	m2.ConnectLeave(func() { b.peek(false) })
	b.side.AddController(m2)

	// --- across the top ---
	b.top = gtk.NewWindowHandle()
	tb := gtk.NewBox(gtk.OrientationHorizontal, 6)
	tb.AddCSSClass("top-bar")
	tb.Append(gtk.NewWindowControls(gtk.PackStart))
	b.topNav = gtk.NewBox(gtk.OrientationHorizontal, 0)
	tb.Append(b.topNav)
	chips := gtk.NewScrolledWindow()
	chips.SetHExpand(true)
	chips.SetPolicy(gtk.PolicyExternal, gtk.PolicyNever)
	chips.SetChild(b.strip.chips)
	tb.Append(chips)
	plus := gtk.NewButtonFromIconName("list-add-symbolic")
	plus.SetTooltipText("New Tab (Ctrl+T)")
	plus.AddCSSClass("flat")
	plus.ConnectClicked(func() { b.newTab(false) })
	tb.Append(plus)
	b.topMenu = gtk.NewBox(gtk.OrientationHorizontal, 0)
	tb.Append(b.topMenu)
	tb.Append(gtk.NewWindowControls(gtk.PackEnd))
	b.top.SetChild(tb)
	b.topField = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.topField.AddCSSClass("top-field")

	// --- put together ---
	b.body = gtk.NewBox(gtk.OrientationHorizontal, 0)
	b.body.SetVExpand(true)
	b.root = gtk.NewBox(gtk.OrientationVertical, 0)
	b.root.Append(b.body)
	b.SetContent(b.root)
	b.layout()

	b.keys()
	b.app.extensions.b = b
	b.app.extensions.listeners = append(b.app.extensions.listeners, func() { b.rebuildExtensionButtons() })
	b.rebuildExtensionButtons()
	b.setupExtensionDrop()
	s.Listen(b.settingChanged)
	app.curtain.Listen(b.veilsChanged)
	app.downloads.onAdded = append(app.downloads.onAdded, b.downloadAdded)
	app.downloads.onChanged = append(app.downloads.onChanged, b.downloadsBadge)
	b.ConnectCloseRequest(func() bool { b.closing(); return false })
	b.NotifyProperty("fullscreened", b.layout)
	b.applyLook()
	adw.StyleManagerGetDefault().NotifyProperty("dark", b.applyLook)
	// Tabs you haven't looked at in a while give their memory back.
	coreglib.TimeoutSecondsAdd(60, func() bool { b.sleepIdle(); return true })
	return b
}

func detach(w gtk.Widgetter) {
	base := gtk.BaseWidget(w)
	if p := base.Parent(); p != nil {
		if box, ok := p.(*gtk.Box); ok {
			box.Remove(w)
		} else {
			base.Unparent()
		}
	}
}

func hasParent(w gtk.Widgetter) bool { return gtk.BaseWidget(w).Parent() != nil }

func (b *Browser) layout() {
	clearBox(b.body)
	if hasParent(b.top) {
		b.root.Remove(b.top)
	}
	if hasParent(b.topField) {
		b.root.Remove(b.topField)
	}
	detach(b.nav)
	detach(b.omnibox)
	detach(b.menu)
	s := b.app.settings
	folded := s.SidebarFolded && !b.peeking
	if s.TabsSide == "left" {
		b.headBox.InsertChildAfter(b.nav, b.headBox.FirstChild())
		b.sideField.Append(b.omnibox)
		b.sideFoot.Prepend(b.menu)
		if !b.fullPage {
			if folded {
				b.body.Append(b.edge)
			} else {
				b.body.Append(b.side)
				b.body.Append(b.grip)
			}
		}
		b.body.Append(b.card)
		b.card.RemoveCSSClass("top")
		setClass(b.card, "bare-left", folded || b.fullPage)
	} else {
		b.topMenu.Append(b.menu)
		b.topNav.Append(b.nav)
		b.topField.Append(b.omnibox)
		if !b.fullPage {
			b.root.Prepend(b.topField)
			b.root.Prepend(b.top)
		}
		b.body.Append(b.card)
		b.card.AddCSSClass("top")
		b.card.RemoveCSSClass("bare-left")
	}
	setClass(b.card, "full", b.fullPage)
	b.strip.rebuild()
}

func (b *Browser) peek(on bool) {
	s := b.app.settings
	if !s.SidebarFolded || s.TabsSide != "left" || on == b.peeking {
		return
	}
	// Not while the field is in use.
	if !on && b.omnibox.editing {
		return
	}
	b.peeking = on
	b.layout()
}

func (b *Browser) applyLook() {
	sm := adw.StyleManagerGetDefault()
	switch b.app.settings.Appearance {
	case "light":
		sm.SetColorScheme(adw.ColorSchemeForceLight)
	case "dark":
		sm.SetColorScheme(adw.ColorSchemeForceDark)
	default:
		sm.SetColorScheme(adw.ColorSchemeDefault)
	}
	setClass(b, "dark", sm.Dark())
}

func (b *Browser) settingChanged(key string) {
	switch key {
	case "tabs_side", "sidebar_folded":
		b.peeking = false
		b.layout()
	case "appearance":
		b.applyLook()
	case "shield":
		b.retuneShield()
	}
}

// --- the parts ---

func flatButton(icon, tip string, fn func()) *gtk.Button {
	btn := gtk.NewButtonFromIconName(icon)
	btn.SetTooltipText(tip)
	btn.AddCSSClass("flat")
	btn.ConnectClicked(fn)
	return btn
}

func (b *Browser) navButtons() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("nav")
	b.back = flatButton("go-previous-symbolic", "Back (Alt+←)", b.goBack)
	b.fwd = flatButton("go-next-symbolic", "Forward (Alt+→)", b.goForward)
	b.reloadBtn = flatButton("view-refresh-symbolic", "Reload (Ctrl+R)", b.reloadOrStop)
	for _, x := range []*gtk.Button{b.back, b.fwd, b.reloadBtn} {
		x.AddCSSClass("nav-button")
		box.Append(x)
	}
	rc := gtk.NewGestureClick()
	rc.SetButton(3)
	rc.ConnectPressed(func(n int, x, y float64) { b.reload(b.current, true) })
	b.reloadBtn.AddController(rc)
	return box
}

func (b *Browser) menuButton() *gtk.MenuButton {
	menu := gio.NewMenu()
	section := func(items ...string) {
		s := gio.NewMenu()
		for i := 0; i < len(items); i += 2 {
			s.Append(items[i], items[i+1])
		}
		menu.AppendSection("", s)
	}
	section("New Tab", "win.new-tab", "New Private Tab", "win.private-tab", "Reopen Closed Tab", "win.reopen")
	section("History", "win.history", "Bookmarks", "win.bookmarks", "Downloads", "win.downloads")
	section("Reading Mode", "win.reader", "Hide Something…", "win.hide", "What Is Hidden Here", "win.hidden",
		"Find on Page…", "win.find", "Print…", "win.print", "Web Inspector", "win.inspector")
	section("Extensions", "win.extensions",
		"Tabs Across the Top / Down the Left", "win.tabs-side", "Fold Sidebar Away", "win.fold",
		"Keyboard Shortcuts", "win.shortcuts", "Settings", "win.settings")
	mb := gtk.NewMenuButton()
	mb.SetIconName("open-menu-symbolic")
	mb.SetMenuModel(menu)
	mb.SetTooltipText("Menu")
	mb.AddCSSClass("flat")
	mb.SetDirection(gtk.ArrowUp)
	return mb
}

func (b *Browser) blankPage() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.SetVAlign(gtk.AlignCenter)
	box.SetHAlign(gtk.AlignCenter)
	box.AddCSSClass("blank")
	t := gtk.NewLabel("Fasty")
	t.AddCSSClass("blank-title")
	box.Append(t)
	hint := gtk.NewLabel("Type an address or a few words. Ctrl+L to the field, Ctrl+K for your tabs.")
	hint.AddCSSClass("dim-label")
	box.Append(hint)
	return box
}

func (b *Browser) findBar() *gtk.Revealer {
	rev := gtk.NewRevealer()
	rev.SetTransitionType(gtk.RevealerTransitionTypeSlideDown)
	rev.SetHAlign(gtk.AlignEnd)
	rev.SetVAlign(gtk.AlignStart)
	box := gtk.NewBox(gtk.OrientationHorizontal, 4)
	box.AddCSSClass("find-bar")
	b.findEntry = gtk.NewSearchEntry()
	b.findEntry.SetPlaceholderText("Find on page")
	b.findEntry.SetObjectProperty("width-chars", 22)
	b.findCount = gtk.NewLabel("")
	b.findCount.AddCSSClass("dim-label")
	b.findCount.AddCSSClass("caption")
	box.Append(b.findEntry)
	box.Append(b.findCount)
	box.Append(flatButton("go-up-symbolic", "Previous (Shift+Enter)", func() { b.findStep(false) }))
	box.Append(flatButton("go-down-symbolic", "Next (Enter)", func() { b.findStep(true) }))
	box.Append(flatButton("window-close-symbolic", "Done (Esc)", func() { b.closeFind(false) }))
	rev.SetChild(box)
	b.findEntry.ConnectSearchChanged(func() { b.find(b.findEntry.Text()) })
	b.findEntry.ConnectActivate(func() { b.findStep(true) })
	b.findEntry.ConnectStopSearch(func() { b.closeFind(false) })
	k := gtk.NewEventControllerKey()
	k.ConnectKeyPressed(func(kv, _ uint, st gdk.ModifierType) bool {
		if (kv == gdk.KEY_Return || kv == gdk.KEY_KP_Enter) && st&gdk.ShiftMask != 0 {
			b.findStep(false)
			return true
		}
		return false
	})
	b.findEntry.AddController(k)
	return rev
}

func (b *Browser) hideBanner() *gtk.Revealer {
	rev := gtk.NewRevealer()
	rev.SetTransitionType(gtk.RevealerTransitionTypeSlideUp)
	rev.SetHAlign(gtk.AlignCenter)
	rev.SetVAlign(gtk.AlignEnd)
	box := gtk.NewBox(gtk.OrientationHorizontal, 10)
	box.AddCSSClass("hide-banner")
	b.bannerLabel = gtk.NewLabel("Click what you want gone. Ctrl+Z puts the last one back.")
	box.Append(b.bannerLabel)
	undo := gtk.NewButtonWithLabel("Undo")
	undo.AddCSSClass("flat")
	undo.ConnectClicked(b.undoHide)
	box.Append(undo)
	done := gtk.NewButtonWithLabel("Done")
	done.AddCSSClass("suggested-action")
	done.ConnectClicked(b.stopPicking)
	box.Append(done)
	rev.SetChild(box)
	return rev
}

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

// popup opens a window.open popup in a window of its own. Sign-in popups are
// expected to be separate windows; showing them as tabs of the main window
// makes OAuth flows confusing and made the opener go blank when the popup
// closed. The view is still a related view, so window.opener and postMessage
// between the two pages keep working.
func (b *Browser) popup(opener *Tab, view *webkit.WebView, uri string) {
	t := adoptTab(b, view, uri, opener.private)
	t.opener = opener

	// An application window belongs to the GTK app, so it gets the app's icon
	// in the taskbar and groups with the other windows. A plain GtkWindow has
	// neither. Set the icon explicitly too, for desktops that read it from
	// the window.
	win := gtk.NewApplicationWindow(&b.app.Application.Application)
	win.SetTitle("Fasty")
	win.SetIconName("fasty-browser")
	win.SetDefaultSize(560, 680)
	win.AddCSSClass("popup-window")
	if b.HasCSSClass("dark") {
		win.AddCSSClass("dark")
	}
	win.SetChild(view)
	win.ConnectCloseRequest(func() bool {
		b.closeTab(t)
		// False lets GTK run the default handler, which actually destroys
		// the window. Returning true left an empty, blank window on screen
		// after the page called window.close() at the end of a sign-in.
		return false
	})
	t.popupWin = win
	b.popups = append(b.popups, t)
	log.Printf("fasty: popup opened: %s (total %d)", hostOf(uri), len(b.popups))

	// WebKit says a view made for window.open must not be shown until
	// ready-to-show: showing it on the create signal leaves a white, blank
	// window (seen next to the real sign-in window). Wait for the page, and
	// also for the first committed load, for WebKits that don't emit it for
	// popups. A popup that never loads anything is then never put on screen.
	shown := false
	show := func() {
		if !shown {
			shown = true
			win.Present()
		}
	}
	view.ConnectReadyToShow(show)
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		if ev == webkit.LoadCommitted {
			show()
		}
	})
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

// --- the page ---

func (b *Browser) focusPage() {
	if b.current != nil && b.current.view != nil {
		b.current.view.GrabFocus()
	}
	if b.peeking {
		b.peeking = false
		coreglib.TimeoutAdd(150, func() bool { b.layout(); return false })
	}
}

func (b *Browser) goBack() {
	if t := b.current; t != nil && t.view != nil && t.view.CanGoBack() {
		t.view.GoBack()
	}
}

func (b *Browser) goForward() {
	if t := b.current; t != nil && t.view != nil && t.view.CanGoForward() {
		t.view.GoForward()
	}
}

func (b *Browser) reload(t *Tab, origin bool) {
	if t == nil {
		t = b.current
	}
	if t == nil {
		return
	}
	if t.view == nil {
		if t == b.current {
			b.selectTab(t)
		}
		return
	}
	switch {
	case t.crashed || t.view.URI() == "":
		t.view.LoadURI(t.url)
	case origin:
		t.view.ReloadBypassCache()
	default:
		t.view.Reload()
	}
}

func (b *Browser) reloadOrStop() {
	if t := b.current; t != nil && t.loading && t.view != nil {
		t.view.StopLoading()
	} else {
		b.reload(nil, false)
	}
}

var zoomLevels = []float64{0.3, 0.5, 0.67, 0.8, 0.9, 1.0, 1.1, 1.25, 1.5, 1.75, 2.0, 2.5, 3.0}

func (b *Browser) zoom(step int) {
	t := b.current
	if t == nil || t.view == nil {
		return
	}
	s := b.app.settings
	z := t.view.ZoomLevel()
	switch {
	case step == 0:
		z = s.Zoom
	case step > 0:
		next := zoomLevels[len(zoomLevels)-1]
		for _, l := range zoomLevels {
			if l > z+0.001 {
				next = l
				break
			}
		}
		z = next
	default:
		next := zoomLevels[0]
		for i := len(zoomLevels) - 1; i >= 0; i-- {
			if zoomLevels[i] < z-0.001 {
				next = zoomLevels[i]
				break
			}
		}
		z = next
	}
	t.view.SetZoomLevel(z)
	if host := hostOf(t.url); host != "" {
		s.Set("site_zoom", func(d *SettingsData) {
			if z-d.Zoom < 0.001 && d.Zoom-z < 0.001 {
				delete(d.SiteZoom, host)
			} else {
				d.SiteZoom[host] = z
			}
		})
	}
	b.toast(fmt.Sprintf("Zoom %d%%", int(z*100+0.5)), 1)
}

func (b *Browser) setDefaultZoom(z float64) {
	b.app.settings.Set("zoom", func(d *SettingsData) { d.Zoom = z })
	for _, t := range b.tabs {
		if t.view != nil {
			t.view.SetZoomLevel(b.app.zoomFor(t.url))
		}
	}
}

func (b *Browser) hoverLink(t *Tab, u string) {
	if t != b.current {
		return
	}
	if u == "" {
		b.status.SetVisible(false)
		return
	}
	if strings.HasPrefix(u, "https://") {
		u = editable(u)
	}
	b.status.SetLabel(u)
	b.status.SetVisible(true)
}

func (b *Browser) pageFullscreen(on bool) {
	b.fullPage = on
	if on {
		b.Fullscreen()
	} else {
		b.Unfullscreen()
	}
	b.layout()
}

func (b *Browser) toggleFullscreen() {
	if b.IsFullscreen() {
		b.Unfullscreen()
	} else {
		b.Fullscreen()
	}
}

// --- find ---

func (b *Browser) openFind() {
	t := b.current
	if t == nil || t.view == nil {
		return
	}
	b.findbar.SetRevealChild(true)
	b.findEntry.GrabFocus()
	b.findEntry.SelectRegion(0, -1)
	fc := t.view.FindController()
	if !t.findWired {
		t.findWired = true
		matches := func(n uint) string {
			switch {
			case n == 0:
				return ""
			case n >= 1000:
				return "1000+"
			case n == 1:
				return "1 match"
			}
			return fmt.Sprintf("%d matches", n)
		}
		fc.ConnectCountedMatches(func(n uint) { b.findCount.SetLabel(matches(n)) })
		fc.ConnectFailedToFindText(func() { b.findCount.SetLabel("Not found") })
		fc.ConnectFoundText(func(n uint) { b.findCount.SetLabel(matches(n)) })
	}
	if txt := b.findEntry.Text(); txt != "" {
		b.find(txt)
	}
}

func (b *Browser) find(text string) {
	t := b.current
	if t == nil || t.view == nil {
		return
	}
	fc := t.view.FindController()
	if text == "" {
		fc.SearchFinish()
		b.findCount.SetLabel("")
		return
	}
	opts := uint32(webkit.FindOptionsWrapAround)
	if text == strings.ToLower(text) {
		opts |= uint32(webkit.FindOptionsCaseInsensitive)
	}
	fc.CountMatches(text, opts, 1000)
	fc.Search(text, opts, 1000)
}

func (b *Browser) findStep(forward bool) {
	t := b.current
	if t == nil || t.view == nil {
		return
	}
	fc := t.view.FindController()
	switch {
	case fc.SearchText() != b.findEntry.Text():
		b.find(b.findEntry.Text())
	case forward:
		fc.SearchNext()
	default:
		fc.SearchPrevious()
	}
}

func (b *Browser) closeFind(quiet bool) bool {
	if !b.findbar.RevealChild() {
		return false
	}
	b.findbar.SetRevealChild(false)
	for _, t := range b.tabs {
		if t.view != nil {
			t.view.FindController().SearchFinish()
		}
	}
	if !quiet {
		b.focusPage()
	}
	return true
}

// --- reading mode ---

func (b *Browser) toggleReader() {
	t := b.current
	if t == nil || t.view == nil {
		return
	}
	if t.reading {
		t.reading = false
		t.view.Reload()
		return
	}
	look := "light"
	if adw.StyleManagerGetDefault().Dark() {
		look = "dark"
	}
	args := glib.NewVariantDict(nil)
	args.InsertValue("sheet", glib.NewVariantString(asset("js/reader.css")))
	args.InsertValue("readerLook", glib.NewVariantString(look))
	view := t.view
	view.CallAsyncJavascriptFunction(context.Background(), asset("js/reader.js"), args.End(), "", "",
		func(res gio.AsyncResulter) {
			r, err := view.CallAsyncJavascriptFunctionFinish(res)
			if err == nil && r != nil && r.IsString() && r.String() == "read" {
				t.reading = true
			} else {
				b.toast("Nothing here reads like an article", 2)
			}
		})
}

// --- hiding things (Curtain) ---

func (b *Browser) startPicking() {
	t := b.current
	if t == nil || t.view == nil || !(strings.HasPrefix(t.url, "http://") || strings.HasPrefix(t.url, "https://")) {
		b.toast("Open a web page first", 2)
		return
	}
	if t.picking {
		b.stopPicking()
		return
	}
	t.picking = true
	t.run("window.__officeVeil && window.__officeVeil.on()", nil)
	b.banner.SetRevealChild(true)
	t.view.GrabFocus()
}

func (b *Browser) stopPicking() {
	b.banner.SetRevealChild(false)
	if t := b.current; t != nil && t.picking {
		t.picking = false
		t.run("window.__officeVeil && window.__officeVeil.off()", nil)
	}
}

func (b *Browser) pickingEnded(t *Tab) {
	if t == b.current {
		b.banner.SetRevealChild(false)
	}
}

func (b *Browser) picked(t *Tab, selector, lbl, note string) {
	host := hostOf(t.url)
	if host == "" {
		return
	}
	b.app.curtain.Hide(host, selector, lbl, note)
	b.bannerLabel.SetLabel("Hid “" + lbl + "”. Click another, or Ctrl+Z to put it back.")
}

func (b *Browser) undoHide() {
	if t := b.current; t != nil {
		if v := b.app.curtain.Undo(hostOf(t.url)); v != nil {
			b.bannerLabel.SetLabel("Put back “" + v.Label + "”.")
		}
	}
}

func (b *Browser) veilsChanged() {
	for _, t := range b.tabs {
		if t.ucm != nil {
			veilSheets(t.ucm, b.app.curtain)
		}
	}
}

func (b *Browser) showHidden() {
	if t := b.current; t != nil {
		if host := hostOf(t.url); host != "" {
			showHiddenPanel(b, host)
		}
	}
}

func (b *Browser) retuneShield() {
	for _, t := range b.tabs {
		if t.ucm != nil {
			b.app.shield.Tune(t.ucm, hostOf(t.url), true)
		}
	}
}

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

func (b *Browser) makeDefault() {
	err := exec.Command("xdg-settings", "set", "default-web-browser", appID+".desktop").Run()
	if err == nil {
		b.toast("Fasty is now your default browser", 2)
	} else {
		b.toast("Couldn't set the default browser", 2)
	}
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

func (b *Browser) printPage() {
	if t := b.current; t != nil && t.view != nil {
		webkit.NewPrintOperation(t.view).RunDialog(&b.Window)
	}
}

func (b *Browser) inspector() {
	if t := b.current; t != nil && t.view != nil {
		t.view.Inspector().Show()
	}
}

func (b *Browser) toast(text string, timeout uint) {
	t := adw.NewToast(text)
	t.SetTimeout(timeout)
	b.toasts.AddToast(t)
}

// --- session ---

type sessionData struct {
	Tabs     []tabSnap `json:"tabs"`
	Selected int       `json:"selected"`
}

func (b *Browser) saveSession() {
	if b.saving != 0 {
		return
	}
	b.saving = coreglib.TimeoutAdd(600, func() bool {
		b.saving = 0
		b.writeSession()
		return false
	})
}

func (b *Browser) writeSession() {
	d := sessionData{Tabs: []tabSnap{}, Selected: -1}
	for _, t := range b.tabs {
		if t.private || t.url == "" {
			continue
		}
		if t == b.current {
			d.Selected = len(d.Tabs)
		}
		d.Tabs = append(d.Tabs, t.snapshot())
	}
	if d.Selected < 0 {
		d.Selected = len(d.Tabs) - 1
	}
	writeJSON("session.json", d)
}

func (b *Browser) restore(urls []string) {
	var d sessionData
	d.Selected = -1
	readJSON("session.json", &d)
	var restored []*Tab
	var snaps []tabSnap
	for _, s := range d.Tabs {
		if s.URL == "" {
			continue
		}
		snaps = append(snaps, s)
		if !b.app.settings.RestoreSession && !s.Pinned {
			continue
		}
		t := newTab(b, s.URL, s.Title, s.Pinned, false)
		b.tabs = append(b.tabs, t)
		restored = append(restored, t)
	}
	sort.SliceStable(b.tabs, func(i, j int) bool { return b.tabs[i].pinned && !b.tabs[j].pinned })
	b.strip.rebuild()
	for _, u := range urls {
		b.openTab(u, true, nil, false, "", false)
	}
	if len(urls) > 0 {
		return
	}
	switch {
	case len(restored) > 0 && d.Selected >= 0 && d.Selected < len(snaps):
		pick := restored[len(restored)-1]
		for _, t := range restored {
			if t.url == snaps[d.Selected].URL {
				pick = t
				break
			}
		}
		b.selectTab(pick)
	case len(restored) > 0:
		b.selectTab(restored[len(restored)-1])
	default:
		b.syncChrome()
		b.omnibox.focus(false, false)
	}
}

func (b *Browser) closing() {
	for _, t := range b.tabs {
		b.dropPiP(t)
	}
	if b.saving != 0 {
		coreglib.SourceRemove(b.saving)
		b.saving = 0
	}
	b.writeSession()
	w, h := b.Width(), b.Height()
	max := b.IsMaximized()
	s := b.app.settings
	if w > 0 && h > 0 {
		s.Window = WindowSize{W: w, H: h, Max: max}
	}
	s.changed()
	b.app.flushAll()
}

// --- keys ---

func (b *Browser) keys() {
	s := b.app.settings
	tabsSide := func() {
		s.Set("tabs_side", func(d *SettingsData) {
			if d.TabsSide == "left" {
				d.TabsSide = "top"
			} else {
				d.TabsSide = "left"
			}
		})
	}
	fold := func() { s.Set("sidebar_folded", func(d *SettingsData) { d.SidebarFolded = !d.SidebarFolded }) }
	acts := map[string]func(){
		"new-tab":     func() { b.newTab(false) },
		"private-tab": func() { b.newTab(true) },
		"reopen":      b.reopenClosed,
		"history":     func() { showHistoryPanel(b) },
		"bookmarks":   func() { showBookmarksPanel(b) },
		"downloads":   func() { showDownloadsPanel(b) },
		"reader":      b.toggleReader,
		"hide":        b.startPicking,
		"hidden":      b.showHidden,
		"find":        b.openFind,
		"print":       b.printPage,
		"inspector":   b.inspector,
		"tabs-side":   tabsSide,
		"fold":        fold,
		"settings":    func() { showSettings(b) },
		"shortcuts":   b.showShortcuts,
		"extensions":  func() { showExtensionsPanel(b) },
	}
	for name, fn := range acts {
		fn := fn
		a := gio.NewSimpleAction(name, nil)
		a.ConnectActivate(func(*glib.Variant) { fn() })
		b.AddAction(a)
	}
	C, S, A := gdk.ControlMask, gdk.ShiftMask, gdk.AltMask
	focus := func() { b.omnibox.focus(false, false) }
	copyClean := func() {
		if b.current != nil && b.current.url != "" {
			b.copyText(cleanLink(b.current.url))
		}
	}
	b.bindings = map[binding]func(){
		{C, gdk.KEY_l}: focus, {A, gdk.KEY_d}: focus, {0, gdk.KEY_F6}: focus,
		{C, gdk.KEY_k}:            func() { showTabSwitcher(b) },
		{C, gdk.KEY_t}:            func() { b.newTab(false) },
		{C | S, gdk.KEY_n}:        func() { b.newTab(true) },
		{C, gdk.KEY_n}:            func() { b.newTab(false) },
		{C, gdk.KEY_w}:            func() { b.closeTab(nil) },
		{C, gdk.KEY_F4}:           func() { b.closeTab(nil) },
		{C | S, gdk.KEY_t}:        b.reopenClosed,
		{C, gdk.KEY_bracketleft}:  b.goBack,
		{C, gdk.KEY_bracketright}: b.goForward,
		{A, gdk.KEY_Left}:         b.goBack,
		{A, gdk.KEY_Right}:        b.goForward,
		{0, gdk.KEY_Back}:         b.goBack,
		{0, gdk.KEY_Forward}:      b.goForward,
		{C | S, gdk.KEY_bracketleft}:  func() { b.stepTab(-1) },
		{C | S, gdk.KEY_bracketright}: func() { b.stepTab(1) },
		{C, gdk.KEY_Tab}:              func() { b.stepTab(1) },
		{C | S, gdk.KEY_Tab}:          func() { b.stepTab(-1) },
		{C | S, gdk.KEY_ISO_Left_Tab}: func() { b.stepTab(-1) },
		{C, gdk.KEY_Page_Down}:        func() { b.stepTab(1) },
		{C, gdk.KEY_Page_Up}:          func() { b.stepTab(-1) },
		{C | S, gdk.KEY_s}:            tabsSide,
		{C, gdk.KEY_s}:                fold,
		{C | S, gdk.KEY_b}:            b.toggleBookmark,
		{C, gdk.KEY_d}:                func() { b.duplicate(nil) },
		{C | A, gdk.KEY_b}:            func() { showBookmarksPanel(b) },
		{C | S, gdk.KEY_o}:            func() { showBookmarksPanel(b) },
		{C | S, gdk.KEY_r}:            b.toggleReader,
		{C | S, gdk.KEY_h}:            b.startPicking,
		{C | S, gdk.KEY_u}:            b.showHidden,
		{C, gdk.KEY_f}:                b.openFind,
		{C, gdk.KEY_g}:                func() { b.findStep(true) },
		{C | S, gdk.KEY_g}:            func() { b.findStep(false) },
		{C | S, gdk.KEY_c}:            copyClean,
		{C | S, gdk.KEY_v}:            b.pasteAndGo,
		{C, gdk.KEY_y}:                func() { showHistoryPanel(b) },
		{C, gdk.KEY_h}:                func() { showHistoryPanel(b) },
		{C | S, gdk.KEY_j}:            func() { showDownloadsPanel(b) },
		{C, gdk.KEY_comma}:            func() { showSettings(b) },
		{C, gdk.KEY_r}:                func() { b.reload(nil, false) },
		{0, gdk.KEY_F5}:               func() { b.reload(nil, false) },
		{C | A, gdk.KEY_r}:            func() { b.reload(nil, true) },
		{C, gdk.KEY_F5}:               func() { b.reload(nil, true) },
		{C, gdk.KEY_plus}:             func() { b.zoom(1) },
		{C, gdk.KEY_equal}:            func() { b.zoom(1) },
		{C, gdk.KEY_KP_Add}:           func() { b.zoom(1) },
		{C, gdk.KEY_minus}:            func() { b.zoom(-1) },
		{C, gdk.KEY_KP_Subtract}:      func() { b.zoom(-1) },
		{C, gdk.KEY_0}:                func() { b.zoom(0) },
		{C, gdk.KEY_KP_0}:             func() { b.zoom(0) },
		{0, gdk.KEY_F11}:              b.toggleFullscreen,
		{0, gdk.KEY_F12}:              b.inspector,
		{C | S, gdk.KEY_i}:            b.inspector,
		{C | A, gdk.KEY_i}:            b.inspector,
		{C, gdk.KEY_p}:                b.printPage,
		{C | S, gdk.KEY_p}:            b.togglePiP,
		{C | S, gdk.KEY_e}:            func() { showExtensionsPanel(b) },
		{C | A, gdk.KEY_l}:            func() { showSettings(b) },
		{C, gdk.KEY_question}:         b.showShortcuts,
		{C, gdk.KEY_q}:                b.app.quitAll,
	}
	for n := 1; n <= 9; n++ {
		n := n
		b.bindings[binding{C, gdk.KEY_0 + uint(n)}] = func() { b.jump(n) }
		b.bindings[binding{A, gdk.KEY_0 + uint(n)}] = func() { b.jump(n) }
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(kv, code uint, state gdk.ModifierType) bool { return b.key(kv, state) })
	b.AddController(keys)
}

var shifted = map[uint]uint{
	gdk.KEY_braceleft: gdk.KEY_bracketleft, gdk.KEY_braceright: gdk.KEY_bracketright,
	gdk.KEY_exclam: gdk.KEY_1, gdk.KEY_at: gdk.KEY_2, gdk.KEY_numbersign: gdk.KEY_3,
	gdk.KEY_dollar: gdk.KEY_4, gdk.KEY_percent: gdk.KEY_5, gdk.KEY_asciicircum: gdk.KEY_6,
	gdk.KEY_ampersand: gdk.KEY_7, gdk.KEY_asterisk: gdk.KEY_8, gdk.KEY_parenleft: gdk.KEY_9,
}

func (b *Browser) key(keyval uint, state gdk.ModifierType) bool {
	mods := state & (gdk.ControlMask | gdk.ShiftMask | gdk.AltMask)
	kv := gdk.KeyvalToLower(keyval)
	if k, ok := shifted[kv]; ok {
		kv = k
	}
	if kv == gdk.KEY_Escape && mods == 0 {
		return b.escape()
	}
	if t := b.current; t != nil && t.picking && mods == gdk.ControlMask && kv == gdk.KEY_z {
		b.undoHide()
		return true
	}
	fn := b.bindings[binding{mods, kv}]
	if fn == nil && mods&gdk.ShiftMask != 0 && (kv == gdk.KEY_plus || kv == gdk.KEY_question) {
		fn = b.bindings[binding{mods &^ gdk.ShiftMask, kv}]
	}
	if fn == nil {
		if x := b.app.extensions; x != nil {
			return x.runCommand(kv, mods)
		}
		return false
	}
	// Leave text fields their own paste.
	if mods == gdk.ControlMask|gdk.ShiftMask && kv == gdk.KEY_v {
		if f := b.Focus(); f != nil {
			if _, ok := f.(*gtk.Text); ok {
				return false
			}
		}
	}
	fn()
	return true
}

func (b *Browser) escape() bool {
	if t := b.current; t != nil && t.picking {
		b.stopPicking()
		return true
	}
	if b.closeFind(false) {
		return true
	}
	if b.omnibox.editing {
		b.omnibox.cancel()
		return true
	}
	if b.status.IsVisible() {
		b.status.SetVisible(false)
	}
	return false
}

func (b *Browser) showShortcuts() {
	rows := [][2]string{
		{"Ctrl+L", "Address"}, {"Ctrl+K", "Switch tab"}, {"Ctrl+T", "New tab"},
		{"Ctrl+Shift+N", "New private tab"}, {"Ctrl+W", "Close tab"}, {"Ctrl+Shift+T", "Reopen closed tab"},
		{"Ctrl+[  Ctrl+]  Alt+←  Alt+→", "Back, forward"}, {"Ctrl+Shift+[  Ctrl+Shift+]  Ctrl+Tab", "Previous, next tab"},
		{"Ctrl+1 – Ctrl+9", "Jump to tab"}, {"Ctrl+Shift+S", "Tabs on the left or across the top"},
		{"Ctrl+S", "Fold the sidebar away"}, {"Ctrl+Shift+B", "Bookmark this page"},
		{"Ctrl+Alt+B", "Bookmarks"}, {"Ctrl+Shift+R", "Reading mode"}, {"Ctrl+Shift+H", "Hide something"},
		{"Ctrl+Shift+U", "What is hidden here"}, {"Ctrl+F", "Find"}, {"Ctrl+D", "Duplicate tab"},
		{"Ctrl+Shift+C", "Copy address (clean)"}, {"Ctrl+Shift+V", "Paste and go"}, {"Ctrl+Y", "History"},
		{"Ctrl+Shift+J", "Downloads"}, {"Ctrl+Shift+E", "Extensions"}, {"Ctrl+,", "Settings"}, {"Ctrl+R  F5", "Reload"},
		{"Ctrl+Alt+R  Ctrl+F5", "Reload from origin"}, {"Ctrl+ +  −  0", "Zoom"}, {"F11", "Full screen"},
		{"F12", "Web Inspector"}, {"Esc", "Put away whatever is open"},
	}
	d := adw.NewDialog()
	d.SetTitle("Keyboard")
	d.SetContentWidth(520)
	d.SetContentHeight(640)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(24)
	grid.SetRowSpacing(8)
	grid.SetMarginStart(24)
	grid.SetMarginEnd(24)
	grid.SetMarginBottom(24)
	for i, r := range rows {
		k := gtk.NewLabel(r[0])
		k.SetXAlign(1)
		k.AddCSSClass("monospace")
		k.AddCSSClass("dim-label")
		grid.Attach(k, 0, i, 1, 1)
		w := gtk.NewLabel(r[1])
		w.SetXAlign(0)
		grid.Attach(w, 1, i, 1, 1)
	}
	sw.SetChild(grid)
	tv.SetContent(sw)
	d.SetChild(tv)
	d.Present(b)
}
