package main

// FASTY_SELFTEST=script: drive the window through its features and save
// pictures of it, for checking a build without anyone at the keyboard.
//
// One command per line:
//
//	wait MS            let the page get on with it
//	open URL           a new tab, in front
//	go URL             load in the current tab
//	select N           the Nth tab (from 1)
//	key ctrl+shift+t   a key, through the window's own bindings
//	type TEXT          into the address field (or the new-tab field), then Return
//	fill TEXT          the same, without Return
//	passkey allow|deny [N]  answer the passkey question on screen (N: which account, from 1)
//	auth open | add ISSUER|ACCOUNT|SECRET | pin on 1234 | pin off 1234 | lock   Authenticator
//	toast TEXT         show a toast (it should appear inside an open dialog)
//	copyaddr           select all in the address field and copy it; the clipboard text goes in the log
//	eval JS            run in the page; the result goes in the log
//	state              tabs, the current one, and how it is doing
//	shot NAME          a PNG of the window
//	shotpip NAME       a PNG of the floating video window
//	quit
//
// The log goes to $FASTY_SELFTEST_OUT/selftest.json.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

func selftestOut() string {
	if d := os.Getenv("FASTY_SELFTEST_OUT"); d != "" {
		return d
	}
	return os.TempDir()
}

func shot(b *Browser, name string) string {
	p := filepath.Join(selftestOut(), name+".png")
	if !shotPNG(b, p) {
		return "shot " + name + ": couldn't save"
	}
	return "shot " + p
}

func parseKey(spec string) (uint, gdk.ModifierType) {
	parts := strings.Split(spec, "+")
	var mods gdk.ModifierType
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl":
			mods |= gdk.ControlMask
		case "shift":
			mods |= gdk.ShiftMask
		case "alt":
			mods |= gdk.AltMask
		}
	}
	return gdk.KeyvalFromName(parts[len(parts)-1]), mods
}

func (b *Browser) state() map[string]any {
	var tabs []map[string]any
	for _, t := range b.tabs {
		tabs = append(tabs, map[string]any{"url": t.url, "title": t.title, "built": t.built(),
			"pinned": t.pinned, "private": t.private, "crashed": t.crashed, "loading": t.loading,
			"current": t == b.current})
	}
	return map[string]any{"tabs": tabs, "side": b.app.settings.TabsSide, "folded": b.app.settings.SidebarFolded,
		"field": b.omnibox.entry.Text(), "sideWidth": b.side.Width(), "find": b.findCount.Label(),
		"reading": b.current != nil && b.current.reading, "downloads": b.downloadsState(),
		"popups": len(b.popups)}
}

