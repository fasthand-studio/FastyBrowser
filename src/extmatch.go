// Chrome match patterns turned into regular expressions and allow lists.
package main

import (
	"regexp"
	"strings"
)

// --- content script injection ------------------------------------------------

// matchPatternToRegex converts a WebExtension match pattern to a regexp.
// Patterns: <scheme>://<host>/<path>
// <all_urls> matches http and https.
func matchPatternToRegex(pattern string) string {
	if pattern == "<all_urls>" {
		return `^https?://.*$`
	}
	parts := strings.SplitN(pattern, "://", 2)
	if len(parts) != 2 {
		return ""
	}
	scheme := parts[0]
	rest := parts[1]
	hostPath := strings.SplitN(rest, "/", 2)
	host := hostPath[0]
	path := ""
	if len(hostPath) > 1 {
		path = hostPath[1]
	}

	var re strings.Builder
	re.WriteString("^")

	switch scheme {
	case "*":
		re.WriteString("https?")
	case "http", "https", "file", "ftp":
		re.WriteString(regexp.QuoteMeta(scheme))
	default:
		return ""
	}
	re.WriteString("://")

	if host == "*" {
		re.WriteString("[^/]*")
	} else if strings.HasPrefix(host, "*.") {
		re.WriteString("([^/]+\\.")
		re.WriteString(regexp.QuoteMeta(host[2:]))
		re.WriteString("|")
		re.WriteString(regexp.QuoteMeta(host[2:]))
		re.WriteString(")")
	} else {
		re.WriteString(regexp.QuoteMeta(host))
	}

	re.WriteString("(:[0-9]+)?/")
	for _, c := range path {
		if c == '*' {
			re.WriteString(".*")
		} else {
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	return re.String()
}

// matchPatternsToAllowList converts match patterns to WebKit allow-list format.
func matchPatternsToAllowList(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p == "<all_urls>" {
			return nil // nil means all URLs in WebKit
		}
		// WebKit uses shell-like patterns: *://host/*
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
