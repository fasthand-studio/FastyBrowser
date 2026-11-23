// A video-only pop-up window while the tab keeps its page.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

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
