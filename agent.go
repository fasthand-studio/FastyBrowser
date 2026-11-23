package main

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
		label, target, alias := aliasMatch(cmd.Text)
		if alias {
			u = target
		}
		if t := agentTab(b); t != nil && t.view != nil {
			t.aliasLabel, t.aliasUser = "", ""
			if alias {
				t.aliasLabel = label
			}
			t.load(u)
			b.syncChrome()
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
