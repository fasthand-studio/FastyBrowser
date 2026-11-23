// Find in page.
package main

import (
	"fmt"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

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
