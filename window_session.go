// Saving and restoring the open tabs between runs.
package main

import (
	"sort"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

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
