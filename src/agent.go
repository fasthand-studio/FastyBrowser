package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fastybrowser/src/redact"
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

	Rule    json.RawMessage `json:"rule,omitempty"`    // intercept-rule / intercept-forward: the rule or edits
	Timeout int             `json:"timeout,omitempty"` // intercept-hold: seconds before an unanswered request is let through
	Private bool            `json:"private,omitempty"` // open/perf: use a private tab (empty cache)
	Warm    bool            `json:"warm,omitempty"`    // perf: normal tab, may use the cache
	Clear   bool            `json:"clear,omitempty"`   // perf warm: clear the cache first

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
	data := bytes.TrimRight(redact.MarshalNoEscape(redactReply(r)), "\n")
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
		b.openTab(cmd.URL, true, nil, cmd.Private, "", false)
		ok(nil)

	case "clear-cache":
		clearCache(b, func(err error) {
			if err != nil {
				ch <- agentReply{Error: err.Error()}
				return
			}
			ch <- agentReply{OK: true, Result: "HTTP disk and memory cache cleared (cookies and logins kept)"}
		})
		return

	case "intercept-rule":
		id, err := icpt.addRule(cmd.Rule)
		if err != nil {
			fail(err.Error())
			return
		}
		icpt.sync(b)
		ok(map[string]any{"rule": id, "note": "applies to page-made fetch/XHR/beacon requests in every tab, including pages loaded from now on"})

	case "intercept-hold":
		if strings.TrimSpace(cmd.Filter) == "" {
			fail("filter required: a URL substring, or re:<regexp>")
			return
		}
		h := map[string]any{"match": cmd.Filter, "timeout": 60}
		if cmd.Method != "" {
			h["method"] = cmd.Method
		}
		if cmd.Timeout > 0 {
			h["timeout"] = cmd.Timeout
		}
		icpt.mu.Lock()
		icpt.hold = h
		icpt.mu.Unlock()
		icpt.sync(b)
		ok("holding matching requests: intercept-list to see them, intercept-forward / intercept-drop to release")

	case "intercept-list":
		icpt.mu.Lock()
		held := append([]*icptHeld{}, icpt.held...)
		out := map[string]any{"held": held, "rules": icpt.rules, "hold": icpt.hold, "applied": len(icpt.log)}
		icpt.mu.Unlock()
		ok(out)

	case "intercept-log":
		icpt.mu.Lock()
		lg := append([]icptApplied{}, icpt.log...)
		icpt.mu.Unlock()
		ok(lg)

	case "intercept-forward":
		if err := icpt.decide(cmd.ID, cmd.Rule, false); err != nil {
			fail(err.Error())
			return
		}
		ok(nil)

	case "intercept-drop":
		if err := icpt.decide(cmd.ID, nil, true); err != nil {
			fail(err.Error())
			return
		}
		ok(nil)

	case "intercept-stop":
		ok(map[string]any{"released": icpt.reset(b)})

	case "perf":
		agentPerf(b, cmd, ch)
		return

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
				out = append(out, map[string]any{"name": c.Name(), "value": redact.Placeholder, "domain": c.Domain(), "path": c.Path(),
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
Speed: clear-cache (HTTP cache only, logins kept); perf{url?,warm?,clear?} measures a load and lists findings:
  perf url=... loads it in a private tab (empty cache = cold), measures, closes it; add warm=true clear=true to use a normal tab after clearing the cache;
  perf with no url measures the current tab. open also takes private=true.
Interception (page-made fetch/XHR/beacon only; for testing your own sites; match is required):
  intercept-rule{rule:{match,method?,set_headers?,remove_headers?,url?,url_replace?{from,to},body?,body_replace?{from,to},json_set?{path:value},block?,mock?{status,headers,body},delay_ms?,resp_json_set?{path:value},resp_body_replace?{from,to},resp_status?,resp_body?}}
  intercept-hold{filter,method?,timeout?}  hold matching requests; intercept-list shows them; intercept-forward{id,rule?: edits} or intercept-drop{id}
  intercept-log (which rules fired), intercept-stop (clears rules and hold, releases anything held)
Popups: popups, popup-select{tab}`

const elementsScript = `JSON.stringify([...document.querySelectorAll('a[href],button,input,select,textarea,[role=button],[onclick]')].filter(e=>{const r=e.getBoundingClientRect();return r.width>0&&r.height>0}).slice(0,150).map((e,i)=>{e.setAttribute('data-fasty-i',i);return {selector:'[data-fasty-i="'+i+'"]',tag:e.tagName.toLowerCase(),type:e.type||undefined,text:(e.innerText||e.value||e.placeholder||e.getAttribute('aria-label')||'').trim().slice(0,80),href:e.href||undefined}}))`

// redactReply makes a reply safe to hand to an agent.
func redactReply(r agentReply) agentReply {
	r.Error = redact.String(r.Error)
	if r.Result == nil {
		return r
	}
	raw, err := json.Marshal(r.Result)
	if err != nil {
		return r
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return r
	}
	r.Result = redact.Value(v)
	return r
}
