// Hiding parts of a page: the element picker and the hidden-element list.
package main

import (
	"strings"
)

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
