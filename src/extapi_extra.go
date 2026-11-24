// The rest of the chrome.* API for extensions: management, bookmarks, history,
// cookies, downloads, identity, tab capture and others.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-webkitgtk/pkg/soup/v3"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// --- everything else ---------------------------------------------------------------

func (m *ExtensionManager) targetTab(src extSource, id *int) *Tab {
	if id != nil {
		return m.tabByNum(*id)
	}
	if src.tab != nil {
		return src.tab
	}
	return m.b.current
}

func cookieMap(c *soup.Cookie) map[string]any {
	d := map[string]any{
		"name": c.Name(), "value": c.Value(), "domain": c.Domain(), "path": c.Path(),
		"secure": c.Secure(), "httpOnly": c.HTTPOnly(), "hostOnly": !strings.HasPrefix(c.Domain(), "."),
		"sameSite": "unspecified", "storeId": "0", "session": true,
	}
	if e := c.Expires(); e != nil {
		d["session"] = false
		d["expirationDate"] = float64(e.ToUnix())
	}
	return d
}

func (m *ExtensionManager) cookiesFor(uris []string, done func([]*soup.Cookie)) {
	cm := m.engine.session.CookieManager()
	var all []*soup.Cookie
	var next func(i int)
	next = func(i int) {
		if i >= len(uris) {
			done(all)
			return
		}
		cm.Cookies(context.Background(), uris[i], func(res gio.AsyncResulter) {
			cs, err := cm.CookiesFinish(res)
			if err == nil {
				all = append(all, cs...)
			}
			next(i + 1)
		})
	}
	next(0)
}

