package main

import (
	"context"
	"encoding/json"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

const pipTitle = "Picture in Picture · Fasty"

type PiP struct {
	win         *gtk.Window
	title       *gtk.Label
	placeholder *adw.StatusPage
	bar         *gtk.Revealer
	onBar       bool                  // the pointer is on the bar: keep it
	resting     coreglib.SourceHandle // hides the bar
	mirror      *webkit.WebView       // set when only the video plays here; the tab keeps its page
	origin      string                // the tab's page when the video went out
}

// evalOn runs script in any web view and hands the result to done.
func evalOn(view *webkit.WebView, script string, done func(string)) {
	view.EvaluateJavascript(context.Background(), script, "", "", func(res gio.AsyncResulter) {
		v, err := view.EvaluateJavascriptFinish(res)
		out := ""
		if err != nil {
			out = "error: " + err.Error()
		} else if v != nil {
			out = v.String()
		}
		if done != nil {
			done(out)
		}
	})
}

// wake shows the bar, and hides it again after two still seconds.
func (p *PiP) wake(t *Tab) {
	p.bar.SetRevealChild(true)
	p.rest(t, 2000)
}

// rest hides the bar after ms, unless the pointer moves first or is on it.
func (p *PiP) rest(t *Tab, ms uint) {
	if p.resting != 0 {
		coreglib.SourceRemove(p.resting)
	}
	p.resting = coreglib.TimeoutAdd(ms, func() bool {
		p.resting = 0
		if t.pip == p && !p.onBar {
			p.bar.SetRevealChild(false)
		}
		return false
	})
}

// popOut moves t's page, and its video, into the floating window.
func (b *Browser) popOut(t *Tab) {
	if t == nil {
		t = b.current
	}
	if t == nil || t.view == nil || t.pip != nil {
		return
	}
	t.run(pipInfoJS, func(out string) {
		var info pipInfo
		if json.Unmarshal([]byte(out), &info) != nil || info.Kind == "none" || info.Kind == "" {
			b.toast("No video on this page", 2)
			return
		}
		if t.pip != nil || t.view == nil || b.index(t) < 0 {
			return
		}
		switch info.Kind {
		case "youtube", "direct", "frame":
			b.makeMirrorPiP(t, info)
		default:
			b.makePiP(t)
		}
	})
}

func (b *Browser) makePiP(t *Tab) {
	view := t.view
	p := &PiP{}
	t.pip = p

	// What the tab shows while its page is away.
	p.placeholder = adw.NewStatusPage()
	p.placeholder.SetIconName("video-display-symbolic")
	p.placeholder.SetTitle("Playing in a separate window")
	back := gtk.NewButtonWithLabel("Bring Video Back")
	back.SetHAlign(gtk.AlignCenter)
	back.AddCSSClass("pill")
	back.ConnectClicked(func() { b.popIn(t) })
	p.placeholder.SetChild(back)
	if hasParent(view) {
		b.stack.Remove(view)
	}
	b.stack.AddNamed(p.placeholder, t.id+"-pip")
	if b.current == t {
		b.stack.SetVisibleChild(p.placeholder)
	}

	b.openPiPWindow(t, p, view)
}

// openPiPWindow builds the floating window around view (the tab's own page in
// the old mode, or the video-only mirror).
func (b *Browser) openPiPWindow(t *Tab, p *PiP, view *webkit.WebView) {
	// The floating window: a slim bar to drag it by, and the page.
	win := gtk.NewWindow()
	p.win = win
	win.SetTitle(pipTitle)
	win.SetDefaultSize(480, 270)
	win.AddCSSClass("pip-window")
	if b.HasCSSClass("dark") {
		win.AddCSSClass("dark")
	}
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("pip-bar")
	p.title = gtk.NewLabel(t.displayTitle())
	p.title.SetHExpand(true)
	p.title.SetXAlign(0)
	p.title.SetEllipsize(pango.EllipsizeEnd)
	p.title.AddCSSClass("caption")
	bar.Append(p.title)
	fit := flatButton("view-fullscreen-symbolic", "Fill the Window with the Video", func() { evalOn(view, videoJS, nil) })
	toTab := flatButton("view-paged-symbolic", "Back to Tab", func() {
		b.popIn(t)
		b.selectTab(t)
		b.Present()
	})
	closeBtn := flatButton("window-close-symbolic", "Close (Video Goes Back to Its Tab)", func() { b.popIn(t) })
	for _, btn := range []*gtk.Button{fit, toTab, closeBtn} {
		btn.AddCSSClass("pip-button")
		bar.Append(btn)
	}
	handle := gtk.NewWindowHandle()
	handle.SetChild(bar)

	// Nothing but the video until you move the mouse over it: then the bar
	// floats over the top, and goes again after a moment of stillness, or
	// when the mouse leaves. (The video's own controls come and go the same
	// way.) An empty, hidden title bar keeps GTK from adding one of its own;
	// the window can still be resized by its edges.
	p.bar = gtk.NewRevealer()
	p.bar.SetTransitionType(gtk.RevealerTransitionTypeCrossfade)
	p.bar.SetTransitionDuration(150)
	p.bar.SetVAlign(gtk.AlignStart)
	p.bar.SetChild(handle)
	stage := gtk.NewOverlay()
	stage.SetChild(view)
	stage.AddOverlay(p.bar)
	none := gtk.NewBox(gtk.OrientationHorizontal, 0)
	none.SetVisible(false)
	win.SetTitlebar(none)
	win.SetChild(stage)
	moving := gtk.NewEventControllerMotion()
	moving.SetPropagationPhase(gtk.PhaseCapture)
	moving.ConnectEnter(func(_, _ float64) { p.wake(t) })
	moving.ConnectMotion(func(_, _ float64) { p.wake(t) })
	moving.ConnectLeave(func() { p.rest(t, 300) })
	stage.AddController(moving)
	onBar := gtk.NewEventControllerMotion()
	onBar.ConnectEnter(func(_, _ float64) { p.onBar = true })
	onBar.ConnectLeave(func() { p.onBar = false })
	handle.AddController(onBar)
	p.wake(t)
	win.ConnectCloseRequest(func() bool {
		b.popIn(t)
		return true
	})
	win.Present()
	if p.mirror == nil {
		coreglib.TimeoutAdd(250, func() bool {
			if t.pip == p {
				t.run(videoJS, nil)
				go keepAbove(pipTitle)
			}
			return false
		})
	}
	b.strip.refresh(t)
}

// togglePiP is Ctrl+Shift+P: the current tab's video out, or back.
func (b *Browser) togglePiP() {
	for _, t := range b.tabs {
		if t.pip != nil && (t == b.current || b.current == nil) {
			b.popIn(t)
			return
		}
	}
	b.popOut(b.current)
}

// popIn puts t's page back in its tab and closes the floating window.
func (b *Browser) popIn(t *Tab) {
	p := t.pip
	if p == nil {
		return
	}
	if p.mirror != nil {
		b.closeMirror(t, p, true)
		return
	}
	t.pip = nil
	p.stopResting()
	view := t.view
	if view == nil {
		p.win.Destroy()
		b.stack.Remove(p.placeholder)
		b.strip.refresh(t)
		return
	}
	// Clear the PiP styles while the view is still in the floating window,
	// then move it back once the page looks normal again.
	t.run(unVideoJS, func(string) {
		p.win.SetChild(nil)
		p.win.Destroy()
		b.stack.Remove(p.placeholder)
		if b.index(t) < 0 {
			return
		}
		b.stack.AddNamed(view, t.id)
		if b.current == t {
			b.stack.SetVisibleChild(view)
		}
		b.strip.refresh(t)
	})
}

// dropPiP closes the floating window of a tab that is going away.
func (b *Browser) dropPiP(t *Tab) {
	p := t.pip
	if p == nil {
		return
	}
	if p.mirror != nil {
		b.closeMirror(t, p, false)
		return
	}
	t.pip = nil
	p.stopResting()
	p.win.SetChild(nil)
	p.win.Destroy()
	b.stack.Remove(p.placeholder)
}

func (p *PiP) stopResting() {
	if p.resting != 0 {
		coreglib.SourceRemove(p.resting)
		p.resting = 0
	}
}
