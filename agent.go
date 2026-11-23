package main

// Agent mode: a Unix socket at $XDG_RUNTIME_DIR/fasty.sock lets AI agents
// and scripts drive the browser. Each connection sends one JSON command per
// line and gets one JSON reply per line.
//
// Commands:
//
//	{"action":"tabs"}                              list open tabs
//	{"action":"navigate","url":"https://…"}        load URL in the current tab
//	{"action":"open","url":"https://…"}            open URL in a new tab
//	{"action":"select","tab":2}                    switch to tab N (1-based)
//	{"action":"close"}                             close the current tab
//	{"action":"back"}                              go back
//	{"action":"forward"}                           go forward
//	{"action":"reload"}                            reload the current page
//	{"action":"eval","script":"document.title"}    run JS, return result
//	{"action":"content"}                           page text (innerText)
//	{"action":"html"}                              page HTML (outerHTML)
//	{"action":"url"}                               current URL
//	{"action":"title"}                             current title
//	{"action":"screenshot","path":"/tmp/s.png"}    save PNG, return path
//	{"action":"state"}                             full browser state
//	{"action":"type","text":"hello"}               type into the address bar and go
//	{"action":"click","selector":"#btn"}           click first element matching CSS selector
//	{"action":"pip-eval","script":"…"}             run JS in the current tab's Picture-in-Picture window
//	{"action":"popups"}                            list pop-up windows
//	{"action":"popup-select","tab":1}              drive pop-up N with eval/click/content
//
// Extensions:
//
//	{"action":"extensions"}                                  list installed extensions
//	{"action":"ext-install","url":"<store url|id|.crx|.xpi|path>"}  install one
//	{"action":"ext-remove","id":"x"}                         uninstall
//	{"action":"ext-toggle","id":"x"}                         enable/disable (or "enabled":true|false)
//	{"action":"ext-reload","id":"x"}                         re-read from disk, restart its background page
//	{"action":"ext-eval","id":"x","script":"…"}              run in the background page (async ok; add "target":"content" for the current tab's content-script world, or "offscreen")
//	{"action":"ext-call","id":"x","message":{…}}             runtime.sendMessage to the background page, returns its response
//	{"action":"ext-open","id":"x","page":"popup|options|path"} open an extension page in a tab (then drive it with eval/click/content)
//	{"action":"ext-action","id":"x"}                         press the extension's toolbar button
//	{"action":"ext-logs","id":"x"}                           background console output and errors
//	{"action":"ext-storage","id":"x","area":"local","items":{…}}  read (or write, with items) extension storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// agentPopup, when set, makes tab commands drive a popup window instead of
// the main window's current tab. All use happens on the GTK main thread.
var agentPopup *Tab

// agentTab is the tab an agent command acts on: a selected popup, if any,
// otherwise the main window's current tab.
func agentTab(b *Browser) *Tab {
	if agentPopup != nil {
		if agentPopup.view != nil {
			return agentPopup
		}
		agentPopup = nil
	}
	return b.current
}

type agentCmd struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Script   string `json:"script,omitempty"`
	Tab      int    `json:"tab,omitempty"`
	Text     string `json:"text,omitempty"`
	Path     string `json:"path,omitempty"`
	Selector string `json:"selector,omitempty"`

	ID      string                     `json:"id,omitempty"`
	Page    string                     `json:"page,omitempty"`
	Target  string                     `json:"target,omitempty"`
	Area    string                     `json:"area,omitempty"`
	Message json.RawMessage            `json:"message,omitempty"`
	Items   map[string]json.RawMessage `json:"items,omitempty"`
	Enabled *bool                      `json:"enabled,omitempty"`
}

type agentReply struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func agentSockPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "fasty.sock")
}

func startAgent(b *Browser) net.Listener {
	sock := agentSockPath()
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Printf("fasty: agent socket: %v", err)
		return nil
	}
	os.Chmod(sock, 0o600)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleAgent(b, conn)
		}
	}()
	return ln
}

func stopAgent(ln net.Listener) {
	if ln == nil {
		return
	}
	ln.Close()
	os.Remove(agentSockPath())
}

func handleAgent(b *Browser, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var cmd agentCmd
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			writeReply(conn, agentReply{Error: "bad json: " + err.Error()})
			continue
		}
		reply := runOnMain(b, cmd)
		writeReply(conn, reply)
	}
}

func writeReply(conn net.Conn, r agentReply) {
	r.OK = r.Error == ""
	data, _ := json.Marshal(r)
	conn.Write(append(data, '\n'))
}

// runOnMain dispatches the command to the GTK main thread and waits for
// the result. For async operations (eval), the main-thread callback sends
// the result back through the channel.
func runOnMain(b *Browser, cmd agentCmd) agentReply {
	ch := make(chan agentReply, 1)
	coreglib.IdleAdd(func() {
		doAgentCmd(b, cmd, ch)
	})
	return <-ch
}

