package main

// Address alias: type fasthand.<anything> (fasthand.com, fasthand.xyz, ...) and
// you land on fasthandbd.com. The address bar keeps showing what you typed,
// but Copy Address, sharing and history all use the real address. Once you
// are signed in to the site, the bar shows fasthand.<your username> in place
// of the TLD.
//
// The page tells the browser who is signed in through localStorage:
//
//	localStorage.fasthand_username = "hasib"
//
// (set on sign-in/register, removed on sign-out).

import (
	"regexp"
	"strings"
	"unicode"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

const (
	aliasHost   = "fasthandbd.com"
	aliasTarget = "https://fasthandbd.com"
)

var aliasRe = regexp.MustCompile(`^fasthand\.([a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*)(/.*)?$`)

// aliasMatch recognises fasthand.<tld> typed in the address bar. It returns
// the label to show in place of the TLD (what was typed after "fasthand.")
// and the real address to load.
func aliasMatch(typed string) (label, target string, ok bool) {
	s := strings.TrimSpace(typed)
	low := strings.ToLower(s)
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(low, p) {
			s, low = s[len(p):], low[len(p):]
		}
	}
	if strings.HasPrefix(low, "www.") {
		s, low = s[4:], low[4:]
	}
	// Only the host is case-insensitive; the path keeps what was typed.
	i := strings.IndexByte(s, '/')
	if i < 0 {
		i = len(s)
	}
	m := aliasRe.FindStringSubmatch(low[:i] + s[i:])
	if m == nil {
		return "", "", false
	}
	return m[1], aliasTarget + m[2], true
}

// aliasUser cleans a username into something that reads as part of a host.
func cleanAliasUser(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
		if b.Len() >= 30 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// shown is the address as the bar displays it: the fasthand.<tld> (or
// fasthand.<username>) the user typed, with the real address's path.
func (t *Tab) aliasShown() (string, bool) {
	if t.aliasLabel == "" || hostOf(t.url) != aliasHost {
		return "", false
	}
	label := t.aliasLabel
	if t.aliasUser != "" {
		label = t.aliasUser
	}
	rest := strings.TrimPrefix(editable(t.url), "www.")
	rest = strings.TrimPrefix(rest, aliasHost)
	return "fasthand." + label + rest, true
}

// aliasPoll checks, every couple of seconds, who is signed in on the current
// tab's fasthandbd.com page and updates the bar when that changes.
func (b *Browser) aliasPoll() {
	coreglib.TimeoutAdd(2000, func() bool {
		t := b.current
		if t == nil || t.view == nil || t.aliasLabel == "" || hostOf(t.url) != aliasHost {
			return true
		}
		t.run(`(() => { try { return localStorage.getItem('fasthand_username') || ''; } catch (e) { return ''; } })()`, func(out string) {
			user := cleanAliasUser(out)
			if user != t.aliasUser && b.current == t {
				t.aliasUser = user
				b.syncChrome()
			} else {
				t.aliasUser = user
			}
		})
		return true
	})
}
