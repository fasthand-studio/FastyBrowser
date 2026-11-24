package main

// The one field: an address and you go there, words and you search. It
// finishes addresses from your own history and never sends what you type
// anywhere until you press Return (Omnibox.swift).

import (
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

type suggestion struct {
	kind  string // tab | url | search
	tab   *Tab
	value string
	title string
	sub   string
}

type Omnibox struct {
	*gtk.Box
	b          *Browser
	entry      *gtk.Entry
	pop        *gtk.Popover
	list       *gtk.ListBox
	items      []suggestion
	newTab     bool
	private    bool
	editing    bool
	completing bool
	lastLen    int
	typed      string
}

const placeholder = "Search or enter address"

func newOmnibox(b *Browser) *Omnibox {
	o := &Omnibox{Box: gtk.NewBox(gtk.OrientationHorizontal, 0), b: b}
	o.AddCSSClass("omnibox")
	o.entry = gtk.NewEntry()
	o.entry.SetHExpand(true)
	o.entry.AddCSSClass("address")
	o.entry.SetPlaceholderText(placeholder)
	o.entry.SetInputPurpose(gtk.InputPurposeURL)
	o.entry.SetIconFromIconName(gtk.EntryIconPrimary, "system-search-symbolic")
	o.entry.SetIconActivatable(gtk.EntryIconPrimary, true)
	o.entry.SetIconTooltipText(gtk.EntryIconPrimary, "About this site")
	o.entry.ConnectIconPress(func(pos gtk.EntryIconPosition) {
		if pos == gtk.EntryIconPrimary {
			siteCard(b, o.entry)
		}
	})
	o.Append(o.entry)
	// Copying the address bar's text copies the real address when it is
	// showing a fasthand alias.
	text := coreglib.BaseObject(o.entry.Delegate()) // the GtkText inside the entry owns copy-clipboard
	text.Connect("copy-clipboard", func() {
		t := o.b.current
		if t == nil {
			return
		}
		shown, ok := t.aliasShown()
		if !ok || o.entry.Text() != shown {
			return
		}
		if start, end, sel := o.entry.SelectionBounds(); !sel || (start != 0 || end != len([]rune(shown))) {
			return
		}
		gdk.DisplayGetDefault().Clipboard().SetText(cleanLink(t.url))
		text.StopEmission("copy-clipboard")
	})
	o.entry.ConnectActivate(o.activate)
	o.entry.ConnectChanged(o.changed)
	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(func() {
		o.editing = true
		coreglib.IdleAdd(func() { o.entry.SelectRegion(0, -1) })
	})
	focus.ConnectLeave(func() {
		o.editing = false
		o.pop.Popdown()
		if o.newTab {
			o.newTab = false
			o.entry.SetPlaceholderText(placeholder)
		}
		o.showTab(b.current)
	})
	o.entry.AddController(focus)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(o.key)
	o.entry.AddController(keys)

	o.pop = gtk.NewPopover()
	o.pop.SetHasArrow(false)
	o.pop.SetAutohide(false)
	o.pop.SetCanFocus(false)
	o.pop.AddCSSClass("suggestions")
	o.pop.SetParent(o.entry)
	o.pop.SetPosition(gtk.PosBottom)
	o.list = gtk.NewListBox()
	o.list.SetSelectionMode(gtk.SelectionSingle)
	o.list.SetCanFocus(false)
	o.list.ConnectRowActivated(func(r *gtk.ListBoxRow) { o.goRow(r.Index()) })
	o.pop.SetChild(o.list)
	return o
}

func (o *Omnibox) showTab(t *Tab) {
	if o.editing {
		return
	}
	if t != nil && t.url != "" {
		if shown, ok := t.aliasShown(); ok {
			o.set(shown)
		} else {
			o.set(editable(t.url))
		}
	} else {
		o.set("")
	}
	o.secureIcon(t)
}

func (o *Omnibox) secureIcon(t *Tab) {
	icon := "system-search-symbolic"
	if t != nil && t.url != "" {
		switch {
		case t.private:
			icon = "view-conceal-symbolic"
		case strings.HasPrefix(t.url, "https://"):
			icon = "channel-secure-symbolic"
		case strings.HasPrefix(t.url, "http://"):
			icon = "channel-insecure-symbolic"
		default:
			icon = "text-x-generic-symbolic"
		}
	}
	o.entry.SetIconFromIconName(gtk.EntryIconPrimary, icon)
	if t != nil && t.url != "" && o.b.app.bookmarks.Has(t.url) {
		o.entry.SetIconFromIconName(gtk.EntryIconSecondary, "starred-symbolic")
	} else {
		o.entry.SetIconFromIconName(gtk.EntryIconSecondary, "")
	}
}

func (o *Omnibox) set(text string) {
	o.completing = true
	o.entry.SetText(text)
	o.completing = false
	o.lastLen = len([]rune(text))
}

func (o *Omnibox) focus(newTab, private bool) {
	o.newTab = newTab
	o.private = private
	if newTab {
		o.set("")
		if private {
			o.entry.SetPlaceholderText("New private tab: search or enter address")
		} else {
			o.entry.SetPlaceholderText("New tab: search or enter address")
		}
	}
	o.entry.GrabFocus()
	o.entry.SelectRegion(0, -1)
}

func (o *Omnibox) cancel() {
	o.newTab = false
	o.editing = false
	o.entry.SetPlaceholderText(placeholder)
	o.pop.Popdown()
	o.showTab(o.b.current)
	o.b.focusPage()
}

func (o *Omnibox) changed() {
	if o.completing {
		return
	}
	text := o.entry.Text()
	n := len([]rune(text))
	grew := n > o.lastLen
	o.lastLen = n
	o.typed = text
	if grew && o.entry.Position() >= n-1 {
		coreglib.IdleAdd(func() { o.finishHost(text) })
	}
	o.suggest(text)
}

func (o *Omnibox) finishHost(typed string) {
	if o.entry.Text() != typed {
		return
	}
	if host := o.b.app.history.CompleteHost(typed); host != "" {
		o.completing = true
		o.entry.SetText(host)
		o.entry.SelectRegion(len([]rune(typed)), -1)
		o.completing = false
		o.lastLen = len([]rune(typed))
	}
}

func (o *Omnibox) suggest(text string) {
	o.list.RemoveAll()
	t := strings.TrimSpace(text)
	if t == "" {
		o.items = nil
		o.pop.Popdown()
		return
	}
	o.items = o.b.suggestions(t, o.b.current)
	for _, it := range o.items {
		o.list.Append(suggestionRow(it))
	}
	if len(o.items) > 0 {
		w := o.entry.Width()
		if w < 340 {
			w = 340
		}
		o.pop.SetSizeRequest(w, -1)
		o.list.UnselectAll()
		o.pop.Popup()
	} else {
		o.pop.Popdown()
	}
}

// suggestions for what is typed (trimmed, not empty): a search first unless it
// is an address, then open tabs other than skip, then history.
func (b *Browser) suggestions(t string, skip *Tab) []suggestion {
	var items []suggestion
	low := strings.ToLower(t)
	// Open tabs first: switching beats opening the same page twice.
	for _, tab := range b.tabs {
		if tab == skip || tab.url == "" {
			continue
		}
		if strings.Contains(strings.ToLower(tab.url), low) || strings.Contains(strings.ToLower(tab.title), low) {
			items = append(items, suggestion{kind: "tab", tab: tab, title: tab.displayTitle(), sub: pretty(tab.url)})
		}
		if len(items) >= 2 {
			break
		}
	}
	for _, e := range b.app.history.Suggest(t, 6) {
		dup := false
		for _, i := range items {
			if i.kind == "tab" && i.tab.url == e.URL {
				dup = true
			}
		}
		if dup {
			continue
		}
		title := e.Title
		if title == "" {
			title = pretty(e.URL)
		}
		items = append(items, suggestion{kind: "url", value: e.URL, title: title, sub: editable(e.URL)})
	}
	if urlFrom(t) == "" {
		name := engineByKey(b.app.settings.Engine).name
		items = append([]suggestion{{kind: "search", value: t, title: t, sub: "Search " + name}}, items...)
	}
	return items
}

func suggestionRow(it suggestion) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 10)
	row.AddCSSClass("suggestion")
	icon := map[string]string{"tab": "view-paged-symbolic", "url": "document-open-recent-symbolic",
		"search": "system-search-symbolic"}[it.kind]
	row.Append(gtk.NewImageFromIconName(icon))
	col := gtk.NewBox(gtk.OrientationVertical, 0)
	col.SetHExpand(true)
	a := gtk.NewLabel(it.title)
	a.SetXAlign(0)
	a.SetEllipsize(pango.EllipsizeEnd)
	sub := it.sub
	if it.kind == "tab" {
		sub = "Switch to tab · " + sub
	}
	s := gtk.NewLabel(sub)
	s.SetXAlign(0)
	s.SetEllipsize(pango.EllipsizeMiddle)
	s.AddCSSClass("dim-label")
	s.AddCSSClass("caption")
	col.Append(a)
	col.Append(s)
	row.Append(col)
	return row
}

