// Bookmarks.
package main

import (
	"strings"
)

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
