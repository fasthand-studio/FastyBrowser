// Putting extension scripts and styles into pages.
package main

import (
	"bytes"
	"encoding/json"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
)

// --- injection ---------------------------------------------------------------

// preamble is the script that gives a context its chrome.* / browser.* API.
func (m *ExtensionManager) preamble(ext *Extension, ctx string) string {
	raw := ext.localizedManifest()
	msgs := ext.messages
	if msgs == nil {
		msgs = json.RawMessage("{}")
	}
	cfg, _ := json.Marshal(map[string]any{
		"id": ext.ID, "rid": ext.realID(), "base": extURL(ext.ID, ""), "name": ext.Manifest.Name, "ctx": ctx, "secret": m.secret,
		"manifest": raw, "messages": msgs, "reason": ext.reason, "locale": uiLocale(),
	})
	return "window.__fastyCfg=" + string(cfg) + ";" + m.apiShim
}

// InjectExtensions adds every enabled extension's content scripts, and the
// API for its own pages, to a tab's content manager.
func (m *ExtensionManager) InjectExtensions(ucm *webkit.UserContentManager) {
	for _, ext := range m.enabledList() {
		m.injectOne(ucm, ext)
		if ext.dnrFilter != nil {
			ucm.AddFilter(ext.dnrFilter)
		}
	}
	if m.storeShim != "" {
		storeAllow := []string{
			"*://chromewebstore.google.com/*",
			"*://chrome.google.com/webstore/*",
			"*://addons.mozilla.org/*",
		}
		ucm.AddScript(webkit.NewUserScript(m.storeShim,
			webkit.UserContentInjectTopFrame,
			webkit.UserScriptInjectAtDocumentEnd,
			storeAllow, nil))
	}
}

func (m *ExtensionManager) injectOne(ucm *webkit.UserContentManager, ext *Extension) {
	world := extWorld(ext.ID)
	ucm.RegisterScriptMessageHandler("fastyExt", world)
	ucm.RegisterScriptMessageHandler("fastyExt", extUSWorld(ext.ID))
	m.injectContentScripts(ucm, ext, world, ext.contentDefs())
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "page"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart,
		[]string{extURL(ext.ID, "*")}, nil))
}

// injectIntoOpenTabs gives tabs that are already open a freshly installed
// extension's scripts; they apply to the next page each tab loads.
func (m *ExtensionManager) injectIntoOpenTabs(ext *Extension) {
	if m.b == nil {
		return
	}
	for _, t := range m.b.tabs {
		if t.ucm != nil {
			m.injectOne(t.ucm, ext)
			m.injectStyles(t.ucm, ext, ext.contentDefs())
		}
	}
}

// InjectExtensionStyles adds the extensions' content-script CSS. WebKit only
// applies style sheets that are added after the view exists, unlike scripts.
func (m *ExtensionManager) InjectExtensionStyles(ucm *webkit.UserContentManager) {
	for _, ext := range m.enabledList() {
		m.injectStyles(ucm, ext, ext.contentDefs())
	}
}

func (m *ExtensionManager) injectStyles(ucm *webkit.UserContentManager, ext *Extension, defs []ContentScriptDef) {
	for _, cs := range defs {
		frames := webkit.UserContentInjectTopFrame
		if cs.AllFrames {
			frames = webkit.UserContentInjectAllFrames
		}
		allow := matchPatternsToAllowList(cs.Matches)
		block := matchPatternsToAllowList(cs.ExcludeM)
		var sheets []string
		for _, cssFile := range cs.CSS {
			data, err := ext.readFile(cssFile)
			if err != nil {
				continue
			}
			sheets = append(sheets, string(bytes.TrimPrefix(data, utf8BOM)))
		}
		sheets = append(sheets, cs.CSSCode...)
		for _, css := range sheets {
			ucm.AddStyleSheet(webkit.NewUserStyleSheet(css, frames, webkit.UserStyleLevelUser, allow, block))
		}
	}
}

func globJS(globs []string) string {
	var parts []string
	for _, g := range globs {
		parts = append(parts, jsq(g))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// gateGlobs wraps a script so it only runs where include_globs/exclude_globs allow.
func gateGlobs(cs ContentScriptDef, src string) string {
	if len(cs.IncludeGlobs)+len(cs.ExcludeGlobs) == 0 {
		return src
	}
	return `(function(){var g=function(l,u){return l.some(function(p){return new RegExp('^'+p.replace(/[.+^${}()|[\]\\]/g,'\\$&').replace(/\*/g,'.*').replace(/\?/g,'.')+'$').test(u)})};` +
		`var u=location.href;if(` + globJS(cs.IncludeGlobs) + `.length&&!g(` + globJS(cs.IncludeGlobs) + `,u))return;` +
		`if(g(` + globJS(cs.ExcludeGlobs) + `,u))return;` + "\n" + src + "\n}).call(this);"
}

func (m *ExtensionManager) injectContentScripts(ucm *webkit.UserContentManager, ext *Extension, world string, defs []ContentScriptDef) {
	for _, cs := range defs {
		when := webkit.UserScriptInjectAtDocumentEnd
		if cs.RunAt == "document_start" {
			when = webkit.UserScriptInjectAtDocumentStart
		}
		frames := webkit.UserContentInjectTopFrame
		if cs.AllFrames {
			frames = webkit.UserContentInjectAllFrames
		}
		allow := matchPatternsToAllowList(cs.Matches)
		block := matchPatternsToAllowList(cs.ExcludeM)
		main := strings.EqualFold(cs.World, "MAIN")
		w := world
		ctx := "content"
		if cs.UserScript {
			w, ctx = extUSWorld(ext.ID), "userscript"
		}

		if !main {
			ucm.AddScript(webkit.NewUserScriptForWorld(m.preamble(ext, ctx), frames,
				webkit.UserScriptInjectAtDocumentStart, w, allow, block))
		}
		var sources []string
		for _, jsFile := range cs.JS {
			data, err := ext.readFile(jsFile)
			if err != nil {
				continue
			}
			sources = append(sources, string(bytes.TrimPrefix(data, utf8BOM)))
		}
		sources = append(sources, cs.JSCode...)
		if len(cs.IncludeGlobs)+len(cs.ExcludeGlobs) > 0 && len(sources) > 0 {
			sources = []string{gateGlobs(cs, strings.Join(sources, "\n;\n"))}
		}
		for _, src := range sources {
			if main {
				ucm.AddScript(webkit.NewUserScript(src, frames, when, allow, block))
			} else {
				ucm.AddScript(webkit.NewUserScriptForWorld(src, frames, when, w, allow, block))
			}
		}
	}
}
