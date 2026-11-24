package main

// Picture in picture the way Firefox does it. The video stays in its tab,
// playing in its own page (so the sound and the site's own player carry on
// untouched), and its picture is copied out into a small window of its own:
// no second copy of the page is loaded. assets/js/pipcapture.js draws each
// frame small, encodes it and hands it over; this file shows it and gives the
// window play, pause and seek controls that act on the page's own video.
//
// Videos whose pixels the page doesn't let scripts read (some cross-origin
// files) can't be copied this way and fall back to pip_mirror.go.

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// capturePiP is what the window of a copied video needs beyond a PiP.
type capturePiP struct {
	pic      *gtk.Picture
	controls *gtk.Revealer
	play     *gtk.Button
	scale    *gtk.Scale
	cur      *gtk.Label
	dur      *gtk.Label
	paused   bool
	duration float64
	dragging bool
	capW     int  // the width frames are being made at
	sized    bool // the window has been sized to the video
	frames   int  // frames shown so far
	parked   bool // the tab's page waits in Browser.keep while another tab is in front
}

func fmtClock(s float64) string {
	if s < 0 || math.IsNaN(s) || math.IsInf(s, 0) {
		s = 0
	}
	t := int(s)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// makeCapturePiP opens the window and starts copying. fallback runs if the
// page doesn't allow it.
func (b *Browser) makeCapturePiP(t *Tab, fallback func()) {
	p := &PiP{origin: t.url, capture: &capturePiP{capW: 480, paused: false}}
	c := p.capture
	t.pip = p

	win := gtk.NewWindow()
	p.win = win
	win.SetTitle(pipTitle)
	win.SetDefaultSize(480, 270)
	win.AddCSSClass("pip-window")
	if b.HasCSSClass("dark") {
		win.AddCSSClass("dark")
	}

	c.pic = gtk.NewPicture()
	c.pic.SetContentFit(gtk.ContentFitContain)
	c.pic.SetCanShrink(true)
	c.pic.SetHExpand(true)
	c.pic.SetVExpand(true)

	// Top: drag handle, title, back to tab, close.
	top := gtk.NewBox(gtk.OrientationHorizontal, 4)
	top.AddCSSClass("pip-bar")
	p.title = gtk.NewLabel(t.displayTitle())
	p.title.SetHExpand(true)
	p.title.SetXAlign(0)
	p.title.SetEllipsize(pango.EllipsizeEnd)
	p.title.AddCSSClass("caption")
	top.Append(p.title)
	toTab := flatButton("view-paged-symbolic", "Back to Tab", func() {
		b.popIn(t)
		b.selectTab(t)
		b.Present()
	})
	full := flatButton("view-fullscreen-symbolic", "Full Screen (F)", func() { toggleWindowFullscreen(win) })
	closeBtn := flatButton("window-close-symbolic", "Close (the video keeps playing in its tab)", func() { b.popIn(t) })
	for _, btn := range []*gtk.Button{full, toTab, closeBtn} {
		btn.AddCSSClass("pip-button")
		top.Append(btn)
	}
	handle := gtk.NewWindowHandle()
	handle.SetChild(top)
	p.bar = gtk.NewRevealer()
	p.bar.SetTransitionType(gtk.RevealerTransitionTypeCrossfade)
	p.bar.SetTransitionDuration(150)
	p.bar.SetVAlign(gtk.AlignStart)
	p.bar.SetChild(handle)

	// Bottom: play or pause, where we are, a seek bar, how long it is.
	bottom := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bottom.AddCSSClass("pip-controls")
	c.play = flatButton("media-playback-pause-symbolic", "Play or Pause (Space)", func() {
		t.run("window.__fastyPipCap && window.__fastyPipCap.toggle(); 0", nil)
	})
	c.play.AddCSSClass("pip-button")
	c.cur = gtk.NewLabel("0:00")
	c.cur.AddCSSClass("caption")
	c.cur.AddCSSClass("numeric")
	c.dur = gtk.NewLabel("0:00")
	c.dur.AddCSSClass("caption")
	c.dur.AddCSSClass("numeric")
	c.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, 0, 100, 0.1)
	c.scale.SetHExpand(true)
	c.scale.SetDrawValue(false)
	c.scale.ConnectChangeValue(func(_ gtk.ScrollType, value float64) bool {
		c.dragging = true
		c.cur.SetText(fmtClock(value))
		t.run(fmt.Sprintf("window.__fastyPipCap && window.__fastyPipCap.seek(%f); 0", value), nil)
		coreglib.TimeoutAdd(600, func() bool { c.dragging = false; return false })
		return false
	})
	bottom.Append(c.play)
	bottom.Append(c.cur)
	bottom.Append(c.scale)
	bottom.Append(c.dur)
	c.controls = gtk.NewRevealer()
	c.controls.SetTransitionType(gtk.RevealerTransitionTypeCrossfade)
	c.controls.SetTransitionDuration(150)
	c.controls.SetVAlign(gtk.AlignEnd)
	c.controls.SetChild(bottom)

	stage := gtk.NewOverlay()
	stage.SetChild(c.pic)
	stage.AddOverlay(p.bar)
	stage.AddOverlay(c.controls)
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
	keep := gtk.NewEventControllerMotion()
	keep.ConnectEnter(func(_, _ float64) { p.onBar = true })
	keep.ConnectLeave(func() { p.onBar = false })
	handle.AddController(keep)
	keepB := gtk.NewEventControllerMotion()
	keepB.ConnectEnter(func(_, _ float64) { p.onBar = true })
	keepB.ConnectLeave(func() { p.onBar = false })
	bottom.AddController(keepB)

	// Space plays and pauses, the arrows skip, F is full screen, Esc leaves it.
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(kv, _ uint, _ gdk.ModifierType) bool {
		switch kv {
		case gdk.KEY_space, gdk.KEY_k:
			t.run("window.__fastyPipCap && window.__fastyPipCap.toggle(); 0", nil)
		case gdk.KEY_Left:
			t.run("window.__fastyPipCap && window.__fastyPipCap.skip(-5); 0", nil)
		case gdk.KEY_Right:
			t.run("window.__fastyPipCap && window.__fastyPipCap.skip(5); 0", nil)
		case gdk.KEY_f:
			toggleWindowFullscreen(win)
		case gdk.KEY_Escape:
			if win.IsFullscreen() {
				win.Unfullscreen()
			}
		default:
			return false
		}
		return true
	})
	win.AddController(keys)
	dbl := gtk.NewGestureClick()
	dbl.SetPropagationPhase(gtk.PhaseBubble)
	dbl.ConnectPressed(func(n int, _, _ float64) {
		if n == 2 {
			toggleWindowFullscreen(win)
		}
	})
	c.pic.AddController(dbl)

	p.wake(t)
	win.ConnectCloseRequest(func() bool {
		b.popIn(t)
		return true
	})
	win.Present()
	go keepAbove(pipTitle)
	b.strip.refresh(t)

	// Start copying; if the page won't allow it, undo and use another way.
	script := strings.Replace(asset("js/pipcapture.js"), "__MAXW__", strconv.Itoa(c.capW), 1)
	t.run(script, func(res string) {
		if t.pip != p {
			return
		}
		if res != "ok" {
			b.closeCapture(t, p, false)
			if fallback != nil {
				fallback()
			}
			return
		}
		t.run(`window.__fastyPipUI && window.__fastyPipUI.on(); 0`, nil)
	})
}

