// The ad blocker: compiling the rules into WebKit filters and keeping them up to date.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

type tuneReq struct {
	ucm  *webkit.UserContentManager
	host string
}

// Shield is a content filter compiled once and enforced inside WebKit's
// networking, before a request is made.
type Shield struct {
	settings *Settings
	store    *webkit.UserContentFilterStore
	filters  []*webkit.UserContentFilter
	idents   []string
	pending  int
	waiting  []tuneReq
	holding  map[*webkit.UserContentManager]bool
	updating bool
}

func newShield(s *Settings) *Shield {
	folder := filepath.Join(cacheDir(), "filters")
	os.MkdirAll(folder, 0o755)
	sh := &Shield{settings: s, store: webkit.NewUserContentFilterStore(folder),
		holding: map[*webkit.UserContentManager]bool{}}
	sh.compile()
	return sh
}

// shieldBuild loads or compiles each filter group, then calls done once all
// are ready. The same path is used at startup and for a background refresh.
type shieldBuild struct {
	idents  []string
	filters []*webkit.UserContentFilter
	pending int
	done    func([]string, []*webkit.UserContentFilter)
}

// shieldVersion goes into every filter's identity: bump it when the rules
// this code adds (not the lists) or the adblock engine's output change.
const shieldVersion = "2"

