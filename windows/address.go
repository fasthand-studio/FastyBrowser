package main

import (
	"net"
	"net/url"
	"strings"
)

// searchEngines maps a settings key to a search URL; %s is the escaped query.
var searchEngines = map[string]string{
	"google":     "https://www.google.com/search?q=%s",
	"duckduckgo": "https://duckduckgo.com/?q=%s",
	"bing":       "https://www.bing.com/search?q=%s",
	"ecosia":     "https://www.ecosia.org/search?q=%s",
	"startpage":  "https://www.startpage.com/sp/search?query=%s",
	"kagi":       "https://kagi.com/search?q=%s",
	"brave":      "https://search.brave.com/search?q=%s",
	"qwant":      "https://www.qwant.com/?q=%s",
}

// EngineNames is the order engines are listed in Settings, same as Linux.
var EngineNames = []struct{ Key, Name string }{
	{"google", "Google"}, {"duckduckgo", "DuckDuckGo"}, {"bing", "Bing"}, {"ecosia", "Ecosia"},
	{"startpage", "Startpage"}, {"kagi", "Kagi"}, {"brave", "Brave Search"}, {"qwant", "Qwant"},
}

// CustomSearch is the user's own search address (with %s for the words), used when the engine is "custom".
var CustomSearch string

// ResolveInput turns what the user typed in the address bar into a URL to load.
// Text that looks like an address is opened; anything else becomes a search.
func ResolveInput(input, engine string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	for _, p := range []string{"http://", "https://"} {
		if strings.HasPrefix(lower, p) {
			return s
		}
	}
	if lower == "about:blank" {
		return s
	}
	// Anything else with a URL scheme (javascript:, data:, file: ...) is searched, never run.
	if !strings.ContainsAny(s, " \t") && looksLikeHost(s) {
		scheme := "https://"
		if isLocalHost(s) {
			scheme = "http://"
		}
		return scheme + s
	}
	tmpl, ok := searchEngines[engine]
	if engine == "custom" && strings.Contains(CustomSearch, "%s") && IsAddress(strings.ReplaceAll(CustomSearch, "%s", "x")) {
		tmpl, ok = CustomSearch, true
	}
	if !ok {
		tmpl = searchEngines["duckduckgo"]
	}
	return strings.Replace(tmpl, "%s", url.QueryEscape(s), 1)
}

// IsAddress reports whether ResolveInput would open the text rather than search for it.
func IsAddress(input string) bool {
	s := strings.TrimSpace(input)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	return s != "" && !strings.ContainsAny(s, " \t") && looksLikeHost(s)
}

func hostOf(s string) string {
	h := s
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if hh, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(hh, "[]")
	}
	return strings.Trim(h, "[]")
}

func looksLikeHost(s string) bool {
	h := hostOf(s)
	if h == "" {
		return false
	}
	if strings.EqualFold(h, "localhost") || net.ParseIP(h) != nil {
		return true
	}
	// "dot.separated" with a plausible last label, and no stray scheme like "javascript:alert(1)".
	if strings.Contains(strings.Split(s, "/")[0], ":") {
		if _, _, err := net.SplitHostPort(strings.Split(s, "/")[0]); err != nil {
			return false
		}
	}
	i := strings.LastIndex(h, ".")
	return i > 0 && i < len(h)-1 && !strings.ContainsAny(h, "()<>\"'")
}

func isLocalHost(s string) bool {
	h := hostOf(s)
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// DisplayURL is what the address bar shows for a page URL.
func DisplayURL(u string) string {
	if u == "" || u == "about:blank" {
		return ""
	}
	return u
}
