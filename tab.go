package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type Tab struct {
	aliasLabel string // alias: what to show in the bar in place of the TLD
	aliasUser  string // alias: the signed-in username on fasthandbd.com
	id         string
	b          *Browser
	url        string
	title      string
	pinned     bool
	private    bool
	view       *webkit.WebView
	ucm        *webkit.UserContentManager
	favicon    gdk.Paintabler
	loading    bool
	progress   float64
	audio      bool
	muted      bool
	reading    bool
	picking    bool
	crashed    bool
	opener     *Tab
	popupWin   *gtk.ApplicationWindow // set when this tab lives in its own window
	lastSeen   float64
	findWired  bool
	pip        *PiP // its page is in the floating video window
	num        int  // the id extensions know this tab by
	win        int  // window id for extension windows (0 for the main window)
}

var tabSeq int

func newTab(b *Browser, url, title string, pinned, private bool) *Tab {
	tabSeq++
	t := &Tab{id: fmt.Sprintf("%08x", rand.Uint32()), b: b, url: url, title: title, pinned: pinned, private: private,
		lastSeen: now(), num: tabSeq}
	if url != "" && !private {
		t.favicon = savedIcon(hostOf(url))
		t.lookupIcon()
	}
	return t
}

// adoptTab wraps a page another page opened (window.open), which shares its
// opener's session and content manager.
func adoptTab(b *Browser, view *webkit.WebView, url string, private bool) *Tab {
	t := newTab(b, url, "", false, private)
	t.ucm = view.UserContentManager()
	t.wire(view)
	return t
}

func (t *Tab) built() bool { return t.view != nil }

func (t *Tab) build() *webkit.WebView {
	if t.view != nil {
		return t.view
	}
	e := t.b.app.engine
	t.ucm = e.contentManagerWithExtensions(t.b.app.extensions)
	session := e.session
	if t.private {
		session = e.privateSession()
	}
	view := e.newView(session, t.ucm)
	t.wire(view)
	if t.url != "" {
		t.load(t.url)
	}
	return view
}

func (t *Tab) changed() { t.b.tabChanged(t) }

func (t *Tab) wire(view *webkit.WebView) {
	a := t.b.app
	t.view = view
	veilSheets(t.ucm, a.curtain)
	if a.extensions != nil {
		a.extensions.InjectExtensionStyles(t.ucm)
	}
	a.shield.Tune(t.ucm, hostOf(t.url), true)
	view.SetZoomLevel(a.zoomFor(t.url))
	view.SetVExpand(true)
	view.SetHExpand(true)
	t.ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		// Picture-in-picture frames arrive as plain strings, often 100 KB: don't
		// turn them into JSON just to look at the first letters.
		if v.IsString() {
			if s := v.String(); strings.HasPrefix(s, "PIPF|") || strings.HasPrefix(s, "PIPS|") {
				t.b.pipFrame(t, s)
				return
			}
		}
		t.onMessage(v.ToJson(0))
	})
	view.NotifyProperty("title", func() {
		if s := view.Title(); s != "" {
			t.title = s
			if !t.private {
				a.history.Titled(t.url, s)
			}
		}
		t.changed()
	})
	view.NotifyProperty("uri", func() {
		if u := view.URI(); u != "" {
			t.url = u
		}
		t.changed()
	})
	view.NotifyProperty("favicon", func() {
		t.favicon = nil
		try(func() {
			if f := view.Favicon(); f != nil {
				t.favicon = gdk.BaseTexture(f)
				if !t.private {
					saveIcon(hostOf(t.url), f)
				}
			}
		})
		if t.favicon == nil && !t.private {
			t.favicon = savedIcon(hostOf(t.url)) // keep the old icon while the page loads
		}
		t.changed()
	})
	view.NotifyProperty("estimated-load-progress", func() {
		t.progress = view.EstimatedLoadProgress()
		t.changed()
	})
	view.NotifyProperty("is-loading", func() {
		t.loading = view.IsLoading()
		t.changed()
	})
	view.NotifyProperty("is-playing-audio", func() {
		t.audio = view.IsPlayingAudio()
		t.changed()
	})
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		switch ev {
		case webkit.LoadStarted:
			t.crashed, t.picking, t.reading = false, false, false
		case webkit.LoadCommitted:
			if u := view.URI(); u != "" {
				t.url = u
				// The alias ends once a page on another site has really loaded
				// (not on the address updates that arrive before it does).
				if t.pip != nil && t.pip.capture != nil && t.pip.origin != "" && u != t.pip.origin {
					// The tab went to another page: the video being copied is gone.
					t.b.popIn(t)
				}
				if t.aliasLabel != "" && hostOf(u) != aliasHost {
					t.aliasLabel, t.aliasUser = "", ""
				}
			}
			if !t.private {
				a.history.Visit(t.url, view.Title())
			}
			view.SetZoomLevel(a.zoomFor(t.url))
		}
		a.extensions.tabLoad(t, ev)
		t.changed()
	})
	view.ConnectDecidePolicy(t.onPolicy)
	view.ConnectCreate(t.onCreate)
	view.ConnectResourceLoadStarted(func(res *webkit.WebResource, req *webkit.URIRequest) { rec.watch(t, res, req) })
	if rec.active() {
		t.hookRecording("window.__fastyRecOn=true;\n" + t.b.app.engine.netrec)
	}
	view.ConnectClose(func() { t.b.closeTab(t) })
	view.ConnectMouseTargetChanged(func(hit *webkit.HitTestResult, _ uint) {
		if hit.ContextIsLink() {
			t.b.hoverLink(t, hit.LinkURI())
		} else {
			t.b.hoverLink(t, "")
		}
	})
	// Full screen from the floating video window takes that window full screen.
	view.ConnectEnterFullscreen(func() bool {
		if t.pip != nil && t.pip.mirror == nil {
			t.pip.win.Fullscreen()
			return true
		}
		t.b.pageFullscreen(true)
		return false
	})
	view.ConnectLeaveFullscreen(func() bool {
		if t.pip != nil && t.pip.mirror == nil {
			t.pip.win.Unfullscreen()
			return true
		}
		t.b.pageFullscreen(false)
		return false
	})
	view.ConnectPermissionRequest(func(req webkit.PermissionRequester) bool {
		switch req.(type) {
		case *webkit.ClipboardPermissionRequest:
			req.Allow()
			return true
		}
		askPermission(t.b, t, req)
		return true
	})
	view.ConnectWebProcessTerminated(t.onCrash)
	view.ConnectContextMenu(t.onContextMenu)
	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(n int, x, y float64) {
		switch click.CurrentButton() {
		case 8:
			if view.CanGoBack() {
				view.GoBack()
				click.SetState(gtk.EventSequenceClaimed)
			}
		case 9:
			if view.CanGoForward() {
				view.GoForward()
				click.SetState(gtk.EventSequenceClaimed)
			}
		}
	})
	view.AddController(click)
}

