package main

// Picture in picture. WebKitGTK has none of its own, so the tab's whole web
// view moves into a small window of its own, with the video filling it. The
// page keeps
// running as it was: nothing reloads, the video doesn't stop. The tab shows
// where its page went; closing the small window, or Back to Tab, puts it back.
//
// On KDE the window is kept above the others, on every desktop, through a
// KWin script, since Wayland gives apps no way to do that. (KWin ignores a
// script moving it, so it opens where KWin puts it; drag it anywhere.)

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// pipInfoJS describes the page's main video: which site, where to find it
// again, and how far it has played.
const pipInfoJS = `(() => {
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  if (!vs.length) return JSON.stringify({kind: 'none'});
  const area = v => v.clientWidth * v.clientHeight;
  vs.sort((a, b) => (a.paused - b.paused) || (area(b) - area(a)));
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0];
  const u = new URL(location.href);
  let id = '';
  if (/(^|\.)youtube\.com$/.test(u.hostname)) {
    id = u.searchParams.get('v') || (u.pathname.match(/^\/(?:shorts|live|embed)\/([\w-]{11})/) || [])[1] || '';
  }
  return JSON.stringify({kind: id ? 'youtube' : (/^https?:/.test(v.currentSrc) ? 'direct' : 'other'),
    id: id, src: v.currentSrc, time: v.currentTime || 0, paused: v.paused, loop: v.loop, rate: v.playbackRate || 1,
    volume: v.volume, page: location.href});
})()`

// pauseJS stops the page's own copy while its video plays in the window.
const pauseJS = `(() => { document.querySelectorAll('video').forEach(v => { try { v.pause(); } catch (e) {} }); return 0; })()`

