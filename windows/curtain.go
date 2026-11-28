package main

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Veil is one hidden page element on a site (Linux calls the store the Curtain).
type Veil struct {
	Selector string `json:"selector"`
	Label    string `json:"label"`
	Date     int64  `json:"date"`
}

// Curtain keeps what the user chose to hide, per site.
type Curtain struct {
	dir   string
	Sites map[string][]Veil
	dirty bool
}

func LoadCurtain(dir string) *Curtain {
	c := &Curtain{dir: dir, Sites: map[string][]Veil{}}
	if b, err := os.ReadFile(filepath.Join(dir, "hidden.json")); err == nil {
		_ = json.Unmarshal(b, &c.Sites)
	}
	if c.Sites == nil {
		c.Sites = map[string][]Veil{}
	}
	return c
}

func (c *Curtain) Veils(host string) []Veil { return c.Sites[host] }

func (c *Curtain) Hide(host, selector, label string) bool {
	if host == "" || selector == "" || len(selector) > 500 {
		return false
	}
	for _, v := range c.Sites[host] {
		if v.Selector == selector {
			return false
		}
	}
	c.Sites[host] = append(c.Sites[host], Veil{Selector: selector, Label: label, Date: time.Now().Unix()})
	c.dirty = true
	return true
}

func (c *Curtain) Show(host, selector string) {
	list := c.Sites[host]
	for i, v := range list {
		if v.Selector == selector {
			c.Sites[host] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(c.Sites[host]) == 0 {
		delete(c.Sites, host)
	}
	c.dirty = true
}

func (c *Curtain) ShowAll(host string) {
	delete(c.Sites, host)
	c.dirty = true
}

func (c *Curtain) Hosts() []string {
	var h []string
	for k := range c.Sites {
		h = append(h, k)
	}
	sort.Strings(h)
	return h
}

// CSS is the style sheet that hides a site's elements.
func (c *Curtain) CSS(host string) string {
	var b strings.Builder
	for _, v := range c.Sites[host] {
		b.WriteString(v.Selector + "{display:none!important}")
	}
	return b.String()
}

func (c *Curtain) Flush() {
	if !c.dirty {
		return
	}
	if b, err := json.Marshal(c.Sites); err == nil && os.MkdirAll(c.dir, 0o700) == nil {
		if writeFileAtomic(filepath.Join(c.dir, "hidden.json"), b) == nil {
			c.dirty = false
		}
	}
}

var (
	reLink = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	reTag  = regexp.MustCompile(`(?s)<[^>]*>`)
)

// ImportBookmarksHTML reads a Netscape-format bookmarks file (what Chrome, Edge and Firefox export).
// Only web addresses are taken; titles are cleaned of markup.
func ImportBookmarksHTML(doc string) []Bookmark {
	var out []Bookmark
	seen := map[string]bool{}
	for _, m := range reLink.FindAllStringSubmatch(doc, -1) {
		u := strings.TrimSpace(html.UnescapeString(m[1]))
		if !isWebURL(u) || seen[u] {
			continue
		}
		seen[u] = true
		title := strings.TrimSpace(html.UnescapeString(reTag.ReplaceAllString(m[2], "")))
		if title == "" {
			title = u
		}
		out = append(out, Bookmark{URL: u, Title: title, Added: time.Now().Unix()})
	}
	return out
}

// AddMany adds imported bookmarks that are not there yet and returns how many were new.
func (b *Bookmarks) AddMany(items []Bookmark) int {
	n := 0
	for _, it := range items {
		if !b.Has(it.URL) {
			b.Items = append(b.Items, it)
			n++
		}
	}
	if n > 0 {
		b.dirty = true
	}
	return n
}
