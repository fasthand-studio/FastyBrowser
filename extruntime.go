package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

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
