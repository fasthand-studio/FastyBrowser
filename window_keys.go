// Keyboard shortcuts.
package main

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

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
		{C, gdk.KEY_k}:                func() { showTabSwitcher(b) },
		{C, gdk.KEY_t}:                func() { b.newTab(false) },
		{C | S, gdk.KEY_n}:            func() { b.newTab(true) },
		{C, gdk.KEY_n}:                func() { b.newTab(false) },
		{C, gdk.KEY_w}:                func() { b.closeTab(nil) },
		{C, gdk.KEY_F4}:               func() { b.closeTab(nil) },
		{C | S, gdk.KEY_t}:            b.reopenClosed,
		{C, gdk.KEY_bracketleft}:      b.goBack,
		{C, gdk.KEY_bracketright}:     b.goForward,
		{A, gdk.KEY_Left}:             b.goBack,
		{A, gdk.KEY_Right}:            b.goForward,
		{0, gdk.KEY_Back}:             b.goBack,
		{0, gdk.KEY_Forward}:          b.goForward,
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
		{C | S, gdk.KEY_a}:            func() { showAuthenticator(b) },
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
		{"Ctrl+Shift+J", "Downloads"}, {"Ctrl+Shift+E", "Extensions"}, {"Ctrl+Shift+A", "Authenticator"}, {"Ctrl+,", "Settings"}, {"Ctrl+R  F5", "Reload"},
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
