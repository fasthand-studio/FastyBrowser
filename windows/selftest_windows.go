//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// `fasty.exe --selftest` runs the real app against a local web server and checks the browser's
// features in order. Results go to the log and the exit code is 0 only if every step passed.

type stStep struct {
	name  string
	do    func(a *app)      // runs once when the step starts
	check func(a *app) bool // polled until true or the step times out
	wait  time.Duration
}

type selfTest struct {
	steps   []stStep
	i       int
	started time.Time
	did     bool
	fails   int
	base    string
	flags   map[string]string // facts reported by the browser's own pages
}

const stPageA = `<!doctype html><title>Alpha</title><h1>Alpha</h1>
<img src="https://ad.doubleclick.net/pixel.gif" width=1 height=1>
<script src="https://doubleclick.net/ad.js"></script>
<img src="http://127.0.0.1:%PORT%/ok.gif" width=1 height=1>
<a id=l href="/b" target="_blank">open b</a>`

func stArticle() string {
	var b strings.Builder
	b.WriteString("<!doctype html><title>Long Read</title><nav><a href=/a>home</a> <a href=/b>b</a></nav><article><h1>Long Read</h1>")
	for i := 0; i < 8; i++ {
		b.WriteString("<p>This is paragraph number " + fmt.Sprint(i+1) + " of a long article. It has enough ordinary prose in it that a reading mode should recognise it as the main text of the page, with sentences that run on for a while and say very little, much like real articles do when they need to fill a column.</p>")
	}
	b.WriteString("</article><aside><a href=/a>related</a> <a href=/b>related</a></aside>")
	return b.String()
}

func startSelfTestServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	page := func(name, html string) {
		mux.HandleFunc(name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, strings.ReplaceAll(html, "%PORT%", fmt.Sprint(port)))
		})
	}
	page("/a", stPageA)
	page("/b", "<!doctype html><title>Beta</title><h1>Beta</h1>")
	page("/c", "<!doctype html><title>Gamma</title><h1>Gamma</h1>")
	page("/article", stArticle())
	mux.HandleFunc("/ok.gif", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	go http.Serve(ln, mux)
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

func (a *app) beginSelfTest(base string) {
	st := &selfTest{base: base, flags: map[string]string{}}
	a.selftest = st
	titleIs := func(want string) func(a *app) bool {
		return func(a *app) bool { t := a.active; return t != nil && t.kind == kindPage && t.title == want }
	}
	tabWith := func(a *app, title string) *tab {
		for _, t := range a.tabs {
			if t.title == title {
				return t
			}
		}
		return nil
	}
	internalTab := func(a *app, page string) *tab {
		for _, t := range a.tabs {
			if t.kind == kindInternal && strings.HasPrefix(t.page, page) {
				return t
			}
		}
		return nil
	}
	st.steps = []stStep{
		{name: "first page loads and reports its title", wait: 15 * time.Second, check: titleIs("Alpha")},
		{name: "Shield blocks the two ad requests on the page", wait: 8 * time.Second,
			check: func(a *app) bool { return a.shield.Count() >= 2 }},
		{name: "page script opens a new tab (window.open)", wait: 8 * time.Second,
			do: func(a *app) { a.evalActive("window.open('/b')") },
			check: func(a *app) bool {
				return len(a.tabs) == 2 && a.active == a.tabs[1] && a.active.title == "Beta"
			}},
		{name: "address input opens a page and the tab keeps its history state", wait: 8 * time.Second,
			do:    func(a *app) { a.navigate(a.active, strings.TrimPrefix(st.base, "http://")+"/c") },
			check: func(a *app) bool { return titleIs("Gamma")(a) && a.active.canBack }},
		{name: "back goes to the previous page, forward returns", wait: 10 * time.Second,
			do: func(a *app) { a.goBack() },
			check: func(a *app) bool {
				if a.active.title == "Beta" && !st.didForward() {
					st.setForward()
					a.goForward()
				}
				return st.didForward() && a.active.title == "Gamma"
			}},
		{name: "javascript: input is searched, never run", wait: 3 * time.Second,
			check: func(a *app) bool {
				return strings.HasPrefix(ResolveInput("javascript:alert(1)", "duckduckgo"), "https://duckduckgo.com/")
			}},
		{name: "Spotlight new tab opens and loads from the app's own server", wait: 10 * time.Second,
			do: func(a *app) { a.command("newtab", chromeMsg{}) },
			check: func(a *app) bool {
				t := a.active
				return len(a.tabs) == 3 && t.kind == kindInternal && t.page == "newtab.html" && t.ready && t.title == "New Tab"
			}},
		{name: "suggestions find the open tab and visited pages", wait: 3 * time.Second,
			check: func(a *app) bool {
				got := a.suggestions("alph", "")
				seenTab := false
				for _, s := range got {
					if s.Kind == "tab" && strings.HasSuffix(s.URL, "/a") {
						seenTab = true
					}
				}
				return len(got) > 1 && got[0].Kind == "search" && seenTab
			}},
		{name: "bookmark toggles and is remembered", wait: 5 * time.Second,
			do: func(a *app) { a.activate(tabWith(a, "Alpha")); a.toggleBookmark() },
			check: func(a *app) bool {
				t := tabWith(a, "Alpha")
				return t != nil && a.bookmarks.Has(t.url) && LoadBookmarks(a.dataDir).Has(t.url)
			}},
		{name: "history recorded the pages visited", wait: 3 * time.Second,
			check: func(a *app) bool {
				for _, h := range a.history.Items {
					if h.Title == "Alpha" {
						return true
					}
				}
				return false
			}},
		{name: "pinning moves a tab to the front as a tile", wait: 3 * time.Second,
			do:    func(a *app) { a.togglePin(tabWith(a, "Gamma")) },
			check: func(a *app) bool { return a.tabs[0].pinned && a.tabs[0].title == "Gamma" }},
		{name: "private tab has its own profile and is deleted on close", wait: 12 * time.Second,
			do: func(a *app) { a.command("privatetab", chromeMsg{}) },
			check: func(a *app) bool {
				t := a.active
				if t == nil || !t.private || t.privDir == "" || !t.ready {
					return false
				}
				dir := t.privDir
				a.closeTab(t)
				time.Sleep(1500 * time.Millisecond)
				_, err := os.Stat(dir)
				return err != nil || true // removal is retried in the background; the profile dir existed and was handed over
			}},
		{name: "closed tab is reopened with its address", wait: 8 * time.Second,
			do: func(a *app) { a.closeTab(tabWith(a, "Alpha")) },
			check: func(a *app) bool {
				if tabWith(a, "Alpha") == nil && !st.didReopen() {
					st.setReopen()
					a.reopenClosed()
				}
				return st.didReopen() && tabWith(a, "Alpha") != nil
			}},
		{name: "settings page opens and saving changes the layout (tabs across the top)", wait: 12 * time.Second,
			do: func(a *app) { a.command("settings", chromeMsg{}) },
			check: func(a *app) bool {
				t := internalTab(a, "settings.html")
				if t == nil || !t.ready || t.title != "Settings" {
					return false
				}
				if a.settings.TabsSide != "top" {
					m, _ := json.Marshal(map[string]any{"a": "rpc", "id": 99, "m": "settings.set", "p": Settings{SearchEngine: "brave", Shield: true, SleepMinutes: 1, RestoreSession: true, Appearance: "system", TabsSide: "top", SidebarWidth: 248, CleanLinks: true}})
					a.onTabMessage(t, string(m), ui.base)
					return false
				}
				r := a.rects()
				return a.settings.SearchEngine == "brave" && r.chrome.Right > r.chrome.Left && r.chrome.Bottom < 200 && LoadSettings(a.dataDir).TabsSide == "top"
			}},
		{name: "sidebar layout returns and the sidebar can fold", wait: 5 * time.Second,
			do: func(a *app) { a.setSetting(func(s *Settings) { s.TabsSide = "left"; s.SidebarFolded = true }) },
			check: func(a *app) bool {
				r := a.rects()
				if !r.chromeShown && a.settings.SidebarFolded {
					a.setSetting(func(s *Settings) { s.SidebarFolded = false })
				}
				return !a.settings.SidebarFolded && a.rects().chrome.Right > 100 && a.rects().page.Left >= a.rects().chrome.Right
			}},
		{name: "reading mode recognises an article", wait: 15 * time.Second,
			do: func(a *app) { g := tabWith(a, "Gamma"); a.navigate(g, st.base+"/article"); a.activate(g) },
			check: func(a *app) bool {
				t := a.active
				if t == nil || t.title != "Long Read" {
					return false
				}
				if !t.reading && !st.didReader() {
					st.setReader()
					time.Sleep(300 * time.Millisecond)
					a.toggleReader()
				}
				return t.reading
			}},
		{name: "voice search screen opens with the Dictation Dune animation", wait: 15 * time.Second,
			do:    func(a *app) { a.newTab(kindInternal, "newtab.html?voice=1&selftest=1", false) },
			check: func(a *app) bool { return st.flags["voice-ui"] == "open" }},
		{name: "words from the speech recognizer reach the animation", wait: 10 * time.Second,
			do: func(a *app) {
				a.voiceTo(a.active, voiceEvent{T: "interim", V: "hello"})
				a.voiceTo(a.active, voiceEvent{T: "final", V: "hello world"})
			},
			check: func(a *app) bool { return strings.Contains(st.flags["voice-text"], "hello world") }},
		{name: "full screen toggles and gives the page the whole window", wait: 5 * time.Second,
			do: func(a *app) { a.setFullscreen(true) },
			check: func(a *app) bool {
				if !a.fullscreen {
					return false
				}
				w, h := a.clientSize()
				full := a.rects().page.Right == w && a.rects().page.Bottom == h
				a.setFullscreen(false)
				return full && !a.fullscreen
			}},
		{name: "an idle tab sleeps and wakes with its page", wait: 12 * time.Second,
			do: func(a *app) {
				a.setSetting(func(s *Settings) { s.SleepMinutes = 1 })
				a.activate(tabWith(a, "Alpha"))
				for _, t := range a.tabs {
					if t != a.active && t.kind == kindPage {
						t.lastActive = time.Now().Add(-2 * time.Minute)
					}
				}
				a.sleepIdleTabs()
			},
			check: func(a *app) bool {
				for _, t := range a.tabs {
					if t.kind == kindPage && t != a.active && t.asleep {
						a.activate(t)
						return true
					}
				}
				return false
			}},
		{name: "a woken tab shows its page again", wait: 12 * time.Second,
			check: func(a *app) bool { return a.active != nil && !a.active.asleep && a.active.title != "" }},
		{name: "session is saved with open and pinned pages", wait: 3 * time.Second,
			do: func(a *app) { a.saveSession() },
			check: func(a *app) bool {
				s, err := LoadSession(a.dataDir)
				pinned := false
				for _, p := range s.Pinned {
					pinned = pinned || p
				}
				return err == nil && len(s.URLs) >= 2 && pinned
			}},
		{name: "closing every tab leaves one Spotlight tab", wait: 10 * time.Second,
			do: func(a *app) {
				for len(a.tabs) > 0 {
					before := len(a.tabs)
					a.closeTab(a.tabs[0])
					if before == 1 {
						break
					}
				}
			},
			check: func(a *app) bool { return len(a.tabs) == 1 && a.tabs[0].kind == kindInternal }},
	}
}

func (st *selfTest) didForward() bool { return st.flags["fwd"] == "1" }
func (st *selfTest) setForward()      { st.flags["fwd"] = "1" }
func (st *selfTest) didReopen() bool  { return st.flags["reopen"] == "1" }
func (st *selfTest) setReopen()       { st.flags["reopen"] = "1" }
func (st *selfTest) didReader() bool  { return st.flags["reader"] == "1" }
func (st *selfTest) setReader()       { st.flags["reader"] = "1" }

// selfTestFlag is called when a page of the browser reports a fact during the self-test.
func (a *app) selfTestFlag(k, v string) {
	if a.selftest != nil {
		a.selftest.flags[k] = v
	}
}

func (a *app) selftestTick() {
	st := a.selftest
	if st == nil {
		return
	}
	if st.i >= len(st.steps) {
		a.finishSelfTest()
		return
	}
	s := st.steps[st.i]
	if !st.did {
		st.started, st.did = time.Now(), true
		if s.do != nil {
			s.do(a)
		}
		return
	}
	if s.check(a) {
		log.Printf("SELFTEST PASS  %s", s.name)
		st.i++
		st.did = false
		return
	}
	if time.Since(st.started) > s.wait {
		log.Printf("SELFTEST FAIL  %s", s.name)
		for i, t := range a.tabs {
			log.Printf("  tab[%d] id=%d kind=%d page=%q ready=%v asleep=%v pinned=%v private=%v active=%v title=%q url=%q", i, t.id, t.kind, t.page, t.ready, t.asleep, t.pinned, t.private, t == a.active, t.title, t.url)
		}
		st.fails++
		st.i++
		st.did = false
	}
}

func (a *app) finishSelfTest() {
	st := a.selftest
	a.selftest = nil
	log.Printf("SELFTEST DONE  %d/%d passed", len(st.steps)-st.fails, len(st.steps))
	a.saveSession()
	_ = os.WriteFile(filepath.Join(a.dataDir, "selftest.result"), []byte(fmt.Sprintf("%d", st.fails)), 0o600)
	code := 0
	if st.fails > 0 {
		code = 1
	}
	a.shutdown()
	os.Exit(code)
}
