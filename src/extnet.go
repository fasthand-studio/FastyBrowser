package main

// Extension pages may call hosts their manifest has permission for without
// CORS, as in other browsers. WebKit enforces CORS on extension pages, so the
// shim hands those requests to this proxy.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-webkitgtk/pkg/soup/v3"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

const chromeUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

func (e *Extension) hostAllowed(u string) bool {
	if e.hostRes == nil {
		e.hostRes = []*regexp.Regexp{}
		pats := append(append(append([]string{}, e.Manifest.Permissions...), e.Manifest.HostPerms...), e.Manifest.OptHostPerms...)
		pats = append(pats, e.Manifest.OptPermissions...)
		for _, p := range pats {
			if p != "<all_urls>" && !strings.Contains(p, "://") {
				continue
			}
			if re, err := regexp.Compile(matchPatternToRegex(p)); err == nil {
				e.hostRes = append(e.hostRes, re)
			}
		}
	}
	for _, re := range e.hostRes {
		if re.MatchString(u) {
			return true
		}
	}
	return false
}

type netReq struct {
	URL         string      `json:"url"`
	Method      string      `json:"method"`
	Headers     [][2]string `json:"headers"`
	Body        string      `json:"body"`
	Credentials string      `json:"credentials"`
	Redirect    string      `json:"redirect"`
}

func (m *ExtensionManager) handleFetch(ext *Extension, payload json.RawMessage, done func(any, string)) {
	var r netReq
	if json.Unmarshal(payload, &r) != nil || !strings.HasPrefix(r.URL, "http") || !ext.hostAllowed(r.URL) {
		done(nil, "Failed to fetch")
		return
	}
	if r.Credentials != "include" || true {
		m.fetchViaWebKit(ext, r, func(out json.RawMessage, ok bool) {
			if ok {
				m.extLog(ext, "net", r.Method+" "+r.URL+" -> (webkit)")
				done(out, "")
				return
			}
			m.fetchViaGo(ext, r, done)
		})
	}
}

func (m *ExtensionManager) fetchViaGo(ext *Extension, r netReq, done func(any, string)) {
	run := func(cookie string) {
		go func() {
			res, errMsg := doProxyFetch(ext, r, cookie)
			coreglib.IdleAdd(func() {
				if errMsg != "" {
					m.extLog(ext, "net", r.Method+" "+r.URL+" failed")
					done(nil, errMsg)
					return
				}
				m.extLog(ext, "net", fmt.Sprintf("%s %s -> %v", r.Method, r.URL, res.out["status"]))
				m.storeSetCookies(r.URL, res.cookies)
				done(res.out, "")
			})
		}()
	}
	if r.Credentials != "include" {
		run("")
		return
	}
	m.cookiesFor([]string{r.URL}, func(cs []*soup.Cookie) {
		var parts []string
		for _, c := range cs {
			parts = append(parts, c.Name()+"="+c.Value())
		}
		run(strings.Join(parts, "; "))
	})
}

type proxyResult struct {
	out     map[string]any
	cookies []*http.Cookie
}

func doProxyFetch(ext *Extension, r netReq, cookie string) (*proxyResult, string) {
	var body io.Reader
	if r.Body != "" {
		b, err := base64.StdEncoding.DecodeString(r.Body)
		if err != nil {
			return nil, "Failed to fetch"
		}
		body = strings.NewReader(string(b))
	}
	if r.Method == "" {
		r.Method = "GET"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(r.Method), r.URL, body)
	if err != nil {
		return nil, "Failed to fetch"
	}
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	origin := "chrome-extension://" + ext.ID
	if ext.Source == "firefox" {
		origin = "moz-extension://" + ext.ID
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		req.Header.Set("Origin", origin)
	}
	for _, h := range r.Headers {
		req.Header.Set(h[0], h[1])
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	client := &http.Client{}
	if r.Redirect == "manual" || r.Redirect == "error" {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "Failed to fetch"
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, "Failed to fetch"
	}
	var hs [][2]string
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Set-Cookie") {
			continue
		}
		for _, v := range vs {
			hs = append(hs, [2]string{k, v})
		}
	}
	return &proxyResult{out: map[string]any{
		"status": resp.StatusCode, "statusText": strings.TrimSpace(strings.TrimPrefix(resp.Status, " ")),
		"url": resp.Request.URL.String(), "headers": hs, "body": base64.StdEncoding.EncodeToString(data),
	}, cookies: resp.Cookies()}, ""
}