func doAgentCmd(b *Browser, cmd agentCmd, ch chan<- agentReply) {
	ok := func(v any) { ch <- agentReply{Result: v} }
	fail := func(msg string) { ch <- agentReply{Error: msg} }

	switch cmd.Action {
	case "tabs":
		var tabs []map[string]any
		for i, t := range b.tabs {
			tabs = append(tabs, map[string]any{
				"index":   i + 1,
				"url":     t.url,
				"title":   t.title,
				"current": t == b.current,
				"loading": t.loading,
				"pinned":  t.pinned,
				"private": t.private,
			})
		}
		if tabs == nil {
			tabs = []map[string]any{}
		}
		ok(tabs)

	case "navigate":
		if cmd.URL == "" {
			fail("url required")
			return
		}
		if t := agentTab(b); t != nil && t.view != nil {
			t.load(cmd.URL)
		} else {
			b.openTab(cmd.URL, true, nil, false, "", false)
		}
		ok(nil)

	case "open":
		if cmd.URL == "" {
			fail("url required")
			return
		}
		b.openTab(cmd.URL, true, nil, false, "", false)
		ok(nil)

	case "select":
		if cmd.Tab < 1 || cmd.Tab > len(b.tabs) {
			fail(fmt.Sprintf("tab %d out of range (1–%d)", cmd.Tab, len(b.tabs)))
			return
		}
		agentPopup = nil
		b.selectTab(b.tabs[cmd.Tab-1])
		ok(nil)

	case "popups":
		var pops []map[string]any
		for i, t := range b.popups {
			pops = append(pops, map[string]any{
				"index": i + 1, "url": t.url, "title": t.title,
				"loading": t.loading, "built": t.built(), "selected": t == agentPopup,
			})
		}
		if pops == nil {
			pops = []map[string]any{}
		}
		ok(pops)

	case "popup-select":
		if cmd.Tab < 1 || cmd.Tab > len(b.popups) {
			fail(fmt.Sprintf("popup %d out of range (1–%d)", cmd.Tab, len(b.popups)))
			return
		}
		agentPopup = b.popups[cmd.Tab-1]
		ok(nil)

	case "close":
		if agentPopup != nil {
			t := agentPopup
			agentPopup = nil
			b.closeTab(t)
		} else {
			b.closeTab(nil)
		}
		ok(nil)

	case "back":
		if t := agentTab(b); t != nil && t.popupWin != nil && t.view != nil {
			t.view.GoBack()
		} else {
			b.goBack()
		}
		ok(nil)

	case "forward":
		if t := agentTab(b); t != nil && t.popupWin != nil && t.view != nil {
			t.view.GoForward()
		} else {
			b.goForward()
		}
		ok(nil)

	case "reload":
		if t := agentTab(b); t != nil && t.popupWin != nil && t.view != nil {
			t.view.Reload()
		} else {
			b.reload(nil, false)
		}
		ok(nil)

	case "url":
		if t := agentTab(b); t != nil {
			ok(t.url)
		} else {
			ok("")
		}

	case "title":
		if t := agentTab(b); t != nil {
			ok(t.displayTitle())
		} else {
			ok("")
		}

	case "eval":
		if cmd.Script == "" {
			fail("script required")
			return
		}
		t := agentTab(b)
		if t == nil || t.view == nil {
			fail("no page")
			return
		}
		t.run(cmd.Script, func(result string) {
			if strings.HasPrefix(result, "error: ") {
				ch <- agentReply{Error: result}
			} else {
				ch <- agentReply{OK: true, Result: result}
			}
		})
		return // async — the callback sends the reply

	case "pip-eval":
		t := agentTab(b)
		if t == nil || t.pip == nil || t.pip.mirror == nil {
			fail("no video-only Picture-in-Picture window")
			return
		}
		evalOn(t.pip.mirror, cmd.Script, func(result string) {
			if strings.HasPrefix(result, "error: ") {
				ch <- agentReply{Error: result}
			} else {
				ch <- agentReply{OK: true, Result: result}
			}
		})
		return

	case "content":
		t := agentTab(b)
		if t == nil || t.view == nil {
			fail("no page")
			return
		}
		t.run("document.body.innerText", func(result string) {
			ch <- agentReply{OK: true, Result: result}
		})
		return

	case "html":
		t := agentTab(b)
		if t == nil || t.view == nil {
			fail("no page")
			return
		}
		t.run("document.documentElement.outerHTML", func(result string) {
			ch <- agentReply{OK: true, Result: result}
		})
		return

	case "screenshot":
		path := cmd.Path
		if path == "" {
			path = filepath.Join(os.TempDir(), "fasty-screenshot.png")
		}
		if !shotPNG(b, path) {
			fail("screenshot failed")
			return
		}
		ok(path)

	case "state":
		ok(b.state())

	case "type":
		if cmd.Text == "" {
			fail("text required")
			return
		}
		s := b.app.settings
		u := resolve(cmd.Text, s.Engine, s.CustomEngine)
		if t := agentTab(b); t != nil && t.view != nil {
			t.load(u)
		} else {
			b.openTab(u, true, nil, false, "", false)
		}
		ok(nil)

	case "click":
		if cmd.Selector == "" {
			fail("selector required")
			return
		}
		t := agentTab(b)
		if t == nil || t.view == nil {
			fail("no page")
			return
		}
		js := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el)return "not found";el.click();return "clicked"})()`,
			cmd.Selector)
		t.run(js, func(result string) {
			if result == "not found" {
				ch <- agentReply{Error: "element not found: " + cmd.Selector}
			} else {
				ch <- agentReply{OK: true, Result: result}
			}
		})
		return

	case "wait":
		ok(nil)

	case "extensions", "ext-install", "ext-remove", "ext-toggle", "ext-reload", "ext-eval",
		"ext-call", "ext-open", "ext-action", "ext-logs", "ext-storage":
		doAgentExt(b, cmd, ch)
		return

	default:
		fail("unknown action: " + cmd.Action)
	}
}

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
