package main

// The row of tabs: down the left (rows and pinned tiles) or across the top
// (chips). Drawn from the browser's list; rebuilt when the list changes shape,
// updated in place when a tab only changes what it says.

import (
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

func faviconWidget(t *Tab, size int) gtk.Widgetter {
	if t.loading && t.favicon == nil {
		sp := gtk.NewSpinner()
		sp.SetSpinning(true)
		sp.SetSizeRequest(size, size)
		return sp
	}
	if t.favicon != nil {
		img := gtk.NewImageFromPaintable(t.favicon)
		img.SetPixelSize(size)
		return img
	}
	letter := pretty(t.url)
	if letter == "" {
		letter = t.title
	}
	if letter == "" {
		letter = "·"
	}
	l := gtk.NewLabel(strings.ToUpper(string([]rune(letter)[:1])))
	l.AddCSSClass("letter")
	l.SetSizeRequest(size, size)
	return l
}

func setClass(w interface {
	AddCSSClass(string)
	RemoveCSSClass(string)
}, class string, on bool) {
	if on {
		w.AddCSSClass(class)
	} else {
		w.RemoveCSSClass(class)
	}
}

func dropString(v *coreglib.Value) string {
	if v == nil {
		return ""
	}
	s, _ := v.GoValue().(string)
	return s
}

type tabWidget struct {
	box     *gtk.Box
	refresh func()
}

type TabStrip struct {
	b       *Browser
	widgets map[string]*tabWidget
	pins    *gtk.FlowBox
	rows    *gtk.Box
	chips   *gtk.Box
}

func newTabStrip(b *Browser) *TabStrip {
	s := &TabStrip{b: b, widgets: map[string]*tabWidget{}}
	s.pins = gtk.NewFlowBox()
	s.pins.SetSelectionMode(gtk.SelectionNone)
	s.pins.SetHomogeneous(true)
	s.pins.SetMaxChildrenPerLine(4)
	s.pins.SetMinChildrenPerLine(3)
	s.pins.SetColumnSpacing(6)
	s.pins.SetRowSpacing(6)
	s.pins.AddCSSClass("pins")
	s.pins.SetVAlign(gtk.AlignStart)
	s.pins.SetVExpand(false)
	s.rows = gtk.NewBox(gtk.OrientationVertical, 2)
	s.rows.AddCSSClass("rows")
	s.chips = gtk.NewBox(gtk.OrientationHorizontal, 4)
	s.chips.AddCSSClass("chips")
	// Dropping on the empty end of the list moves a tab to the end.
	for _, box := range []*gtk.Box{s.rows, s.chips} {
		drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove|gdk.ActionCopy)
		drop.ConnectDrop(func(v *coreglib.Value, x, y float64) bool { return s.dropOn(dropString(v), nil, false) })
		box.AddController(drop)
	}
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove)
	drop.ConnectDrop(func(v *coreglib.Value, x, y float64) bool {
		if id, ok := strings.CutPrefix(dropString(v), "fasty-tab:"); ok {
			b.moveTab(id, nil, true)
			return true
		}
		return false
	})
	s.pins.AddController(drop)
	return s
}

func (s *TabStrip) dropOn(value string, before *Tab, pinned bool) bool {
	if id, ok := strings.CutPrefix(value, "fasty-tab:"); ok {
		s.b.moveTab(id, before, pinned)
		return true
	}
	if u := urlFrom(strings.TrimSpace(value)); u != "" {
		s.b.openTab(u, true, nil, false, "", false)
		return true
	}
	return false
}

