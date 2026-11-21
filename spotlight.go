package main

// Spotlight: a new tab starts as a search field in the middle of the window,
// over a dimmed page. Type an address or words, Return goes there in a new
// tab; the same suggestions as the address field (a search, open tabs,
// history) fill the space under the field. With nothing typed it is just the
// field; once you type it opens to one size, the same however many
// suggestions there are (a long list scrolls inside it). Escape, or a click
// outside, puts it away.

import (
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// The box always has room for spotlightRows suggestions of rowHeight px.
const (
	spotlightRows = 6
	rowHeight     = 52
)

type Spotlight struct {
	*gtk.Revealer
	b          *Browser
	card       *gtk.Box
	entry      *gtk.Entry
	list       *gtk.ListBox
	scroll     *gtk.ScrolledWindow
	sep        *gtk.Separator
	items      []suggestion
	private    bool
	completing bool
	lastLen    int
	typed      string
}

func newSpotlight(b *Browser) *Spotlight {
	s := &Spotlight{Revealer: gtk.NewRevealer(), b: b}
	s.SetTransitionType(gtk.RevealerTransitionTypeCrossfade)
	s.SetTransitionDuration(120)
	s.SetHExpand(true)
	s.SetVExpand(true)
	s.SetCanTarget(false)
	s.SetVisible(false)
	s.NotifyProperty("child-revealed", func() {
		if !s.ChildRevealed() {
			s.SetVisible(false)
		}
	})

	scrim := gtk.NewBox(gtk.OrientationVertical, 0)
	scrim.AddCSSClass("spotlight-scrim")
	click := gtk.NewGestureClick()
	click.ConnectPressed(func(_ int, x, y float64) {
		if pick := scrim.Pick(x, y, gtk.PickDefault); pick == nil || coreglib.InternObject(pick).Eq(scrim) {
			s.close(true)
		}
	})
	scrim.AddController(click)

	s.card = gtk.NewBox(gtk.OrientationVertical, 0)
	s.card.AddCSSClass("spotlight")
	s.card.SetHAlign(gtk.AlignCenter)
	s.card.SetVAlign(gtk.AlignStart)
	scrim.Append(s.card)

	s.entry = gtk.NewEntry()
	s.entry.AddCSSClass("spotlight-entry")
	s.entry.SetPlaceholderText(placeholder)
	s.entry.SetInputPurpose(gtk.InputPurposeURL)
	s.entry.SetIconFromIconName(gtk.EntryIconPrimary, "system-search-symbolic")
	s.entry.ConnectActivate(s.activate)
	s.entry.ConnectChanged(s.changed)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(s.key)
	s.entry.AddController(keys)
	s.card.Append(s.entry)

	s.sep = gtk.NewSeparator(gtk.OrientationHorizontal)
	s.sep.AddCSSClass("spotlight-sep")
	s.card.Append(s.sep)

	s.list = gtk.NewListBox()
	s.list.AddCSSClass("spotlight-list")
	s.list.SetSelectionMode(gtk.SelectionSingle)
	s.list.SetCanFocus(false)
	s.list.ConnectRowActivated(func(r *gtk.ListBoxRow) { s.goRow(r.Index()) })
	s.scroll = gtk.NewScrolledWindow()
	s.scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	s.scroll.SetMinContentHeight(spotlightRows * rowHeight)
	s.scroll.SetMaxContentHeight(spotlightRows * rowHeight)
	s.scroll.SetChild(s.list)
	s.card.Append(s.scroll)

	s.SetChild(scrim)
	return s
}

// place puts the box about a fifth of the way down, like Spotlight, as wide
// as fits up to 620 px, and all of it on screen in a short window.
func (s *Spotlight) place() {
	h := s.b.overlay.Height()
	top := max(h/5, 24)
	if room := h - 24 - (spotlightRows*rowHeight + 80); top > room {
		top = max(room, 12)
	}
	s.card.SetMarginTop(top)
	s.card.SetSizeRequest(min(max(s.b.overlay.Width()-48, 200), 620), -1)
}

func (s *Spotlight) open(private bool) {
	s.private = private
	if private {
		s.entry.SetPlaceholderText("Private tab: search or enter address")
		s.AddCSSClass("private")
	} else {
		s.entry.SetPlaceholderText(placeholder)
		s.RemoveCSSClass("private")
	}
	s.set("")
	s.typed = ""
	s.fill("")
	s.place()
	s.SetVisible(true)
	s.SetCanTarget(true)
	s.SetRevealChild(true)
	s.entry.GrabFocus()
}

func (s *Spotlight) shown() bool { return s.RevealChild() }

func (s *Spotlight) close(refocus bool) {
	if !s.shown() {
		return
	}
	s.SetRevealChild(false)
	s.SetCanTarget(false)
	if refocus {
		s.b.focusPage()
	}
}

func (s *Spotlight) set(text string) {
	s.completing = true
	s.entry.SetText(text)
	s.completing = false
	s.lastLen = len([]rune(text))
}

func (s *Spotlight) changed() {
	if s.completing {
		return
	}
	text := s.entry.Text()
	n := len([]rune(text))
	grew := n > s.lastLen
	s.lastLen = n
	s.typed = text
	if grew && s.entry.Position() >= n-1 {
		coreglib.IdleAdd(func() { s.finishHost(text) })
	}
	s.fill(text)
}

// finishHost completes a host you have been to, selected so typing on
// replaces it, as the address field does.
func (s *Spotlight) finishHost(typed string) {
	if s.entry.Text() != typed {
		return
	}
	if host := s.b.app.history.CompleteHost(typed); host != "" {
		s.completing = true
		s.entry.SetText(host)
		s.entry.SelectRegion(len([]rune(typed)), -1)
		s.completing = false
		s.lastLen = len([]rune(typed))
	}
}

func (s *Spotlight) fill(text string) {
	s.list.RemoveAll()
	s.items = nil
	t := strings.TrimSpace(text)
	if t != "" {
		s.items = s.b.suggestions(t, s.b.current)
	}
	// Just the field until you type; then the full box, whatever the count.
	s.sep.SetVisible(t != "")
	s.scroll.SetVisible(t != "")
	for _, it := range s.items {
		s.list.Append(suggestionRow(it))
	}
	s.scroll.VAdjustment().SetValue(0)
	if len(s.items) > 0 {
		s.list.SelectRow(s.list.RowAtIndex(0))
	}
}

// reveal scrolls the list so row i is in view.
func (s *Spotlight) reveal(i int) {
	adj := s.scroll.VAdjustment()
	top := float64(i * rowHeight)
	if r := s.list.RowAtIndex(i); r != nil {
		if b, ok := r.ComputeBounds(s.list); ok {
			top = float64(b.Y())
		}
	}
	page := adj.PageSize()
	switch {
	case top < adj.Value():
		adj.SetValue(top)
	case top+rowHeight > adj.Value()+page:
		adj.SetValue(top + rowHeight - page)
	}
}

func (s *Spotlight) key(keyval, _ uint, _ gdk.ModifierType) bool {
	switch keyval {
	case gdk.KEY_Escape:
		s.close(true)
		return true
	case gdk.KEY_Down, gdk.KEY_Up:
		if len(s.items) == 0 {
			return true
		}
		i := 0
		if r := s.list.SelectedRow(); r != nil {
			i = r.Index()
		}
		if keyval == gdk.KEY_Down {
			i = min(i+1, len(s.items)-1)
		} else {
			i = max(i-1, 0)
		}
		s.list.SelectRow(s.list.RowAtIndex(i))
		s.reveal(i)
		return true
	case gdk.KEY_BackSpace, gdk.KEY_Delete:
		// Taking back a completion removes the completion, not a letter.
		start, end, ok := s.entry.SelectionBounds()
		typed := len([]rune(s.typed))
		if ok && end == len([]rune(s.entry.Text())) && start == typed && start > 0 {
			s.set(s.typed)
			s.entry.SetPosition(-1)
			s.fill(s.typed)
			return true
		}
	}
	return false
}

func (s *Spotlight) activate() {
	text := strings.TrimSpace(s.entry.Text())
	if text == "" {
		return
	}
	// A row you moved to with the arrows wins. Otherwise what is in the field
	// goes: a search for words, the address for an address or a finished host.
	if r := s.list.SelectedRow(); r != nil && r.Index() > 0 {
		s.goRow(r.Index())
		return
	}
	st := s.b.app.settings
	s.go_(resolve(text, st.Engine, st.CustomEngine))
}

func (s *Spotlight) goRow(i int) {
	if i < 0 || i >= len(s.items) {
		return
	}
	it := s.items[i]
	st := s.b.app.settings
	switch it.kind {
	case "tab":
		s.close(false)
		s.b.selectTab(it.tab)
		s.b.focusPage()
	case "search":
		s.go_(searchURL(it.value, st.Engine, st.CustomEngine))
	default:
		s.go_(it.value)
	}
}

func (s *Spotlight) go_(u string) {
	s.close(false)
	s.b.openTab(u, true, nil, s.private, "", false)
	s.b.focusPage()
}