func toggleWindowFullscreen(w *gtk.Window) {
	if w.IsFullscreen() {
		w.Unfullscreen()
	} else {
		w.Fullscreen()
	}
}

// pipFrame takes one message from the page: a frame ("PIPF|time|duration|
// paused|width|height|jpeg") or just where the video is ("PIPS|time|duration|
// paused").
func (b *Browser) pipFrame(t *Tab, msg string) {
	p := t.pip
	if p == nil || p.capture == nil {
		return
	}
	c := p.capture
	parts := strings.SplitN(msg, "|", 7)
	if len(parts) < 4 {
		return
	}
	cur, _ := strconv.ParseFloat(parts[1], 64)
	dur, _ := strconv.ParseFloat(parts[2], 64)
	paused := parts[3] == "1"
	if dur != c.duration && dur > 0 {
		c.duration = dur
		c.scale.SetRange(0, dur)
		c.dur.SetText(fmtClock(dur))
	}
	if !c.dragging {
		c.scale.SetValue(cur)
		c.cur.SetText(fmtClock(cur))
	}
	if paused != c.paused || c.play.IconName() == "" {
		c.paused = paused
		if paused {
			c.play.SetIconName("media-playback-start-symbolic")
		} else {
			c.play.SetIconName("media-playback-pause-symbolic")
		}
	}
	if parts[0] != "PIPF" || len(parts) < 7 || len(parts[6]) > 6<<20 {
		return
	}
	w, _ := strconv.Atoi(parts[4])
	h, _ := strconv.Atoi(parts[5])
	jpeg, err := base64.StdEncoding.DecodeString(parts[6])
	if err != nil || len(jpeg) == 0 {
		return
	}
	tex, err := gdk.NewTextureFromBytes(glib.NewBytes(jpeg))
	if err != nil {
		return
	}
	c.pic.SetPaintable(tex)
	c.frames++
	if !c.sized && w > 0 && h > 0 {
		c.sized = true
		width := 480
		p.win.SetDefaultSize(width, max(120, width*h/w))
	}
	// Ask for sharper frames when the window has grown past what they have.
	if pw := c.pic.Width(); pw > 0 {
		want := min(1920, max(320, pw*c.pic.ScaleFactor()))
		if want > c.capW+60 || want < c.capW-60 {
			c.capW = want
			t.run(fmt.Sprintf("window.__fastyPipCap && window.__fastyPipCap.width(%d); 0", want), nil)
		}
	}
}

// parkView keeps the page of a tab whose video is being copied alive and
// "on screen" while another tab is in front.
func (b *Browser) parkView(t *Tab) {
	p := t.pip
	if p == nil || p.capture == nil || p.capture.parked || t.view == nil || !hasParent(t.view) {
		return
	}
	v := t.view
	w, h := v.Width(), v.Height()
	b.stack.Remove(v)
	if w > 0 && h > 0 {
		v.SetSizeRequest(w, h)
	}
	b.keep.Put(v, 0, 0)
	p.capture.parked = true
}

// unparkView puts a parked page back with the other tabs.
func (b *Browser) unparkView(t *Tab) {
	p := t.pip
	if p == nil || p.capture == nil || !p.capture.parked || t.view == nil {
		return
	}
	v := t.view
	b.keep.Remove(v)
	v.SetSizeRequest(-1, -1)
	b.stack.AddNamed(v, t.id)
	p.capture.parked = false
}

// closeCapture ends a copied-video window. The video was never touched, so it
// is simply still playing in its tab.
func (b *Browser) closeCapture(t *Tab, p *PiP, _ bool) {
	if t.pip == p {
		t.pip = nil
	}
	p.stopResting()
	t.pip = p // (parked pages are put back below)
	b.unparkView(t)
	t.pip = nil
	if t.view != nil {
		t.run(`window.__fastyPipCap && window.__fastyPipCap.stop(); window.__fastyPipUI && window.__fastyPipUI.off(); 0`, nil)
	}
	p.win.Destroy()
	b.strip.refresh(t)
}
