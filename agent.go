package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
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

	Filter string `json:"filter,omitempty"` // record-list: URL text, or :failed :post :redirect :cookies
	Method string `json:"method,omitempty"`
	Entry  int    `json:"entry,omitempty"` // record-get: the entry id
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`

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

// agentConns are the connections open now, so turning the socket off can end
// them as well as refuse new ones.
var (
	agentMu    sync.Mutex
	agentConns = map[net.Conn]bool{}
)

func startAgent(b *Browser) net.Listener {
	sock := agentSockPath()
	os.Remove(sock)
	// Make the socket private from the moment it exists, not just after a chmod.
	old := syscall.Umask(0o177)
	ln, err := net.Listen("unix", sock)
	syscall.Umask(old)
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
			agentMu.Lock()
			agentConns[conn] = true
			agentMu.Unlock()
			go func() {
				handleAgent(b, conn)
				agentMu.Lock()
				delete(agentConns, conn)
				agentMu.Unlock()
			}()
		}
	}()
	return ln
}

// stopAgent closes the socket and every connection to it.
func stopAgent(ln net.Listener) {
	if ln == nil {
		return
	}
	ln.Close()
	os.Remove(agentSockPath())
	agentMu.Lock()
	for c := range agentConns {
		c.Close()
	}
	agentMu.Unlock()
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
	// Nothing leaves the browser for an agent without passing the redactor.
	data := bytes.TrimRight(marshalNoEscape(redactReply(r)), "\n")
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

	case "help":
		ok(agentHelp)

	case "record-start":
		rec.start(b)
		ok("recording started: every request from every tab is logged until record-stop. Secrets are redacted in all output.")

	case "record-stop":
		rec.stop(b)
		ok(map[string]any{"recording": false, "entries": rec.count()})

	case "record-status":
		ok(map[string]any{"recording": rec.active(), "entries": rec.count()})

	case "record-clear":
		rec.mu.Lock()
		rec.list, rec.next = nil, 0
		rec.mu.Unlock()
		ok(nil)

	case "record-list":
		ok(rec.query(cmd.Filter, cmd.Method, cmd.Offset, cmd.Limit))

	case "record-get":
		e := rec.get(cmd.Entry)
		if e == nil {
			fail(fmt.Sprintf("no recorded request %d", cmd.Entry))
			return
		}
		ok(e)

	case "record-export":
		p, err := rec.export(cmd.Path)
		if err != nil {
			fail(err.Error())
			return
		}
		ok(p)

	case "cookies":
		t := agentTab(b)
		if t == nil {
			fail("no page")
			return
		}
		uri := cmd.URL
		if uri == "" {
			uri = t.url
		}
		b.app.engine.session.CookieManager().Cookies(context.Background(), uri, func(res gio.AsyncResulter) {
			cs, err := b.app.engine.session.CookieManager().CookiesFinish(res)
			if err != nil {
				ch <- agentReply{Error: err.Error()}
				return
			}
			var out []map[string]any
			for _, c := range cs {
				out = append(out, map[string]any{"name": c.Name(), "value": redacted, "domain": c.Domain(), "path": c.Path(),
					"secure": c.Secure(), "http_only": c.HTTPOnly(), "same_site": fmt.Sprint(c.SameSitePolicy())})
			}
			ch <- agentReply{OK: true, Result: out}
		})
		return

	case "elements":
		t := agentTab(b)
		if t == nil || t.view == nil {
			fail("no page")
			return
		}
		t.run(elementsScript, func(result string) {
			var v any
			if json.Unmarshal([]byte(result), &v) != nil {
				v = result
			}
			ch <- agentReply{OK: true, Result: v}
		})
		return

	default:
		fail("unknown action: " + cmd.Action)
	}
}

const agentHelp = `Fasty agent socket: one JSON object per line, reply {"ok":bool,"result":...,"error":...}.
All replies are redacted: tokens, API keys, passwords, session cookies and OAuth codes read [REDACTED].
Pages: tabs, open{url}, navigate{url}, select{tab}, close, back, forward, reload, url, title, content, html, screenshot{path}, state
Interact: elements (clickable things with selectors), click{selector}, type{selector,text}, eval{script} (sync result only), wait
Cookies: cookies{url?} (names, domain, path, secure, http_only, same_site; values hidden)
Network recording (also switchable by the user in Settings > Agent control):
  record-start, record-stop, record-status, record-clear
  record-list{filter?,method?,limit?,offset?}  filter: URL text, or :failed :post :redirect :cookies
  record-get{entry}  full request/response headers, redirects (with Set-Cookie), bodies
  record-export{path?}  redacted JSON file, mode 600
Popups: popups, popup-select{tab}`

const elementsScript = `JSON.stringify([...document.querySelectorAll('a[href],button,input,select,textarea,[role=button],[onclick]')].filter(e=>{const r=e.getBoundingClientRect();return r.width>0&&r.height>0}).slice(0,150).map((e,i)=>{e.setAttribute('data-fasty-i',i);return {selector:'[data-fasty-i="'+i+'"]',tag:e.tagName.toLowerCase(),type:e.type||undefined,text:(e.innerText||e.value||e.placeholder||e.getAttribute('aria-label')||'').trim().slice(0,80),href:e.href||undefined}}))`
