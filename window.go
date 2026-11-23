package main

import (
	"os/exec"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
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
	popups      []*Tab         // popups live in their own windows, not in tabs
	passkeyAsk  *passkeyPrompt // the passkey question on screen, if any
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

func (b *Browser) makeDefault() {
	err := exec.Command("xdg-settings", "set", "default-web-browser", appID+".desktop").Run()
	if err == nil {
		b.toast("Fasty is now your default browser", 2)
	} else {
		b.toast("Couldn't set the default browser", 2)
	}
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
