package main

// Extension runtime: content-script injection, background pages, and the
// message bus between them (runtime.*, storage.*, tabs.*, ports) plus the
// helpers the agent socket uses to drive extensions.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func extWorld(id string) string { return "fasty-ext-" + id }

var theExt *ExtensionManager

// extScheme is the URL scheme an extension sees for its own files, the one
// its browser family uses: extensions compare against it and build URLs with it.
func extScheme(id string) string {
	if theExt != nil {
		if e := theExt.find(id); e != nil && e.Source == "firefox" {
			return "moz-extension"
		}
	}
	return "chrome-extension"
}

func extURL(id, path string) string {
	return extScheme(id) + "://" + id + "/" + strings.TrimLeft(path, "/")
}

func isExtURL(u string) bool {
	return strings.HasPrefix(u, "chrome-extension://") || strings.HasPrefix(u, "moz-extension://") ||
		strings.HasPrefix(u, "fasty-extension://")
}

func jsq(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

var utf8BOM = []byte("\xef\xbb\xbf")

// --- localisation ------------------------------------------------------------

// uiLocaleCandidates lists the user's locales from most to least specific
// ("bn_BD" then "bn").
func uiLocaleCandidates() []string {
	raw := ""
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			raw = v
			break
		}
	}
	var out []string
	for _, part := range strings.Split(raw, ":") {
		part = strings.SplitN(strings.SplitN(part, ".", 2)[0], "@", 2)[0]
		part = strings.ReplaceAll(part, "-", "_")
		if part == "" {
			continue
		}
		out = append(out, part)
		if i := strings.Index(part, "_"); i > 0 {
			out = append(out, part[:i])
		}
	}
	return out
}

func uiLocale() string {
	if c := uiLocaleCandidates(); len(c) > 0 {
		return strings.ReplaceAll(c[0], "_", "-")
	}
	return "en-US"
}

// loadMessages merges the extension's locales: the user's language wins,
// then its default locale, then English; missing keys fall back in that order.
func (e *Extension) loadMessages() {
	order := append([]string{}, uiLocaleCandidates()...)
	if e.Manifest.DefaultLocale != "" {
		order = append(order, e.Manifest.DefaultLocale)
	}
	order = append(order, "en", "en_US", "en_GB")
	merged := map[string]json.RawMessage{}
	for i := len(order) - 1; i >= 0; i-- {
		d, err := os.ReadFile(filepath.Join(e.Dir, "_locales", order[i], "messages.json"))
		if err != nil {
			continue
		}
		var msgs map[string]json.RawMessage
		if json.Unmarshal(bytes.TrimPrefix(d, utf8BOM), &msgs) != nil {
			continue
		}
		for k, v := range msgs {
			merged[strings.ToLower(k)] = v
		}
	}
	if len(merged) > 0 {
		e.messages, _ = json.Marshal(merged)
	}
	e.Manifest.Name = e.localize(e.Manifest.Name)
	e.Manifest.Description = e.localize(e.Manifest.Description)
	if a := e.action(); a != nil {
		a.DefaultTitle = e.localize(a.DefaultTitle)
	}
}

func (e *Extension) localize(s string) string {
	if !strings.HasPrefix(s, "__MSG_") || !strings.HasSuffix(s, "__") || e.messages == nil {
		return s
	}
	key := s[len("__MSG_") : len(s)-2]
	var msgs map[string]struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(e.messages, &msgs) != nil {
		return s
	}
	for k, v := range msgs {
		if strings.EqualFold(k, key) && v.Message != "" {
			return v.Message
		}
	}
	return s
}

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

// --- background pages --------------------------------------------------------

func (m *ExtensionManager) StartBackgrounds() {
	for _, ext := range m.enabledList() {
		m.startBackground(ext)
	}
}

func (m *ExtensionManager) StopBackgrounds() {
	m.stopAllCaptures()
	for _, ext := range m.extensions {
		m.stopBackground(ext)
	}
}

func (m *ExtensionManager) stopBackground(ext *Extension) {
	if ext.bgView != nil {
		// Navigating or destroying a hidden, unparented view crashes WebKit, so
		// a retired background page is muted and kept for the rest of the session.
		m.evalIn(ext.bgView, "", "window.__fastyDead=true")
		m.retired = append(m.retired, ext.bgView, ext.bgUCM)
		ext.bgView = nil
		ext.bgUCM = nil
	}
}

