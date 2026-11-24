// Request interception for the agent socket: rules that change, block or
// fake page-made requests (fetch, XHR, beacon), and a hold queue where a
// request waits until the agent forwards (optionally edited) or drops it.
// All of it is page-side (assets/js/intercept.js); this file keeps the state
// and pushes it into the tabs. Everything an agent reads still passes the
// redactor, so edits are deltas (set or remove a header) and never need the
// secret itself.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
)

type icptHeld struct {
	ID      string            `json:"id"`
	Tab     int               `json:"tab"`
	Kind    string            `json:"kind"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
	Page    string            `json:"page"`
	tab     *Tab
}

type icptApplied struct {
	Rule   string `json:"rule"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Block  bool   `json:"block,omitempty"`
	Tab    int    `json:"tab"`
}

type icptState struct {
	mu      sync.Mutex
	rules   []map[string]any
	hold    map[string]any
	held    []*icptHeld
	log     []icptApplied
	nextID  int
	scripts map[*Tab]*webkit.UserScript
}

var icpt = &icptState{scripts: map[*Tab]*webkit.UserScript{}}

func (s *icptState) active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rules) > 0 || s.hold != nil
}

// script is the source injected at document start: config, then the hook.
func (s *icptState) script(b *Browser) string {
	cfg := map[string]any{"rules": s.rules, "hold": s.hold}
	if s.rules == nil {
		cfg["rules"] = []any{}
	}
	j, _ := json.Marshal(cfg)
	return "window.__fastyIcptCfg=" + string(j) + ";\n" + b.app.engine.intercept
}

// sync installs the current config in every tab: as a document-start script
// for pages loaded from now on, and straight into the page that is open.
func (s *icptState) sync(b *Browser) {
	s.mu.Lock()
	on := len(s.rules) > 0 || s.hold != nil
	js := ""
	if on {
		js = s.script(b)
	} else {
		js = "window.__fastyIcptCfg={rules:[],hold:null};"
	}
	s.mu.Unlock()
	for _, t := range append(append([]*Tab(nil), b.tabs...), b.popups...) {
		s.syncTab(t, js, on)
	}
}

func (s *icptState) syncTab(t *Tab, js string, on bool) {
	if t.ucm == nil {
		return
	}
	if old := s.scripts[t]; old != nil {
		t.ucm.RemoveScript(old)
		delete(s.scripts, t)
	}
	if on {
		us := webkit.NewUserScript(js, webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil)
		t.ucm.AddScript(us)
		s.scripts[t] = us
	}
	if t.view != nil {
		t.run(js, func(string) {})
	}
}

// tabOpened is called for a tab created while interception is on.
func (s *icptState) tabOpened(t *Tab) {
	if !s.active() {
		return
	}
	s.mu.Lock()
	js := s.script(t.b)
	s.mu.Unlock()
	s.syncTab(t, js, true)
}

func (s *icptState) forget(t *Tab) {
	s.mu.Lock()
	delete(s.scripts, t)
	kept := s.held[:0]
	for _, h := range s.held {
		if h.tab != t {
			kept = append(kept, h)
		}
	}
	s.held = kept
	s.mu.Unlock()
}

// fromPage handles what intercept.js reports.
func (s *icptState) fromPage(t *Tab, body string) {
	var m struct {
		Ev      string            `json:"ev"`
		ID      string            `json:"id"`
		Kind    string            `json:"kind"`
		Rule    string            `json:"rule"`
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    *string           `json:"body"`
		Block   bool              `json:"block"`
		Frame   string            `json:"frame"`
	}
	if json.Unmarshal([]byte(body), &m) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch m.Ev {
	case "held":
		s.held = append(s.held, &icptHeld{ID: m.ID, Tab: t.num, Kind: m.Kind, Method: m.Method, URL: m.URL,
			Headers: m.Headers, Body: m.Body, Page: m.Frame, tab: t})
	case "applied":
		s.log = append(s.log, icptApplied{Rule: m.Rule, Method: m.Method, URL: m.URL, Block: m.Block, Tab: t.num})
		if len(s.log) > 500 {
			s.log = s.log[len(s.log)-500:]
		}
	}
}

// decide releases a held request: edits nil means forward as it is.
func (s *icptState) decide(id string, edits json.RawMessage, drop bool) error {
	s.mu.Lock()
	var h *icptHeld
	for i, x := range s.held {
		if x.ID == id {
			h = x
			s.held = append(s.held[:i:i], s.held[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	if h == nil {
		return fmt.Errorf("no held request %q (already released, timed out, or the page navigated)", id)
	}
	d := "{}"
	if drop {
		d = `{"drop":true}`
	} else if len(edits) > 0 {
		d = string(edits)
	}
	idj, _ := json.Marshal(h.ID)
	if h.tab.view != nil {
		h.tab.run("window.__fastyIcptDecide("+string(idj)+","+d+")", func(string) {})
	}
	return nil
}

var icptRuleKeys = map[string]bool{"match": true, "method": true, "set_headers": true, "remove_headers": true,
	"url": true, "url_replace": true, "body": true, "body_replace": true, "json_set": true,
	"block": true, "mock": true, "delay_ms": true,
	"resp_json_set": true, "resp_body_replace": true, "resp_status": true, "resp_body": true}

// addRule validates and stores a rule; match is required so a rule can never
// silently apply to every request of every site.
func (s *icptState) addRule(raw json.RawMessage) (string, error) {
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("rule must be a JSON object: %v", err)
	}
	if m, _ := r["match"].(string); strings.TrimSpace(m) == "" {
		return "", fmt.Errorf("rule.match is required: a URL substring, or re:<regexp>")
	}
	for k := range r {
		if !icptRuleKeys[k] {
			return "", fmt.Errorf("unknown rule field %q (allowed: match, method, set_headers, remove_headers, url, url_replace{from,to}, body, body_replace{from,to}, json_set{path:value}, block, mock{status,headers,body}, delay_ms, resp_json_set{path:value}, resp_body_replace{from,to}, resp_status, resp_body)", k)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("r%d", s.nextID)
	r["id"] = id
	s.rules = append(s.rules, r)
	return id, nil
}

func (s *icptState) reset(b *Browser) int {
	s.mu.Lock()
	held := append([]*icptHeld(nil), s.held...)
	s.rules, s.hold, s.held = nil, nil, nil
	s.mu.Unlock()
	for _, h := range held {
		idj, _ := json.Marshal(h.ID)
		if h.tab.view != nil {
			h.tab.run("window.__fastyIcptDecide("+string(idj)+",{})", func(string) {})
		}
	}
	s.sync(b)
	return len(held)
}