// base gives a tab's widget its clicks, hover, and drag and drop.
func (s *TabStrip) base(t *Tab, box *gtk.Box) {
	box.AddCSSClass("tab")
	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.ConnectPressed(func(n int, x, y float64) {
		switch click.CurrentButton() {
		case 1:
			s.b.selectTab(t)
		case 3:
			click.SetState(gtk.EventSequenceClaimed)
			s.menuFor(box, t, x, y)
		}
	})
	click.ConnectReleased(func(n int, x, y float64) {
		if click.CurrentButton() == 2 {
			s.b.closeTab(t)
		}
	})
	box.AddController(click)
	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(func(x, y float64) { box.AddCSSClass("hover") })
	motion.ConnectLeave(func() { box.RemoveCSSClass("hover") })
	box.AddController(motion)
	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionMove)
	drag.ConnectPrepare(func(x, y float64) *gdk.ContentProvider {
		return gdk.NewContentProviderForValue(coreglib.NewValue("fasty-tab:" + t.id))
	})
	drag.ConnectDragBegin(func(d gdk.Dragger) {
		drag.SetIcon(gtk.NewWidgetPaintable(box), 10, 10)
	})
	box.AddController(drag)
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove|gdk.ActionCopy)
	drop.ConnectDrop(func(v *coreglib.Value, x, y float64) bool {
		box.RemoveCSSClass("drop")
		return s.dropOn(dropString(v), t, t.pinned)
	})
	drop.ConnectEnter(func(x, y float64) gdk.DragAction {
		box.AddCSSClass("drop")
		return gdk.ActionMove
	})
	drop.ConnectLeave(func() { box.RemoveCSSClass("drop") })
	box.AddController(drop)
}

func iconSlot(slot *gtk.Box, t *Tab, size int) {
	if c := slot.FirstChild(); c != nil {
		slot.Remove(c)
	}
	slot.Append(faviconWidget(t, size))
}

func smallButton(icon, tip string, fn func()) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.SetVAlign(gtk.AlignCenter)
	b.AddCSSClass("flat")
	b.AddCSSClass("tab-button")
	if tip != "" {
		b.SetTooltipText(tip)
	}
	b.ConnectClicked(fn)
	return b
}

// tabRow is a tab in the column: its icon, its name, and a close button while
// the pointer is on it.
func (s *TabStrip) tabRow(t *Tab) *tabWidget {
	box := gtk.NewBox(gtk.OrientationHorizontal, 8)
	s.base(t, box)
	box.AddCSSClass("tab-row")
	slot := gtk.NewBox(gtk.OrientationHorizontal, 0)
	slot.SetSizeRequest(16, 16)
	slot.SetVAlign(gtk.AlignCenter)
	box.Append(slot)
	label := gtk.NewLabel("")
	label.SetXAlign(0)
	label.SetHExpand(true)
	label.SetEllipsize(pango.EllipsizeEnd)
	box.Append(label)
	audio := smallButton("audio-volume-high-symbolic", "", func() { s.b.toggleMute(t) })
	box.Append(audio)
	closeB := smallButton("window-close-symbolic", "Close Tab", func() { s.b.closeTab(t) })
	closeB.AddCSSClass("tab-close")
	box.Append(closeB)
	w := &tabWidget{box: box}
	w.refresh = func() {
		iconSlot(slot, t, 16)
		label.SetLabel(t.displayTitle())
		tip := t.displayTitle()
		if t.url != "" {
			tip += "\n" + t.url
		}
		box.SetTooltipText(tip)
		audio.SetVisible(t.audio || t.muted)
		if t.muted {
			audio.SetIconName("audio-volume-muted-symbolic")
		} else {
			audio.SetIconName("audio-volume-high-symbolic")
		}
		setClass(box, "private", t.private)
		setClass(box, "asleep", !t.built())
		setClass(box, "live", s.b.current == t)
	}
	w.refresh()
	return w
}

// pinTile is a pinned page: a tile with its icon, a letter if it has none.
func (s *TabStrip) pinTile(t *Tab) *tabWidget {
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	s.base(t, box)
	box.AddCSSClass("pin-tile")
	box.SetSizeRequest(-1, 40)
	box.SetHExpand(true)
	box.SetVAlign(gtk.AlignStart)
	w := &tabWidget{box: box}
	w.refresh = func() {
		if c := box.FirstChild(); c != nil {
			box.Remove(c)
		}
		ic := faviconWidget(t, 18)
		iw := gtk.BaseWidget(ic)
		iw.SetHAlign(gtk.AlignCenter)
		iw.SetVAlign(gtk.AlignCenter)
		iw.SetMarginTop(11)
		iw.SetMarginBottom(11)
		box.Append(ic)
		box.SetTooltipText(t.displayTitle())
		setClass(box, "live", s.b.current == t)
	}
	w.refresh()
	return w
}

