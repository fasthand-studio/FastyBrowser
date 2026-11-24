// Network recording for the agent socket. When switched on (the button in
// Settings, or the record-start command) every request any tab makes is
// logged: method, URL, request and response headers, redirects and their
// Set-Cookie lines, status, failures, and bodies where WebKit or the page
// lets us see them. Secrets are redacted when the log is read, not stored
// as-is in anything an agent can reach (see redact/redact.go).
package main

import (
	"encoding/json"
	"fastybrowser/src/redact"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
)

const recMaxEntries = 3000

type recHop struct {
	Status  int        `json:"status"`
	From    string     `json:"from"`
	Headers [][]string `json:"headers"`
}

type recEntry struct {
	ID        int        `json:"id"`
	Tab       int        `json:"tab"`
	AtMS      int64      `json:"at_ms"`
	Method    string     `json:"method"`
	URL       string     `json:"url"`
	ReqHdrs   [][]string `json:"request_headers,omitempty"`
	ReqBody   string     `json:"request_body,omitempty"`
	Status    int        `json:"status,omitempty"`
	Mime      string     `json:"mime,omitempty"`
	RespHdrs  [][]string `json:"response_headers,omitempty"`
	RespBody  string     `json:"response_body,omitempty"`
	Redirects []recHop   `json:"redirects,omitempty"`
	Error     string     `json:"error,omitempty"`
	Kind      string     `json:"kind,omitempty"` // fetch, xhr, beacon, form (seen by the page's script)
	Done      bool       `json:"done"`
	native    bool
	merged    bool
}

type recorder struct {
	mu      sync.Mutex
	on      bool
	started time.Time
	next    int
	list    []*recEntry
}

var rec = &recorder{}

func (r *recorder) active() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.on }

func (r *recorder) add(e *recEntry) {
	r.next++
	e.ID = r.next
	e.AtMS = time.Since(r.started).Milliseconds()
	r.list = append(r.list, e)
	if len(r.list) > recMaxEntries {
		r.list = r.list[len(r.list)-recMaxEntries:]
	}
}

func noFragment(u string) string {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		return u[:i]
	}
	return u
}

// start clears the log, begins recording and puts the page-side hook into
// every tab, loaded or not.
func (r *recorder) start(b *Browser) {
	r.mu.Lock()
	r.on = true
	r.started = time.Now()
	r.next = 0
	r.list = nil
	r.mu.Unlock()
	js := "window.__fastyRecOn=true;\n" + b.app.engine.netrec
	for _, t := range b.tabs {
		if t.ucm != nil {
			t.hookRecording(js)
		}
	}
	for _, t := range b.popups {
		if t.ucm != nil {
			t.hookRecording(js)
		}
	}
}

