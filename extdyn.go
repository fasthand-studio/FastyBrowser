// Content scripts registered at run time.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// --- dynamic content scripts -------------------------------------------------

// dynFile is a script or style given as a file path or inline code.
type dynFile struct{ File, Code string }

func (d *dynFile) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		d.File = s
		return nil
	}
	var o struct {
		File string `json:"file"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	d.File, d.Code = o.File, o.Code
	return nil
}

func (d dynFile) MarshalJSON() ([]byte, error) {
	if d.Code != "" {
		return json.Marshal(map[string]string{"code": d.Code})
	}
	return json.Marshal(d.File)
}

type dynScript struct {
	ID             string    `json:"id"`
	Matches        []string  `json:"matches"`
	ExcludeMatches []string  `json:"excludeMatches"`
	JS             []dynFile `json:"js"`
	CSS            []dynFile `json:"css"`
	RunAt          string    `json:"runAt"`
	AllFrames      bool      `json:"allFrames"`
	World          string    `json:"world"`
	Persist        *bool     `json:"persistAcrossSessions"`
	User           bool      `json:"userScript,omitempty"`
}

func (d dynScript) def() ContentScriptDef {
	c := ContentScriptDef{ID: d.ID, Matches: d.Matches, ExcludeM: d.ExcludeMatches,
		RunAt: d.RunAt, AllFrames: d.AllFrames, World: d.World, UserScript: d.User && !strings.EqualFold(d.World, "MAIN")}
	for _, f := range d.JS {
		if f.Code != "" {
			c.JSCode = append(c.JSCode, f.Code)
		} else if f.File != "" {
			c.JS = append(c.JS, f.File)
		}
	}
	for _, f := range d.CSS {
		if f.Code != "" {
			c.CSSCode = append(c.CSSCode, f.Code)
		} else if f.File != "" {
			c.CSS = append(c.CSS, f.File)
		}
	}
	return c
}

func (e *Extension) contentDefs() []ContentScriptDef {
	defs := append([]ContentScriptDef{}, e.Manifest.ContentScripts...)
	for _, d := range e.dyn {
		defs = append(defs, d.def())
	}
	return defs
}

func (m *ExtensionManager) loadDyn(ext *Extension) {
	data, err := os.ReadFile(filepath.Join(ext.Dir, ".dynscripts.json"))
	if err == nil {
		json.Unmarshal(data, &ext.dyn)
	}
}

func (m *ExtensionManager) saveDyn(ext *Extension) {
	keep := []dynScript{}
	for _, d := range ext.dyn {
		if d.Persist == nil || *d.Persist {
			keep = append(keep, d)
		}
	}
	data, _ := json.Marshal(keep)
	os.WriteFile(filepath.Join(ext.Dir, ".dynscripts.json"), data, 0o644)
}
