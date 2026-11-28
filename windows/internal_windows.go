//go:build windows

package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// handleRPC answers calls from the browser's own pages (ui/*.html). The page sends
// {a:"rpc", id, m, p}; the reply goes back as window.fastyReply(id, result).
func (a *app) handleRPC(s *sink, m pageMsg) {
	var res any
	var p map[string]any
	_ = json.Unmarshal(m.P, &p)
	str := func(k string) string { s, _ := p[k].(string); return s }
	switch m.M {
	case "state":
		res = map[string]any{"private": s.private, "theme": a.themeName(), "engine": a.settings.SearchEngine, "version": appVersion}
	case "settings.get":
		res = a.settings
	case "settings.set":
		next := a.settings
		if json.Unmarshal(m.P, &next) == nil {
			a.setSetting(func(s *Settings) { *s = next })
		}
		res = a.settings
	case "bookmarks.list":
		res = a.bookmarks.Items
	case "bookmarks.remove":
		a.bookmarks.Remove(str("url"))
		a.bookmarks.Flush()
		a.syncChrome()
		res = a.bookmarks.Items
	case "bookmarks.add":
		if u := str("url"); IsAddress(u) && !a.bookmarks.Has(u) {
			a.bookmarks.Toggle(u, str("title"))
			a.bookmarks.Flush()
		}
		res = a.bookmarks.Items
	case "history.list":
		q := strings.ToLower(str("q"))
		var out []HistoryEntry
		for _, it := range a.history.Items {
			if q == "" || strings.Contains(strings.ToLower(it.Title), q) || strings.Contains(strings.ToLower(it.URL), q) {
				out = append(out, it)
				if len(out) >= 500 {
					break
				}
			}
		}
		res = out
	case "history.remove":
		a.history.Remove(str("url"))
		a.history.Flush()
		res = true
	case "history.clear":
		a.history.Clear()
		a.history.Flush()
		res = true
	case "suggest":
		res = a.suggestions(str("q"), str("mode"))
	case "nav":
		a.rpcNav(s, str("v"), str("mode"), p["private"] == true)
		return // the tab or overlay that asked is done
	case "open":
		if u := str("url"); IsAddress(u) {
			a.newTab(kindPage, u, s.private)
		}
		res = true
	case "voice.start":
		a.startVoice(s, str("lang"))
		res = true
	case "voice.stop":
		a.stopVoice()
		res = true
	case "copy":
		clipboardSet(str("text"))
		res = true
	case "auth.list":
		res = map[string]any{"items": a.authStore().Views(time.Now()), "error": a.authErr}
	case "auth.add":
		acc := AuthAccount{Issuer: str("issuer"), Name: str("name"), Secret: str("secret")}
		var err error
		if uri := str("uri"); uri != "" {
			acc, err = ParseOTPAuth(uri)
		}
		if err == nil {
			_, err = a.authStore().Add(acc)
		}
		if err != nil {
			res = map[string]any{"error": err.Error()}
		} else {
			res = map[string]any{"ok": true}
		}
	case "auth.remove":
		_ = a.authStore().Remove(str("id"))
		res = true
	case "selftest.flag":
		a.selfTestFlag(str("k"), str("v"))
		res = true
	case "openpage":
		a.hideOverlay()
		a.openInternal(str("name"), true)
		res = true
	case "app.state":
		res = map[string]any{"version": appVersion, "downloads": downloadsDir(), "engines": EngineNames, "paused": a.settings.ShieldPaused, "hidden": a.hiddenSummary(), "host": a.activeHost()}
	case "veils.show":
		a.curtain.ShowAll(str("host"))
		a.curtain.Flush()
		a.applyVeilsAll()
		res = a.hiddenSummary()
	case "shield.resume":
		h := str("host")
		a.setSetting(func(s *Settings) { s.ShieldPaused = removeString(s.ShieldPaused, h) })
		res = a.settings.ShieldPaused
	case "data.clear":
		a.history.Clear()
		a.history.Flush()
		_ = os.WriteFile(filepath.Join(a.dataDir, "clear-webdata"), []byte("1"), 0o600)
		res = map[string]any{"restart": true}
	case "app.restart":
		a.restart()
		res = true
	case "defaultbrowser":
		shellOpen("ms-settings:defaultapps")
		res = true
	case "bookmarks.import":
		n := a.bookmarks.AddMany(ImportBookmarksHTML(str("html")))
		a.bookmarks.Flush()
		res = map[string]any{"added": n, "total": len(a.bookmarks.Items)}
	case "downloads":
		shellOpen(downloadsDir())
		res = true
	default:
		log.Println("unknown rpc:", m.M)
		return
	}
	b, err := json.Marshal(res)
	if err != nil {
		return
	}
	id := m.ID.String()
	if id == "" {
		return
	}
	s.eval("window.fastyReply&&window.fastyReply(" + id + "," + string(b) + ")")
}

