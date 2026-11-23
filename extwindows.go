// Extension windows and sign-in flows.
package main

import (
	"encoding/json"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// --- identity.launchWebAuthFlow -----------------------------------------------------

type authFlow struct {
	ext    *Extension
	prefix string
	tab    *Tab
	done   func(any, string)
}

func (m *ExtensionManager) startAuth(ext *Extension, rawURL string, done func(any, string)) {
	prefix := "https://" + authHost(ext) + ".chromiumapp.org/"
	t := m.b.openTab(rawURL, true, nil, false, "", false)
	m.auths = append(m.auths, &authFlow{ext: ext, prefix: prefix, tab: t, done: done})
}

func authHost(ext *Extension) string {
	var b strings.Builder
	for _, c := range strings.ToLower(ext.realID()) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// authRedirect ends a sign-in flow when the page is sent to the extension's
// redirect URL; the caller then cancels that navigation.
func (m *ExtensionManager) authRedirect(uri string) bool {
	if m == nil {
		return false
	}
	for i, f := range m.auths {
		if strings.HasPrefix(uri, f.prefix) {
			m.auths = append(m.auths[:i], m.auths[i+1:]...)
			tab, done := f.tab, f.done
			coreglib.IdleAdd(func() {
				m.b.closeTab(tab)
				done(uri, "")
			})
			return true
		}
	}
	return false
}

func (m *ExtensionManager) authTabClosed(t *Tab) {
	if m == nil {
		return
	}
	for i, f := range m.auths {
		if f.tab == t {
			m.auths = append(m.auths[:i], m.auths[i+1:]...)
			f.done(nil, "The user did not approve access.")
			return
		}
	}
}

// --- windows -------------------------------------------------------------------------

var windowSeq = 1

func (m *ExtensionManager) windowInfo(t *Tab, populate bool) map[string]any {
	w := map[string]any{"id": 1, "focused": true, "type": "normal", "state": "normal", "incognito": false,
		"alwaysOnTop": false, "left": 0, "top": 0, "width": 1280, "height": 800}
	if t != nil && t.win > 1 {
		w["id"], w["type"], w["focused"] = t.win, "popup", false
		if populate {
			w["tabs"] = []any{m.tabInfo(t)}
		}
		return w
	}
	if populate {
		var tabs []any
		for _, x := range m.b.tabs {
			tabs = append(tabs, m.tabInfo(x))
		}
		w["tabs"] = tabs
	}
	return w
}

func (m *ExtensionManager) openExtWindow(ext *Extension, rawURL string, width, height int) *Tab {
	b := m.b
	if width <= 0 {
		width = 500
	}
	if height <= 0 {
		height = 600
	}
	ucm := b.app.engine.contentManager()
	m.InjectExtensions(ucm)
	view := b.app.engine.newView(b.app.engine.session, ucm)
	t := newTab(b, rawURL, "", false, false)
	t.ucm = ucm
	t.wire(view)
	windowSeq++
	t.win = windowSeq

	win := gtk.NewApplicationWindow(&b.app.Application.Application)
	win.SetTitle(ext.Manifest.Name)
	win.SetIconName("fasty-browser")
	win.SetDefaultSize(width, height)
	win.AddCSSClass("popup-window")
	if b.HasCSSClass("dark") {
		win.AddCSSClass("dark")
	}
	win.SetChild(view)
	win.ConnectCloseRequest(func() bool {
		m.fire("windows.onRemoved", t.win)
		b.closeTab(t)
		return false
	})
	t.popupWin = win
	b.popups = append(b.popups, t)
	view.LoadURI(rawURL)
	win.Present()
	return t
}

func (m *ExtensionManager) handleWindows(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) {
	b := m.b
	switch typ {
	case "windows.create":
		var p struct {
			URL    json.RawMessage `json:"url"`
			Type   string          `json:"type"`
			Width  int             `json:"width"`
			Height int             `json:"height"`
		}
		json.Unmarshal(payload, &p)
		var u string
		var list []string
		if json.Unmarshal(p.URL, &u) != nil {
			json.Unmarshal(p.URL, &list)
			if len(list) > 0 {
				u = list[0]
			}
		}
		u = m.resolveURL(ext, u)
		if p.Type == "popup" || p.Type == "panel" {
			t := m.openExtWindow(ext, u, p.Width, p.Height)
			done(m.windowInfo(t, true), "")
			return
		}
		t := b.openTab(u, true, nil, false, "", false)
		done(m.windowInfo(t, true), "")
	case "windows.remove":
		var p struct {
			ID int `json:"id"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID && t.popupWin != nil {
				t.popupWin.Close()
			}
		}
		done(nil, "")
	case "windows.getAll":
		var p struct {
			Populate bool `json:"populate"`
		}
		json.Unmarshal(payload, &p)
		out := []map[string]any{m.windowInfo(nil, p.Populate)}
		for _, t := range b.popups {
			if t.win > 1 {
				out = append(out, m.windowInfo(t, p.Populate))
			}
		}
		done(out, "")
	case "windows.get":
		var p struct {
			ID       int  `json:"id"`
			Populate bool `json:"populate"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID {
				done(m.windowInfo(t, p.Populate), "")
				return
			}
		}
		done(m.windowInfo(nil, p.Populate), "")
	case "windows.update":
		var p struct {
			ID    int  `json:"id"`
			Focus bool `json:"focused"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID && t.popupWin != nil && p.Focus {
				t.popupWin.Present()
			}
		}
		done(m.windowInfo(nil, false), "")
	}
}
