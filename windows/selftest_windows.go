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
	flags   map[string]string // facts reported by the browser's own pages and by the steps
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

// rpcCall runs a call from the glass overlay the way the page would.
func (a *app) rpcCall(method string, params any) {
	p, _ := json.Marshal(params)
	a.handleRPC(a.overlaySink(), pageMsg{A: "rpc", M: method, P: p, ID: json.Number("1")})
}

// probe asks a page a yes/no question; the answer comes back through the tab's loading flag.
func probe(t *tab, expr string) {
	t.loading = false
	t.view.Eval("window.chrome.webview.postMessage(JSON.stringify({a:'loading',on:!!(" + expr + ")}))")
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
	flag := func(k string) bool { return st.flags[k] == "1" }
	setFlag := func(k string) { st.flags[k] = "1" }
	// asks the page once, then waits for its answer
	ask := func(a *app, t *tab, key, expr string) (answered, yes bool) {
		if !flag(key + ":asked") {
			setFlag(key + ":asked")
			probe(t, expr)
			st.flags[key+":at"] = time.Now().Format(time.RFC3339Nano)
			return false, false
		}
		at, _ := time.Parse(time.RFC3339Nano, st.flags[key+":at"])
		if time.Since(at) < 600*time.Millisecond {
			return false, false
		}
		return true, t.loading
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
				if a.active.title == "Beta" && !flag("fwd") {
					setFlag("fwd")
					a.goForward()
				}
				return flag("fwd") && a.active.title == "Gamma"
			}},
		{name: "javascript: input is searched, never run", wait: 3 * time.Second,
			check: func(a *app) bool {
				return strings.HasPrefix(ResolveInput("javascript:alert(1)", "duckduckgo"), "https://duckduckgo.com/")
			}},
		{name: "Ctrl+T opens the glass Spotlight over a snapshot of the page", wait: 10 * time.Second,
			do: func(a *app) { a.shortcut("ctrl+t") },
			check: func(a *app) bool {
				return a.ov != nil && a.ov.shown && a.ov.mode == "newtab" && ui.ImageCount() == 1
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
		{name: "Spotlight Enter opens the page in a new tab and closes the pop-up", wait: 10 * time.Second,
			do: func(a *app) { a.rpcCall("nav", map[string]any{"v": st.base + "/article", "mode": "newtab"}) },
			check: func(a *app) bool {
				return !a.ov.shown && len(a.tabs) == 3 && a.active.title == "Long Read" && ui.ImageCount() == 0
			}},
		{name: "reading mode recognises an article", wait: 15 * time.Second,
			check: func(a *app) bool {
				t := a.active
				if t == nil || t.title != "Long Read" {
					return false
				}
				if !t.reading && !flag("reader") {
					setFlag("reader")
					time.Sleep(300 * time.Millisecond)
					a.toggleReader()
				}
				return t.reading
			}},
		{name: "Spotlight from the address field edits the current tab", wait: 8 * time.Second,
			do: func(a *app) { a.openSpotlight("address", false) },
			check: func(a *app) bool {
				if !a.ov.shown || a.ov.mode != "address" {
					return false
				}
				a.rpcCall("nav", map[string]any{"v": st.base + "/c", "mode": "address"})
				return true
			}},
		{name: "the current tab shows the new address", wait: 10 * time.Second,
			check: func(a *app) bool { return len(a.tabs) == 3 && a.active.title == "Gamma" && !a.ov.shown }},
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
		{name: "importing a bookmarks file adds the new links", wait: 5 * time.Second,
			do: func(a *app) {
				a.rpcCall("bookmarks.import", map[string]any{"html": `<DL><DT><A HREF="https://imported.example/one">One</A><DT><A HREF="https://imported.example/two">Two</A></DL>`})
			},
			check: func(a *app) bool {
				return a.bookmarks.Has("https://imported.example/one") && a.bookmarks.Has("https://imported.example/two")
			}},
		{name: "Authenticator adds an account, shows a code and keeps the secret encrypted", wait: 12 * time.Second,
			do: func(a *app) { a.command("authenticator", chromeMsg{}) },
			check: func(a *app) bool {
				t := internalTab(a, "authenticator.html")
				if t == nil || !t.ready || t.title != "Authenticator" {
					return false
				}
				if a.auth == nil || len(a.auth.Items) == 0 {
					m, _ := json.Marshal(map[string]any{"a": "rpc", "id": 7, "m": "auth.add", "p": map[string]any{"uri": "otpauth://totp/Example:me@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example"}})
					a.onTabMessage(t, string(m), ui.base)
					return false
				}
				v := a.auth.Views(time.Now())
				raw, err := os.ReadFile(filepath.Join(a.dataDir, "authenticator.dat"))
				return len(v) == 1 && len(v[0].Code) == 6 && err == nil && !strings.Contains(string(raw), "JBSWY3DPEHPK3PXP") && len(raw) > 20
			}},
		{name: "pinning moves a tab to the front as a tile", wait: 3 * time.Second,
			do:    func(a *app) { a.togglePin(tabWith(a, "Alpha")) },
			check: func(a *app) bool { return a.tabs[0].pinned && a.tabs[0].title == "Alpha" }},
		{name: "Ctrl+Shift+N opens a private Spotlight and a private tab from it", wait: 12 * time.Second,
			do: func(a *app) {
				a.history.Remove(st.base + "/b") // so a record of /b afterwards could only come from the private tab
				a.shortcut("ctrl+shift+n")
			},
			check: func(a *app) bool {
				if !a.ov.shown || a.ov.mode != "newtab" || !a.ov.private {
					return false
				}
				a.rpcCall("nav", map[string]any{"v": st.base + "/b", "mode": "newtab", "private": true})
				return true
			}},
		{name: "the private tab keeps no history and is closed", wait: 12 * time.Second,
			check: func(a *app) bool {
				t := a.active
				if t == nil || !t.private || t.title != "Beta" || t.privDir == "" {
					return false
				}
				for _, h := range a.history.Items {
					if h.Title == "Beta" {
						return false
					}
				}
				a.closeTab(t)
				return true
			}},
		{name: "closed tab is reopened with its address", wait: 8 * time.Second,
			do: func(a *app) {
				st.flags["n"] = fmt.Sprint(len(a.tabs))
				a.closeTab(tabWith(a, "Gamma"))
			},
			check: func(a *app) bool {
				if !flag("reopen") {
					setFlag("reopen")
					a.reopenClosed()
				}
				n := 0
				for _, t := range a.tabs {
					if strings.HasSuffix(t.url, "/c") && t.ready {
						n++
					}
				}
				return st.flags["n"] == fmt.Sprint(len(a.tabs)) && n == 2
			}},
		{name: "Settings opens as a centered pop-up and saving changes the layout (tabs across the top)", wait: 12 * time.Second,
			do: func(a *app) { a.command("settings", chromeMsg{}) },
			check: func(a *app) bool {
				if !a.ov.shown || a.ov.mode != "settings" {
					return false
				}
				if a.settings.TabsSide != "top" {
					cur := a.settings
					cur.TabsSide, cur.SearchEngine = "top", "brave"
					a.rpcCall("settings.set", cur)
					return false
				}
				r := a.rects()
				return a.settings.SearchEngine == "brave" && r.chrome.Bottom < 200 && LoadSettings(a.dataDir).TabsSide == "top"
			}},
		{name: "custom search address is used for searches", wait: 5 * time.Second,
			do: func(a *app) {
				cur := a.settings
				cur.SearchEngine, cur.CustomEngine = "custom", "https://search.example/?q=%s"
				a.rpcCall("settings.set", cur)
			},
			check: func(a *app) bool {
				return a.settings.SearchEngine == "custom" && ResolveInput("two words", a.settings.SearchEngine) == "https://search.example/?q=two+words"
			}},
		{name: "sidebar layout returns, the sidebar folds and peeks at the edge", wait: 5 * time.Second,
			do: func(a *app) {
				a.hideOverlay()
				a.setSetting(func(s *Settings) { s.TabsSide = "left"; s.SearchEngine = "duckduckgo"; s.SidebarFolded = true })
			},
			check: func(a *app) bool {
				if a.rects().chromeShown {
					return false
				}
				a.peeking = true
				a.layout()
				ok := a.rects().chromeShown && a.rects().chrome.Right > 100
				a.peeking = false
				a.setSetting(func(s *Settings) { s.SidebarFolded = false })
				return ok && a.rects().chrome.Right > 100 && a.rects().page.Left >= a.rects().chrome.Right
			}},
		{name: "voice search opens inside Spotlight and shows the words heard", wait: 20 * time.Second,
			do: func(a *app) {
				a.showOverlay(overlayParams{Mode: "newtab", Voice: true})
				st.flags["voiceAt"] = time.Now().Format(time.RFC3339Nano)
			},
			check: func(a *app) bool {
				at, _ := time.Parse(time.RFC3339Nano, st.flags["voiceAt"])
				if !flag("voiceSent") && time.Since(at) > 2*time.Second {
					setFlag("voiceSent")
					a.voiceTo(a.overlaySink(), voiceEvent{T: "interim", V: "hello"})
					a.voiceTo(a.overlaySink(), voiceEvent{T: "final", V: "hello world"})
				}
				return strings.Contains(st.flags["voice-text"], "hello world")
			}},
		{name: "hiding an element hides it on that site and it can be shown again", wait: 14 * time.Second,
			do: func(a *app) { a.hideOverlay(); a.activate(tabWith(a, "Alpha")) },
			check: func(a *app) bool {
				t := a.active
				if t == nil || t.title != "Alpha" {
					return false
				}
				if !flag("veil") {
					setFlag("veil")
					a.curtain.Hide(siteHost(t.url), "h1", "Alpha heading")
					a.applyVeils(t)
					return false
				}
				answered, yes := ask(a, t, "hidden", "getComputedStyle(document.querySelector('h1')).display==='none'")
				if answered && yes && !flag("shown") {
					setFlag("shown")
					a.rpcCall("veils.show", map[string]any{"host": siteHost(t.url)})
					delete(st.flags, "hidden:asked")
					return false
				}
				if answered && !yes {
					return flag("shown") // it came back after "Show again"
				}
				return false
			}},
		{name: "the hide-something picker, picture in picture and page zoom are present", wait: 14 * time.Second,
			do: func(a *app) {
				a.setSetting(func(s *Settings) { s.PageZoom = 125 })
				a.applyVeilsAll()
			},
			check: func(a *app) bool {
				t := a.active
				if t == nil || t.title != "Alpha" {
					return false
				}
				answered, yes := ask(a, t, "pick", "typeof window.__fastyPick==='function' && typeof window.__fastyPip==='function' && document.documentElement.style.zoom==='1.25' && document.pictureInPictureEnabled")
				if answered {
					a.setSetting(func(s *Settings) { s.PageZoom = 100 })
					return yes
				}
				return false
			}},
		{name: "Shield can be paused for one site and the count stops rising", wait: 20 * time.Second,
			do: func(a *app) {
				a.activate(tabWith(a, "Alpha"))
				a.pauseShieldHere()
				st.flags["count"] = fmt.Sprint(a.shield.Count())
				a.reloadTab(a.active, false)
			},
			check: func(a *app) bool {
				if len(a.settings.ShieldPaused) != 1 {
					return false
				}
				if !flag("waited") {
					setFlag("waited")
					st.flags["waitAt"] = time.Now().Format(time.RFC3339Nano)
					return false
				}
				at, _ := time.Parse(time.RFC3339Nano, st.flags["waitAt"])
				if time.Since(at) < 4*time.Second {
					return false
				}
				same := st.flags["count"] == fmt.Sprint(a.shield.Count())
				a.pauseShieldHere() // back on
				return same && len(a.settings.ShieldPaused) == 0
			}},
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
		{name: "clearing browsing data empties history and schedules the web data wipe", wait: 5 * time.Second,
			do: func(a *app) { a.rpcCall("data.clear", map[string]any{}) },
			check: func(a *app) bool {
				_, err := os.Stat(filepath.Join(a.dataDir, "clear-webdata"))
				if err == nil && len(a.history.Items) == 0 {
					_ = os.Remove(filepath.Join(a.dataDir, "clear-webdata")) // the test profile is thrown away anyway
					return true
				}
				return false
			}},
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
		{name: "closing every tab leaves one blank page", wait: 10 * time.Second,
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
		if a.ov != nil {
			log.Printf("  overlay ready=%v shown=%v mode=%q", a.ov.ready, a.ov.shown, a.ov.mode)
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
