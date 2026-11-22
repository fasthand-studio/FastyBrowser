package main

// declarativeNetRequest: an extension's rules (static rulesets from its
// manifest, plus dynamic and session rules) are translated to a WebKit content
// filter, so ad blockers built on DNR block inside WebKit's networking.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

type dnrRule struct {
	ID       int `json:"id"`
	Priority int `json:"priority"`
	Action   struct {
		Type string `json:"type"`
	} `json:"action"`
	Condition struct {
		URLFilter                string   `json:"urlFilter"`
		RegexFilter              string   `json:"regexFilter"`
		CaseSensitive            *bool    `json:"isUrlFilterCaseSensitive"`
		Domains                  []string `json:"domains"`
		InitiatorDomains         []string `json:"initiatorDomains"`
		ExcludedDomains          []string `json:"excludedDomains"`
		ExcludedInitiatorDomains []string `json:"excludedInitiatorDomains"`
		RequestDomains           []string `json:"requestDomains"`
		ResourceTypes            []string `json:"resourceTypes"`
		ExcludedResourceTypes    []string `json:"excludedResourceTypes"`
		RequestMethods           []string `json:"requestMethods"`
		DomainType               string   `json:"domainType"`
	} `json:"condition"`
}

type dnrManifest struct {
	RuleResources []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		Path    string `json:"path"`
	} `json:"rule_resources"`
}

// dnrSaved is what survives a restart.
type dnrSaved struct {
	Rulesets map[string]bool   `json:"rulesets"`
	Dynamic  []json.RawMessage `json:"dynamic"`
}

func (m *ExtensionManager) dnrInit(ext *Extension) {
	ext.dnrEnabled = map[string]bool{}
	var dm dnrManifest
	if len(ext.Manifest.DNR) > 0 {
		json.Unmarshal(ext.Manifest.DNR, &dm)
	}
	for _, r := range dm.RuleResources {
		ext.dnrEnabled[r.ID] = r.Enabled
	}
	var saved dnrSaved
	if data, err := os.ReadFile(filepath.Join(ext.Dir, ".dnr.json")); err == nil && json.Unmarshal(data, &saved) == nil {
		for id, on := range saved.Rulesets {
			if _, known := ext.dnrEnabled[id]; known {
				ext.dnrEnabled[id] = on
			}
		}
		ext.dnrDynamic = saved.Dynamic
	}
}

func (m *ExtensionManager) dnrPersist(ext *Extension) {
	data, _ := json.Marshal(dnrSaved{Rulesets: ext.dnrEnabled, Dynamic: ext.dnrDynamic})
	os.WriteFile(filepath.Join(ext.Dir, ".dnr.json"), data, 0o644)
}

func (m *ExtensionManager) dnrStoreInit() {
	if m.dnrStore != nil {
		return
	}
	folder := filepath.Join(cacheDir(), "extfilters")
	os.MkdirAll(folder, 0o755)
	m.dnrStore = webkit.NewUserContentFilterStore(folder)
}

func dnrIdentPrefix(ext *Extension) string {
	sum := sha256.Sum256([]byte(ext.ID))
	return "ext-" + hex.EncodeToString(sum[:])[:10] + "-"
}

// dnrRebuildSoon waits for a burst of rule updates (extensions send several
// at start-up) to settle before compiling, since compiling large lists is slow.
func (m *ExtensionManager) dnrRebuildSoon(ext *Extension) {
	if ext.dnrPending {
		return
	}
	ext.dnrPending = true
	glib.TimeoutAdd(1500, func() bool {
		ext.dnrPending = false
		m.dnrRebuild(ext)
		return false
	})
}