// listsKey identifies a filter group by what goes into it, so a compiled
// filter already in WebKit's store is found without regenerating its rules
// (which takes hundreds of MB of memory and a good part of a second).
func listsKey(names ...string) string {
	h := sha256.New()
	fmt.Fprint(h, shieldVersion, shield(), len(names))
	for _, n := range names {
		fmt.Fprint(h, n, "\x00")
		io.WriteString(h, filterList(n))
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// shield is the built-in part of the rules, for listsKey.
func shield() string {
	return fmt.Sprint(slots, youtubeSlots, youtubePaths, slotsBySite, unwanted)
}

type shieldGroup struct {
	key string
	gen func() []rule // only called when the filter isn't in the store yet
}

func shieldGroups() []shieldGroup {
	return []shieldGroup{
		{listsKey("easylist", "easyprivacy"), shieldRules},
		{listsKey("stevenblack"), hostRules},
	}
}

func (sh *Shield) build(groups []shieldGroup, done func([]string, []*webkit.UserContentFilter)) {
	b := &shieldBuild{
		idents:  make([]string, len(groups)),
		filters: make([]*webkit.UserContentFilter, len(groups)),
		pending: len(groups),
		done:    done,
	}
	for i, g := range groups {
		ident := fmt.Sprintf("office-shield-%d-%s", i, g.key)
		b.idents[i] = ident
		i, ident, g := i, ident, g
		sh.store.Load(context.Background(), ident, func(res gio.AsyncResulter) {
			f, err := sh.store.LoadFinish(res)
			if err == nil && f != nil {
				b.put(i, f)
				return
			}
			data, _ := json.Marshal(g.gen())
			sh.store.Save(context.Background(), ident, glib.NewBytes(data), func(res gio.AsyncResulter) {
				f, err := sh.store.SaveFinish(res)
				if err != nil {
					log.Printf("fasty: shield: %v", err)
					b.put(i, nil)
					return
				}
				b.put(i, f)
			})
		})
	}
}

func (b *shieldBuild) put(i int, f *webkit.UserContentFilter) {
	b.filters[i] = f
	b.pending--
	if b.pending == 0 {
		b.done(b.idents, b.filters)
		// Compiling leaves a lot of garbage behind; give it back to the system.
		go func() { debug.FreeOSMemory(); trimHeap() }()
	}
}

func (sh *Shield) compile() {
	groups := shieldGroups()
	sh.pending = len(groups)
	sh.build(groups, func(idents []string, filters []*webkit.UserContentFilter) {
		sh.idents, sh.filters = idents, filters
		sh.pending = 0
		for _, w := range sh.waiting {
			sh.Tune(w.ucm, w.host, true)
		}
		sh.waiting = nil
		sh.cleanupOld()
		sh.startUpdates()
	})
}

// refresh swaps in freshly compiled rules without restarting the browser.
func (sh *Shield) refresh() {
	sh.build(shieldGroups(), func(idents []string, filters []*webkit.UserContentFilter) {
		old := sh.filters
		sh.idents, sh.filters = idents, filters
		coreglib.IdleAdd(func() bool {
			for ucm, on := range sh.holding {
				if !on {
					continue
				}
				for _, f := range old {
					if f != nil {
						ucm.RemoveFilter(f)
					}
				}
				for _, f := range filters {
					if f != nil {
						ucm.AddFilter(f)
					}
				}
			}
			sh.cleanupOld()
			return false
		})
	})
}

func (sh *Shield) cleanupOld() {
	sh.store.FetchIdentifiers(context.Background(), func(res gio.AsyncResulter) {
		for _, old := range sh.store.FetchIdentifiersFinish(res) {
			keep := false
			for _, id := range sh.idents {
				if old == id {
					keep = true
				}
			}
			if strings.HasPrefix(old, "office-shield") && !keep {
				sh.store.Remove(context.Background(), old, func(res gio.AsyncResulter) {
					if err := sh.store.RemoveFinish(res); err != nil {
						log.Printf("fasty: shield: couldn't remove old filter %s: %v", old, err)
					}
				})
			}
		}
	})
}

// startUpdates seeds the cache from the bundled lists, then fetches any list
// older than filterAge in the background and swaps the filters in when done.
// A failed download leaves the current list in place.
func (sh *Shield) startUpdates() {
	if sh.updating {
		return
	}
	sh.updating = true
	os.MkdirAll(filtersDir(), 0o755)
	for _, src := range filterSources {
		if _, err := os.Stat(listPath(src.name)); err == nil {
			continue
		}
		if text := bundledList(src.name); text != "" {
			os.WriteFile(listPath(src.name), []byte(text), 0o600)
		}
	}
	go func() {
		changed := false
		for _, src := range filterSources {
			p := listPath(src.name)
			if info, err := os.Stat(p); err == nil && time.Since(info.ModTime()) < filterAge {
				continue
			}
			body, err := fetchFilterList(src.url)
			if err != nil {
				log.Printf("fasty: shield: %s: %v", src.name, err)
				continue
			}
			if len(body) < 1024 {
				log.Printf("fasty: shield: %s: list too small (%d bytes), keeping old copy", src.name, len(body))
				continue
			}
			tmp := p + ".tmp"
			if err := os.WriteFile(tmp, body, 0o600); err != nil {
				log.Printf("fasty: shield: %s: %v", src.name, err)
				continue
			}
			if err := os.Rename(tmp, p); err != nil {
				log.Printf("fasty: shield: %s: %v", src.name, err)
				continue
			}
			log.Printf("fasty: shield: updated %s (%d bytes)", src.name, len(body))
			changed = true
		}
		sh.updating = false
		if changed {
			coreglib.IdleAdd(func() bool { sh.refresh(); return false })
		}
	}()
	coreglib.TimeoutAdd(24*60*60*1000, func() bool {
		sh.startUpdates()
		return false
	})
}

func fetchFilterList(url string) ([]byte, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func (sh *Shield) Paused(host string) bool {
	if host == "" {
		return false
	}
	for _, h := range sh.settings.ShieldPaused {
		if h == host {
			return true
		}
	}
	return false
}

func (sh *Shield) Pause(host string, off bool) {
	sh.settings.Set("shield_paused", func(d *SettingsData) {
		var keep []string
		for _, h := range d.ShieldPaused {
			if h != host {
				keep = append(keep, h)
			}
		}
		if off {
			keep = append(keep, host)
		}
		if keep == nil {
			keep = []string{}
		}
		d.ShieldPaused = keep
	})
}

// Tune puts the filter on a page's content manager, or takes it off, for the
// site it is about to show.
func (sh *Shield) Tune(ucm *webkit.UserContentManager, host string, again bool) {
	if ucm == nil {
		return
	}
	if sh.pending > 0 {
		sh.waiting = append(sh.waiting, tuneReq{ucm, host})
		return
	}
	on := sh.settings.Shield && !sh.Paused(host)
	if was, ok := sh.holding[ucm]; ok && was == on && !again {
		return
	}
	for _, f := range sh.filters {
		if f == nil {
			continue
		}
		ucm.RemoveFilter(f)
		if on {
			ucm.AddFilter(f)
		}
	}
	sh.holding[ucm] = on
}

func (sh *Shield) Forget(ucm *webkit.UserContentManager) { delete(sh.holding, ucm) }