func runSelftest(b *Browser, script string) {
	raw, err := os.ReadFile(script)
	if err != nil {
		fmt.Fprintln(os.Stderr, "selftest:", err)
		return
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	var log []any
	finish := func() {
		out, _ := json.MarshalIndent(log, "", " ")
		os.WriteFile(filepath.Join(selftestOut(), "selftest.json"), out, 0o644)
		b.app.quitAll()
	}
	var step func(i int)
	step = func(i int) {
		if i >= len(lines) {
			finish()
			return
		}
		cmd, arg, _ := strings.Cut(lines[i], " ")
		fmt.Fprintln(os.Stderr, "selftest:", lines[i])
		next := func(delay uint) {
			coreglib.TimeoutAdd(delay, func() bool { step(i + 1); return false })
		}
		defer func() {
			if r := recover(); r != nil {
				log = append(log, fmt.Sprintf("ERROR %s: %v", lines[i], r))
				next(10)
			}
		}()
		switch cmd {
		case "wait":
			ms, _ := strconv.Atoi(arg)
			next(uint(ms))
			return
		case "open":
			b.openTab(arg, true, nil, false, "", false)
		case "go":
			if b.current != nil {
				b.current.load(arg)
			}
		case "select":
			n, _ := strconv.Atoi(arg)
			if n >= 1 && n <= len(b.tabs) {
				b.selectTab(b.tabs[n-1])
			}
		case "key":
			kv, mods := parseKey(arg)
			log = append(log, fmt.Sprintf("key %s handled=%v", arg, b.key(kv, mods)))
		case "shotpip":
			// a PNG of the floating video window
			done := false
			for _, t := range b.tabs {
				if t.pip != nil {
					pth := filepath.Join(selftestOut(), arg+".png")
					if shotPNG(t.pip.win.Child(), pth) {
						log = append(log, "shot "+pth)
					} else {
						log = append(log, "shotpip: couldn't save")
					}
					done = true
				}
			}
			if !done {
				log = append(log, "shotpip: no floating window")
			}
		case "passkey":
			if p := b.passkeyAsk; p != nil {
				f := strings.Fields(arg)
				pick := 0
				if len(f) > 1 {
					pick, _ = strconv.Atoi(f[1])
					pick--
				}
				if len(f) > 0 && f[0] == "allow" {
					p.respond("allow", pick)
				} else {
					p.respond("deny", 0)
				}
				p.dialog.ForceClose()
				log = append(log, "passkey: answered")
			} else {
				log = append(log, "passkey: no question on screen")
			}
		case "auth":
			v := b.app.authenticatorVault()
			f := strings.SplitN(arg, " ", 2)
			rest := ""
			if len(f) > 1 {
				rest = f[1]
			}
			switch f[0] {
			case "open":
				showAuthenticator(b)
			case "add":
				p := strings.Split(rest, "|")
				if len(p) == 3 {
					v.Open()
					err := v.Save(authEntry{Issuer: p[0], Account: p[1], Secret: p[2]})
					log = append(log, fmt.Sprintf("auth add: %v", err))
				}
			case "pin":
				p := strings.Fields(rest)
				if len(p) == 2 && p[0] == "on" {
					v.Open()
					log = append(log, fmt.Sprintf("auth pin on: %v", v.SetPIN(p[1])))
				} else if len(p) == 2 && p[0] == "off" {
					log = append(log, fmt.Sprintf("auth pin off: %v", v.RemovePIN(p[1])))
				}
			case "exportdlg":
				v.Open()
				exportAuthFile(b)
			case "lock":
				v.Lock()
				log = append(log, fmt.Sprintf("auth locked: %v, has pin: %v", !v.unlocked(), v.hasPIN()))
			case "unlock":
				log = append(log, fmt.Sprintf("auth unlock: %v", v.Unlock(rest)))
			}
		case "toast":
			b.toast(arg, 30)
		case "copyaddr":
			b.omnibox.entry.GrabFocus()
			b.omnibox.entry.SelectRegion(0, -1)
			coreglib.BaseObject(b.omnibox.entry.Delegate()).Emit("copy-clipboard")
			cb := gdk.DisplayGetDefault().Clipboard()
			cb.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
				text, _ := cb.ReadTextFinish(res)
				log = append(log, "clipboard: "+text)
			})
		case "fill":
			// type without Return, to see the suggestions
			if b.spotlight.shown() {
				b.spotlight.entry.GrabFocus()
				b.spotlight.entry.SetText(arg)
				b.spotlight.entry.SetPosition(-1)
			} else {
				b.omnibox.focus(false, false)
				b.omnibox.entry.SetText(arg)
			}
		case "type":
			if b.spotlight.shown() {
				b.spotlight.entry.SetText(arg)
				b.spotlight.activate()
				break
			}
			if !b.omnibox.newTab {
				b.omnibox.focus(false, false)
			}
			b.omnibox.entry.SetText(arg)
			b.omnibox.activate()
		case "hide":
			if b.current != nil {
				b.picked(b.current, arg, arg, "")
			}
		case "find":
			b.openFind()
			b.findEntry.SetText(arg)
		case "sleep":
			n, _ := strconv.Atoi(arg)
			if n >= 1 && n <= len(b.tabs) {
				b.sleepTab(b.tabs[n-1])
			}
		case "pin":
			n, _ := strconv.Atoi(arg)
			if n >= 1 && n <= len(b.tabs) {
				b.setPinned(b.tabs[n-1], true)
			}
		case "private":
			b.newTab(true)
		case "eval":
			if b.current == nil || b.current.view == nil {
				log = append(log, "eval: no page")
				break
			}
			b.current.run(arg, func(v string) {
				log = append(log, "eval "+arg+" => "+v)
				next(10)
			})
			return
		case "state":
			log = append(log, b.state())
		case "shot":
			log = append(log, shot(b, arg))
		case "quit":
			finish()
			return
		default:
			log = append(log, "unknown: "+lines[i])
		}
		next(50)
	}
	coreglib.TimeoutAdd(300, func() bool { step(0); return false })
}

func (b *Browser) downloadsState() []string {
	var out []string
	for _, it := range b.app.downloads.items {
		out = append(out, it.state+" "+it.path+" "+it.err)
	}
	return out
}