func (o *Omnibox) key(keyval, _ uint, _ gdk.ModifierType) bool {
	switch keyval {
	case gdk.KEY_Escape:
		if o.pop.IsVisible() {
			o.pop.Popdown()
		} else {
			o.cancel()
		}
		return true
	case gdk.KEY_Down, gdk.KEY_Up:
		if !o.pop.IsVisible() || len(o.items) == 0 {
			return false
		}
		i := -1
		if r := o.list.SelectedRow(); r != nil {
			i = r.Index()
		}
		if keyval == gdk.KEY_Down {
			i++
		} else {
			i--
		}
		if i < 0 {
			o.list.UnselectAll()
			o.set(o.typed)
			o.entry.SetPosition(-1)
			return true
		}
		if i >= len(o.items) {
			i = len(o.items) - 1
		}
		o.list.SelectRow(o.list.RowAtIndex(i))
		it := o.items[i]
		switch it.kind {
		case "tab":
			o.set(it.tab.url)
		default:
			o.set(it.value)
		}
		o.entry.SetPosition(-1)
		return true
	case gdk.KEY_BackSpace, gdk.KEY_Delete:
		// Taking back a completion removes the completion, not a letter.
		start, end, ok := o.entry.SelectionBounds()
		typed := len([]rune(o.typed))
		if ok && end == len([]rune(o.entry.Text())) && start == typed && start > 0 {
			o.set(o.typed)
			o.entry.SetPosition(-1)
			o.suggest(o.typed)
			return true
		}
	}
	return false
}

