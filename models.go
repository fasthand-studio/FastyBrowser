package main

// History, bookmarks and hidden elements: the things that are yours.

import (
	"regexp"
	"sort"
	"strings"
)

type Visit struct {
	URL    string  `json:"url"`
	Title  string  `json:"title"`
	Visits int     `json:"visits"`
	Last   float64 `json:"last"`
}

type History struct {
	saver
	list  []*Visit
	byURL map[string]*Visit
}

const historyLimit = 20000

func loadHistory() *History {
	h := &History{byURL: map[string]*Visit{}}
	readJSON("history.json", &h.list)
	for _, e := range h.list {
		h.byURL[e.URL] = e
	}
	h.saver = saver{name: "history.json", delay: 1500, value: func() any { return h.list }}
	return h
}

func (h *History) removeEntry(e *Visit) {
	for i, x := range h.list {
		if x == e {
			h.list = append(h.list[:i], h.list[i+1:]...)
			return
		}
	}
}

func (h *History) Visit(u, title string) {
	if !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "file://")) {
		return
	}
	e := h.byURL[u]
	if e != nil {
		e.Visits++
		e.Last = now()
		if title != "" {
			e.Title = title
		}
		h.removeEntry(e)
	} else {
		e = &Visit{URL: u, Title: title, Visits: 1, Last: now()}
		h.byURL[u] = e
	}
	h.list = append(h.list, e)
	if over := len(h.list) - historyLimit; over > 0 {
		for _, old := range h.list[:over] {
			delete(h.byURL, old.URL)
		}
		h.list = h.list[over:]
	}
	h.changed()
}

func (h *History) Titled(u, title string) {
	if e := h.byURL[u]; e != nil && title != "" && e.Title != title {
		e.Title = title
		h.changed()
	}
}

func (h *History) Remove(u string) {
	if e := h.byURL[u]; e != nil {
		delete(h.byURL, u)
		h.removeEntry(e)
		h.changed()
	}
}

// Clear forgets everything, or everything since a moment when since > 0.
func (h *History) Clear(since float64) {
	if since <= 0 {
		h.list = nil
		h.byURL = map[string]*Visit{}
	} else {
		var keep []*Visit
		h.byURL = map[string]*Visit{}
		for _, e := range h.list {
			if e.Last < since {
				keep = append(keep, e)
				h.byURL[e.URL] = e
			}
		}
		h.list = keep
	}
	h.changed()
}

func (h *History) Search(q string, limit int) []*Visit {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []*Visit
	for i := len(h.list) - 1; i >= 0 && len(out) < limit; i-- {
		e := h.list[i]
		if q == "" || strings.Contains(strings.ToLower(e.URL), q) || strings.Contains(strings.ToLower(e.Title), q) {
			out = append(out, e)
		}
	}
	return out
}

var schemeWWW = regexp.MustCompile(`^https?://(www\.)?`)
var hostRe = regexp.MustCompile(`^https?://(?:www\.)?([^/:?#]+)`)