// dnrRebuild recompiles an extension's rules and swaps the filter on every tab.
func (m *ExtensionManager) dnrRebuild(ext *Extension) {
	var rules []dnrRule
	var dm dnrManifest
	if len(ext.Manifest.DNR) > 0 {
		json.Unmarshal(ext.Manifest.DNR, &dm)
	}
	if ext.Enabled {
		for _, rs := range dm.RuleResources {
			if !ext.dnrEnabled[rs.ID] {
				continue
			}
			data, err := ext.readFile(rs.Path)
			if err != nil {
				continue
			}
			var list []dnrRule
			if json.Unmarshal(data, &list) == nil {
				rules = append(rules, list...)
			}
		}
		for _, raw := range append(append([]json.RawMessage{}, ext.dnrDynamic...), ext.dnrSession...) {
			var r dnrRule
			if json.Unmarshal(raw, &r) == nil {
				rules = append(rules, r)
			}
		}
	}
	conv := convertDNR(rules)
	if len(conv) == 0 {
		m.dnrApply(ext, nil)
		return
	}
	data, _ := json.Marshal(conv)
	sum := sha256.Sum256(data)
	ident := dnrIdentPrefix(ext) + hex.EncodeToString(sum[:])[:24]
	m.dnrStoreInit()
	ext.dnrGen++
	gen := ext.dnrGen
	apply := func(f *webkit.UserContentFilter) {
		if gen != ext.dnrGen {
			return
		}
		m.dnrApply(ext, f)
		m.dnrStore.FetchIdentifiers(context.Background(), func(res gio.AsyncResulter) {
			for _, old := range m.dnrStore.FetchIdentifiersFinish(res) {
				if strings.HasPrefix(old, dnrIdentPrefix(ext)) && old != ident {
					m.dnrStore.Remove(context.Background(), old, func(r gio.AsyncResulter) { m.dnrStore.RemoveFinish(r) })
				}
			}
		})
	}
	m.dnrStore.Load(context.Background(), ident, func(res gio.AsyncResulter) {
		f, err := m.dnrStore.LoadFinish(res)
		if err == nil && f != nil {
			apply(f)
			return
		}
		m.dnrStore.Save(context.Background(), ident, glib.NewBytes(data), func(res gio.AsyncResulter) {
			f, err := m.dnrStore.SaveFinish(res)
			if err != nil {
				log.Printf("fasty: ext %s: content rules: %v", ext.ID, err)
				return
			}
			apply(f)
		})
	})
}

func (m *ExtensionManager) dnrApply(ext *Extension, f *webkit.UserContentFilter) {
	old := ext.dnrFilter
	ext.dnrFilter = f
	if m.b == nil {
		return
	}
	for _, t := range m.b.tabs {
		if t.ucm == nil {
			continue
		}
		if old != nil {
			t.ucm.RemoveFilter(old)
		}
		if f != nil {
			t.ucm.AddFilter(f)
		}
	}
}

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

// --- chrome.declarativeNetRequest calls ----------------------------------------

func (m *ExtensionManager) handleDNR(ext *Extension, typ string, payload json.RawMessage, done func(any, string)) {
	switch typ {
	case "dnr.updateDynamic", "dnr.updateSession":
		var p struct {
			Add    []json.RawMessage `json:"addRules"`
			Remove []int             `json:"removeRuleIds"`
		}
		json.Unmarshal(payload, &p)
		list := &ext.dnrDynamic
		if typ == "dnr.updateSession" {
			list = &ext.dnrSession
		}
		drop := map[int]bool{}
		for _, id := range p.Remove {
			drop[id] = true
		}
		for _, raw := range p.Add {
			var r struct {
				ID int `json:"id"`
			}
			json.Unmarshal(raw, &r)
			drop[r.ID] = true
		}
		kept := (*list)[:0:0]
		for _, raw := range *list {
			var r struct {
				ID int `json:"id"`
			}
			json.Unmarshal(raw, &r)
			if !drop[r.ID] {
				kept = append(kept, raw)
			}
		}
		*list = append(kept, p.Add...)
		if typ == "dnr.updateDynamic" {
			m.dnrPersist(ext)
		}
		m.dnrRebuildSoon(ext)
		done(nil, "")
	case "dnr.getDynamic":
		if ext.dnrDynamic == nil {
			done([]any{}, "")
		} else {
			done(ext.dnrDynamic, "")
		}
	case "dnr.getSession":
		if ext.dnrSession == nil {
			done([]any{}, "")
		} else {
			done(ext.dnrSession, "")
		}
	case "dnr.updateRulesets":
		var p struct {
			Enable  []string `json:"enableRulesetIds"`
			Disable []string `json:"disableRulesetIds"`
		}
		json.Unmarshal(payload, &p)
		for _, id := range p.Disable {
			if _, ok := ext.dnrEnabled[id]; ok {
				ext.dnrEnabled[id] = false
			}
		}
		for _, id := range p.Enable {
			if _, ok := ext.dnrEnabled[id]; ok {
				ext.dnrEnabled[id] = true
			}
		}
		m.dnrPersist(ext)
		m.dnrRebuildSoon(ext)
		done(nil, "")
	case "dnr.getEnabledRulesets":
		ids := []string{}
		for id, on := range ext.dnrEnabled {
			if on {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		done(ids, "")
	case "dnr.isRegexSupported":
		var p struct {
			Regex string `json:"regex"`
		}
		json.Unmarshal(payload, &p)
		_, err := regexp.Compile(p.Regex)
		if err != nil || !dnrRegexOK.MatchString(p.Regex) {
			done(map[string]any{"isSupported": false, "reason": "syntaxError"}, "")
		} else {
			done(map[string]any{"isSupported": true}, "")
		}
	}
}
