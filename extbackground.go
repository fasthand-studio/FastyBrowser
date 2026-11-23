// Starting and stopping extension background pages.
package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
)

// --- background pages --------------------------------------------------------

func (m *ExtensionManager) StartBackgrounds() {
	for _, ext := range m.enabledList() {
		m.startBackground(ext)
	}
}

func (m *ExtensionManager) StopBackgrounds() {
	m.stopAllCaptures()
	for _, ext := range m.extensions {
		m.stopBackground(ext)
	}
}

func (m *ExtensionManager) stopBackground(ext *Extension) {
	if ext.bgView != nil {
		// Navigating or destroying a hidden, unparented view crashes WebKit, so
		// a retired background page is muted and kept for the rest of the session.
		m.evalIn(ext.bgView, "", "window.__fastyDead=true")
		m.retired = append(m.retired, ext.bgView, ext.bgUCM)
		ext.bgView = nil
		ext.bgUCM = nil
	}
}

func (m *ExtensionManager) startBackground(ext *Extension) {
	bg := ext.Manifest.Background
	if bg == nil || ext.bgView != nil {
		return
	}

	var page string
	switch {
	case bg.Page != "":
		page = extURL(ext.ID, bg.Page)
	case len(bg.Scripts) > 0 || bg.ServiceWorker != "":
		typ := ""
		if bg.Type == "module" {
			typ = ` type="module"`
		}
		var sb strings.Builder
		sb.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"></head><body>\n")
		if len(bg.Scripts) > 0 {
			for _, s := range bg.Scripts {
				fmt.Fprintf(&sb, "<script%s src=\"%s\"></script>\n", typ, html.EscapeString(s))
			}
		} else {
			fmt.Fprintf(&sb, "<script%s src=\"%s\"></script>\n", typ, html.EscapeString(bg.ServiceWorker))
		}
		sb.WriteString("</body></html>")
		if err := os.WriteFile(filepath.Join(ext.Dir, "__fastybg.html"), []byte(sb.String()), 0o644); err != nil {
			return
		}
		page = extURL(ext.ID, "__fastybg.html")
	default:
		return
	}

	ucm := webkit.NewUserContentManager()
	ucm.RegisterScriptMessageHandler("fastyExtBg", "")
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "background"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		if ext.bgView != nil {
			m.HandleMessage(v.ToJson(0), extSource{view: ext.bgView})
		}
	})
	view := m.engine.newView(m.engine.session, ucm)
	view.SetVisible(false)
	ext.bgView, ext.bgUCM = view, ucm
	ext.reason = ""
	view.LoadURI(page)
}
