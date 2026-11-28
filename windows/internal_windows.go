//go:build windows

package main

import (
	"encoding/json"
	"log"
	"strings"
)

// handleRPC answers calls from the browser's own pages (ui/*.html). The page sends
// {a:"rpc", id, m, p}; the reply goes back as window.fastyReply(id, result).
func (a *app) handleRPC(t *tab, m pageMsg) {
	var res any
	var p map[string]any
	_ = json.Unmarshal(m.P, &p)
	str := func(k string) string { s, _ := p[k].(string); return s }
	switch m.M {
	case "state":
		res = map[string]any{"private": t.private, "theme": a.themeName(), "engine": a.settings.SearchEngine, "version": appVersion}
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
		res = a.suggestions(str("q"), "")
	case "nav":
		a.navigate(t, str("v"))
		return // this tab is replaced by the page
	case "open":
		if u := str("url"); IsAddress(u) {
			a.newTab(kindPage, u, t.private)
		}
		res = true
	case "voice.start":
		a.startVoice(t, str("lang"))
		res = true
	case "voice.stop":
		a.stopVoice()
		res = true
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
	t.view.Eval("window.fastyReply&&window.fastyReply(" + id + "," + string(b) + ")")
}

const appVersion = "1.2.0"

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
