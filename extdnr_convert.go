// Turning declarativeNetRequest rules into WebKit filters.
package main

import (
	"regexp"
	"sort"
	"strings"
)

// --- translation ---------------------------------------------------------------

var dnrTypeMap = map[string]string{
	"sub_frame": "document", "stylesheet": "style-sheet", "script": "script", "image": "image",
	"font": "font", "object": "raw", "xmlhttprequest": "raw", "ping": "ping", "csp_report": "ping",
	"media": "media", "websocket": "raw", "webtransport": "raw", "webbundle": "raw", "other": "raw",
}

var dnrAllTypes = []string{"sub_frame", "stylesheet", "script", "image", "font", "object", "xmlhttprequest",
	"ping", "csp_report", "media", "websocket", "other"}

// WebKit accepts only a small regular-expression subset; anything outside it
// would make the whole list fail to compile.
var dnrRegexOK = regexp.MustCompile(`^\^?(?:[A-Za-z0-9_\-:/%&=~!,;@#'" .*+?]|\\[.\-/?+*:\\]|\[\^?[A-Za-z0-9_\-.:/%]+\])*\$?$`)

func dnrURLFilter(f string, caseSensitive bool) (string, bool) {
	if !caseSensitive {
		f = strings.ToLower(f)
	}
	prefix := ""
	switch {
	case strings.HasPrefix(f, "||"):
		prefix, f = `^[^:]+://+([^/]+\.)?`, f[2:]
	case strings.HasPrefix(f, "|"):
		prefix, f = "^", f[1:]
	}
	suffix := ""
	if strings.HasSuffix(f, "|") {
		suffix, f = "$", f[:len(f)-1]
	}
	var b strings.Builder
	b.WriteString(prefix)
	for _, c := range f {
		switch {
		case c > 126 || c < 32:
			return "", false
		case c == '*':
			b.WriteString(".*")
		case c == '^':
			b.WriteString(`[^a-zA-Z0-9_.%-]`)
		case c == '.' || c == '+' || c == '?':
			b.WriteString(`\` + string(c))
		case strings.ContainsRune("|(){}[]$\\", c):
			return "", false
		default:
			b.WriteRune(c)
		}
	}
	b.WriteString(suffix)
	out := b.String()
	if out == "" {
		out = ".*"
	}
	return out, true
}

func domainGlobs(list []string) []string {
	out := make([]string, 0, len(list))
	for _, d := range list {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			out = append(out, "*"+d)
		}
	}
	return out
}

func convertDNR(rules []dnrRule) []map[string]any {
	type ordered struct {
		prio, kind int
		rule       map[string]any
	}
	var out []ordered
	for i := range rules {
		r := &rules[i]
		var act map[string]any
		kind := 0
		switch r.Action.Type {
		case "block":
			act = map[string]any{"type": "block"}
		case "allow":
			act, kind = map[string]any{"type": "ignore-previous-rules"}, 2
		case "upgradeScheme":
			act, kind = map[string]any{"type": "make-https"}, 1
		default:
			continue
		}
		c := &r.Condition
		if len(c.RequestMethods) > 0 {
			continue
		}

		var patterns []string
		switch {
		case c.RegexFilter != "":
			if !dnrRegexOK.MatchString(c.RegexFilter) {
				continue
			}
			p := c.RegexFilter
			if c.CaseSensitive == nil || !*c.CaseSensitive {
				p = strings.ToLower(p)
			}
			patterns = []string{p}
		case c.URLFilter != "":
			p, ok := dnrURLFilter(c.URLFilter, c.CaseSensitive != nil && *c.CaseSensitive)
			if !ok {
				continue
			}
			patterns = []string{p}
		case len(c.RequestDomains) > 0:
			for i, d := range c.RequestDomains {
				if i >= 40 || strings.ContainsAny(d, `\|()[]{}*^$+?`) {
					continue
				}
				patterns = append(patterns, `^[^:]+://+([^/]+\.)?`+strings.ReplaceAll(strings.ToLower(d), ".", `\.`)+`[:/]`)
			}
		default:
			if len(c.Domains)+len(c.InitiatorDomains) == 0 {
				continue
			}
			patterns = []string{".*"}
		}
		if len(patterns) == 0 {
			continue
		}

		types := c.ResourceTypes
		if len(types) == 0 {
			types = append([]string{}, dnrAllTypes...)
			if len(c.ExcludedResourceTypes) > 0 {
				types = append(types, "main_frame")
			}
		}
		excl := map[string]bool{}
		for _, t := range c.ExcludedResourceTypes {
			excl[t] = true
		}
		var wk []string
		seen := map[string]bool{}
		main, sub := false, false
		for _, t := range types {
			if excl[t] {
				continue
			}
			switch t {
			case "main_frame":
				main = true
			case "sub_frame":
				sub = true
			default:
				if w, ok := dnrTypeMap[t]; ok && !seen[w] {
					seen[w] = true
					wk = append(wk, w)
				}
			}
		}
		sort.Strings(wk)

		var domains, unless []string
		if d := append(append([]string{}, c.Domains...), c.InitiatorDomains...); len(d) > 0 {
			domains = domainGlobs(d)
		} else if d := append(append([]string{}, c.ExcludedDomains...), c.ExcludedInitiatorDomains...); len(d) > 0 {
			unless = domainGlobs(d)
		}
		var loadType []string
		switch c.DomainType {
		case "thirdParty":
			loadType = []string{"third-party"}
		case "firstParty":
			loadType = []string{"first-party"}
		}

		trigger := func(pattern string, mut func(map[string]any)) map[string]any {
			t := map[string]any{"url-filter": pattern}
			if c.CaseSensitive != nil && *c.CaseSensitive {
				t["url-filter-is-case-sensitive"] = true
			}
			if len(domains) > 0 {
				t["if-domain"] = domains
			} else if len(unless) > 0 {
				t["unless-domain"] = unless
			}
			if loadType != nil {
				t["load-type"] = loadType
			}
			mut(t)
			return t
		}
		add := func(t map[string]any) {
			out = append(out, ordered{r.Priority, kind, map[string]any{"trigger": t, "action": act}})
		}
		for _, p := range patterns {
			if len(wk) > 0 {
				add(trigger(p, func(t map[string]any) { t["resource-type"] = wk }))
			}
			switch {
			case main && sub:
				add(trigger(p, func(t map[string]any) { t["resource-type"] = []string{"document"} }))
			case main:
				add(trigger(p, func(t map[string]any) {
					t["resource-type"] = []string{"document"}
					t["load-context"] = []string{"top-frame"}
				}))
			case sub:
				add(trigger(p, func(t map[string]any) {
					t["resource-type"] = []string{"document"}
					t["load-context"] = []string{"child-frame"}
				}))
			}
		}
	}
	// Later rules win in WebKit, so order by priority; on a tie allow beats block.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].prio != out[j].prio {
			return out[i].prio < out[j].prio
		}
		return out[i].kind < out[j].kind
	})
	res := make([]map[string]any, len(out))
	for i, o := range out {
		res[i] = o.rule
	}
	return res
}
