package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Bookmark is a saved page.
type Bookmark struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Added int64  `json:"added"`
}

// Bookmarks keeps the user's saved pages, newest first.
type Bookmarks struct {
	dir   string
	Items []Bookmark
	dirty bool
}

func LoadBookmarks(dir string) *Bookmarks {
	b := &Bookmarks{dir: dir}
	if data, err := os.ReadFile(filepath.Join(dir, "bookmarks.json")); err == nil {
		_ = json.Unmarshal(data, &b.Items)
	}
	return b
}

func (b *Bookmarks) Has(u string) bool {
	for _, it := range b.Items {
		if it.URL == u {
			return true
		}
	}
	return false
}

// Toggle adds or removes a page and reports whether it is bookmarked afterwards.
func (b *Bookmarks) Toggle(u, title string) bool {
	if b.Remove(u) {
		return false
	}
	if u == "" {
		return false
	}
	if title == "" {
		title = u
	}
	b.Items = append([]Bookmark{{URL: u, Title: title, Added: time.Now().Unix()}}, b.Items...)
	b.dirty = true
	return true
}

func (b *Bookmarks) Remove(u string) bool {
	for i, it := range b.Items {
		if it.URL == u {
			b.Items = append(b.Items[:i], b.Items[i+1:]...)
			b.dirty = true
			return true
		}
	}
	return false
}

func (b *Bookmarks) Flush() {
	if !b.dirty {
		return
	}
	if data, err := json.Marshal(b.Items); err == nil && os.MkdirAll(b.dir, 0o700) == nil {
		if writeFileAtomic(filepath.Join(b.dir, "bookmarks.json"), data) == nil {
			b.dirty = false
		}
	}
}

// HistoryEntry is a page the user visited; repeat visits update one entry.
type HistoryEntry struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Visited int64  `json:"visited"`
	Visits  int    `json:"visits"`
}

const historyLimit = 5000

type History struct {
	dir   string
	Items []HistoryEntry // most recent first
	dirty bool
}

func LoadHistory(dir string) *History {
	h := &History{dir: dir}
	if data, err := os.ReadFile(filepath.Join(dir, "history.json")); err == nil {
		_ = json.Unmarshal(data, &h.Items)
	}
	return h
}

// Record notes a visit. Only web pages are kept, and a page's title is updated when it arrives.
func (h *History) Record(u, title string) {
	if !isWebURL(u) {
		return
	}
	now := time.Now().Unix()
	for i, it := range h.Items {
		if it.URL == u {
			it.Visits++
			it.Visited = now
			if title != "" {
				it.Title = title
			}
			h.Items = append(h.Items[:i], h.Items[i+1:]...)
			h.Items = append([]HistoryEntry{it}, h.Items...)
			h.dirty = true
			return
		}
	}
	h.Items = append([]HistoryEntry{{URL: u, Title: title, Visited: now, Visits: 1}}, h.Items...)
	if len(h.Items) > historyLimit {
		h.Items = h.Items[:historyLimit]
	}
	h.dirty = true
}

// SetTitle fills in a title that arrived after the visit was recorded.
func (h *History) SetTitle(u, title string) {
	if title == "" {
		return
	}
	for i := range h.Items {
		if h.Items[i].URL == u {
			if h.Items[i].Title != title {
				h.Items[i].Title = title
				h.dirty = true
			}
			return
		}
	}
}

func (h *History) Remove(u string) {
	for i, it := range h.Items {
		if it.URL == u {
			h.Items = append(h.Items[:i], h.Items[i+1:]...)
			h.dirty = true
			return
		}
	}
}

func (h *History) Clear() { h.Items = nil; h.dirty = true }

func (h *History) Flush() {
	if !h.dirty {
		return
	}
	if data, err := json.Marshal(h.Items); err == nil && os.MkdirAll(h.dir, 0o700) == nil {
		if writeFileAtomic(filepath.Join(h.dir, "history.json"), data) == nil {
			h.dirty = false
		}
	}
}

func isWebURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// Suggestion is one line under the address field or in the new-tab box.
type Suggestion struct {
	Kind  string `json:"kind"` // search | url | bookmark | history | tab
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Suggest builds the list for what the user typed: bookmarks first, then history by how often
// and how recently it was visited, then open tabs, then a search or address entry.
func Suggest(q string, engine string, bm *Bookmarks, hist *History, tabs []Suggestion, limit int) []Suggestion {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	lq := strings.ToLower(q)
	type scored struct {
		s     Suggestion
		score int
	}
	var out []scored
	seen := map[string]bool{}
	add := func(s Suggestion, score int) {
		key := s.URL
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, scored{s, score})
	}
	match := func(title, u string) int {
		lt, lu := strings.ToLower(title), strings.ToLower(u)
		host := lu
		if pu, err := url.Parse(u); err == nil {
			host = strings.TrimPrefix(strings.ToLower(pu.Hostname()), "www.")
		}
		switch {
		case strings.HasPrefix(host, lq):
			return 100
		case strings.HasPrefix(lt, lq):
			return 90
		case strings.Contains(host, lq):
			return 70
		case strings.Contains(lt, lq):
			return 60
		case strings.Contains(lu, lq):
			return 40
		}
		return 0
	}
	for _, t := range tabs {
		if m := match(t.Title, t.URL); m > 0 {
			add(Suggestion{Kind: "tab", Title: t.Title, URL: t.URL}, m+30)
		}
	}
	if bm != nil {
		for _, it := range bm.Items {
			if m := match(it.Title, it.URL); m > 0 {
				add(Suggestion{Kind: "bookmark", Title: it.Title, URL: it.URL}, m+20)
			}
		}
	}
	if hist != nil {
		for i, it := range hist.Items {
			if m := match(it.Title, it.URL); m > 0 {
				rec := 10 - i/50
				if rec < 0 {
					rec = 0
				}
				v := it.Visits
				if v > 10 {
					v = 10
				}
				add(Suggestion{Kind: "history", Title: it.Title, URL: it.URL}, m+v+rec)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	res := make([]Suggestion, 0, limit)
	// What was typed always leads: an address to open, or a search for the words.
	if u := ResolveInput(q, engine); u != "" {
		kind := "search"
		if IsAddress(q) {
			kind = "url"
		}
		res = append(res, Suggestion{Kind: kind, Title: q, URL: u})
		seen[u] = true
	}
	for _, o := range out {
		if len(res) >= limit {
			break
		}
		if o.s.URL == res[0].URL {
			continue
		}
		res = append(res, o.s)
	}
	return res
}
