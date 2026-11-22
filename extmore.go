package main

// The rest of the extension APIs that need the browser: dynamically
// registered content scripts, context menus, offscreen documents, tab audio
// capture, screenshots, and cookies, history, bookmarks and downloads.

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	"github.com/diamondburned/gotk4-webkitgtk/pkg/soup/v3"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// --- dynamic content scripts -------------------------------------------------

// dynFile is a script or style given as a file path or inline code.
type dynFile struct{ File, Code string }

func (d *dynFile) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		d.File = s
		return nil
	}
	var o struct {
		File string `json:"file"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	d.File, d.Code = o.File, o.Code
	return nil
}

func (d dynFile) MarshalJSON() ([]byte, error) {
	if d.Code != "" {
		return json.Marshal(map[string]string{"code": d.Code})
	}
	return json.Marshal(d.File)
}

type dynScript struct {
	ID             string    `json:"id"`
	Matches        []string  `json:"matches"`
	ExcludeMatches []string  `json:"excludeMatches"`
	JS             []dynFile `json:"js"`
	CSS            []dynFile `json:"css"`
	RunAt          string    `json:"runAt"`
	AllFrames      bool      `json:"allFrames"`
	World          string    `json:"world"`
	Persist        *bool     `json:"persistAcrossSessions"`
	User           bool      `json:"userScript,omitempty"`
}

func (d dynScript) def() ContentScriptDef {
	c := ContentScriptDef{ID: d.ID, Matches: d.Matches, ExcludeM: d.ExcludeMatches,
		RunAt: d.RunAt, AllFrames: d.AllFrames, World: d.World, UserScript: d.User && !strings.EqualFold(d.World, "MAIN")}
	for _, f := range d.JS {
		if f.Code != "" {
			c.JSCode = append(c.JSCode, f.Code)
		} else if f.File != "" {
			c.JS = append(c.JS, f.File)
		}
	}
	for _, f := range d.CSS {
		if f.Code != "" {
			c.CSSCode = append(c.CSSCode, f.Code)
		} else if f.File != "" {
			c.CSS = append(c.CSS, f.File)
		}
	}
	return c
}

func (e *Extension) contentDefs() []ContentScriptDef {
	defs := append([]ContentScriptDef{}, e.Manifest.ContentScripts...)
	for _, d := range e.dyn {
		defs = append(defs, d.def())
	}
	return defs
}

func (m *ExtensionManager) loadDyn(ext *Extension) {
	data, err := os.ReadFile(filepath.Join(ext.Dir, ".dynscripts.json"))
	if err == nil {
		json.Unmarshal(data, &ext.dyn)
	}
}

func (m *ExtensionManager) saveDyn(ext *Extension) {
	keep := []dynScript{}
	for _, d := range ext.dyn {
		if d.Persist == nil || *d.Persist {
			keep = append(keep, d)
		}
	}
	data, _ := json.Marshal(keep)
	os.WriteFile(filepath.Join(ext.Dir, ".dynscripts.json"), data, 0o644)
}

// --- context menus -------------------------------------------------------------

type extMenu struct {
	RawID    json.RawMessage `json:"id"`
	Title    string          `json:"title"`
	Contexts []string        `json:"contexts"`
	Parent   json.RawMessage `json:"parentId"`
	Type     string          `json:"type"`
	Visible  *bool           `json:"visible"`
	Enabled  *bool           `json:"enabled"`
}

func (mn *extMenu) key() string        { return strings.Trim(string(mn.RawID), `"`) }
func (mn *extMenu) parentKey() string  { return strings.Trim(string(mn.Parent), `"`) }
func (mn *extMenu) shown(c map[string]bool) bool {
	if mn.Visible != nil && !*mn.Visible {
		return false
	}
	if len(mn.Contexts) == 0 {
		return c["page"]
	}
	for _, x := range mn.Contexts {
		if c[x] {
			return true
		}
	}
	return false
}

func (m *ExtensionManager) handleMenus(ext *Extension, typ string, payload json.RawMessage) {
	switch typ {
	case "menus.create":
		var mn extMenu
		if json.Unmarshal(payload, &mn) == nil && len(mn.RawID) > 0 {
			for i, old := range ext.menus {
				if old.key() == mn.key() {
					ext.menus = append(ext.menus[:i], ext.menus[i+1:]...)
					break
				}
			}
			ext.menus = append(ext.menus, mn)
		}
	case "menus.update":
		var p struct {
			ID    json.RawMessage `json:"id"`
			Props extMenu         `json:"props"`
		}
		json.Unmarshal(payload, &p)
		id := strings.Trim(string(p.ID), `"`)
		var raw map[string]json.RawMessage
		var props struct {
			Props map[string]json.RawMessage `json:"props"`
		}
		json.Unmarshal(payload, &props)
		raw = props.Props
		for i := range ext.menus {
			if ext.menus[i].key() != id {
				continue
			}
			if v, ok := raw["title"]; ok {
				json.Unmarshal(v, &ext.menus[i].Title)
			}
			if v, ok := raw["contexts"]; ok {
				json.Unmarshal(v, &ext.menus[i].Contexts)
			}
			if v, ok := raw["visible"]; ok {
				json.Unmarshal(v, &ext.menus[i].Visible)
			}
			if v, ok := raw["enabled"]; ok {
				json.Unmarshal(v, &ext.menus[i].Enabled)
			}
		}
	case "menus.remove":
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		json.Unmarshal(payload, &p)
		id := strings.Trim(string(p.ID), `"`)
		kept := ext.menus[:0]
		for _, mn := range ext.menus {
			if mn.key() != id && mn.parentKey() != id {
				kept = append(kept, mn)
			}
		}
		ext.menus = kept
	case "menus.removeAll":
		ext.menus = nil
	}
}

// AppendMenus adds the enabled extensions' context-menu items to a tab's menu.
func (m *ExtensionManager) AppendMenus(t *Tab, menu *webkit.ContextMenu, hit *webkit.HitTestResult) {
	ctx := map[string]bool{"all": true, "page": true}
	info := map[string]any{"pageUrl": t.url, "frameUrl": t.url}
	if hit.ContextIsLink() {
		ctx["link"] = true
		info["linkUrl"] = hit.LinkURI()
	}
	if hit.ContextIsImage() {
		ctx["image"] = true
		info["srcUrl"] = hit.ImageURI()
		info["mediaType"] = "image"
	}
	if hit.ContextIsMedia() {
		ctx["video"], ctx["audio"] = true, true
		info["srcUrl"] = hit.MediaURI()
	}
	if hit.ContextIsSelection() {
		ctx["selection"] = true
	}
	if hit.ContextIsEditable() {
		ctx["editable"] = true
		info["editable"] = true
	}

	first := true
	for _, ext := range m.enabledList() {
		var top []*extMenu
		kids := map[string][]*extMenu{}
		for i := range ext.menus {
			mn := &ext.menus[i]
			if !mn.shown(ctx) {
				continue
			}
			if p := mn.parentKey(); p != "" && p != "null" {
				kids[p] = append(kids[p], mn)
			} else {
				top = append(top, mn)
			}
		}
		if len(top) == 0 {
			continue
		}
		if first {
			menu.Append(webkit.NewContextMenuItemSeparator())
			first = false
		}
		build := func(mn *extMenu) *webkit.ContextMenuItem {
			if mn.Type == "separator" {
				return webkit.NewContextMenuItemSeparator()
			}
			title := strings.ReplaceAll(mn.Title, "%s", "…")
			if sub := kids[mn.key()]; len(sub) > 0 {
				sm := webkit.NewContextMenu()
				for _, k := range sub {
					sm.Append(m.menuItem(ext, k, t, info, nil))
				}
				return webkit.NewContextMenuItemWithSubmenu(title, sm)
			}
			return m.menuItem(ext, mn, t, info, kids)
		}
		if len(top) == 1 {
			menu.Append(build(top[0]))
			continue
		}
		sm := webkit.NewContextMenu()
		for _, mn := range top {
			sm.Append(build(mn))
		}
		menu.Append(webkit.NewContextMenuItemWithSubmenu(ext.Manifest.Name, sm))
	}
}

func (m *ExtensionManager) menuItem(ext *Extension, mn *extMenu, t *Tab, info map[string]any, _ map[string][]*extMenu) *webkit.ContextMenuItem {
	m.reqSeq++
	ga := gio.NewSimpleAction(fmt.Sprintf("ctx-ext-%d", m.reqSeq), nil)
	id := mn.RawID
	ga.SetEnabled(mn.Enabled == nil || *mn.Enabled)
	ga.ConnectActivate(func(*glib.Variant) {
		click := map[string]any{"menuItemId": id}
		for k, v := range info {
			click[k] = v
		}
		fire := func(sel string) {
			if sel != "" {
				click["selectionText"] = sel
			}
			m.fireTo(ext, "contextMenus.onClicked", click, m.tabInfo(t))
			m.fireTo(ext, "menus.onClicked", click, m.tabInfo(t))
		}
		if t.view != nil {
			t.run("window.getSelection().toString()", fire)
		} else {
			fire("")
		}
	})
	return webkit.NewContextMenuItemFromGaction(ga, strings.ReplaceAll(mn.Title, "%s", "…"), nil)
}

// --- offscreen documents ---------------------------------------------------------

func (m *ExtensionManager) startOffscreen(ext *Extension, page string, loaded func()) string {
	if ext.offscreen != nil {
		return "Only a single offscreen document may be created."
	}
	ucm := webkit.NewUserContentManager()
	ucm.RegisterScriptMessageHandler("officeVeil", "")
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "page"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	view := m.engine.newView(m.engine.session, ucm)
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		m.HandleMessage(v.ToJson(0), extSource{view: view})
	})
	view.SetVisible(false)
	ext.offscreen, ext.offscreenUCM = view, ucm
	once := false
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		if ev == webkit.LoadFinished && !once {
			once = true
			loaded()
		}
	})
	view.LoadURI(extURL(ext.ID, page))
	return ""
}

