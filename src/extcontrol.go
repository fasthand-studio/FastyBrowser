// Driving extensions from the browser and the agent socket.
package main

import (
	"encoding/json"
	"strings"
)

// --- helpers for the agent socket and the UI ---------------------------------

// OpenPage opens one of the extension's pages ("popup", "options", or a path) in a tab.
func (m *ExtensionManager) OpenPage(ext *Extension, page string) *Tab {
	if m.b == nil {
		return nil
	}
	switch page {
	case "popup", "":
		page = ext.popupPath()
	case "options":
		page = ext.optionsPage()
	}
	if page == "" {
		return nil
	}
	return m.b.openTab(extURL(ext.ID, page), true, nil, false, "", false)
}

// PressAction is a click on the extension's toolbar button.
func (m *ExtensionManager) PressAction(ext *Extension) string {
	if ext.popupPath() != "" {
		openExtensionPopup(m.b, ext.ID)
		return "popup opened"
	}
	if m.b != nil && m.b.current != nil {
		m.fireTo(ext, "action.onClicked", m.tabInfo(m.b.current))
		m.fireTo(ext, "browserAction.onClicked", m.tabInfo(m.b.current))
		return "action.onClicked sent to the background page"
	}
	return "nothing to click"
}

func (m *ExtensionManager) fireTo(ext *Extension, name string, args ...any) {
	if ext.bgView != nil {
		m.deliver(ext.bgView, "", "event", map[string]any{"name": name, "args": args})
	}
}

// CallBackground sends a runtime message to the background page as if from
// another extension page and waits for the response.
func (m *ExtensionManager) CallBackground(ext *Extension, message json.RawMessage, done func(json.RawMessage, string)) {
	m.route(m.bgEP(ext), message, map[string]any{"id": ext.ID}, done)
}

// EvalBackground runs a function body (or an expression) in the background page.
func (m *ExtensionManager) EvalBackground(ext *Extension, script string, done func(json.RawMessage, string)) {
	if ext.bgView == nil {
		done(nil, "no background page is running")
		return
	}
	if !strings.Contains(script, "return") {
		script = "return (" + script + ")"
	}
	m.evalResult(ext.bgView, "", script, true, done)
}

func (m *ExtensionManager) Describe() []map[string]any {
	out := []map[string]any{}
	for _, e := range m.extensions {
		cs := 0
		for _, c := range e.Manifest.ContentScripts {
			cs += len(c.JS) + len(c.CSS)
		}
		d := map[string]any{
			"id": e.ID, "name": e.Manifest.Name, "version": e.Manifest.Version, "enabled": e.Enabled,
			"source": e.Source, "manifest_version": e.Manifest.ManifestVersion,
			"background": e.Manifest.Background != nil, "background_running": e.bgView != nil,
			"content_script_files": cs, "permissions": e.Manifest.Permissions,
			"options_page": e.optionsPage(), "badge": e.badge,
			"net_rules_active": e.dnrFilter != nil, "dynamic_scripts": len(e.dyn),
			"context_menus": len(e.menus), "offscreen": e.offscreen != nil,
		}
		if len(e.dnrEnabled) > 0 {
			d["rulesets"] = e.dnrEnabled
		}
		if a := e.action(); a != nil {
			d["popup"] = a.DefaultPopup
		}
		out = append(out, d)
	}
	return out
}

func (m *ExtensionManager) StorageDump(ext *Extension, area string) map[string]json.RawMessage {
	if area == "" {
		area = "local"
	}
	return m.loadStore(ext, area)
}

// Reload re-reads an extension from disk and restarts its background page.
func (m *ExtensionManager) Reload(ext *Extension) *Extension {
	m.stopBackground(ext)
	fresh := m.loadOne(ext.Dir)
	if fresh == nil {
		return nil
	}
	for i, e := range m.extensions {
		if e == ext {
			m.extensions[i] = fresh
		}
	}
	if fresh.Enabled {
		m.startBackground(fresh)
	}
	m.tell()
	return fresh
}
