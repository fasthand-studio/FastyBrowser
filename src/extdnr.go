package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