// Suggest ranks matches for the address field: a host that starts with what
// you typed wins, then title and address matches, weighted by visits and how
// recently.
func (h *History) Suggest(typed string, limit int) []*Visit {
	q := strings.ToLower(strings.TrimSpace(typed))
	if q == "" {
		return nil
	}
	t := now()
	words := strings.Fields(q)
	type scored struct {
		s float64
		e *Visit
	}
	var all []scored
	for _, e := range h.list {
		bare := schemeWWW.ReplaceAllString(strings.ToLower(e.URL), "")
		title := strings.ToLower(e.Title)
		var score float64
		if strings.HasPrefix(bare, q) {
			score = 100
		} else {
			ok := true
			for _, w := range words {
				if !strings.Contains(bare, w) && !strings.Contains(title, w) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			score = 40
		}
		age := (t - e.Last) / 86400
		if age < 0 {
			age = 0
		}
		v := e.Visits
		if v > 30 {
			v = 30
		}
		if age > 60 {
			age = 60
		}
		score += float64(v)*2 - age*0.5
		// A shorter address that matches is more likely the one you mean.
		score -= float64(len(bare)) / 40
		all = append(all, scored{score, e})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].s > all[j].s })
	seen := map[string]bool{}
	var out []*Visit
	for _, s := range all {
		if seen[s.e.URL] {
			continue
		}
		seen[s.e.URL] = true
		out = append(out, s.e)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// CompleteHost is the rest of a host you have been to, for finishing what you
// type.
func (h *History) CompleteHost(typed string) string {
	q := strings.ToLower(typed)
	if q == "" || strings.ContainsAny(q, " /") {
		return ""
	}
	best, bestVisits := "", 0
	for _, e := range h.list {
		m := hostRe.FindStringSubmatch(strings.ToLower(e.URL))
		if m == nil {
			continue
		}
		host := m[1]
		if strings.HasPrefix(host, q) && host != q && e.Visits >= bestVisits {
			if best == "" || e.Visits > bestVisits || len(host) < len(best) {
				best, bestVisits = host, e.Visits
			}
		}
	}
	return best
}

type Bookmark struct {
	URL   string  `json:"url"`
	Title string  `json:"title"`
	Date  float64 `json:"date"`
}

type Bookmarks struct {
	saver
	list []Bookmark
}

func loadBookmarks() *Bookmarks {
	b := &Bookmarks{}
	readJSON("bookmarks.json", &b.list)
	b.saver = saver{name: "bookmarks.json", delay: 400, value: func() any {
		if b.list == nil {
			return []Bookmark{}
		}
		return b.list
	}}
	return b
}

func (b *Bookmarks) Has(u string) bool {
	for _, x := range b.list {
		if x.URL == u {
			return true
		}
	}
	return false
}

func (b *Bookmarks) Add(u, title string) {
	if title == "" {
		title = u
	}
	b.list = append(b.list, Bookmark{u, title, now()})
	b.changed()
}

func (b *Bookmarks) Toggle(u, title string) bool {
	if b.Has(u) {
		b.Remove(u)
		return false
	}
	b.Add(u, title)
	return true
}

func (b *Bookmarks) Remove(u string) {
	var keep []Bookmark
	for _, x := range b.list {
		if x.URL != u {
			keep = append(keep, x)
		}
	}
	b.list = keep
	b.changed()
}

func (b *Bookmarks) Search(q string) []Bookmark {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []Bookmark
	for i := len(b.list) - 1; i >= 0; i-- {
		x := b.list[i]
		if q == "" || strings.Contains(strings.ToLower(x.URL), q) || strings.Contains(strings.ToLower(x.Title), q) {
			out = append(out, x)
		}
	}
	return out
}

type Veil struct {
	Selector string  `json:"selector"`
	Label    string  `json:"label"`
	Note     string  `json:"note"`
	Date     float64 `json:"date"`
}

// Curtain keeps per-site lists of selectors, put back on as a stylesheet
// before the page has drawn (Curtain.swift).
type Curtain struct {
	saver
	sites     map[string][]Veil
	listeners []func()
}

func loadCurtain() *Curtain {
	c := &Curtain{sites: map[string][]Veil{}}
	readJSON("hidden.json", &c.sites)
	if c.sites == nil {
		c.sites = map[string][]Veil{}
	}
	c.saver = saver{name: "hidden.json", delay: 400, value: func() any { return c.sites }}
	return c
}

func (c *Curtain) Listen(fn func()) { c.listeners = append(c.listeners, fn) }

func (c *Curtain) tell() {
	c.changed()
	for _, l := range c.listeners {
		l()
	}
}

func (c *Curtain) Veils(host string) []Veil {
	if host == "" {
		return nil
	}
	return c.sites[host]
}

func (c *Curtain) Hide(host, selector, label, note string) {
	for _, v := range c.sites[host] {
		if v.Selector == selector {
			return
		}
	}
	c.sites[host] = append(c.sites[host], Veil{selector, label, note, now()})
	c.tell()
}

func (c *Curtain) Restore(host, selector string) {
	var keep []Veil
	for _, v := range c.sites[host] {
		if v.Selector != selector {
			keep = append(keep, v)
		}
	}
	if len(keep) > 0 {
		c.sites[host] = keep
	} else {
		delete(c.sites, host)
	}
	c.tell()
}

func (c *Curtain) Undo(host string) *Veil {
	l := c.sites[host]
	if len(l) == 0 {
		return nil
	}
	last := l[len(l)-1]
	if len(l) == 1 {
		delete(c.sites, host)
	} else {
		c.sites[host] = l[:len(l)-1]
	}
	c.tell()
	return &last
}

func (c *Curtain) RestoreAll(host string) {
	delete(c.sites, host)
	c.tell()
}

func (c *Curtain) CSS(host string) string {
	var b strings.Builder
	for _, v := range c.Veils(host) {
		b.WriteString(v.Selector + " { display: none !important; }\n")
	}
	return b.String()
}

func (c *Curtain) Hosts() []string {
	out := make([]string, 0, len(c.sites))
	for h := range c.sites {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
