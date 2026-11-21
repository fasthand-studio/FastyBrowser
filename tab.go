package main

// One tab per page. Its web view is built lazily: a tab restored from last
// session costs nothing until you look at it (Tab.swift).

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math/rand"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type Tab struct {
	id       string
	b        *Browser
	url      string
	title    string
	pinned   bool
	private  bool
	view     *webkit.WebView
	ucm      *webkit.UserContentManager
	favicon  gdk.Paintabler
	loading  bool
	progress float64
	audio    bool
	muted    bool
	reading  bool
	picking  bool
	crashed  bool
	opener   *Tab
	popupWin *gtk.ApplicationWindow // set when this tab lives in its own window
	lastSeen float64
	findWired bool
	pip      *PiP // its page is in the floating video window
}

func newTab(b *Browser, url, title string, pinned, private bool) *Tab {
	t := &Tab{id: fmt.Sprintf("%08x", rand.Uint32()), b: b, url: url, title: title, pinned: pinned, private: private,
		lastSeen: now()}
	if url != "" && !private {
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
	t.ucm = e.contentManager()
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
	a.shield.Tune(t.ucm, hostOf(t.url), true)
	view.SetZoomLevel(a.zoomFor(t.url))
	view.SetVExpand(true)
	view.SetHExpand(true)
	t.ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) { t.onVeil(v.ToJson(0)) })
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
			}
		})
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
			}
			if !t.private {
				a.history.Visit(t.url, view.Title())
			}
			view.SetZoomLevel(a.zoomFor(t.url))
		}
		t.changed()
	})
	view.ConnectDecidePolicy(t.onPolicy)
	view.ConnectCreate(t.onCreate)
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
		if t.pip != nil {
			t.pip.win.Fullscreen()
			return true
		}
		t.b.pageFullscreen(true)
		return false
	})
	view.ConnectLeaveFullscreen(func() bool {
		if t.pip != nil {
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

var externalSchemes = map[string]bool{"mailto": true, "tel": true, "sms": true, "magnet": true, "irc": true,
	"steam": true, "zoommtg": true, "slack": true, "spotify": true, "vscode": true, "discord": true, "tg": true,
	"matrix": true, "xmpp": true, "sip": true, "webcal": true, "itms-apps": true}

func scheme(u string) string {
	if i := strings.Index(u, ":"); i > 0 {
		return strings.ToLower(u[:i])
	}
	return ""
}

func (t *Tab) onPolicy(decision webkit.PolicyDecisioner, kind webkit.PolicyDecisionType) bool {
	switch kind {
	case webkit.PolicyDecisionTypeNavigationAction:
		d := decision.(*webkit.NavigationPolicyDecision)
		action := d.NavigationAction()
		uri := action.Request().URI()
		sc := scheme(uri)
		if externalSchemes[sc] {
			d.Ignore()
			openExternal(uri)
			return true
		}
		mods := gdk.ModifierType(action.Modifiers())
		ctrl := mods&gdk.ControlMask != 0
		if action.NavigationType() == webkit.NavigationTypeLinkClicked && (action.MouseButton() == 2 || ctrl) {
			d.Ignore()
			t.b.openTab(uri, mods&gdk.ShiftMask != 0, t, t.private, "", false)
			return true
		}
		if t.b.app.settings.CleanLinks && (sc == "http" || sc == "https") {
			if c := cleanLink(uri); c != uri {
				d.Ignore()
				view := t.view
				coreglib.IdleAdd(func() { view.LoadURI(c) })
				return true
			}
		}
		if !action.IsRedirect() {
			t.b.app.shield.Tune(t.ucm, hostOf(uri), false)
		}
		return false
	case webkit.PolicyDecisionTypeNewWindowAction:
		// Let WebKit handle it: it fires the Create signal, which builds a
		// related view that shares the opener's session and keeps
		// window.opener alive — needed for sign-in popups that talk back
		// with postMessage.
		return false
	case webkit.PolicyDecisionTypeResponse:
		d := decision.(*webkit.ResponsePolicyDecision)
		if !d.IsMIMETypeSupported() && d.IsMainFrameMainResource() {
			d.Download()
			return true
		}
	}
	return false
}

// onCreate answers window.open (and target=_blank) with a real web view
// related to its opener, so sign-in popups share the session and can talk
// back with postMessage and window.close.
func (t *Tab) onCreate(action *webkit.NavigationAction) gtk.Widgetter {
	ucm := t.b.app.engine.contentManager()
	nv := t.b.app.engine.relatedView(t.view, ucm)
	uri := ""
	if action != nil {
		uri = action.Request().URI()
	}
	// The view must be in the widget tree before this handler returns;
	// WebKitGTK won't realize an unparented view, so the popup never loads.
	t.b.popup(t, nv, uri)
	return nv
}

func (t *Tab) onCrash(reason webkit.WebProcessTerminationReason) {
	// Closing a tab or putting it to sleep ends its process on purpose.
	if t.view == nil || reason == webkit.WebProcessTerminatedByAPI {
		return
	}
	t.crashed = true
	t.loading = false
	t.changed()
	why := "This page stopped working."
	if reason == webkit.WebProcessExceededMemoryLimit {
		why = "This page used too much memory and was stopped."
	}
	u := t.url
	if u == "" {
		u = "about:blank"
	}
	page := fmt.Sprintf(`<html><body style="font:15px system-ui;color:#8c8c8c;display:flex;align-items:center;`+
		`justify-content:center;height:90vh;background:Canvas"><div style="text-align:center">`+
		`<p>%s</p><p><a href="%s" style="color:CanvasText">Load it again</a></p></div></body></html>`,
		why, html.EscapeString(u))
	view := t.view
	coreglib.IdleAdd(func() { view.LoadAlternateHtml(page, u, "") })
}

func (t *Tab) onContextMenu(menu *webkit.ContextMenu, hit *webkit.HitTestResult) bool {
	if !hit.ContextIsLink() {
		return false
	}
	link := hit.LinkURI()
	// WebKit's own "Open Link in New Window" opens a tab here, so say so.
	for i := uint(0); i < menu.NItems(); i++ {
		item := menu.ItemAtPosition(i)
		if item.StockAction() == webkit.ContextMenuActionOpenLinkInNewWindow {
			menu.Remove(item)
			ga := gio.NewSimpleAction("ctx-open-tab-"+t.id, nil)
			ga.ConnectActivate(func(*glib.Variant) { t.b.openTab(link, true, t, t.private, "", false) })
			menu.Insert(webkit.NewContextMenuItemFromGaction(ga, "Open Link in New Tab", nil), int(i))
			break
		}
	}
	cp := gio.NewSimpleAction("ctx-copy-clean-"+t.id, nil)
	cp.ConnectActivate(func(*glib.Variant) { gdk.DisplayGetDefault().Clipboard().SetText(cleanLink(link)) })
	menu.Append(webkit.NewContextMenuItemFromGaction(cp, "Copy Clean Link", nil))
	return false
}

func (t *Tab) onVeil(body string) {
	var d struct {
		Off      bool   `json:"off"`
		Selector string `json:"selector"`
		Label    string `json:"label"`
		Note     string `json:"note"`
	}
	if json.Unmarshal([]byte(body), &d) != nil {
		return
	}
	if d.Off {
		t.picking = false
		t.b.pickingEnded(t)
	} else if d.Selector != "" {
		if d.Label == "" {
			d.Label = d.Selector
		}
		t.b.picked(t, d.Selector, d.Label, d.Note)
	}
}

func (t *Tab) lookupIcon() {
	db := t.b.app.engine.favicons
	db.Favicon(context.Background(), t.url, func(res gio.AsyncResulter) {
		var tex gdk.Texturer
		var err error
		try(func() { tex, err = db.FaviconFinish(res) })
		if err != nil || tex == nil || t.favicon != nil {
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
