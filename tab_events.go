// What a tab does when its page asks: navigation, new windows, crashes, menus and messages.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

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
		if t.b.app.extensions.authRedirect(uri) {
			d.Ignore()
			return true
		}
		sc := scheme(uri)
		if externalSchemes[sc] {
			d.Ignore()
			// A page may only open another app because you did something (a click
			// or a key). One that does it on its own (a redirect, a script) is
			// how pages launch Steam, Zoom or an editor behind your back.
			if action.IsUserGesture() {
				openExternal(uri)
			} else {
				t.b.toast("A page tried to open another app and was stopped", 3)
			}
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
	if x := t.b.app.extensions; x != nil && !isExtURL(t.url) {
		x.AppendMenus(t, menu, hit)
	}
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

// onMessage routes incoming script messages to the right handler based on
// content. WebKit fires the same signal for all registered handlers on a UCM.
func (t *Tab) onMessage(body string) {
	var probe struct {
		Channel string `json:"_ch"`
	}
	if json.Unmarshal([]byte(body), &probe) == nil && probe.Channel != "" {
		switch probe.Channel {
		case "ext":
			if t.b.app.extensions != nil {
				t.b.app.extensions.HandleMessage(body, extSource{view: t.view, tab: t})
			}
		case "extExt":
			if t.b.app.extensions != nil {
				t.b.app.extensions.HandleWebExternal(body, t)
			}
		case "netrec":
			rec.fromPage(t, body)
		case "webauthn":
			t.b.webauthn(t, body)
		case "pip":
			var m struct {
				Frame  string  `json:"frame"`
				Time   float64 `json:"time"`
				Rate   float64 `json:"rate"`
				Paused bool    `json:"paused"`
			}
			json.Unmarshal([]byte(body), &m)
			switch {
			case t.pip != nil:
				t.b.popIn(t)
			case m.Frame != "":
				t.b.popOutFrame(t, pipInfo{Kind: "frame", Page: m.Frame, Time: m.Time, Rate: m.Rate, Paused: m.Paused})
			default:
				t.b.popOut(t)
			}
		case "extStore":
			if t.b.app.extensions != nil && isStoreHost(hostOf(t.url)) {
				t.b.app.extensions.HandleStoreMessage(body, t.view)
			}
		}
		return
	}
	t.onVeil(body)
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