func (m *ExtensionManager) closeOffscreen(ext *Extension) {
	if ext.offscreen != nil {
		m.evalIn(ext.offscreen, "", "window.__fastyDead=true")
		m.retired = append(m.retired, ext.offscreen, ext.offscreenUCM)
		ext.offscreen, ext.offscreenUCM = nil, nil
	}
}

// --- tab audio capture -------------------------------------------------------------

// tabCapture hands the extension the system's output audio (what the speakers
// play), read from the PipeWire/PulseAudio monitor and streamed to the page as
// float32 PCM. WebKit has no per-tab audio tap.
func (m *ExtensionManager) startCapture(ext *Extension, src extSource, id string, rate int) {
	key := ext.ID + ":" + id
	m.stopCapture(key)
	sink := "@DEFAULT_SINK@"
	if out, err := exec.Command("pactl", "get-default-sink").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			sink = s
		}
	}
	cmd := exec.Command("parec", "--raw", "--format=float32le", fmt.Sprintf("--rate=%d", rate),
		"--channels=1", "--latency-msec=40", "-d", sink+".monitor")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	m.captures[key] = cmd
	view, world := src.view, src.world(ext.ID)
	go func() {
		r := bufio.NewReaderSize(stdout, 1<<16)
		buf := make([]byte, 4096*4)
		deadline := time.Now().Add(20 * time.Minute)
		for time.Now().Before(deadline) {
			n, err := readFull(r, buf)
			if n > 0 {
				chunk := base64.StdEncoding.EncodeToString(buf[:n])
				coreglib.IdleAdd(func() {
					m.evalIn(view, world, "window.__fastyAudio&&window.__fastyAudio("+jsq(id)+","+jsq(chunk)+")")
				})
			}
			if err != nil {
				break
			}
		}
		coreglib.IdleAdd(func() { m.stopCapture(key) })
	}()
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		k, err := r.Read(buf[n:])
		n += k
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (m *ExtensionManager) stopCapture(key string) {
	if cmd := m.captures[key]; cmd != nil {
		delete(m.captures, key)
		cmd.Process.Kill()
		go cmd.Wait()
	}
}

