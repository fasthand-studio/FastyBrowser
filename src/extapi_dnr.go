// chrome.declarativeNetRequest.
package main

import (
	"encoding/json"
	"regexp"
	"sort"
)

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