func (o *Omnibox) goRow(i int) {
	if i < 0 || i >= len(o.items) {
		return
	}
	it := o.items[i]
	o.pop.Popdown()
	s := o.b.app.settings
	switch it.kind {
	case "tab":
		o.newTab = false
		o.b.selectTab(it.tab)
		o.b.focusPage()
	case "search":
		o.go_(searchURL(it.value, s.Engine, s.CustomEngine))
	default:
		o.go_(it.value)
	}
}

func (o *Omnibox) activate() {
	if o.pop.IsVisible() {
		if r := o.list.SelectedRow(); r != nil {
			o.goRow(r.Index())
			return
		}
	}
	text := strings.TrimSpace(o.entry.Text())
	if text == "" {
		return
	}
	if label, target, ok := aliasMatch(text); ok {
		o.goAlias(label, target)
		return
	}
	s := o.b.app.settings
	o.go_(resolve(text, s.Engine, s.CustomEngine))
}

// goAlias opens fasthandbd.com but keeps showing what was typed.
func (o *Omnibox) goAlias(label, target string) {
	b := o.b
	o.go_(target)
	if t := b.current; t != nil {
		t.aliasLabel, t.aliasUser = label, ""
		o.editing = false
		b.syncChrome()
	}
}

func (o *Omnibox) go_(u string) {
	o.pop.Popdown()
	b := o.b
	if o.newTab || b.current == nil {
		o.newTab = false
		o.entry.SetPlaceholderText(placeholder)
		b.openTab(u, true, nil, o.private, "", false)
		o.private = false
	} else {
		b.current.aliasLabel, b.current.aliasUser = "", ""
		b.current.load(u)
	}
	b.focusPage()
}
