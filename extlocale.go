// Extension text: locale messages, localized manifests, icons and badge colours.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- localisation ------------------------------------------------------------

// uiLocaleCandidates lists the user's locales from most to least specific
// ("bn_BD" then "bn").
func uiLocaleCandidates() []string {
	raw := ""
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			raw = v
			break
		}
	}
	var out []string
	for _, part := range strings.Split(raw, ":") {
		part = strings.SplitN(strings.SplitN(part, ".", 2)[0], "@", 2)[0]
		part = strings.ReplaceAll(part, "-", "_")
		if part == "" {
			continue
		}
		out = append(out, part)
		if i := strings.Index(part, "_"); i > 0 {
			out = append(out, part[:i])
		}
	}
	return out
}

func uiLocale() string {
	if c := uiLocaleCandidates(); len(c) > 0 {
		return strings.ReplaceAll(c[0], "_", "-")
	}
	return "en-US"
}

// loadMessages merges the extension's locales: the user's language wins,
// then its default locale, then English; missing keys fall back in that order.
func (e *Extension) loadMessages() {
	order := append([]string{}, uiLocaleCandidates()...)
	if e.Manifest.DefaultLocale != "" {
		order = append(order, e.Manifest.DefaultLocale)
	}
	order = append(order, "en", "en_US", "en_GB")
	merged := map[string]json.RawMessage{}
	for i := len(order) - 1; i >= 0; i-- {
		d, err := os.ReadFile(filepath.Join(e.Dir, "_locales", order[i], "messages.json"))
		if err != nil {
			continue
		}
		var msgs map[string]json.RawMessage
		if json.Unmarshal(bytes.TrimPrefix(d, utf8BOM), &msgs) != nil {
			continue
		}
		for k, v := range msgs {
			merged[strings.ToLower(k)] = v
		}
	}
	if len(merged) > 0 {
		e.messages, _ = json.Marshal(merged)
	}
	e.Manifest.Name = e.localize(e.Manifest.Name)
	e.Manifest.Description = e.localize(e.Manifest.Description)
	if a := e.action(); a != nil {
		a.DefaultTitle = e.localize(a.DefaultTitle)
	}
}

func (e *Extension) localize(s string) string {
	if !strings.HasPrefix(s, "__MSG_") || !strings.HasSuffix(s, "__") || e.messages == nil {
		return s
	}
	key := s[len("__MSG_") : len(s)-2]
	var msgs map[string]struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(e.messages, &msgs) != nil {
		return s
	}
	for k, v := range msgs {
		if strings.EqualFold(k, key) && v.Message != "" {
			return v.Message
		}
	}
	return s
}

func (e *Extension) popupPath() string {
	if e.popupOverride != "" {
		return e.popupOverride
	}
	if a := e.action(); a != nil {
		return a.DefaultPopup
	}
	return ""
}

// badgeColorCSS accepts [r,g,b,a] or a CSS colour string.
func badgeColorCSS(raw json.RawMessage) string {
	var arr []float64
	if json.Unmarshal(raw, &arr) == nil && len(arr) >= 3 {
		a := 1.0
		if len(arr) > 3 {
			a = arr[3] / 255
		}
		return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", int(arr[0]), int(arr[1]), int(arr[2]), a)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// iconPathOf takes a path string or a {size: path} map and returns one path.
func iconPathOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		for _, k := range []string{"32", "38", "24", "19", "16", "48", "128"} {
			if p, ok := m[k]; ok {
				return p
			}
		}
		for _, p := range m {
			return p
		}
	}
	return ""
}

// localizedManifest is the manifest as runtime.getManifest() returns it: with
// every "__MSG_key__" replaced by the extension's own text.
func (e *Extension) localizedManifest() json.RawMessage {
	var v any
	if json.Unmarshal(e.raw, &v) != nil {
		return json.RawMessage("{}")
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case string:
			return e.localize(t)
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case map[string]any:
			for k := range t {
				t[k] = walk(t[k])
			}
		}
		return x
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}
