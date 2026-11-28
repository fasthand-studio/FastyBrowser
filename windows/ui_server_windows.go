//go:build windows

package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
)

//go:embed ui
var uiFiles embed.FS

// The browser's own pages (new tab, settings, bookmarks, voice search ...) are served from a
// loopback-only HTTP server so they have a secure origin (needed for the microphone) and can
// load bundled scripts. Every URL carries a random token, so a web page that guesses the port
// still cannot read them, and the app only accepts privileged messages from this origin.
type uiServer struct {
	base  string // http://127.0.0.1:PORT/TOKEN/
	token string

	mu     sync.Mutex
	images map[string][]byte // page snapshots shown behind the glass overlay
}

var ui *uiServer

func startUIServer() (*uiServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 12)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	s := &uiServer{token: hex.EncodeToString(tok), images: map[string][]byte{}}
	s.base = "http://" + ln.Addr().String() + "/" + s.token + "/"
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(sub))
	mux := http.NewServeMux()
	mux.HandleFunc("/"+s.token+"/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/"+s.token+"/")
		if strings.HasPrefix(name, "bg/") {
			s.mu.Lock()
			img := s.images[name]
			s.mu.Unlock()
			if img == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(img)
			return
		}
		if name == "" || strings.Contains(path.Clean("/"+name), "..") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https: http:; connect-src 'self'; frame-ancestors 'none'")
		r.URL.Path = "/" + name
		files.ServeHTTP(w, r)
	})
	go http.Serve(ln, mux)
	return s, nil
}

// URL returns the address of an internal page.
func (s *uiServer) URL(name string) string { return s.base + name }

// IsInternal reports whether a page address belongs to the browser's own pages.
func (s *uiServer) IsInternal(u string) bool { return s != nil && strings.HasPrefix(u, s.base) }

// SetImage stores a snapshot and returns its address; Drop forgets it.
func (s *uiServer) SetImage(name string, jpg []byte) {
	s.mu.Lock()
	s.images["bg/"+name] = jpg
	s.mu.Unlock()
}

func (s *uiServer) DropImages() {
	s.mu.Lock()
	s.images = map[string][]byte{}
	s.mu.Unlock()
}

// ImageCount is how many snapshots are held (used by the self-test).
func (s *uiServer) ImageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.images)
}