func (t *Tab) load(u string) {
	t.url = u
	if t.view == nil {
		t.changed()
		return
	}
	t.reading = false
	t.b.app.shield.Tune(t.ucm, hostOf(u), false)
	t.view.LoadURI(u)
}

// unload puts the page to sleep: the process goes, the tab stays.
func (t *Tab) unload() *webkit.WebView {
	if t.view == nil || t.private {
		return nil
	}
	t.b.app.shield.Forget(t.ucm)
	v := t.view
	t.view = nil
	t.ucm = nil
	t.loading = false
	t.audio = false
	t.changed()
	return v
}

func (t *Tab) destroy() {
	if t.view != nil {
		t.b.app.shield.Forget(t.ucm)
		v := t.view
		t.view = nil
		// A popup is a related view: WebKit keeps it in the same web process
		// as its opener so window.opener and postMessage keep working. Killing
		// that process here blanks the opener — exactly what happens when a
		// sign-in popup closes after a successful login. WebKit reclaims a
		// process on its own once its last view is gone, so leave any shared
		// process alone.
		shared := t.opener != nil
		for _, other := range t.b.tabs {
			if other.opener == t {
				shared = true
				break
			}
		}
		if !shared {
			v.TerminateWebProcess()
		}
	}
}

func (t *Tab) lookupIcon() {
	db := t.b.app.engine.favicons
	db.Favicon(context.Background(), t.url, func(res gio.AsyncResulter) {
		var tex gdk.Texturer
		var err error
		try(func() { tex, err = db.FaviconFinish(res) })
		if err != nil || tex == nil {
			return
		}
		if !t.private {
			saveIcon(hostOf(t.url), tex)
		}
		if t.favicon != nil {
			return
		}
		t.favicon = gdk.BaseTexture(tex)
		t.changed()
	})
}

// run evaluates script in the page; done, if given, gets the result as a
// string.
func (t *Tab) run(script string, done func(string)) {
	if t.view == nil {
		return
	}
	view := t.view
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

func (t *Tab) displayTitle() string {
	if t.title != "" {
		return t.title
	}
	if t.url != "" {
		if p := pretty(t.url); p != "" {
			return p
		}
		return t.url
	}
	return "New Tab"
}

type tabSnap struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Pinned bool   `json:"pinned"`
}

func (t *Tab) snapshot() tabSnap { return tabSnap{t.url, t.title, t.pinned} }

func isStoreHost(h string) bool {
	return h == "chromewebstore.google.com" || h == "chrome.google.com" || h == "addons.mozilla.org"
}
