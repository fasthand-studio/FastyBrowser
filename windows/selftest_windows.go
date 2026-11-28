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

// `fasty.exe --selftest` runs the real app against a local web server and checks tabs,
// navigation, Shield, settings, sleeping tabs and session saving. Results go to the log
// and the exit code is 0 only if every step passed.

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
}

const stPageA = `<!doctype html><title>Alpha</title><h1>Alpha</h1>
<img src="https://ad.doubleclick.net/pixel.gif" width=1 height=1>
<script src="https://doubleclick.net/ad.js"></script>
<img src="http://127.0.0.1:%PORT%/ok.gif" width=1 height=1>
<a id=l href="/b" target="_blank">open b</a>`

func startSelfTestServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, strings.ReplaceAll(stPageA, "%PORT%", fmt.Sprint(port)))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>Beta</title><h1>Beta</h1>")
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>Gamma</title><h1>Gamma</h1>")
	})
	mux.HandleFunc("/ok.gif", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	go http.Serve(ln, mux)
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

func (a *app) beginSelfTest(base string) {
	st := &selfTest{base: base}
	a.selftest = st
	cur := func(a *app) *tab { return a.active }
	titleIs := func(want string) func(a *app) bool {
		return func(a *app) bool { t := cur(a); return t != nil && t.kind == kindPage && t.title == want }
	}
	st.steps = []stStep{
		{name: "first page loads and reports its title", wait: 15 * time.Second,
			check: titleIs("Alpha")},
		{name: "Shield blocks the two ad requests on the page", wait: 8 * time.Second,
			check: func(a *app) bool { return a.shield.Count() >= 2 }},
		{name: "page script opens a new tab (window.open)", wait: 8 * time.Second,
			do: func(a *app) { a.evalActive("window.open('/b')") },
			check: func(a *app) bool {
				return len(a.tabs) == 2 && a.active == a.tabs[1] && a.active.title == "Beta"
			}},
		{name: "address bar input opens a page", wait: 8 * time.Second,
			do:    func(a *app) { a.navigate(a.active, strings.TrimPrefix(st.base, "http://")+"/c") },
			check: titleIs("Gamma")},
		{name: "back goes to the previous page", wait: 8 * time.Second,
			do:    func(a *app) { a.evalActive("history.back()") },
			check: titleIs("Beta")},
		{name: "forward returns", wait: 8 * time.Second,
			do:    func(a *app) { a.evalActive("history.forward()") },
			check: titleIs("Gamma")},
		{name: "javascript: input is searched, never run", wait: 3 * time.Second,
			check: func(a *app) bool {
				return strings.HasPrefix(ResolveInput("javascript:alert(1)", "duckduckgo"), "https://duckduckgo.com/")
			}},
		{name: "a new tab shows the start page", wait: 8 * time.Second,
			do: func(a *app) { a.newTab(kindNewTab, "") },
			check: func(a *app) bool {
				return len(a.tabs) == 3 && a.active.kind == kindNewTab && a.active.ready
			}},
		{name: "settings open and saving writes settings.json", wait: 10 * time.Second,
			do: func(a *app) {
				a.openSettings()
				m, _ := json.Marshal(pageMsg{A: "save", S: &Settings{SearchEngine: "brave", Shield: true, SleepMinutes: 1, RestoreSession: true}})
				for _, t := range a.tabs {
					if t.kind == kindSettings {
						a.onTabMessage(t, string(m))
					}
				}
			},
			check: func(a *app) bool {
				return a.active.kind == kindSettings && a.settings.SearchEngine == "brave" &&
					LoadSettings(a.dataDir).SearchEngine == "brave"
			}},
		{name: "an idle tab sleeps and wakes with its page", wait: 12 * time.Second,
			do: func(a *app) {
				a.activate(a.tabs[0]) // the Alpha tab
				for _, t := range a.tabs {
					if t != a.active && t.kind == kindPage {
						t.lastActive = time.Now().Add(-2 * time.Minute)
					}
				}
				// tab index 1 (Gamma) is idle now
				a.activate(a.tabs[0])
				a.sleepIdleTabs()
			},
			check: func(a *app) bool { return a.tabs[1].asleep }},
		{name: "a sleeping tab wakes when selected", wait: 12 * time.Second,
			do:    func(a *app) { a.activate(a.tabs[1]) },
			check: func(a *app) bool { return !a.tabs[1].asleep && a.tabs[1].title == "Gamma" }},
		{name: "session is saved with the open pages", wait: 3 * time.Second,
			do: func(a *app) { a.saveSession() },
			check: func(a *app) bool {
				s, err := LoadSession(a.dataDir)
				return err == nil && len(s.URLs) >= 2
			}},
		{name: "closing every tab leaves one start-page tab", wait: 10 * time.Second,
			do: func(a *app) {
				for len(a.tabs) > 0 {
					before := len(a.tabs)
					a.closeTab(a.tabs[0])
					if len(a.tabs) == 1 && a.tabs[0].kind == kindNewTab && before == 1 {
						break
					}
					if before == 1 {
						break
					}
				}
			},
			check: func(a *app) bool { return len(a.tabs) == 1 && a.tabs[0].kind == kindNewTab }},
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
			log.Printf("  tab[%d] id=%d kind=%d ready=%v asleep=%v active=%v title=%q url=%q", i, t.id, t.kind, t.ready, t.asleep, t == a.active, t.title, t.url)
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
