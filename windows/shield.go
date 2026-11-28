package main

import (
	"bufio"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// Shield blocks requests to known ad and tracker hosts. It matches the host or any parent
// domain, so blocking "doubleclick.net" also blocks "ad.doubleclick.net".
type Shield struct {
	mu      sync.RWMutex
	hosts   map[string]struct{}
	blocked atomic.Int64
}

// A small built-in list so the shield works offline on first run. The extended list
// (StevenBlack's hosts file) is downloaded and cached by the app.
var builtinBlocked = []string{
	"doubleclick.net", "googlesyndication.com", "googleadservices.com", "google-analytics.com",
	"adservice.google.com", "pagead2.googlesyndication.com", "adnxs.com", "adsrvr.org",
	"criteo.com", "criteo.net", "taboola.com", "outbrain.com", "scorecardresearch.com",
	"quantserve.com", "hotjar.com", "amazon-adsystem.com", "rubiconproject.com",
	"pubmatic.com", "openx.net", "casalemedia.com", "moatads.com", "advertising.com",
	"adform.net", "smartadserver.com", "yieldmo.com", "media.net", "zedo.com",
	"2mdn.net", "serving-sys.com", "bluekai.com", "demdex.net", "krxd.net",
	"mathtag.com", "everesttech.net", "adsymptotic.com", "chartbeat.net",
}

func NewShield() *Shield {
	s := &Shield{hosts: make(map[string]struct{}, len(builtinBlocked)*2)}
	for _, h := range builtinBlocked {
		s.hosts[h] = struct{}{}
	}
	return s
}

// LoadHosts adds entries from a hosts-format file ("0.0.0.0 example.com") and returns how many.
func (s *Shield) LoadHosts(r io.Reader) int {
	add := make(map[string]struct{}, 4096)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "0.0.0.0" && f[0] != "127.0.0.1") {
			continue
		}
		h := strings.ToLower(f[1])
		if h == "localhost" || h == "local" || h == "broadcasthost" || !strings.Contains(h, ".") {
			continue
		}
		add[h] = struct{}{}
	}
	s.mu.Lock()
	for h := range add {
		s.hosts[h] = struct{}{}
	}
	s.mu.Unlock()
	return len(add)
}

// Blocked reports whether a request URL points at a blocked host.
func (s *Shield) Blocked(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return s.BlockedHost(strings.ToLower(u.Hostname()))
}

func (s *Shield) BlockedHost(host string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for h := host; h != ""; {
		if _, ok := s.hosts[h]; ok {
			return true
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}
		h = h[i+1:]
		if !strings.Contains(h, ".") { // never match a bare TLD such as "com"
			break
		}
	}
	return false
}

func (s *Shield) Count() int64 { return s.blocked.Load() }
func (s *Shield) Hit()         { s.blocked.Add(1) }
func (s *Shield) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.hosts)
}