// tabChip is a tab across the top.
func (s *TabStrip) tabChip(t *Tab) *tabWidget {
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	s.base(t, box)
	box.AddCSSClass("tab-chip")
	slot := gtk.NewBox(gtk.OrientationHorizontal, 0)
	slot.SetVAlign(gtk.AlignCenter)
	box.Append(slot)
	label := gtk.NewLabel("")
	label.SetXAlign(0)
	label.SetHExpand(true)
	label.SetEllipsize(pango.EllipsizeEnd)
	box.Append(label)
	closeB := smallButton("window-close-symbolic", "", func() { s.b.closeTab(t) })
	closeB.AddCSSClass("tab-close")
	box.Append(closeB)
	w := &tabWidget{box: box}
	w.refresh = func() {
		iconSlot(slot, t, 16)
		label.SetVisible(!t.pinned)
		closeB.SetVisible(!t.pinned)
		label.SetLabel(t.displayTitle())
		box.SetTooltipText(t.displayTitle())
		if t.pinned {
			box.SetSizeRequest(36, -1)
		} else {
			box.SetSizeRequest(180, -1)
		}
		box.SetHExpand(false)
		setClass(box, "live", s.b.current == t)
		setClass(box, "private", t.private)
	}
	w.refresh()
	return w
}

func clearBox(box *gtk.Box) {
	for c := box.FirstChild(); c != nil; c = box.FirstChild() {
		box.Remove(c)
	}
}

func (s *TabStrip) rebuild() {
	s.widgets = map[string]*tabWidget{}
	clearBox(s.rows)
	clearBox(s.chips)
	s.pins.RemoveAll()
	if s.b.app.settings.TabsSide == "left" {
		anyPinned := false
		for _, t := range s.b.tabs {
			if t.pinned {
				w := s.pinTile(t)
				s.pins.Append(w.box)
				s.widgets[t.id] = w
				anyPinned = true
			}
		}
		s.pins.SetVisible(anyPinned)
		for _, t := range s.b.tabs {
			if !t.pinned {
				w := s.tabRow(t)
				s.rows.Append(w.box)
				s.widgets[t.id] = w
			}
		}
	} else {
		for _, t := range s.b.tabs {
			w := s.tabChip(t)
			s.chips.Append(w.box)
			s.widgets[t.id] = w
		}
	}
}

func (s *TabStrip) refresh(t *Tab) {
	if t == nil {
		for _, w := range s.widgets {
			w.refresh()
		}
		return
	}
	if w := s.widgets[t.id]; w != nil {
		w.refresh()
	}
}

func (s *TabStrip) menuFor(anchor *gtk.Box, t *Tab, x, y float64) {
	b := s.b
	pop := gtk.NewPopover()
	pop.SetHasArrow(false)
	pop.SetParent(anchor)
	rect := gdk.NewRectangle(int(x), int(y), 1, 1)
	pop.SetPointingTo(&rect)
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.AddCSSClass("menu-box")
	item := func(label string, fn func()) {
		btn := gtk.NewButtonWithLabel(label)
		btn.AddCSSClass("flat")
		btn.AddCSSClass("menu-item")
		if l, ok := btn.Child().(*gtk.Label); ok {
			l.SetXAlign(0)
		}
		btn.ConnectClicked(func() {
			pop.Popdown()
			coreglib.IdleAdd(fn)
		})
		box.Append(btn)
	}
	if !t.private {
		if t.pinned {
			item("Unpin Tab", func() { b.setPinned(t, false) })
		} else {
			item("Pin Tab", func() { b.setPinned(t, true) })
		}
	}
	item("Duplicate Tab", func() { b.duplicate(t) })
	if t.url != "" {
		item("Copy Address", func() { b.copyText(cleanLink(t.url)) })
	}
	item("Reload", func() { b.reload(t, false) })
	if t.audio || t.muted {
		if t.muted {
			item("Unmute Tab", func() { b.toggleMute(t) })
		} else {
			item("Mute Tab", func() { b.toggleMute(t) })
		}
	}
	if t.pip != nil {
		item("Bring Video Back", func() { b.popIn(t) })
	} else if t.built() {
		item("Pop Out Video", func() { b.popOut(t) })
	}
	if t.built() && !t.private && b.current != t && t.pip == nil {
		item("Put Tab to Sleep", func() { b.sleepTab(t) })
	}
	box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	item("Close Tab", func() { b.closeTab(t) })
	item("Close Other Tabs", func() { b.closeOthers(t) })
	item("Close Tabs Below", func() { b.closeBelow(t) })
	pop.SetChild(box)
	pop.ConnectClosed(func() { coreglib.IdleAdd(func() { pop.Unparent() }) })
	pop.Popup()
}