func (t *Tab) hookRecording(js string) {
	t.ucm.AddScript(webkit.NewUserScript(js, webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	t.run(js, func(string) {})
}

func (r *recorder) stop(b *Browser) {
	r.mu.Lock()
	r.on = false
	r.mu.Unlock()
	for _, t := range append(append([]*Tab(nil), b.tabs...), b.popups...) {
		if t.view != nil {
			t.run("window.__fastyRecOn=false", func(string) {})
		}
	}
}

// watch is called for every resource a page starts loading.
func (r *recorder) watch(t *Tab, res *webkit.WebResource, req *webkit.URIRequest) {
	r.mu.Lock()
	if !r.on {
		r.mu.Unlock()
		return
	}
	if strings.HasPrefix(req.URI(), "data:") || strings.HasPrefix(req.URI(), "blob:") {
		r.mu.Unlock()
		return // inline resources: noise
	}
	e := &recEntry{Tab: t.num, Method: req.HTTPMethod(), URL: req.URI(), native: true}
	if e.Method == "" {
		e.Method = "GET"
	}
	e.ReqHdrs = requestHeaders(req)
	r.add(e)
	// A form submit the page reported just before this request started.
	for _, o := range r.list {
		if o != e && !o.native && o.Kind == "form" && !o.merged && o.Method == e.Method && noFragment(o.URL) == noFragment(e.URL) {
			e.ReqBody, e.Kind, o.merged = o.ReqBody, "form", true
		}
	}
	r.mu.Unlock()

	res.ConnectSentRequest(func(rq *webkit.URIRequest, redirected *webkit.URIResponse) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if st := responseStatus(redirected); st != 0 {
			e.Redirects = append(e.Redirects, recHop{Status: st, From: e.URL, Headers: responseHeaders(redirected)})
		}
		e.URL = rq.URI()
		if m := rq.HTTPMethod(); m != "" {
			e.Method = m
		}
		if h := requestHeaders(rq); h != nil {
			e.ReqHdrs = h
		}
	})
	// (No failed-signal handler: the bindings free its GError a second time and
	// abort. A request with no status when finished is one that failed.)
	res.ConnectFinished(func() {
		var resp *webkit.URIResponse
		try(func() { resp = res.Response() })
		r.mu.Lock()
		e.Done = true
		e.Status = responseStatus(resp)
		e.Mime = responseMime(resp)
		e.RespHdrs = responseHeaders(resp)
		if e.Status == 0 && e.Error == "" {
			e.Error = "no response (failed, blocked or cancelled)"
		}
		r.mu.Unlock()
	})
}

// fromPage takes what netrec.js saw (request bodies, fetch/XHR responses).
func (r *recorder) fromPage(t *Tab, body string) {
	var m struct {
		Kind     string            `json:"kind"`
		Method   string            `json:"method"`
		URL      string            `json:"url"`
		ReqHdrs  map[string]string `json:"reqHeaders"`
		ReqBody  string            `json:"reqBody"`
		RespBody string            `json:"respBody"`
		Status   int               `json:"status"`
		Error    string            `json:"error"`
	}
	if json.Unmarshal([]byte(body), &m) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.on {
		return
	}
	// The newest request to that URL that the page hasn't described yet.
	for i := len(r.list) - 1; i >= 0 && i >= len(r.list)-300; i-- {
		e := r.list[i]
		if e.native && !e.merged && e.Method == m.Method && noFragment(e.URL) == noFragment(m.URL) && e.Tab == t.num {
			e.merged, e.Kind = true, m.Kind
			e.ReqBody = m.ReqBody
			if e.RespBody == "" {
				e.RespBody = m.RespBody
			}
			if e.Status == 0 {
				e.Status = m.Status
			}
			if m.Error != "" && e.Error == "" {
				e.Error = m.Error
			}
			return
		}
	}
	// Not seen by the browser yet (a form about to submit) or never (a failed
	// or blocked request): keep it as an entry of its own.
	e := &recEntry{Tab: t.num, Method: m.Method, URL: m.URL, Kind: m.Kind, ReqBody: m.ReqBody,
		RespBody: m.RespBody, Status: m.Status, Error: m.Error, Done: true}
	for k, v := range m.ReqHdrs {
		e.ReqHdrs = append(e.ReqHdrs, []string{k, redact.Header(k, v)})
	}
	r.add(e)
}

func (e *recEntry) summary() map[string]any {
	return map[string]any{"id": e.ID, "tab": e.Tab, "at_ms": e.AtMS, "method": e.Method, "url": e.URL,
		"status": e.Status, "mime": e.Mime, "redirects": len(e.Redirects), "error": e.Error, "kind": e.Kind,
		"has_request_body": e.ReqBody != "", "has_response_body": e.RespBody != "", "done": e.Done}
}

// list answers record-list: newest last, filtered by URL text, method, and
// "failed" / "post" / "redirect" / "cookies" shortcuts.
func (r *recorder) query(filter, method string, offset, limit int) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	f := strings.ToLower(filter)
	for _, e := range r.list {
		if method != "" && !strings.EqualFold(method, e.Method) {
			continue
		}
		switch f {
		case "":
		case ":failed":
			if e.Error == "" && e.Status < 400 {
				continue
			}
		case ":post":
			if e.Method == "GET" || e.Method == "HEAD" || e.Method == "OPTIONS" {
				continue
			}
		case ":redirect":
			if len(e.Redirects) == 0 {
				continue
			}
		case ":cookies":
			has := false
			for _, h := range e.RespHdrs {
				if strings.EqualFold(h[0], "set-cookie") {
					has = true
				}
			}
			for _, hop := range e.Redirects {
				for _, h := range hop.Headers {
					if strings.EqualFold(h[0], "set-cookie") {
						has = true
					}
				}
			}
			if !has {
				continue
			}
		default:
			if !strings.Contains(strings.ToLower(e.URL), f) {
				continue
			}
		}
		out = append(out, e.summary())
	}
	total := len(out)
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit <= 0 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return map[string]any{"recording": r.on, "total_matching": total, "entries": out}
}

func (r *recorder) get(id int) *recEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.list {
		if e.ID == id {
			cp := *e
			return &cp
		}
	}
	return nil
}

// export writes the redacted log to a file only the user can read.
func (r *recorder) export(path string) (string, error) {
	if path == "" {
		dir := filepath.Join(dataDir(), "recordings")
		os.MkdirAll(dir, 0o700)
		path = filepath.Join(dir, "recording-"+time.Now().Format("20060102-150405")+".json")
	}
	r.mu.Lock()
	raw, _ := json.Marshal(r.list)
	r.mu.Unlock()
	var v any
	json.Unmarshal(raw, &v)
	data := redact.MarshalNoEscape(map[string]any{"redacted": true, "entries": redact.Value(v)})
	return path, os.WriteFile(path, data, 0o600)
}

func (r *recorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.list) }
