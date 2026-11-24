package main

// What you type, turned into a place (Address.swift, Engine.swift,
// CleanLink.swift).

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type engine struct{ key, name, template string }

var engines = []engine{
	{"google", "Google", "https://www.google.com/search?q=%s"},
	{"duckduckgo", "DuckDuckGo", "https://duckduckgo.com/?q=%s"},
	{"bing", "Bing", "https://www.bing.com/search?q=%s"},
	{"ecosia", "Ecosia", "https://www.ecosia.org/search?q=%s"},
	{"startpage", "Startpage", "https://www.startpage.com/sp/search?query=%s"},
	{"kagi", "Kagi", "https://kagi.com/search?q=%s"},
	{"brave", "Brave Search", "https://search.brave.com/search?q=%s"},
	{"qwant", "Qwant", "https://www.qwant.com/?q=%s"},
	{"custom", "Custom", ""},
}

func engineByKey(key string) engine {
	for _, e := range engines {
		if e.key == key {
			return e
		}
	}
	return engines[1]
}

var ourSchemes = map[string]bool{"http": true, "https": true, "file": true, "about": true,
	"data": true, "view-source": true, "webkit": true}

func searchURL(text, key, custom string) string {
	t := engineByKey(key).template
	if key == "custom" {
		if strings.Contains(custom, "%s") {
			t = custom
		} else {
			t = engines[1].template
		}
	}
	return strings.ReplaceAll(t, "%s", url.QueryEscape(text))
}

func ipv4(host string) ([4]int, bool) {
	var out [4]int
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || p == "" {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func privateRange(host string) bool {
	ip, ok := ipv4(host)
	if !ok {
		return false
	}
	a, b := ip[0], ip[1]
	return a == 10 || (a == 192 && b == 168) || (a == 172 && b >= 16 && b <= 31)
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127
}

func looksLikeHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if _, ok := ipv4(host); ok {
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return false
	}
	for _, l := range parts {
		if l == "" || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
		for _, r := range l {
			if !isAlnum(r) && r != '-' {
				return false
			}
		}
	}
	tld := parts[len(parts)-1]
	if len([]rune(tld)) < 2 {
		return false
	}
	for _, r := range tld {
		if !(r > 127 || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

var headSplit = regexp.MustCompile(`[/?#]`)

// urlFrom gives a URL for what was typed, or "" when it reads as a search.
func urlFrom(typed string) string {
	text := strings.TrimSpace(typed)
	if text == "" || strings.ContainsAny(text, " \t") {
		return ""
	}
	if (strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//")) || strings.HasPrefix(text, "~/") {
		p := text
		if strings.HasPrefix(p, "~/") {
			home, _ := os.UserHomeDir()
			p = filepath.Join(home, p[2:])
		}
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return (&url.URL{Scheme: "file", Path: abs}).String()
		}
	}
	if i := strings.Index(text, "://"); i >= 0 {
		if ourSchemes[strings.ToLower(text[:i])] {
			return text
		}
		return ""
	}
	low := strings.ToLower(text)
	for _, p := range []string{"about:", "data:", "view-source:"} {
		if strings.HasPrefix(low, p) {
			return text
		}
	}
	head := headSplit.Split(text, 2)[0]
	if strings.Contains(head, "@") {
		return ""
	}
	if strings.HasPrefix(head, "[::1]") {
		return "http://" + text
	}
	host := strings.Split(head, ":")[0]
	if !looksLikeHost(strings.ToLower(host)) {
		return ""
	}
	if host == "0.0.0.0" {
		text = strings.Replace(text, "0.0.0.0", "localhost", 1)
		host = "localhost"
	}
	local := host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		host == "127.0.0.1" || privateRange(host)
	if local {
		return "http://" + text
	}
	return "https://" + text
}

func resolve(typed, key, custom string) string {
	if u := urlFrom(typed); u != "" {
		return u
	}
	return searchURL(strings.TrimSpace(typed), key, custom)
}

// hostOf is the site a page belongs to, without "www.".
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(h, "www.")
}

// pretty is how an address reads in the field when you aren't editing it.
func pretty(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return raw
}

func editable(raw string) string {
	if strings.HasPrefix(raw, "https://") {
		rest := raw[len("https://"):]
		if strings.HasSuffix(rest, "/") && strings.Count(rest, "/") == 1 {
			return rest[:len(rest)-1]
		}
		return rest
	}
	return raw
}

var tracking = map[string]bool{"fbclid": true, "gclid": true, "dclid": true, "gbraid": true, "wbraid": true,
	"msclkid": true, "yclid": true, "twclid": true, "ttclid": true, "mc_eid": true, "igshid": true, "igsh": true,
	"_hsenc": true, "_hsmi": true, "mkt_tok": true}
var trackingOn = map[string][]string{"si": {"youtube.com", "youtu.be", "spotify.com"}}
var signatures = map[string]bool{"x-amz-signature": true, "x-goog-signature": true, "signature": true, "sig": true}

func tracks(name, host string) bool {
	if strings.HasPrefix(name, "utm_") || tracking[name] {
		return true
	}
	for _, s := range trackingOn[name] {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// cleanLink is the address without parameters that only say where a click
// came from.
func cleanLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery == "" {
		return raw
	}
	host := strings.ToLower(u.Hostname())
	pieces := strings.Split(u.RawQuery, "&")
	names := make([]string, len(pieces))
	for i, p := range pieces {
		n := strings.SplitN(p, "=", 2)[0]
		if d, err := url.QueryUnescape(n); err == nil {
			n = d
		}
		names[i] = strings.ToLower(n)
		if signatures[names[i]] {
			return raw
		}
	}
	var kept []string
	for i, p := range pieces {
		if !tracks(names[i], host) {
			kept = append(kept, p)
		}
	}
	if len(kept) == len(pieces) {
		return raw
	}
	u.RawQuery = strings.Join(kept, "&")
	return u.String()
}