const appVersion = "1.3.0"

// ---- Reading mode (the Linux build's reader.js, run through WebView2) ---------------------------

func readFile(name string) string {
	b, err := uiFiles.ReadFile("ui/" + name)
	if err != nil {
		return ""
	}
	return string(b)
}

func (a *app) toggleReader() {
	t := a.active
	if t == nil || t.kind != kindPage || !t.ready || t.asleep {
		return
	}
	if t.reading {
		t.reading = false
		a.reloadTab(t, false)
		return
	}
	look := "light"
	if a.pal().dark {
		look = "dark"
	}
	sheet, _ := json.Marshal(readFile("reader/reader.css"))
	lk, _ := json.Marshal(look)
	body := readFile("reader/reader.js")
	// reader.js is a function body that takes `sheet` and `readerLook` and returns "read" when the
	// page looked like an article; the result is posted back so the tab can remember it.
	js := "(function(){var sheet=" + string(sheet) + ",readerLook=" + string(lk) + ",r='';" +
		"try{r=(function(){" + body + "\n})()}catch(e){r='error: '+e.message}" +
		"window.chrome.webview.postMessage(JSON.stringify({a:'reader',v:String(r)}))})()"
	t.view.Eval(js)
}

func (a *app) toast(msg string) {
	if a.chrome == nil || !a.chromeReady {
		return
	}
	b, _ := json.Marshal(msg)
	a.chrome.Eval("window.fasty&&window.fasty.toast(" + string(b) + ")")
}

// authStore opens the Authenticator's accounts on first use; accounts are encrypted with DPAPI.
func (a *app) authStore() *AuthStore {
	if a.auth == nil {
		s, err := LoadAuthStore(a.dataDir, dpapi{})
		if err != nil {
			a.authErr = err.Error()
		}
		a.auth = s
	}
	return a.auth
}

func removeString(list []string, s string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// siteHost is a page's host without "www.", used for per-site settings.
func siteHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func (a *app) activeHost() string {
	if t := a.active; t != nil && t.kind == kindPage {
		return siteHost(t.url)
	}
	return ""
}

type hiddenSite struct {
	Host  string `json:"host"`
	Count int    `json:"count"`
	Items []Veil `json:"items"`
}

func (a *app) hiddenSummary() []hiddenSite {
	var out []hiddenSite
	for _, h := range a.curtain.Hosts() {
		out = append(out, hiddenSite{Host: h, Count: len(a.curtain.Veils(h)), Items: a.curtain.Veils(h)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// restart starts a new copy of the browser and closes this one (used after clearing browsing data).
func (a *app) restart() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	a.saveSession()
	a.flushStores()
	if err := exec.Command(exe).Start(); err != nil {
		log.Println("restart:", err)
		return
	}
	procPostMessageW.Call(a.hwnd, wmClose, 0, 0)
}

// pauseShieldHere turns Shield off or on for the current site.
func (a *app) pauseShieldHere() {
	h := a.activeHost()
	if h == "" {
		return
	}
	paused := false
	a.setSetting(func(s *Settings) {
		for _, x := range s.ShieldPaused {
			if x == h {
				s.ShieldPaused = removeString(s.ShieldPaused, h)
				return
			}
		}
		s.ShieldPaused = append(s.ShieldPaused, h)
		paused = true
	})
	if paused {
		a.toast("Shield is off on " + h + ". Reload the page.")
	} else {
		a.toast("Shield is back on for " + h + ". Reload the page.")
	}
}

func (a *app) shieldPausedFor(t *tab) bool {
	h := siteHost(t.url)
	for _, x := range a.settings.ShieldPaused {
		if x == h {
			return true
		}
	}
	return false
}

// applyVeils (re)hides a site's chosen elements in a page and applies the default page zoom.
func (a *app) applyVeils(t *tab) {
	if t == nil || t.kind != kindPage || !t.ready || t.asleep {
		return
	}
	css := a.curtain.CSS(siteHost(t.url))
	b, _ := json.Marshal(css)
	t.view.Eval("(function(){var s=document.getElementById('fasty-veil');if(!s){s=document.createElement('style');s.id='fasty-veil';(document.head||document.documentElement).appendChild(s)}s.textContent=" + string(b) + "})()")
	if z := a.settings.PageZoom; z != 100 {
		t.view.Eval("document.documentElement.style.zoom='" + strconv.FormatFloat(float64(z)/100, 'f', 2, 64) + "'")
	}
}

func (a *app) applyVeilsAll() {
	for _, t := range a.tabs {
		a.applyVeils(t)
	}
}
