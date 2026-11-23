// chrome.contextMenus.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// --- context menus -------------------------------------------------------------

type extMenu struct {
	RawID    json.RawMessage `json:"id"`
	Title    string          `json:"title"`
	Contexts []string        `json:"contexts"`
	Parent   json.RawMessage `json:"parentId"`
	Type     string          `json:"type"`
	Visible  *bool           `json:"visible"`
	Enabled  *bool           `json:"enabled"`
}

func (mn *extMenu) key() string { return strings.Trim(string(mn.RawID), `"`) }

func (mn *extMenu) parentKey() string { return strings.Trim(string(mn.Parent), `"`) }

func (mn *extMenu) shown(c map[string]bool) bool {
	if mn.Visible != nil && !*mn.Visible {
		return false
	}
	if len(mn.Contexts) == 0 {
		return c["page"]
	}
	for _, x := range mn.Contexts {
		if c[x] {
			return true
		}
	}
	return false
}

func (m *ExtensionManager) handleMenus(ext *Extension, typ string, payload json.RawMessage) {
	switch typ {
	case "menus.create":
		var mn extMenu
		if json.Unmarshal(payload, &mn) == nil && len(mn.RawID) > 0 {
			for i, old := range ext.menus {
				if old.key() == mn.key() {
					ext.menus = append(ext.menus[:i], ext.menus[i+1:]...)
					break
				}
			}
			ext.menus = append(ext.menus, mn)
		}
	case "menus.update":
		var p struct {
			ID    json.RawMessage `json:"id"`
			Props extMenu         `json:"props"`
		}
		json.Unmarshal(payload, &p)
		id := strings.Trim(string(p.ID), `"`)
		var raw map[string]json.RawMessage
		var props struct {
			Props map[string]json.RawMessage `json:"props"`
		}
		json.Unmarshal(payload, &props)
		raw = props.Props
		for i := range ext.menus {
			if ext.menus[i].key() != id {
				continue
			}
			if v, ok := raw["title"]; ok {
				json.Unmarshal(v, &ext.menus[i].Title)
			}
			if v, ok := raw["contexts"]; ok {
				json.Unmarshal(v, &ext.menus[i].Contexts)
			}
			if v, ok := raw["visible"]; ok {
				json.Unmarshal(v, &ext.menus[i].Visible)
			}
			if v, ok := raw["enabled"]; ok {
				json.Unmarshal(v, &ext.menus[i].Enabled)
			}
		}
	case "menus.remove":
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		json.Unmarshal(payload, &p)
		id := strings.Trim(string(p.ID), `"`)
		kept := ext.menus[:0]
		for _, mn := range ext.menus {
			if mn.key() != id && mn.parentKey() != id {
				kept = append(kept, mn)
			}
		}
		ext.menus = kept
	case "menus.removeAll":
		ext.menus = nil
	}
}

// AppendMenus adds the enabled extensions' context-menu items to a tab's menu.
func (m *ExtensionManager) AppendMenus(t *Tab, menu *webkit.ContextMenu, hit *webkit.HitTestResult) {
	ctx := map[string]bool{"all": true, "page": true}
	info := map[string]any{"pageUrl": t.url, "frameUrl": t.url}
	if hit.ContextIsLink() {
		ctx["link"] = true
		info["linkUrl"] = hit.LinkURI()
	}
	if hit.ContextIsImage() {
		ctx["image"] = true
		info["srcUrl"] = hit.ImageURI()
		info["mediaType"] = "image"
	}
	if hit.ContextIsMedia() {
		ctx["video"], ctx["audio"] = true, true
		info["srcUrl"] = hit.MediaURI()
	}
	if hit.ContextIsSelection() {
		ctx["selection"] = true
	}
	if hit.ContextIsEditable() {
		ctx["editable"] = true
		info["editable"] = true
	}

	first := true
	for _, ext := range m.enabledList() {
		var top []*extMenu
		kids := map[string][]*extMenu{}
		for i := range ext.menus {
			mn := &ext.menus[i]
			if !mn.shown(ctx) {
				continue
			}
			if p := mn.parentKey(); p != "" && p != "null" {
				kids[p] = append(kids[p], mn)
			} else {
				top = append(top, mn)
			}
		}
		if len(top) == 0 {
			continue
		}
		if first {
			menu.Append(webkit.NewContextMenuItemSeparator())
			first = false
		}
		build := func(mn *extMenu) *webkit.ContextMenuItem {
			if mn.Type == "separator" {
				return webkit.NewContextMenuItemSeparator()
			}
			title := strings.ReplaceAll(mn.Title, "%s", "…")
			if sub := kids[mn.key()]; len(sub) > 0 {
				sm := webkit.NewContextMenu()
				for _, k := range sub {
					sm.Append(m.menuItem(ext, k, t, info, nil))
				}
				return webkit.NewContextMenuItemWithSubmenu(title, sm)
			}
			return m.menuItem(ext, mn, t, info, kids)
		}
		if len(top) == 1 {
			menu.Append(build(top[0]))
			continue
		}
		sm := webkit.NewContextMenu()
		for _, mn := range top {
			sm.Append(build(mn))
		}
		menu.Append(webkit.NewContextMenuItemWithSubmenu(ext.Manifest.Name, sm))
	}
}

func (m *ExtensionManager) menuItem(ext *Extension, mn *extMenu, t *Tab, info map[string]any, _ map[string][]*extMenu) *webkit.ContextMenuItem {
	m.reqSeq++
	ga := gio.NewSimpleAction(fmt.Sprintf("ctx-ext-%d", m.reqSeq), nil)
	id := mn.RawID
	ga.SetEnabled(mn.Enabled == nil || *mn.Enabled)
	ga.ConnectActivate(func(*glib.Variant) {
		click := map[string]any{"menuItemId": id}
		for k, v := range info {
			click[k] = v
		}
		fire := func(sel string) {
			if sel != "" {
				click["selectionText"] = sel
			}
			m.fireTo(ext, "contextMenus.onClicked", click, m.tabInfo(t))
			m.fireTo(ext, "menus.onClicked", click, m.tabInfo(t))
		}
		if t.view != nil {
			t.run("window.getSelection().toString()", fire)
		} else {
			fire("")
		}
	})
	return webkit.NewContextMenuItemFromGaction(ga, strings.ReplaceAll(mn.Title, "%s", "…"), nil)
}
