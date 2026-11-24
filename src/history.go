// Browsing history and address suggestions.
package main

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
