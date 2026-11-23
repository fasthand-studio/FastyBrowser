// chrome.storage.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// --- storage -----------------------------------------------------------------

func (m *ExtensionManager) loadStore(ext *Extension, area string) map[string]json.RawMessage {
	switch area {
	case "session":
		if m.session[ext.ID] == nil {
			m.session[ext.ID] = map[string]json.RawMessage{}
		}
		return m.session[ext.ID]
	case "managed":
		return map[string]json.RawMessage{}
	}
	store := map[string]json.RawMessage{}
	if data, err := os.ReadFile(filepath.Join(ext.Dir, ".storage."+area+".json")); err == nil {
		json.Unmarshal(data, &store)
	}
	return store
}

func (m *ExtensionManager) saveStore(ext *Extension, area string, store map[string]json.RawMessage) {
	switch area {
	case "session":
		m.session[ext.ID] = store
	case "managed":
	default:
		out, _ := json.Marshal(store)
		os.WriteFile(filepath.Join(ext.Dir, ".storage."+area+".json"), out, 0o600)
	}
}

func (m *ExtensionManager) handleStorage(ext *Extension, typ string, payload json.RawMessage, done func(any, string)) {
	var p struct {
		Area  string                     `json:"area"`
		Keys  json.RawMessage            `json:"keys"`
		Items map[string]json.RawMessage `json:"items"`
	}
	json.Unmarshal(payload, &p)
	if p.Area == "" {
		p.Area = "local"
	}
	store := m.loadStore(ext, p.Area)
	changes := map[string]map[string]json.RawMessage{}
	change := func(k string, old, nu json.RawMessage) {
		c := map[string]json.RawMessage{}
		if old != nil {
			c["oldValue"] = old
		}
		if nu != nil {
			c["newValue"] = nu
		}
		changes[k] = c
	}

	switch typ {
	case "storage.get":
		out := map[string]json.RawMessage{}
		var keys any
		json.Unmarshal(p.Keys, &keys)
		switch k := keys.(type) {
		case nil:
			for key, v := range store {
				out[key] = v
			}
		case string:
			if v, ok := store[k]; ok {
				out[k] = v
			}
		case []any:
			for _, x := range k {
				if s, ok := x.(string); ok {
					if v, ok := store[s]; ok {
						out[s] = v
					}
				}
			}
		case map[string]any:
			for key, def := range k {
				if v, ok := store[key]; ok {
					out[key] = v
				} else {
					out[key], _ = json.Marshal(def)
				}
			}
		}
		done(out, "")
		return
	case "storage.set":
		for k, v := range p.Items {
			if !bytes.Equal(store[k], v) {
				change(k, store[k], v)
			}
			store[k] = v
		}
	case "storage.remove":
		var keys []string
		json.Unmarshal(p.Keys, &keys)
		for _, k := range keys {
			if old, ok := store[k]; ok {
				change(k, old, nil)
				delete(store, k)
			}
		}
	case "storage.clear":
		for k, old := range store {
			change(k, old, nil)
		}
		store = map[string]json.RawMessage{}
	case "storage.bytes":
		n := 0
		for k, v := range store {
			n += len(k) + len(v)
		}
		done(n, "")
		return
	}
	if p.Area == "managed" {
		done(nil, "")
		return
	}
	m.saveStore(ext, p.Area, store)
	done(nil, "")
	if len(changes) > 0 {
		m.broadcast(ext, "storageChanged", map[string]any{"area": p.Area, "changes": changes})
	}
}
