// Agent socket commands for extensions.
package main

import (
	"encoding/json"
	"strings"
)

// doAgentExt handles the extension commands; replies go through ch.
func doAgentExt(b *Browser, cmd agentCmd, ch chan<- agentReply) {
	m := b.app.extensions
	ok := func(v any) { ch <- agentReply{Result: v} }
	fail := func(msg string) { ch <- agentReply{Error: msg} }
	if cmd.Action == "extensions" {
		ok(m.Describe())
		return
	}
	if cmd.Action == "ext-install" {
		src := cmd.URL
		if src == "" {
			src = cmd.Path
		}
		if src == "" {
			src = cmd.ID
		}
		m.InstallFromSource(src, func(name string, err error) {
			if err != nil {
				fail(err.Error())
				return
			}
			b.rebuildExtensionButtons()
			ok(map[string]any{"installed": name, "extensions": m.Describe()})
		})
		return
	}

	ext := m.find(cmd.ID)
	if ext == nil {
		fail("no extension with id " + cmd.ID)
		return
	}
	reply := func(d json.RawMessage, e string) {
		if e != "" {
			fail(e)
			return
		}
		var v any
		json.Unmarshal(d, &v)
		ok(v)
	}
	switch cmd.Action {
	case "ext-remove":
		m.Remove(ext.ID)
		b.rebuildExtensionButtons()
		ok("removed " + ext.ID)
	case "ext-toggle":
		if cmd.Enabled == nil || *cmd.Enabled != ext.Enabled {
			m.Toggle(ext.ID)
		}
		b.rebuildExtensionButtons()
		ok(map[string]any{"id": ext.ID, "enabled": ext.Enabled})
	case "ext-reload":
		if m.Reload(ext) == nil {
			fail("could not reload " + ext.ID)
			return
		}
		ok("reloaded " + ext.ID)
	case "ext-eval":
		if cmd.Script == "" {
			fail("script required")
			return
		}
		if cmd.Target == "content" {
			t := agentTab(b)
			if t == nil || t.view == nil {
				fail("no page")
				return
			}
			m.evalIn(t.view, extWorld(ext.ID), m.preamble(ext, "content"))
			script := cmd.Script
			if !strings.Contains(script, "return") {
				script = "return (" + script + ")"
			}
			m.evalResult(t.view, extWorld(ext.ID), script, true, reply)
			return
		}
		if cmd.Target == "popup" {
			if m.popupView == nil {
				fail("no popup is open (use ext-action first)")
				return
			}
			script := cmd.Script
			if !strings.Contains(script, "return") {
				script = "return (" + script + ")"
			}
			m.evalResult(m.popupView, "", script, true, reply)
			return
		}
		if cmd.Target == "offscreen" {
			if ext.offscreen == nil {
				fail("no offscreen document")
				return
			}
			script := cmd.Script
			if !strings.Contains(script, "return") {
				script = "return (" + script + ")"
			}
			m.evalResult(ext.offscreen, "", script, true, reply)
			return
		}
		m.EvalBackground(ext, cmd.Script, reply)
	case "ext-call":
		m.CallBackground(ext, cmd.Message, reply)
	case "ext-open":
		page := cmd.Page
		if page == "" {
			page = "popup"
		}
		t := m.OpenPage(ext, page)
		if t == nil {
			fail("this extension has no " + page + " page")
			return
		}
		agentPopup = nil
		ok(map[string]any{"tab": b.index(t) + 1, "url": t.url})
	case "ext-action":
		ok(m.PressAction(ext))
	case "ext-logs":
		logs := ext.logs
		if logs == nil {
			logs = []string{}
		}
		ok(logs)
	case "ext-storage":
		if cmd.Items != nil {
			payload, _ := json.Marshal(map[string]any{"area": cmd.Area, "items": cmd.Items})
			m.handleStorage(ext, "storage.set", payload, func(any, string) {})
		}
		ok(m.StorageDump(ext, cmd.Area))
	}
}
