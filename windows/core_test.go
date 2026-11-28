package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveInput(t *testing.T) {
	cases := []struct{ in, engine, want string }{
		{"", "duckduckgo", ""},
		{"https://example.com/a?b=c", "google", "https://example.com/a?b=c"},
		{"HTTP://Example.com", "google", "HTTP://Example.com"},
		{"example.com", "duckduckgo", "https://example.com"},
		{"example.com/path?q=1", "duckduckgo", "https://example.com/path?q=1"},
		{"localhost:8080", "duckduckgo", "http://localhost:8080"},
		{"192.168.1.5", "duckduckgo", "http://192.168.1.5"},
		{"8.8.8.8", "duckduckgo", "https://8.8.8.8"},
		{"hello world", "duckduckgo", "https://duckduckgo.com/?q=hello+world"},
		{"cats", "google", "https://www.google.com/search?q=cats"},
		{"cats", "nonsense", "https://duckduckgo.com/?q=cats"},
		// Script and data URLs are searched, never loaded.
		{"javascript:alert(1)", "duckduckgo", "https://duckduckgo.com/?q=javascript%3Aalert%281%29"},
		{"data:text/html,<b>x</b>", "duckduckgo", "https://duckduckgo.com/?q=data%3Atext%2Fhtml%2C%3Cb%3Ex%3C%2Fb%3E"},
		{"file:///C:/Windows/win.ini", "duckduckgo", "https://duckduckgo.com/?q=file%3A%2F%2F%2FC%3A%2FWindows%2Fwin.ini"},
	}
	for _, c := range cases {
		if got := ResolveInput(c.in, c.engine); got != c.want {
			t.Errorf("ResolveInput(%q,%q) = %q, want %q", c.in, c.engine, got, c.want)
		}
	}
}

func TestShieldMatchesParentDomains(t *testing.T) {
	s := NewShield()
	for _, u := range []string{
		"https://doubleclick.net/x",
		"https://ad.doubleclick.net/pixel.gif",
		"https://a.b.googlesyndication.com/ads.js",
	} {
		if !s.Blocked(u) {
			t.Errorf("%s should be blocked", u)
		}
	}
	for _, u := range []string{
		"https://example.com/",
		"https://notdoubleclick.net/", // suffix of the string, not a parent domain
		"https://github.com/",
		"not a url",
	} {
		if s.Blocked(u) {
			t.Errorf("%s should not be blocked", u)
		}
	}
	if s.BlockedHost("com") {
		t.Error("a bare TLD must never match")
	}
}

func TestShieldLoadHosts(t *testing.T) {
	s := NewShield()
	n := s.LoadHosts(strings.NewReader(`
# comment
127.0.0.1 localhost
0.0.0.0 tracker.example # trailing comment
0.0.0.0 ads.example.org
0.0.0.0 broadcasthost
::1 ip6-localhost
`))
	if n != 2 {
		t.Fatalf("loaded %d entries, want 2 (tracker.example and ads.example.org)", n)
	}
	if !s.Blocked("https://sub.ads.example.org/") {
		t.Error("loaded host should block its subdomains")
	}
	if s.Blocked("https://example.org/") {
		t.Error("parent of a blocked host must stay reachable")
	}
}

func TestSettingsNormalize(t *testing.T) {
	s := Settings{SearchEngine: "evil", SleepMinutes: -4, Homepage: "javascript:alert(1)"}.Normalize()
	if s.SearchEngine != "duckduckgo" || s.SleepMinutes != 0 || s.Homepage != "" {
		t.Errorf("unexpected normalize result: %+v", s)
	}
	s = Settings{SearchEngine: "google", SleepMinutes: 99999, Homepage: "example.com"}.Normalize()
	if s.SleepMinutes != 1440 || s.Homepage != "https://example.com" {
		t.Errorf("unexpected normalize result: %+v", s)
	}
}

func TestSettingsAndSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := LoadSettings(dir); !reflect.DeepEqual(got, DefaultSettings()) {
		t.Fatalf("missing file should give defaults, got %+v", got)
	}
	want := DefaultSettings()
	want.SearchEngine = "brave"
	want.Shield = false
	if err := SaveSettings(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: got %+v want %+v", got, want)
	}
	if err := SaveSession(dir, Session{URLs: []string{"https://a.example", "https://b.example"}, Active: 1}); err != nil {
		t.Fatal(err)
	}
	ses, err := LoadSession(dir)
	if err != nil || len(ses.URLs) != 2 || ses.Active != 1 {
		t.Fatalf("session round trip: %+v %v", ses, err)
	}
	if SaveSession(dir, Session{}) == nil {
		t.Error("an empty session should not be saved")
	}
}