func (m *ExtensionManager) startBackground(ext *Extension) {
	bg := ext.Manifest.Background
	if bg == nil || ext.bgView != nil {
		return
	}

	var page string
	switch {
	case bg.Page != "":
		page = extURL(ext.ID, bg.Page)
	case len(bg.Scripts) > 0 || bg.ServiceWorker != "":
		typ := ""
		if bg.Type == "module" {
			typ = ` type="module"`
		}
		var sb strings.Builder
		sb.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"></head><body>\n")
		if len(bg.Scripts) > 0 {
			for _, s := range bg.Scripts {
				fmt.Fprintf(&sb, "<script%s src=\"%s\"></script>\n", typ, html.EscapeString(s))
			}
		} else {
			fmt.Fprintf(&sb, "<script%s src=\"%s\"></script>\n", typ, html.EscapeString(bg.ServiceWorker))
		}
		sb.WriteString("</body></html>")
		if err := os.WriteFile(filepath.Join(ext.Dir, "__fastybg.html"), []byte(sb.String()), 0o644); err != nil {
			return
		}
		page = extURL(ext.ID, "__fastybg.html")
	default:
		return
	}

	ucm := webkit.NewUserContentManager()
	ucm.RegisterScriptMessageHandler("fastyExtBg", "")
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "background"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		if ext.bgView != nil {
			m.HandleMessage(v.ToJson(0), extSource{view: ext.bgView})
		}
	})
	view := m.engine.newView(m.engine.session, ucm)
	view.SetVisible(false)
	ext.bgView, ext.bgUCM = view, ucm
	ext.reason = ""
	view.LoadURI(page)
}

// --- message bus -------------------------------------------------------------

// extSource is where a message came from.
type extSource struct {
	view *webkit.WebView
	tab  *Tab   // nil for the background page
	ctx  string // content | page | background
}

func (s extSource) world(id string) string {
	switch s.ctx {
	case "content":
		return extWorld(id)
	case "userscript":
		return extUSWorld(id)
	}
	return ""
}

func extUSWorld(id string) string { return "fasty-us-" + id }

type extEP struct {
	view  *webkit.WebView
	world string
}

type extPort struct{ a, b extEP }

func (m *ExtensionManager) evalIn(view *webkit.WebView, world, js string) {
	if view != nil {
		view.EvaluateJavascript(context.Background(), js, world, "", nil)
	}
}

func (m *ExtensionManager) deliver(view *webkit.WebView, world, kind string, data any) {
	m.evalIn(view, world, "window.__fastyDeliver&&window.__fastyDeliver("+jsq(kind)+","+jsq(data)+")")
}

// deliverChecked reports through missing() when the context has no API loaded.
func (m *ExtensionManager) deliverChecked(ep extEP, kind string, data any, missing func()) {
	view := ep.view
	if view == nil {
		missing()
		return
	}
	js := "window.__fastyDeliver?(window.__fastyDeliver(" + jsq(kind) + "," + jsq(data) + "),true):false"
	view.EvaluateJavascript(context.Background(), js, ep.world, "", func(res gio.AsyncResulter) {
		v, err := view.EvaluateJavascriptFinish(res)
		if err != nil || v == nil || !v.ToBoolean() {
			missing()
		}
	})
}