func (m *ExtensionManager) stopAllCaptures() {
	for k := range m.captures {
		m.stopCapture(k)
	}
}

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

// --- keyboard commands -------------------------------------------------------------

type extShortcut struct {
	mods gdk.ModifierType
	key  uint
}

func parseShortcut(spec string) (extShortcut, bool) {
	var sc extShortcut
	parts := strings.Split(spec, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			switch strings.ToLower(p) {
			case "ctrl", "command", "macctrl":
				sc.mods |= gdk.ControlMask
			case "alt", "option":
				sc.mods |= gdk.AltMask
			case "shift":
				sc.mods |= gdk.ShiftMask
			default:
				return sc, false
			}
			continue
		}
		names := map[string]string{"comma": "comma", "period": "period", "space": "space", "home": "Home", "end": "End",
			"pageup": "Page_Up", "pagedown": "Page_Down", "insert": "Insert", "delete": "Delete",
			"up": "Up", "down": "Down", "left": "Left", "right": "Right"}
		name := p
		if n, ok := names[strings.ToLower(p)]; ok {
			name = n
		}
		kv := gdk.KeyvalFromName(name)
		if kv == 0 {
			kv = gdk.KeyvalFromName(strings.ToLower(name))
		}
		if kv == 0 {
			return sc, false
		}
		sc.key = gdk.KeyvalToLower(kv)
	}
	return sc, sc.key != 0 && sc.mods != 0
}

