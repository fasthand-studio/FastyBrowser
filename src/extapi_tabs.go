// chrome.tabs and chrome.scripting.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// --- tabs and scripting ------------------------------------------------------

func (m *ExtensionManager) resolveURL(ext *Extension, u string) string {
	if u == "" || strings.Contains(u, "://") || strings.HasPrefix(u, "about:") ||
		strings.HasPrefix(u, "data:") || strings.HasPrefix(u, "javascript:") {
		return u
	}
	return extURL(ext.ID, u)
}

func (m *ExtensionManager) handleTabs(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) {
	if m.b == nil {
		done(nil, "no browser window")
		return
	}
	b := m.b
	switch typ {
	case "tabs.query":
		var q struct {
			Active  *bool           `json:"active"`
			Pinned  *bool           `json:"pinned"`
			Status  string          `json:"status"`
			URL     json.RawMessage `json:"url"`
			Title   string          `json:"title"`
			Current bool            `json:"currentWindow"`
		}
		json.Unmarshal(payload, &q)
		var pats []*regexp.Regexp
		var raw any
		json.Unmarshal(q.URL, &raw)
		var list []string
		switch v := raw.(type) {
		case string:
			list = []string{v}
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					list = append(list, s)
				}
			}
		}
		for _, pat := range list {
			if re, err := regexp.Compile(matchPatternToRegex(pat)); err == nil {
				pats = append(pats, re)
			}
		}
		out := []map[string]any{}
		for _, t := range b.tabs {
			info := m.tabInfo(t)
			if q.Active != nil && *q.Active != info["active"].(bool) {
				continue
			}
			if q.Pinned != nil && *q.Pinned != t.pinned {
				continue
			}
			if q.Status != "" && q.Status != info["status"] {
				continue
			}
			if len(pats) > 0 {
				ok := false
				for _, re := range pats {
					if re.MatchString(t.url) {
						ok = true
					}
				}
				if !ok {
					continue
				}
			}
			if q.Title != "" && !strings.Contains(strings.ToLower(t.title), strings.ToLower(strings.Trim(q.Title, "*"))) {
				continue
			}
			out = append(out, info)
		}
		done(out, "")

	case "tabs.get":
		var p struct {
			TabID int `json:"tabId"`
		}
		json.Unmarshal(payload, &p)
		if t := m.tabByNum(p.TabID); t != nil {
			done(m.tabInfo(t), "")
		} else {
			done(nil, fmt.Sprintf("No tab with id: %d.", p.TabID))
		}

	case "tabs.getCurrent":
		if src.tab != nil {
			done(m.tabInfo(src.tab), "")
		} else {
			done(nil, "")
		}

	case "tabs.create":
		var p struct {
			URL    string `json:"url"`
			Active *bool  `json:"active"`
			Pinned bool   `json:"pinned"`
		}
		json.Unmarshal(payload, &p)
		if m.chromeInternal(p.URL) {
			done(m.tabInfo(b.current), "")
			return
		}
		focus := p.Active == nil || *p.Active
		t := b.openTab(m.resolveURL(ext, p.URL), focus, nil, false, "", p.Pinned)
		done(m.tabInfo(t), "")

	case "tabs.update":
		var p struct {
			TabID *int `json:"tabId"`
			Props struct {
				URL    string `json:"url"`
				Active *bool  `json:"active"`
			} `json:"props"`
		}
		json.Unmarshal(payload, &p)
		t := b.current
		if p.TabID != nil {
			t = m.tabByNum(*p.TabID)
		} else if src.tab != nil {
			t = src.tab
		}
		if t == nil {
			done(nil, "No tab with that id.")
			return
		}
		if p.Props.URL != "" && !m.chromeInternal(p.Props.URL) {
			t.load(m.resolveURL(ext, p.Props.URL))
		}
		if p.Props.Active != nil && *p.Props.Active {
			b.selectTab(t)
		}
		done(m.tabInfo(t), "")

	case "tabs.remove":
		var p struct {
			TabIDs []int `json:"tabIds"`
		}
		json.Unmarshal(payload, &p)
		for _, id := range p.TabIDs {
			if t := m.tabByNum(id); t != nil {
				b.closeTab(t)
			}
		}
		done(nil, "")

	case "tabs.reload":
		var p struct {
			TabID *int `json:"tabId"`
		}
		json.Unmarshal(payload, &p)
		t := b.current
		if p.TabID != nil {
			t = m.tabByNum(*p.TabID)
		}
		if t != nil && t.view != nil {
			t.view.Reload()
		}
		done(nil, "")
	}
}

func (m *ExtensionManager) handleScripting(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) {
	if m.b == nil {
		done(nil, "no browser window")
		return
	}
	var p struct {
		TabID  *int     `json:"tabId"`
		Code   string   `json:"code"`
		CSS    string   `json:"css"`
		Files  []string `json:"files"`
		Legacy bool     `json:"legacy"`
		World  string   `json:"world"`
	}
	json.Unmarshal(payload, &p)
	t := m.b.current
	if p.TabID != nil {
		t = m.tabByNum(*p.TabID)
	} else if src.tab != nil {
		t = src.tab
	}
	if t == nil {
		done(nil, "No tab to run in.")
		return
	}
	if t.view == nil {
		// not loaded yet (restored tab): nothing to inject into
		done([]any{}, "")
		return
	}
	world := extWorld(ext.ID)
	if strings.EqualFold(p.World, "MAIN") {
		world = ""
	}
	var files strings.Builder
	for _, f := range p.Files {
		data, err := ext.readFile(f)
		if err != nil {
			done(nil, "Could not load file: "+f)
			return
		}
		files.Write(bytes.TrimPrefix(data, utf8BOM))
		files.WriteString("\n")
	}

	if typ == "scripting.insertCSS" {
		css := p.CSS + files.String()
		m.evalIn(t.view, world, "(function(){var s=document.createElement('style');s.textContent="+jsq(css)+
			";(document.head||document.documentElement).appendChild(s)})()")
		done(nil, "")
		return
	}

	if world != "" {
		m.evalIn(t.view, world, m.preamble(ext, "content"))
	}
	wrap := func(v json.RawMessage, e string) {
		if e != "" {
			done(nil, e)
		} else if p.Legacy {
			done([]json.RawMessage{v}, "")
		} else {
			done([]map[string]any{{"frameId": 0, "result": v}}, "")
		}
	}
	if files.Len() > 0 || p.Legacy {
		m.evalResult(t.view, world, files.String()+"\n"+p.Code, false, wrap)
	} else {
		m.evalResult(t.view, world, "return ("+p.Code+")", true, wrap)
	}
}

// evalResult runs script in a view and hands back its value as JSON. With
// async the script is a function body whose returned promise is awaited.
func (m *ExtensionManager) evalResult(view *webkit.WebView, world, script string, async bool, done func(json.RawMessage, string)) {
	finish := func(v *javascriptcore.Value, err error) {
		if err != nil {
			done(nil, err.Error())
			return
		}
		out := json.RawMessage("null")
		if v != nil && !v.IsUndefined() {
			if s := v.ToJson(0); s != "" {
				out = json.RawMessage(s)
			}
		}
		done(out, "")
	}
	if async {
		view.CallAsyncJavascriptFunction(context.Background(), script, nil, world, "", func(res gio.AsyncResulter) {
			v, err := view.CallAsyncJavascriptFunctionFinish(res)
			finish(v, err)
		})
		return
	}
	view.EvaluateJavascript(context.Background(), script, world, "", func(res gio.AsyncResulter) {
		v, err := view.EvaluateJavascriptFinish(res)
		finish(v, err)
	})
}