func (m *ExtensionManager) reply(src extSource, extID string, cbID int, result json.RawMessage, errMsg string) {
	if cbID == 0 || src.view == nil {
		return
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	m.evalIn(src.view, src.world(extID),
		fmt.Sprintf("window.__fastyReply(%d,%s,%s)", cbID, string(result), jsq(errMsg)))
}

func (m *ExtensionManager) tabByNum(n int) *Tab {
	if m.b == nil {
		return nil
	}
	for _, t := range m.b.tabs {
		if t.num == n {
			return t
		}
	}
	for _, t := range m.b.popups {
		if t.num == n {
			return t
		}
	}
	return nil
}

func (m *ExtensionManager) tabInfo(t *Tab) map[string]any {
	idx := 0
	for i, x := range m.b.tabs {
		if x == t {
			idx = i
		}
	}
	status := "complete"
	if t.loading {
		status = "loading"
	}
	win := 1
	if t.win > 1 {
		win = t.win
	}
	return map[string]any{
		"id": t.num, "index": idx, "windowId": win, "active": t == m.b.current,
		"highlighted": t == m.b.current, "url": t.url, "title": t.title, "pinned": t.pinned,
		"incognito": t.private, "status": status, "discarded": t.view == nil, "favIconUrl": "",
	}
}

func (m *ExtensionManager) senderOf(ext *Extension, src extSource) map[string]any {
	s := map[string]any{"id": ext.realID()}
	if src.tab == nil && src.view != nil {
		u := src.view.URI()
		if u == "" {
			u = extURL(ext.ID, "")
		}
		s["url"], s["frameId"] = u, 0
		if pu, err := url.Parse(u); err == nil && pu.Host != "" {
			s["origin"] = pu.Scheme + "://" + pu.Host
		}
	}
	if src.tab != nil && m.b != nil {
		s["tab"] = m.tabInfo(src.tab)
		s["url"] = src.tab.url
		s["frameId"] = 0
		if u, err := url.Parse(src.tab.url); err == nil && u.Host != "" {
			s["origin"] = u.Scheme + "://" + u.Host
		}
	}
	return s
}

// extPages are the tabs showing one of the extension's own pages.
func (m *ExtensionManager) extPages(ext *Extension) []extEP {
	var out []extEP
	if m.b == nil {
		return out
	}
	prefix := extURL(ext.ID, "")
	for _, t := range m.b.tabs {
		if t.view != nil && strings.HasPrefix(t.url, prefix) {
			out = append(out, extEP{t.view, ""})
		}
	}
	if ext.offscreen != nil {
		out = append(out, extEP{ext.offscreen, ""})
	}
	return out
}

func (m *ExtensionManager) bgEP(ext *Extension) []extEP {
	if ext.bgView == nil {
		return nil
	}
	return []extEP{{ext.bgView, ""}}
}

// route hands a message to the receiving contexts and calls replyTo once, with
// the first answer, or with an error if nobody is listening.
func (m *ExtensionManager) route(targets []extEP, message json.RawMessage, sender map[string]any, replyTo func(json.RawMessage, string)) {
	const noReceiver = "Could not establish connection. Receiving end does not exist."
	_, external := sender["__external"]
	delete(sender, "__external")
	_, userScript := sender["__userscript"]
	delete(sender, "__userscript")
	if len(targets) == 0 {
		replyTo(nil, noReceiver)
		return
	}
	m.reqSeq++
	id := m.reqSeq
	answered := false
	finish := func(d json.RawMessage, e string) {
		if answered {
			return
		}
		answered = true
		delete(m.pending, id)
		replyTo(d, e)
	}
	m.pending[id] = finish
	time.AfterFunc(60*time.Second, func() {
		coreglib.IdleAdd(func() { finish(nil, "The message port closed before a response was received.") })
	})
	missing := 0
	for _, t := range targets {
		m.deliverChecked(t, "message", map[string]any{"reqId": id, "message": message, "sender": sender, "external": external, "userScript": userScript}, func() {
			missing++
			if missing == len(targets) {
				finish(nil, noReceiver)
			}
		})
	}
}

func (m *ExtensionManager) broadcast(ext *Extension, kind string, data any) {
	if ext.bgView != nil {
		m.deliver(ext.bgView, "", kind, data)
	}
	if m.b == nil {
		return
	}
	prefix := extURL(ext.ID, "")
	for _, t := range m.b.tabs {
		if t.view == nil {
			continue
		}
		if strings.HasPrefix(t.url, prefix) {
			m.deliver(t.view, "", kind, data)
		} else {
			m.deliver(t.view, extWorld(ext.ID), kind, data)
		}
	}
}

// fire sends a browser event (tabs.onUpdated and the like) to the background pages.
func (m *ExtensionManager) fire(name string, args ...any) {
	for _, ext := range m.extensions {
		if ext.Enabled && ext.bgView != nil {
			m.deliver(ext.bgView, "", "event", map[string]any{"name": name, "args": args})
		}
	}
}

func (m *ExtensionManager) anyBackground() bool {
	for _, ext := range m.extensions {
		if ext.Enabled && ext.bgView != nil {
			return true
		}
	}
	return false
}

func (m *ExtensionManager) tabLoad(t *Tab, ev webkit.LoadEvent) {
	if m == nil || m.b == nil || !m.anyBackground() {
		return
	}
	ts := float64(time.Now().UnixMilli())
	switch ev {
	case webkit.LoadStarted:
		req := map[string]any{"requestId": fmt.Sprint(t.num, "-", int64(ts)), "url": t.url, "method": "GET", "frameId": 0,
			"parentFrameId": -1, "tabId": t.num, "type": "main_frame", "timeStamp": ts}
		m.fire("webRequest.onBeforeRequest", req)
		m.fire("webRequest.onBeforeSendHeaders", req)
		m.fire("webRequest.onSendHeaders", req)
		m.fire("tabs.onUpdated", t.num, map[string]any{"status": "loading"}, m.tabInfo(t))
		m.fire("webNavigation.onBeforeNavigate", map[string]any{"tabId": t.num, "url": t.url, "frameId": 0, "timeStamp": ts})
	case webkit.LoadCommitted:
		resp := map[string]any{"requestId": fmt.Sprint(t.num, "-", int64(ts)), "url": t.url, "method": "GET", "frameId": 0,
			"parentFrameId": -1, "tabId": t.num, "type": "main_frame", "timeStamp": ts, "statusCode": 200}
		m.fire("webRequest.onHeadersReceived", resp)
		m.fire("webRequest.onResponseStarted", resp)
		m.fire("webNavigation.onCommitted", map[string]any{"tabId": t.num, "url": t.url, "frameId": 0, "timeStamp": ts})
	case webkit.LoadFinished:
		m.fire("webRequest.onCompleted", map[string]any{"requestId": fmt.Sprint(t.num, "-", int64(ts)), "url": t.url, "method": "GET",
			"frameId": 0, "parentFrameId": -1, "tabId": t.num, "type": "main_frame", "timeStamp": ts, "statusCode": 200})
		m.fire("tabs.onUpdated", t.num, map[string]any{"status": "complete", "url": t.url}, m.tabInfo(t))
		m.fire("webNavigation.onCompleted", map[string]any{"tabId": t.num, "url": t.url, "frameId": 0, "timeStamp": ts})
	}
}

// HandleMessage runs one API call from an extension context.
func (m *ExtensionManager) HandleMessage(body string, src extSource) {
	var msg struct {
		ExtID   string          `json:"extId"`
		Type    string          `json:"type"`
		Ctx     string          `json:"ctx"`
		Secret  string          `json:"secret"`
		CbID    int             `json:"cbId"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(body), &msg) != nil || msg.Secret != m.secret {
		return
	}
	ext := m.find(msg.ExtID)
	if ext == nil || !ext.Enabled {
		return
	}
	src.ctx = msg.Ctx
	done := func(result any, err string) {
		var raw json.RawMessage
		if result != nil {
			raw, _ = json.Marshal(result)
		}
		m.reply(src, ext.ID, msg.CbID, raw, err)
	}

	switch msg.Type {
	case "log":
		var p struct{ Level, Text string }
		json.Unmarshal(msg.Payload, &p)
		ext.logs = append(ext.logs, p.Level+": "+p.Text)
		if len(ext.logs) > 300 {
			ext.logs = ext.logs[len(ext.logs)-300:]
		}

	case "runtime.sendMessage":
		var p struct {
			Message json.RawMessage `json:"message"`
			Target  string          `json:"target"`
		}
		json.Unmarshal(msg.Payload, &p)
		targets := m.bgEP(ext)
		sender := m.senderOf(ext, src)
		if p.Target != "" {
			other := m.findByRealID(p.Target)
			targets = nil
			if other != nil && other.Enabled {
				targets = m.bgEP(other)
			}
			sender["id"] = ext.realID()
			sender["__external"] = true
		} else if src.ctx == "background" {
			targets = m.extPages(ext)
		}
		if src.ctx == "userscript" {
			sender["__userscript"] = true
		}
		m.route(targets, p.Message, sender, func(d json.RawMessage, e string) {
			m.reply(src, ext.ID, msg.CbID, d, e)
		})

	case "runtime.reply":
		var p struct {
			ReqID int             `json:"reqId"`
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		json.Unmarshal(msg.Payload, &p)
		if f := m.pending[p.ReqID]; f != nil {
			f(p.Data, p.Error)
		}

	case "runtime.openOptionsPage":
		if t := m.OpenPage(ext, "options"); t == nil {
			done(nil, "This extension has no options page.")
		} else {
			done(nil, "")
		}

	case "tabs.sendMessage":
		var p struct {
			TabID   int             `json:"tabId"`
			Message json.RawMessage `json:"message"`
		}
		json.Unmarshal(msg.Payload, &p)
		t := m.tabByNum(p.TabID)
		if t == nil || t.view == nil {
			done(nil, fmt.Sprintf("No tab with id: %d.", p.TabID))
			return
		}
		m.route([]extEP{{t.view, extWorld(ext.ID)}}, p.Message, m.senderOf(ext, src), func(d json.RawMessage, e string) {
			m.reply(src, ext.ID, msg.CbID, d, e)
		})

	case "port.open":
		var p struct {
			PortID string `json:"portId"`
			Name   string `json:"name"`
			TabID  *int   `json:"tabId"`
		}
		json.Unmarshal(msg.Payload, &p)
		from := extEP{src.view, src.world(ext.ID)}
		var targets []extEP
		switch {
		case p.TabID != nil:
			if t := m.tabByNum(*p.TabID); t != nil && t.view != nil {
				targets = []extEP{{t.view, extWorld(ext.ID)}}
			}
		case src.ctx == "background":
			targets = m.extPages(ext)
		default:
			targets = m.bgEP(ext)
		}
		if len(targets) == 0 {
			m.deliver(from.view, from.world, "portClose", map[string]any{"portId": p.PortID})
			return
		}
		to := targets[0]
		m.ports[p.PortID] = &extPort{from, to}
		m.deliverChecked(to, "portOpen", map[string]any{"portId": p.PortID, "name": p.Name, "sender": m.senderOf(ext, src)}, func() {
			delete(m.ports, p.PortID)
			m.deliver(from.view, from.world, "portClose", map[string]any{"portId": p.PortID})
		})

	case "port.msg", "port.close":
		var p struct {
			PortID string          `json:"portId"`
			Msg    json.RawMessage `json:"msg"`
		}
		json.Unmarshal(msg.Payload, &p)
		pt := m.ports[p.PortID]
		if pt == nil {
			return
		}
		from := extEP{src.view, src.world(ext.ID)}
		to := pt.b
		if pt.b == from {
			to = pt.a
		}
		if msg.Type == "port.msg" {
			m.deliver(to.view, to.world, "portMsg", map[string]any{"portId": p.PortID, "msg": p.Msg})
		} else {
			delete(m.ports, p.PortID)
			m.deliver(to.view, to.world, "portClose", map[string]any{"portId": p.PortID})
		}

	case "storage.get", "storage.set", "storage.remove", "storage.clear", "storage.bytes":
		m.handleStorage(ext, msg.Type, msg.Payload, done)

	case "tabs.query", "tabs.get", "tabs.getCurrent", "tabs.create", "tabs.update", "tabs.remove", "tabs.reload":
		m.handleTabs(ext, src, msg.Type, msg.Payload, done)

	case "scripting.execute", "scripting.insertCSS":
		m.handleScripting(ext, src, msg.Type, msg.Payload, done)

	case "action.set":
		var p struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		json.Unmarshal(msg.Payload, &p)
		var str string
		json.Unmarshal(p.Value, &str)
		switch p.Key {
		case "text":
			ext.badge = str
		case "color":
			ext.badgeColor = badgeColorCSS(p.Value)
		case "title":
			ext.title = str
		case "popup":
			ext.popupOverride = str
		case "icon":
			ext.iconOverride = iconPathOf(p.Value)
		}
		m.tell()
		done(nil, "")

	case "action.get":
		var p struct {
			Key string `json:"key"`
		}
		json.Unmarshal(msg.Payload, &p)
		switch p.Key {
		case "text":
			done(ext.badge, "")
		case "title":
			t := ext.title
			if t == "" {
				if a := ext.action(); a != nil && a.DefaultTitle != "" {
					t = a.DefaultTitle
				} else {
					t = ext.Manifest.Name
				}
			}
			done(t, "")
		case "popup":
			done(ext.popupPath(), "")
		default:
			done("", "")
		}

	case "action.openPopup":
		if m.b != nil {
			openExtensionPopup(m.b, ext.ID)
		}
		done(nil, "")

	case "notifications.create":
		var p struct{ Title, Message string }
		json.Unmarshal(msg.Payload, &p)
		if m.b != nil {
			m.b.toast(strings.TrimSpace(p.Title+": "+p.Message), 4)
		}
		done(nil, "")

	default:
		if !m.handleExtra(ext, src, msg.Type, msg.Payload, done) {
			done(nil, "")
		}
	}
}

// --- storage -----------------------------------------------------------------

func (m *ExtensionManager) loadStore(ext *Extension, area string) map[string]json.RawMessage {
	switch area {
	case "session":
		if m.session[ext.ID] == nil {
			m.session[ext.ID] = map[string]json.RawMessage{}
		}
		return m.session[ext.ID]
	case "managed":
		return map[string]json.RawMessage{}
	}
	store := map[string]json.RawMessage{}
	if data, err := os.ReadFile(filepath.Join(ext.Dir, ".storage."+area+".json")); err == nil {
		json.Unmarshal(data, &store)
	}
	return store
}

func (m *ExtensionManager) saveStore(ext *Extension, area string, store map[string]json.RawMessage) {
	switch area {
	case "session":
		m.session[ext.ID] = store
	case "managed":
	default:
		out, _ := json.Marshal(store)
		os.WriteFile(filepath.Join(ext.Dir, ".storage."+area+".json"), out, 0o644)
	}
}

func (m *ExtensionManager) handleStorage(ext *Extension, typ string, payload json.RawMessage, done func(any, string)) {
	var p struct {
		Area  string                     `json:"area"`
		Keys  json.RawMessage            `json:"keys"`
		Items map[string]json.RawMessage `json:"items"`
	}
	json.Unmarshal(payload, &p)
	if p.Area == "" {
		p.Area = "local"
	}
	store := m.loadStore(ext, p.Area)
	changes := map[string]map[string]json.RawMessage{}
	change := func(k string, old, nu json.RawMessage) {
		c := map[string]json.RawMessage{}
		if old != nil {
			c["oldValue"] = old
		}
		if nu != nil {
			c["newValue"] = nu
		}
		changes[k] = c
	}

	switch typ {
	case "storage.get":
		out := map[string]json.RawMessage{}
		var keys any
		json.Unmarshal(p.Keys, &keys)
		switch k := keys.(type) {
		case nil:
			for key, v := range store {
				out[key] = v
			}
		case string:
			if v, ok := store[k]; ok {
				out[k] = v
			}
		case []any:
			for _, x := range k {
				if s, ok := x.(string); ok {
					if v, ok := store[s]; ok {
						out[s] = v
					}
				}
			}
		case map[string]any:
			for key, def := range k {
				if v, ok := store[key]; ok {
					out[key] = v
				} else {
					out[key], _ = json.Marshal(def)
				}
			}
		}
		done(out, "")
		return
	case "storage.set":
		for k, v := range p.Items {
			if !bytes.Equal(store[k], v) {
				change(k, store[k], v)
			}
			store[k] = v
		}
	case "storage.remove":
		var keys []string
		json.Unmarshal(p.Keys, &keys)
		for _, k := range keys {
			if old, ok := store[k]; ok {
				change(k, old, nil)
				delete(store, k)
			}
		}
	case "storage.clear":
		for k, old := range store {
			change(k, old, nil)
		}
		store = map[string]json.RawMessage{}
	case "storage.bytes":
		n := 0
		for k, v := range store {
			n += len(k) + len(v)
		}
		done(n, "")
		return
	}
	if p.Area == "managed" {
		done(nil, "")
		return
	}
	m.saveStore(ext, p.Area, store)
	done(nil, "")
	if len(changes) > 0 {
		m.broadcast(ext, "storageChanged", map[string]any{"area": p.Area, "changes": changes})
	}
}

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

// --- helpers for the agent socket and the UI ---------------------------------

// OpenPage opens one of the extension's pages ("popup", "options", or a path) in a tab.
func (m *ExtensionManager) OpenPage(ext *Extension, page string) *Tab {
	if m.b == nil {
		return nil
	}
	switch page {
	case "popup", "":
		page = ext.popupPath()
	case "options":
		page = ext.optionsPage()
	}
	if page == "" {
		return nil
	}
	return m.b.openTab(extURL(ext.ID, page), true, nil, false, "", false)
}

// PressAction is a click on the extension's toolbar button.
func (m *ExtensionManager) PressAction(ext *Extension) string {
	if ext.popupPath() != "" {
		openExtensionPopup(m.b, ext.ID)
		return "popup opened"
	}
	if m.b != nil && m.b.current != nil {
		m.fireTo(ext, "action.onClicked", m.tabInfo(m.b.current))
		m.fireTo(ext, "browserAction.onClicked", m.tabInfo(m.b.current))
		return "action.onClicked sent to the background page"
	}
	return "nothing to click"
}

func (m *ExtensionManager) fireTo(ext *Extension, name string, args ...any) {
	if ext.bgView != nil {
		m.deliver(ext.bgView, "", "event", map[string]any{"name": name, "args": args})
	}
}

// CallBackground sends a runtime message to the background page as if from
// another extension page and waits for the response.
func (m *ExtensionManager) CallBackground(ext *Extension, message json.RawMessage, done func(json.RawMessage, string)) {
	m.route(m.bgEP(ext), message, map[string]any{"id": ext.ID}, done)
}

// EvalBackground runs a function body (or an expression) in the background page.
func (m *ExtensionManager) EvalBackground(ext *Extension, script string, done func(json.RawMessage, string)) {
	if ext.bgView == nil {
		done(nil, "no background page is running")
		return
	}
	if !strings.Contains(script, "return") {
		script = "return (" + script + ")"
	}
	m.evalResult(ext.bgView, "", script, true, done)
}

func (m *ExtensionManager) Describe() []map[string]any {
	out := []map[string]any{}
	for _, e := range m.extensions {
		cs := 0
		for _, c := range e.Manifest.ContentScripts {
			cs += len(c.JS) + len(c.CSS)
		}
		d := map[string]any{
			"id": e.ID, "name": e.Manifest.Name, "version": e.Manifest.Version, "enabled": e.Enabled,
			"source": e.Source, "manifest_version": e.Manifest.ManifestVersion,
			"background": e.Manifest.Background != nil, "background_running": e.bgView != nil,
			"content_script_files": cs, "permissions": e.Manifest.Permissions,
			"options_page": e.optionsPage(), "badge": e.badge,
			"net_rules_active": e.dnrFilter != nil, "dynamic_scripts": len(e.dyn),
			"context_menus": len(e.menus), "offscreen": e.offscreen != nil,
		}
		if len(e.dnrEnabled) > 0 {
			d["rulesets"] = e.dnrEnabled
		}
		if a := e.action(); a != nil {
			d["popup"] = a.DefaultPopup
		}
		out = append(out, d)
	}
	return out
}

func (m *ExtensionManager) StorageDump(ext *Extension, area string) map[string]json.RawMessage {
	if area == "" {
		area = "local"
	}
	return m.loadStore(ext, area)
}

// Reload re-reads an extension from disk and restarts its background page.
func (m *ExtensionManager) Reload(ext *Extension) *Extension {
	m.stopBackground(ext)
	fresh := m.loadOne(ext.Dir)
	if fresh == nil {
		return nil
	}
	for i, e := range m.extensions {
		if e == ext {
			m.extensions[i] = fresh
		}
	}
	if fresh.Enabled {
		m.startBackground(fresh)
	}
	m.tell()
	return fresh
}

var chromeIDRe = regexp.MustCompile(`[a-p]{32}`)

// InstallFromSource installs from a store page URL, a store extension id, a
// direct .crx/.xpi/.zip URL, or a local path.
func (m *ExtensionManager) InstallFromSource(src string, done func(name string, err error)) {
	src = strings.TrimSpace(src)
	switch {
	case strings.HasPrefix(src, "/") || strings.HasPrefix(src, "file://"):
		name, err := m.InstallFromFile(strings.TrimPrefix(src, "file://"))
		done(name, err)
	case strings.Contains(src, "addons.mozilla.org"):
		mt := regexp.MustCompile(`/addon/([^/?#]+)`).FindStringSubmatch(src)
		if mt == nil {
			done("", fmt.Errorf("not an add-on page: %s", src))
			return
		}
		m.installDownloaded("https://addons.mozilla.org/firefox/downloads/latest/"+mt[1]+"/addon-latest.xpi", ".xpi", mt[1], done)
	case strings.Contains(src, "chromewebstore.google.com") || strings.Contains(src, "chrome.google.com") || chromeIDRe.FindString(src) == src:
		id := chromeIDRe.FindString(src)
		if id == "" {
			done("", fmt.Errorf("no extension id in %s", src))
			return
		}
		m.installDownloaded(chromeStoreDownloadURL(id), ".crx", id, done)
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		ext := strings.ToLower(filepath.Ext(strings.SplitN(src, "?", 2)[0]))
		if ext != ".crx" && ext != ".xpi" && ext != ".zip" {
			done("", fmt.Errorf("unsupported url: %s", src))
			return
		}
		m.installDownloaded(src, ext, randomHex(4), done)
	default:
		done("", fmt.Errorf("don't know how to install %q", src))
	}
}

func (m *ExtensionManager) installDownloaded(dl, ext, id string, done func(string, error)) {
	tmp := filepath.Join(cacheDir(), "ext-install-"+sanitizeID(id)+ext)
	go func() {
		err := downloadFile(dl, tmp)
		coreglib.IdleAdd(func() {
			if err != nil {
				done("", err)
				return
			}
			name, err := m.InstallFromFile(tmp, id)
			os.Remove(tmp)
			done(name, err)
		})
	}()
}

// HandleStoreMessage serves the buttons the store helper adds to store pages:
// install, remove, and asking whether an extension is already installed.
func (m *ExtensionManager) HandleStoreMessage(body string, view *webkit.WebView) {
	var req struct {
		Op    string `json:"op"`
		Store string `json:"store"`
		ExtID string `json:"extId"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(body), &req) != nil || req.ExtID == "" {
		return
	}
	state := func(installed bool) {
		m.evalIn(view, "", "window.__fastyStoreState&&window.__fastyStoreState("+jsq(installed)+")")
	}
	switch req.Op {
	case "status":
		state(m.find(sanitizeID(req.ExtID)) != nil)
	case "remove":
		id := sanitizeID(req.ExtID)
		if ext := m.find(id); ext != nil {
			m.Remove(id)
			if m.b != nil {
				m.b.rebuildExtensionButtons()
				m.b.toast("Removed "+ext.Manifest.Name, 2)
			}
		}
		state(false)
	default:
		m.HandleStoreInstall(body, nil, func(name string, err error) {
			if err != nil {
				if m.b != nil {
					m.b.toast("Extension install failed: "+err.Error(), 4)
				}
				state(false)
				return
			}
			if m.b != nil {
				m.b.rebuildExtensionButtons()
				m.b.toast("Installed \""+name+"\"", 3)
			}
			state(true)
		})
	}
}

func (m *ExtensionManager) findByRealID(id string) *Extension {
	if e := m.find(sanitizeID(id)); e != nil {
		return e
	}
	for _, e := range m.extensions {
		if e.realID() == id {
			return e
		}
	}
	return nil
}

// externallyConnectable lists, per extension, the pages that may message it.
func (m *ExtensionManager) injectWebConnect(ucm *webkit.UserContentManager) {
	for _, ext := range m.enabledList() {
		if ext.Manifest.Externally == nil || len(ext.Manifest.Externally.Matches) == 0 {
			continue
		}
		ucm.AddScript(webkit.NewUserScript(webConnectJS, webkit.UserContentInjectTopFrame,
			webkit.UserScriptInjectAtDocumentStart, matchPatternsToAllowList(ext.Manifest.Externally.Matches), nil))
		break
	}
}

const webConnectJS = `(function(){
  if (window.__fastyWebConnect) return; window.__fastyWebConnect = true;
  var pend = {}, seq = 0;
  window.__fastyExtReply = function (n, data, err) {
    var f = pend[n]; if (!f) return; delete pend[n]; f(data === null ? undefined : data, err || '');
  };
  var chromeObj = window.chrome || (window.chrome = {});
  var rt = chromeObj.runtime || (chromeObj.runtime = {});
  rt.sendMessage = function (extId, message, opts, cb) {
    if (typeof opts === 'function') { cb = opts; }
    var n = ++seq;
    var p = new Promise(function (resolve, reject) {
      pend[n] = function (d, e) {
        if (cb) { rt.lastError = e ? { message: e } : null; try { cb(d); } finally { rt.lastError = null; } resolve(d); }
        else if (e) reject(new Error(e)); else resolve(d);
      };
    });
    window.webkit.messageHandlers.officeVeil.postMessage({ _ch: 'extExt', extId: String(extId), message: message, cbId: n });
    return cb ? undefined : p;
  };
})();`

// HandleWebExternal serves runtime.sendMessage(extensionId, ...) from a web
// page the extension lists under externally_connectable.
func (m *ExtensionManager) HandleWebExternal(body string, t *Tab) {
	var req struct {
		ExtID   string          `json:"extId"`
		Message json.RawMessage `json:"message"`
		CbID    int             `json:"cbId"`
	}
	if json.Unmarshal([]byte(body), &req) != nil || t.view == nil {
		return
	}
	reply := func(d json.RawMessage, e string) {
		if len(d) == 0 {
			d = json.RawMessage("null")
		}
		m.evalIn(t.view, "", fmt.Sprintf("window.__fastyExtReply&&window.__fastyExtReply(%d,%s,%s)", req.CbID, string(d), jsq(e)))
	}
	ext := m.findByRealID(req.ExtID)
	allowed := false
	if ext != nil && ext.Enabled && ext.Manifest.Externally != nil {
		for _, p := range ext.Manifest.Externally.Matches {
			if re, err := regexp.Compile(matchPatternToRegex(p)); err == nil && re.MatchString(t.url) {
				allowed = true
			}
		}
	}
	if !allowed {
		reply(nil, "Could not establish connection. Receiving end does not exist.")
		return
	}
	sender := map[string]any{"url": t.url, "tab": m.tabInfo(t), "frameId": 0, "__external": true}
	if u, err := url.Parse(t.url); err == nil && u.Host != "" {
		sender["origin"] = u.Scheme + "://" + u.Host
	}
	m.route(m.bgEP(ext), req.Message, sender, reply)
}

func (e *Extension) popupPath() string {
	if e.popupOverride != "" {
		return e.popupOverride
	}
	if a := e.action(); a != nil {
		return a.DefaultPopup
	}
	return ""
}

// badgeColorCSS accepts [r,g,b,a] or a CSS colour string.
func badgeColorCSS(raw json.RawMessage) string {
	var arr []float64
	if json.Unmarshal(raw, &arr) == nil && len(arr) >= 3 {
		a := 1.0
		if len(arr) > 3 {
			a = arr[3] / 255
		}
		return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", int(arr[0]), int(arr[1]), int(arr[2]), a)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// iconPathOf takes a path string or a {size: path} map and returns one path.
func iconPathOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		for _, k := range []string{"32", "38", "24", "19", "16", "48", "128"} {
			if p, ok := m[k]; ok {
				return p
			}
		}
		for _, p := range m {
			return p
		}
	}
	return ""
}

// localizedManifest is the manifest as runtime.getManifest() returns it: with
// every "__MSG_key__" replaced by the extension's own text.
func (e *Extension) localizedManifest() json.RawMessage {
	var v any
	if json.Unmarshal(e.raw, &v) != nil {
		return json.RawMessage("{}")
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case string:
			return e.localize(t)
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case map[string]any:
			for k := range t {
				t[k] = walk(t[k])
			}
		}
		return x
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}
