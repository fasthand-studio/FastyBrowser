// Moving around a page: back, forward, reload, zoom, full screen, reader mode.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

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