func (m *ExtensionManager) handleExtra(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) bool {
	switch {
	case strings.HasPrefix(typ, "dnr."):
		m.handleDNR(ext, typ, payload, done)
		return true
	case strings.HasPrefix(typ, "native."):
		m.handleNative(ext, src, typ, payload, done)
		return true
	case typ == "commands.getAll":
		out := []map[string]any{}
		for name, c := range ext.Manifest.Commands {
			out = append(out, map[string]any{"name": name, "description": c.Description, "shortcut": ext.shortcutFor(c.SuggestedKey)})
		}
		done(out, "")
		return true
	case typ == "net.fetch":
		m.handleFetch(ext, payload, done)
		return true
	case strings.HasPrefix(typ, "menus."):
		m.handleMenus(ext, typ, payload)
		done(nil, "")
		return true
	}
	if m.b == nil {
		done(nil, "no browser window")
		return true
	}

	switch typ {
	case "scripting.register", "scripting.update", "userscripts.register", "userscripts.update":
		var p struct {
			Scripts []dynScript `json:"scripts"`
		}
		json.Unmarshal(payload, &p)
		user := strings.HasPrefix(typ, "userscripts.")
		update := strings.HasSuffix(typ, ".update")
		for _, sc := range p.Scripts {
			sc.User = user
			if sc.ID == "" {
				done(nil, "Script ID must be a non-empty string.")
				return true
			}
			idx := -1
			for i, d := range ext.dyn {
				if d.ID == sc.ID {
					idx = i
				}
			}
			if !update && idx >= 0 {
				done(nil, "Duplicate script ID '"+sc.ID+"'")
				return true
			}
			if idx >= 0 {
				old := ext.dyn[idx]
				if len(sc.Matches) == 0 {
					sc.Matches = old.Matches
				}
				if len(sc.JS) == 0 {
					sc.JS = old.JS
				}
				if len(sc.CSS) == 0 {
					sc.CSS = old.CSS
				}
				ext.dyn[idx] = sc
			} else {
				ext.dyn = append(ext.dyn, sc)
			}
			defs := []ContentScriptDef{sc.def()}
			for _, t := range m.b.tabs {
				if t.ucm != nil {
					m.injectContentScripts(t.ucm, ext, extWorld(ext.ID), defs)
					m.injectStyles(t.ucm, ext, defs)
				}
			}
		}
		m.saveDyn(ext)
		done(nil, "")
	case "scripting.unregister", "userscripts.unregister":
		var p struct {
			IDs []string `json:"ids"`
		}
		json.Unmarshal(payload, &p)
		user := strings.HasPrefix(typ, "userscripts.")
		kept := ext.dyn[:0]
		for _, d := range ext.dyn {
			drop := d.User == user
			if drop && len(p.IDs) > 0 {
				drop = false
				for _, id := range p.IDs {
					if id == d.ID {
						drop = true
					}
				}
			}
			if !drop {
				kept = append(kept, d)
			}
		}
		ext.dyn = kept
		m.saveDyn(ext)
		done(nil, "")
	case "scripting.getRegistered", "userscripts.get":
		var p struct {
			IDs []string `json:"ids"`
		}
		json.Unmarshal(payload, &p)
		user := strings.HasPrefix(typ, "userscripts.")
		out := []dynScript{}
		for _, d := range ext.dyn {
			if d.User != user {
				continue
			}
			if len(p.IDs) == 0 {
				out = append(out, d)
				continue
			}
			for _, id := range p.IDs {
				if id == d.ID {
					out = append(out, d)
				}
			}
		}
		done(out, "")
	case "userscripts.configure":
		done(nil, "")

	case "tabs.move":
		var p struct {
			TabIDs []int `json:"tabIds"`
			Index  int   `json:"index"`
		}
		json.Unmarshal(payload, &p)
		var moved []any
		for i, id := range p.TabIDs {
			if t := m.tabByNum(id); t != nil && m.b.index(t) >= 0 {
				m.b.removeFromList(t)
				at := p.Index + i
				if p.Index < 0 || at > len(m.b.tabs) {
					at = len(m.b.tabs)
				}
				m.b.insert(t, at)
				moved = append(moved, m.tabInfo(t))
			}
		}
		m.b.strip.rebuild()
		m.b.saveSession()
		done(moved, "")
	case "tabs.zoom":
		var p struct {
			TabID *int     `json:"tabId"`
			Set   *float64 `json:"zoom"`
		}
		json.Unmarshal(payload, &p)
		t := m.targetTab(src, p.TabID)
		if t == nil || t.view == nil {
			done(1.0, "")
			return true
		}
		if p.Set != nil {
			if *p.Set == 0 {
				t.view.SetZoomLevel(m.b.app.zoomFor(t.url))
			} else {
				t.view.SetZoomLevel(*p.Set)
			}
		}
		done(t.view.ZoomLevel(), "")
	case "tabs.discard":
		var p struct {
			TabID *int `json:"tabId"`
		}
		json.Unmarshal(payload, &p)
		if t := m.targetTab(src, p.TabID); t != nil {
			done(m.tabInfo(t), "")
		} else {
			done(nil, "")
		}
	case "search.query":
		var p struct {
			Text        string `json:"text"`
			Disposition string `json:"disposition"`
		}
		json.Unmarshal(payload, &p)
		st := m.b.app.settings
		u := searchURL(p.Text, st.Engine, st.CustomEngine)
		if p.Disposition == "CURRENT_TAB" && m.b.current != nil {
			m.b.current.load(u)
		} else {
			m.b.openTab(u, true, nil, false, "", false)
		}
		done(nil, "")

	case "management.getAll", "management.get", "management.setEnabled", "management.uninstall":
		var p struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		json.Unmarshal(payload, &p)
		info := func(e *Extension) map[string]any {
			return map[string]any{"id": e.realID(), "name": e.Manifest.Name, "shortName": e.Manifest.Name,
				"description": e.Manifest.Description, "version": e.Manifest.Version, "enabled": e.Enabled,
				"mayDisable": true, "type": "extension", "installType": "normal",
				"permissions": e.Manifest.Permissions, "hostPermissions": e.Manifest.HostPerms,
				"optionsUrl": func() string {
					if o := e.optionsPage(); o != "" {
						return extURL(e.ID, o)
					}
					return ""
				}()}
		}
		switch typ {
		case "management.getAll":
			out := []map[string]any{}
			for _, e := range m.extensions {
				out = append(out, info(e))
			}
			done(out, "")
		case "management.get":
			if e := m.findByRealID(p.ID); e != nil {
				done(info(e), "")
			} else {
				done(nil, "Failed to find extension with id "+p.ID)
			}
		case "management.setEnabled":
			if e := m.findByRealID(p.ID); e != nil && e.Enabled != p.Enabled {
				m.Toggle(e.ID)
			}
			done(nil, "")
		case "management.uninstall":
			id := p.ID
			if id == "" {
				id = ext.ID
			}
			if e := m.findByRealID(id); e != nil {
				eid := e.ID
				coreglib.IdleAdd(func() { m.Remove(eid) })
			}
			done(nil, "")
		}

	case "offscreen.create":
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(payload, &p)
		if e := m.startOffscreen(ext, p.URL, func() { done(nil, "") }); e != "" {
			done(nil, e)
		}
	case "offscreen.close":
		m.closeOffscreen(ext)
		done(nil, "")

	case "tabcapture.start":
		var p struct {
			ID   string `json:"id"`
			Rate int    `json:"rate"`
		}
		json.Unmarshal(payload, &p)
		if p.Rate == 0 {
			p.Rate = 44100
		}
		m.startCapture(ext, src, p.ID, p.Rate)
		done(nil, "")
	case "tabcapture.stop":
		var p struct {
			ID string `json:"id"`
		}
		json.Unmarshal(payload, &p)
		m.stopCapture(ext.ID + ":" + p.ID)
		done(nil, "")

	case "tabs.capture":
		t := m.targetTab(src, nil)
		if t == nil || t.view == nil {
			done(nil, "No tab to capture.")
			return true
		}
		view := t.view
		view.Snapshot(context.Background(), webkit.SnapshotRegionVisible, webkit.SnapshotOptionsNone, func(res gio.AsyncResulter) {
			tex, err := view.SnapshotFinish(res)
			if err != nil || tex == nil {
				done(nil, "Could not capture the tab.")
				return
			}
			png := gdk.BaseTexture(tex).SaveToPNGBytes()
			done("data:image/png;base64,"+base64.StdEncoding.EncodeToString(png.Data()), "")
		})
	case "windows.create", "windows.remove", "windows.getAll", "windows.get", "windows.update":
		m.handleWindows(ext, src, typ, payload, done)
	case "identity.auth":
		var p struct {
			URL         string `json:"url"`
			Interactive bool   `json:"interactive"`
		}
		json.Unmarshal(payload, &p)
		if !p.Interactive {
			done(nil, "User interaction required.")
			return true
		}
		m.startAuth(ext, p.URL, done)
	case "tabs.goBack", "tabs.goForward", "tabs.duplicate":
		var p struct {
			TabID *int `json:"tabId"`
		}
		json.Unmarshal(payload, &p)
		t := m.targetTab(src, p.TabID)
		if t == nil {
			done(nil, "No tab with that id.")
			return true
		}
		switch typ {
		case "tabs.goBack":
			if t.view != nil {
				t.view.GoBack()
			}
		case "tabs.goForward":
			if t.view != nil {
				t.view.GoForward()
			}
		default:
			nt := m.b.openTab(t.url, true, t, t.private, "", false)
			done(m.tabInfo(nt), "")
			return true
		}
		done(nil, "")

	case "cookies.getAll", "cookies.get":
		var q struct {
			URL    string `json:"url"`
			Domain string `json:"domain"`
			Name   string `json:"name"`
			Path   string `json:"path"`
			Secure *bool  `json:"secure"`
		}
		json.Unmarshal(payload, &q)
		var uris []string
		switch {
		case q.URL != "":
			uris = []string{q.URL}
		case q.Domain != "":
			d := strings.TrimPrefix(q.Domain, ".")
			uris = []string{"https://" + d + "/", "http://" + d + "/"}
		default:
			seen := map[string]bool{}
			for _, t := range m.b.tabs {
				if u, err := url.Parse(t.url); err == nil && u.Host != "" && !seen[u.Host] {
					seen[u.Host] = true
					uris = append(uris, u.Scheme+"://"+u.Host+"/")
				}
			}
		}
		m.cookiesFor(uris, func(cs []*soup.Cookie) {
			out := []map[string]any{}
			seen := map[string]bool{}
			for _, c := range cs {
				k := c.Domain() + "|" + c.Path() + "|" + c.Name()
				if seen[k] || (q.Name != "" && c.Name() != q.Name) || (q.Path != "" && c.Path() != q.Path) ||
					(q.Secure != nil && c.Secure() != *q.Secure) ||
					(q.Domain != "" && !strings.HasSuffix("."+strings.TrimPrefix(c.Domain(), "."), "."+strings.TrimPrefix(q.Domain, "."))) {
					continue
				}
				seen[k] = true
				out = append(out, cookieMap(c))
			}
			if typ == "cookies.get" {
				if len(out) > 0 {
					done(out[0], "")
				} else {
					done(nil, "")
				}
				return
			}
			done(out, "")
		})
	case "cookies.set":
		var p struct {
			URL      string  `json:"url"`
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Secure   bool    `json:"secure"`
			HTTPOnly bool    `json:"httpOnly"`
			Expires  float64 `json:"expirationDate"`
		}
		json.Unmarshal(payload, &p)
		u, err := url.Parse(p.URL)
		if err != nil || u.Host == "" {
			done(nil, "Invalid url.")
			return true
		}
		if p.Domain == "" {
			p.Domain = u.Hostname()
		}
		if p.Path == "" {
			p.Path = "/"
		}
		maxAge := -1
		if p.Expires > 0 {
			maxAge = int(p.Expires - float64(time.Now().Unix()))
			if maxAge < 0 {
				maxAge = 0
			}
		}
		c := soup.NewCookie(p.Name, p.Value, p.Domain, p.Path, maxAge)
		c.SetSecure(p.Secure || u.Scheme == "https")
		c.SetHTTPOnly(p.HTTPOnly)
		cm := m.engine.session.CookieManager()
		cm.AddCookie(context.Background(), c, func(res gio.AsyncResulter) {
			if err := cm.AddCookieFinish(res); err != nil {
				done(nil, err.Error())
				return
			}
			done(cookieMap(c), "")
		})
	case "cookies.remove":
		var p struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		json.Unmarshal(payload, &p)
		m.cookiesFor([]string{p.URL}, func(cs []*soup.Cookie) {
			cm := m.engine.session.CookieManager()
			for _, c := range cs {
				if c.Name() == p.Name {
					cm.DeleteCookie(context.Background(), c, nil)
					done(map[string]any{"url": p.URL, "name": p.Name, "storeId": "0"}, "")
					return
				}
			}
			done(nil, "")
		})

	case "history.search":
		var p struct {
			Text       string  `json:"text"`
			MaxResults int     `json:"maxResults"`
			Start      float64 `json:"startTime"`
			End        float64 `json:"endTime"`
		}
		json.Unmarshal(payload, &p)
		if p.MaxResults == 0 {
			p.MaxResults = 100
		}
		out := []map[string]any{}
		for _, v := range m.b.app.history.Search(p.Text, 5000) {
			ms := v.Last * 1000
			if (p.Start > 0 && ms < p.Start) || (p.End > 0 && ms > p.End) {
				continue
			}
			out = append(out, map[string]any{"id": v.URL, "url": v.URL, "title": v.Title,
				"lastVisitTime": ms, "visitCount": v.Visits, "typedCount": 0})
			if len(out) >= p.MaxResults {
				break
			}
		}
		done(out, "")
	case "history.deleteUrl":
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(payload, &p)
		m.b.app.history.Remove(p.URL)
		done(nil, "")
	case "history.deleteAll":
		m.b.app.history.Clear(0)
		done(nil, "")
	case "history.addUrl":
		var p struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		}
		json.Unmarshal(payload, &p)
		m.b.app.history.Visit(p.URL, p.Title)
		done(nil, "")

	case "bookmarks.tree", "bookmarks.search", "bookmarks.create", "bookmarks.remove":
		bm := m.b.app.bookmarks
		node := func(b Bookmark) map[string]any {
			return map[string]any{"id": b.URL, "parentId": "1", "title": b.Title, "url": b.URL, "dateAdded": b.Date * 1000}
		}
		switch typ {
		case "bookmarks.tree":
			kids := []map[string]any{}
			for _, b := range bm.list {
				kids = append(kids, node(b))
			}
			done([]map[string]any{{"id": "0", "title": "", "children": []map[string]any{
				{"id": "1", "parentId": "0", "title": "Bookmarks", "children": kids}}}}, "")
		case "bookmarks.search":
			var p struct {
				Query string `json:"query"`
				URL   string `json:"url"`
			}
			json.Unmarshal(payload, &p)
			out := []map[string]any{}
			for _, b := range bm.Search(p.Query) {
				if p.URL == "" || b.URL == p.URL {
					out = append(out, node(b))
				}
			}
			done(out, "")
		case "bookmarks.create":
			var p struct{ URL, Title string }
			json.Unmarshal(payload, &p)
			bm.Add(p.URL, p.Title)
			done(node(Bookmark{URL: p.URL, Title: p.Title, Date: float64(time.Now().Unix())}), "")
		case "bookmarks.remove":
			var p struct{ ID string }
			json.Unmarshal(payload, &p)
			bm.Remove(p.ID)
			done(nil, "")
		}

	case "downloads.download":
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(payload, &p)
		var view *webkit.WebView
		if ext.bgView != nil {
			view = ext.bgView
		} else if m.b.current != nil {
			view = m.b.current.view
		}
		if view == nil || p.URL == "" {
			done(nil, "Could not start the download.")
			return true
		}
		view.DownloadURI(p.URL)
		m.reqSeq++
		done(m.reqSeq, "")

	case "topsites.get":
		out := []map[string]any{}
		for _, v := range m.b.app.history.Search("", 200) {
			if len(out) >= 10 {
				break
			}
			out = append(out, map[string]any{"url": v.URL, "title": v.Title})
		}
		done(out, "")

	default:
		return false
	}
	return true
}

// chromeInternal sends chrome://, about: and similar pages to the matching
// Fasty page and reports whether it did.
func (m *ExtensionManager) chromeInternal(u string) bool {
	l := strings.ToLower(u)
	switch {
	case strings.HasPrefix(l, "chrome://extensions"), strings.HasPrefix(l, "about:addons"),
		strings.HasPrefix(l, "edge://extensions"), strings.HasPrefix(l, "brave://extensions"):
		showExtensionsPanel(m.b)
		return true
	case strings.HasPrefix(l, "chrome://newtab"), strings.HasPrefix(l, "chrome://new-tab-page"):
		m.b.newTab(false)
		return true
	}
	return false
}