// playJS makes the page's main video play from a time, quietly resuming it.
func resumeJS(t float64, play bool) string {
	return fmt.Sprintf(`(() => {
  // YouTube's player manages its own buffer, and undoes a raw seek past it,
  // so go through its API there.
  const yt = document.getElementById('movie_player');
  if (yt && yt.seekTo) {
    try { yt.seekTo(%f, true); (%t ? yt.playVideo : yt.pauseVideo).call(yt); return 0; } catch (e) {}
  }
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  vs.sort((a, b) => b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight);
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0]; if (!v) return 0;
  try { v.currentTime = %f; } catch (e) {}
  if (%t) v.play().catch(() => {});
  return 0;
})()`, t, play, t, play)
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

// videoJS finds the page's main video (one that is playing, else the
// biggest) and makes it fill the window: everything else on the page is
// hidden, the video is stretched over it and given WebKit's own controls
// (the site's sit on top of it, hidden too). Every box the video is in is
// stretched over the window as well: WebKit draws the picture relative to
// them, so one left lower on the page would push the picture down. Full screen would be simpler,
// but WebKitGTK stops drawing video in full screen when the window itself
// stays as it is. Returns "none" if there is no video.
const videoJS = `(() => {
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  if (!vs.length) return 'none';
  const area = v => v.clientWidth * v.clientHeight;
  vs.sort((a, b) => (a.paused - b.paused) || (area(b) - area(a)));
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0];
  if (!v.hasAttribute('data-fasty-pip')) {
    document.querySelectorAll('[data-fasty-pip],[data-fasty-pip-up]').forEach(e => {
      e.removeAttribute('data-fasty-pip'); e.removeAttribute('data-fasty-pip-up'); });
    v.setAttribute('data-fasty-pip', v.controls ? 'controls' : '');
    for (let e = v.parentElement; e; e = e.parentElement) e.setAttribute('data-fasty-pip-up', '');
  }
  v.controls = true;
  let st = document.getElementById('fasty-pip-style');
  if (!st) {
    st = document.createElement('style');
    st.id = 'fasty-pip-style';
    st.textContent = ` + "`" + `
      html, body { overflow: hidden !important; background: #000 !important; }
      body *:not([data-fasty-pip-up]):not([data-fasty-pip]) { visibility: hidden !important; }
      [data-fasty-pip-up] { transform: none !important; filter: none !important; contain: none !important;
        will-change: auto !important; perspective: none !important; backdrop-filter: none !important;
        clip-path: none !important; mask: none !important; container-type: normal !important;
        content-visibility: visible !important; z-index: 2147483647 !important; }
      body[data-fasty-pip-up], body [data-fasty-pip-up] { position: fixed !important; inset: 0 !important;
        left: 0 !important; top: 0 !important; width: 100vw !important; height: 100vh !important;
        min-width: 0 !important; min-height: 0 !important; max-width: none !important; max-height: none !important;
        margin: 0 !important; padding: 0 !important; border: 0 !important; }
      video[data-fasty-pip] { visibility: visible !important; position: fixed !important; inset: 0 !important;
        left: 0 !important; top: 0 !important; width: 100vw !important; height: 100vh !important;
        max-width: none !important; max-height: none !important; margin: 0 !important;
        transform: none !important; object-fit: contain !important; background: #000 !important;
        z-index: 2147483647 !important; }` + "`" + `;
    document.documentElement.appendChild(st);
  }
  return 'ok';
})()`

// unVideoJS puts the page back as it was.
const unVideoJS = `(() => {
  document.getElementById('fasty-pip-style')?.remove();
  const v = document.querySelector('video[data-fasty-pip]');
  if (v) v.controls = v.getAttribute('data-fasty-pip') === 'controls';
  document.querySelectorAll('[data-fasty-pip],[data-fasty-pip-up]').forEach(e => {
    e.removeAttribute('data-fasty-pip'); e.removeAttribute('data-fasty-pip-up'); });
  return 0;
})()`

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

type pipInfo struct {
	Kind   string  `json:"kind"`
	ID     string  `json:"id"`
	Src    string  `json:"src"`
	Time   float64 `json:"time"`
	Paused bool    `json:"paused"`
	Loop   bool    `json:"loop"`
	Rate   float64 `json:"rate"`
	Volume float64 `json:"volume"`
	Page   string  `json:"page"`
}

// makeMirrorPiP opens a small window that plays just the video, in a web view
// of its own, while the tab keeps its page (comments, recommendations and
// all) with its own copy of the video paused. When the window closes the tab
// picks up where the window got to.
func (b *Browser) makeMirrorPiP(t *Tab, info pipInfo) {
	e := b.app.engine
	session := e.session
	if t.private {
		session = e.privateSession()
	}
	ucm := e.contentManager()
	view := e.newView(session, ucm)
	view.SetVExpand(true)
	view.SetHExpand(true)

	p := &PiP{mirror: view, origin: t.url}
	t.pip = p
	b.openPiPWindow(t, p, view)

	// Full screen from the video takes the small window full screen.
	view.ConnectEnterFullscreen(func() bool { p.win.Fullscreen(); return true })
	view.ConnectLeaveFullscreen(func() bool { p.win.Unfullscreen(); return true })

	start := int(info.Time)
	if info.Kind == "frame" {
		view.LoadURI(info.Page)
	} else if info.Kind == "youtube" {
		view.LoadURI(fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%ds", info.ID, start))
	} else {
		attrs := ""
		if info.Loop {
			attrs = "loop"
		}
		esc := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(info.Src)
		view.LoadHtml(fmt.Sprintf(`<!doctype html><meta name=referrer content=origin><body style="margin:0;background:#000">`+
			`<video id=v src="%s" controls %s style="width:100vw;height:100vh;object-fit:contain;background:#000"></video>`+
			`<script>const v=document.getElementById('v');v.addEventListener('loadedmetadata',()=>{v.currentTime=%f;v.playbackRate=%f;v.volume=%f;if(%t)v.play().catch(()=>{})});</script>`,
			esc, attrs, info.Time, max(info.Rate, 0.1), min(max(info.Volume, 0), 1), !info.Paused), info.Page)
	}
	// Pause the tab's copy, and once the mirror has a video, fit and start it.
	if info.Kind != "frame" { // a frame's video was paused, and covered, by the frame itself
		t.run(pauseJS, nil)
		t.run(`window.__fastyPipUI && window.__fastyPipUI.on(); 0`, nil)
	}
	tries, ready := 0, false
	coreglib.TimeoutAdd(700, func() bool {
		if t.pip != p || ready {
			return false
		}
		tries++
		evalOn(view, videoJS, func(r string) {
			if t.pip != p || ready || r != "ok" {
				return
			}
			ready = true
			seek := ""
			if info.Kind == "frame" {
				seek = fmt.Sprintf("try { v.currentTime = %f; v.playbackRate = %f; } catch (e) {} ", info.Time, max(info.Rate, 0.1))
			}
			play := "v.play().catch(() => {});"
			if info.Paused {
				play = "v.pause();"
			}
			evalOn(view, `(() => { const v = document.querySelector('video'); if (v) { `+seek+play+` } return 0; })()`, nil)
		})
		return tries < 30
	})
	go keepAbove(pipTitle)
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

// closeMirror ends a video-only window. With resume, the tab's own video
// carries on from wherever the window had got to.
func (b *Browser) closeMirror(t *Tab, p *PiP, resume bool) {
	t.pip = nil
	p.stopResting()
	finish := func(out string) {
		p.win.Destroy()
		b.strip.refresh(t)
		if t.view != nil {
			t.run(`window.__fastyPipUI && window.__fastyPipUI.off(); 0`, nil)
		}
		// If the tab has gone to another page meanwhile, there is nothing
		// to resume: don't touch whatever video that page has.
		if !resume || t.view == nil || t.url != p.origin {
			return
		}
		var st struct {
			Time   float64 `json:"time"`
			Paused bool    `json:"paused"`
		}
		if json.Unmarshal([]byte(out), &st) == nil && st.Time > 0 {
			t.run(resumeJS(st.Time, !st.Paused), nil)
		}
	}
	if !resume {
		finish("")
		return
	}
	evalOn(p.mirror, `(() => { const v = document.querySelector('video'); return JSON.stringify(v ? {time: v.currentTime, paused: v.paused} : {}); })()`, finish)
}

func (p *PiP) stopResting() {
	if p.resting != 0 {
		coreglib.SourceRemove(p.resting)
		p.resting = 0
	}
}

// keepAbove asks KWin to keep our window titled title above the others, on
// every desktop. Elsewhere it does nothing.
func keepAbove(title string) {
	if !strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") {
		return
	}
	script := fmt.Sprintf(`for (const w of workspace.windowList()) {
  if (w.pid === %d && w.caption === %q) {
    w.keepAbove = true;
    w.onAllDesktops = true;
  }
}`, os.Getpid(), title)
	path := filepath.Join(cacheDir(), "kwin-pip.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return
	}
	call := func(object, method string, args ...string) (string, error) {
		a := append([]string{"call", "--session", "--dest", "org.kde.KWin", "--object-path", object, "--method", method}, args...)
		out, err := exec.Command("gdbus", a...).Output()
		return strings.TrimSpace(string(out)), err
	}
	const plugin = "fasty-browser-pip"
	call("/Scripting", "org.kde.kwin.Scripting.unloadScript", plugin)
	out, err := call("/Scripting", "org.kde.kwin.Scripting.loadScript", path, plugin)
	var id int
	if err != nil || func() bool { _, e := fmt.Sscanf(out, "(%d,)", &id); return e != nil }() || id < 0 {
		log.Printf("fasty: couldn't keep the video window on top: %v %s", err, out)
		return
	}
	if _, err := call(fmt.Sprintf("/Scripting/Script%d", id), "org.kde.kwin.Script.run"); err != nil {
		log.Printf("fasty: couldn't keep the video window on top: %v", err)
	}
	time.Sleep(time.Second)
	call("/Scripting", "org.kde.kwin.Scripting.unloadScript", plugin)
}

// popOutFrame pops out a video that lives in an embedded frame: the frame's
// own page opens in the pop-up and picks up from where the video was.
func (b *Browser) popOutFrame(t *Tab, info pipInfo) {
	if t == nil || t.view == nil || t.pip != nil || b.index(t) < 0 {
		return
	}
	if !strings.HasPrefix(info.Page, "http://") && !strings.HasPrefix(info.Page, "https://") {
		b.toast("This video can't be popped out", 2)
		return
	}
	b.makeMirrorPiP(t, info)
}
