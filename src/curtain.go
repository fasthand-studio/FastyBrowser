// Hidden page elements: what you chose to hide on each site, and the style
// sheets that hide them.
package main

import (
	"sort"
	"strings"
)

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