func (e *Extension) shortcutFor(spec map[string]string) string {
	for _, k := range []string{"linux", "default"} {
		if v := spec[k]; v != "" {
			return v
		}
	}
	return ""
}

// runCommand fires the extension command bound to a key press, if any.
func (m *ExtensionManager) runCommand(kv uint, mods gdk.ModifierType) bool {
	for _, ext := range m.enabledList() {
		for name, c := range ext.Manifest.Commands {
			spec := ext.shortcutFor(c.SuggestedKey)
			if spec == "" {
				continue
			}
			sc, ok := parseShortcut(spec)
			if !ok || sc.key != kv || sc.mods != mods {
				continue
			}
			switch name {
			case "_execute_action", "_execute_browser_action", "_execute_page_action":
				m.PressAction(ext)
			default:
				var tab any
				if m.b != nil && m.b.current != nil {
					tab = m.tabInfo(m.b.current)
				}
				m.fireTo(ext, "commands.onCommand", name, tab)
			}
			return true
		}
	}
	return false
}

// --- identity.launchWebAuthFlow -----------------------------------------------------

type authFlow struct {
	ext    *Extension
	prefix string
	tab    *Tab
	done   func(any, string)
}

func (m *ExtensionManager) startAuth(ext *Extension, rawURL string, done func(any, string)) {
	prefix := "https://" + authHost(ext) + ".chromiumapp.org/"
	t := m.b.openTab(rawURL, true, nil, false, "", false)
	m.auths = append(m.auths, &authFlow{ext: ext, prefix: prefix, tab: t, done: done})
}