func (m *ExtensionManager) storeSetCookies(reqURL string, cs []*http.Cookie) {
	u, err := url.Parse(reqURL)
	if err != nil || len(cs) == 0 {
		return
	}
	cm := m.engine.session.CookieManager()
	for _, c := range cs {
		domain := c.Domain
		if domain == "" {
			domain = u.Hostname()
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		maxAge := -1
		if c.MaxAge > 0 {
			maxAge = c.MaxAge
		} else if !c.Expires.IsZero() {
			maxAge = int(time.Until(c.Expires).Seconds())
			if maxAge < 0 {
				maxAge = 0
			}
		}
		sc := soup.NewCookie(c.Name, c.Value, domain, path, maxAge)
		sc.SetSecure(c.Secure)
		sc.SetHTTPOnly(c.HttpOnly)
		cm.AddCookie(context.Background(), sc, func(res gio.AsyncResulter) { cm.AddCookieFinish(res) })
	}
}

func (m *ExtensionManager) extLog(ext *Extension, level, text string) {
	if len(text) > 300 {
		text = text[:300]
	}
	ext.logs = append(ext.logs, level+": "+text)
	if len(ext.logs) > 300 {
		ext.logs = ext.logs[len(ext.logs)-300:]
	}
}

// --- requests made from inside a same-origin page ---------------------------------

type fetchHelper struct {
	view    *webkit.WebView
	ucm     *webkit.UserContentManager
	ready   bool
	waiting []func()
}

// helperFor gives a hidden page whose origin is the target's. A fetch from
// there is same-origin, so it needs no CORS, uses WebKit's own network stack
// (which servers that reject other clients accept) and carries its cookies.
func (m *ExtensionManager) helperFor(origin string) *fetchHelper {
	if h := m.helpers[origin]; h != nil {
		return h
	}
	ucm := webkit.NewUserContentManager()
	view := m.engine.newView(m.engine.session, ucm)
	view.SetVisible(false)
	h := &fetchHelper{view: view, ucm: ucm}
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		if ev == webkit.LoadFinished && !h.ready {
			h.ready = true
			for _, f := range h.waiting {
				f()
			}
			h.waiting = nil
		}
	})
	view.LoadAlternateHtml("<!DOCTYPE html><html><body></body></html>", origin+"/", origin+"/")
	m.helpers[origin] = h
	return h
}

const fetchScript = `
const r = %s;
const init = { method: r.method || 'GET', headers: r.headers || [], redirect: r.redirect || 'follow',
  credentials: r.credentials === 'include' ? 'include' : 'omit' };
if (r.body) {
  const bin = atob(r.body), u8 = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
  init.body = u8;
}
const resp = await fetch(r.url, init);
const u8 = new Uint8Array(await resp.arrayBuffer());
let s = '';
for (let i = 0; i < u8.length; i += 8192) s += String.fromCharCode.apply(null, u8.subarray(i, i + 8192));
return { status: resp.status, statusText: resp.statusText, url: resp.url, headers: Array.from(resp.headers.entries()), body: btoa(s) };
`

func (m *ExtensionManager) fetchViaWebKit(ext *Extension, r netReq, done func(json.RawMessage, bool)) {
	u, err := url.Parse(r.URL)
	if err != nil || u.Host == "" {
		done(nil, false)
		return
	}
	origin := u.Scheme + "://" + u.Host
	h := m.helperFor(origin)
	req, _ := json.Marshal(r)
	run := func() {
		m.evalResult(h.view, "", fmt.Sprintf(fetchScript, string(req)), true, func(out json.RawMessage, e string) {
			done(out, e == "" && len(out) > 2)
		})
	}
	if h.ready {
		run()
	} else {
		h.waiting = append(h.waiting, run)
	}
}