func authHost(ext *Extension) string {
	var b strings.Builder
	for _, c := range strings.ToLower(ext.realID()) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// authRedirect ends a sign-in flow when the page is sent to the extension's
// redirect URL; the caller then cancels that navigation.
func (m *ExtensionManager) authRedirect(uri string) bool {
	if m == nil {
		return false
	}
	for i, f := range m.auths {
		if strings.HasPrefix(uri, f.prefix) {
			m.auths = append(m.auths[:i], m.auths[i+1:]...)
			tab, done := f.tab, f.done
			coreglib.IdleAdd(func() {
				m.b.closeTab(tab)
				done(uri, "")
			})
			return true
		}
	}
	return false
}

func (m *ExtensionManager) authTabClosed(t *Tab) {
	if m == nil {
		return
	}
	for i, f := range m.auths {
		if f.tab == t {
			m.auths = append(m.auths[:i], m.auths[i+1:]...)
			f.done(nil, "The user did not approve access.")
			return
		}
	}
}

// --- windows -------------------------------------------------------------------------

var windowSeq = 1

func (m *ExtensionManager) windowInfo(t *Tab, populate bool) map[string]any {
	w := map[string]any{"id": 1, "focused": true, "type": "normal", "state": "normal", "incognito": false,
		"alwaysOnTop": false, "left": 0, "top": 0, "width": 1280, "height": 800}
	if t != nil && t.win > 1 {
		w["id"], w["type"], w["focused"] = t.win, "popup", false
		if populate {
			w["tabs"] = []any{m.tabInfo(t)}
		}
		return w
	}
	if populate {
		var tabs []any
		for _, x := range m.b.tabs {
			tabs = append(tabs, m.tabInfo(x))
		}
		w["tabs"] = tabs
	}
	return w
}

func (m *ExtensionManager) openExtWindow(ext *Extension, rawURL string, width, height int) *Tab {
	b := m.b
	if width <= 0 {
		width = 500
	}
	if height <= 0 {
		height = 600
	}
	ucm := b.app.engine.contentManager()
	m.InjectExtensions(ucm)
	view := b.app.engine.newView(b.app.engine.session, ucm)
	t := newTab(b, rawURL, "", false, false)
	t.ucm = ucm
	t.wire(view)
	windowSeq++
	t.win = windowSeq

	win := gtk.NewApplicationWindow(&b.app.Application.Application)
	win.SetTitle(ext.Manifest.Name)
	win.SetIconName("fasty-browser")
	win.SetDefaultSize(width, height)
	win.AddCSSClass("popup-window")
	if b.HasCSSClass("dark") {
		win.AddCSSClass("dark")
	}
	win.SetChild(view)
	win.ConnectCloseRequest(func() bool {
		m.fire("windows.onRemoved", t.win)
		b.closeTab(t)
		return false
	})
	t.popupWin = win
	b.popups = append(b.popups, t)
	view.LoadURI(rawURL)
	win.Present()
	return t
}

func (m *ExtensionManager) handleWindows(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) {
	b := m.b
	switch typ {
	case "windows.create":
		var p struct {
			URL    json.RawMessage `json:"url"`
			Type   string          `json:"type"`
			Width  int             `json:"width"`
			Height int             `json:"height"`
		}
		json.Unmarshal(payload, &p)
		var u string
		var list []string
		if json.Unmarshal(p.URL, &u) != nil {
			json.Unmarshal(p.URL, &list)
			if len(list) > 0 {
				u = list[0]
			}
		}
		u = m.resolveURL(ext, u)
		if p.Type == "popup" || p.Type == "panel" {
			t := m.openExtWindow(ext, u, p.Width, p.Height)
			done(m.windowInfo(t, true), "")
			return
		}
		t := b.openTab(u, true, nil, false, "", false)
		done(m.windowInfo(t, true), "")
	case "windows.remove":
		var p struct {
			ID int `json:"id"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID && t.popupWin != nil {
				t.popupWin.Close()
			}
		}
		done(nil, "")
	case "windows.getAll":
		var p struct {
			Populate bool `json:"populate"`
		}
		json.Unmarshal(payload, &p)
		out := []map[string]any{m.windowInfo(nil, p.Populate)}
		for _, t := range b.popups {
			if t.win > 1 {
				out = append(out, m.windowInfo(t, p.Populate))
			}
		}
		done(out, "")
	case "windows.get":
		var p struct {
			ID       int  `json:"id"`
			Populate bool `json:"populate"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID {
				done(m.windowInfo(t, p.Populate), "")
				return
			}
		}
		done(m.windowInfo(nil, p.Populate), "")
	case "windows.update":
		var p struct {
			ID    int  `json:"id"`
			Focus bool `json:"focused"`
		}
		json.Unmarshal(payload, &p)
		for _, t := range b.popups {
			if t.win == p.ID && t.popupWin != nil && p.Focus {
				t.popupWin.Present()
			}
		}
		done(m.windowInfo(nil, false), "")
	}
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
