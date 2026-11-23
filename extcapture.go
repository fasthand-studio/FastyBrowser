// Offscreen documents and tab capture.
package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// --- offscreen documents ---------------------------------------------------------

func (m *ExtensionManager) startOffscreen(ext *Extension, page string, loaded func()) string {
	if ext.offscreen != nil {
		return "Only a single offscreen document may be created."
	}
	ucm := webkit.NewUserContentManager()
	ucm.RegisterScriptMessageHandler("officeVeil", "")
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "page"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	view := m.engine.newView(m.engine.session, ucm)
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		m.HandleMessage(v.ToJson(0), extSource{view: view})
	})
	view.SetVisible(false)
	ext.offscreen, ext.offscreenUCM = view, ucm
	once := false
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		if ev == webkit.LoadFinished && !once {
			once = true
			loaded()
		}
	})
	view.LoadURI(extURL(ext.ID, page))
	return ""
}

func (m *ExtensionManager) closeOffscreen(ext *Extension) {
	if ext.offscreen != nil {
		m.evalIn(ext.offscreen, "", "window.__fastyDead=true")
		m.retired = append(m.retired, ext.offscreen, ext.offscreenUCM)
		ext.offscreen, ext.offscreenUCM = nil, nil
	}
}

// --- tab audio capture -------------------------------------------------------------

// tabCapture hands the extension the system's output audio (what the speakers
// play), read from the PipeWire/PulseAudio monitor and streamed to the page as
// float32 PCM. WebKit has no per-tab audio tap.
func (m *ExtensionManager) startCapture(ext *Extension, src extSource, id string, rate int) {
	key := ext.ID + ":" + id
	m.stopCapture(key)
	sink := "@DEFAULT_SINK@"
	if out, err := exec.Command("pactl", "get-default-sink").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			sink = s
		}
	}
	cmd := exec.Command("parec", "--raw", "--format=float32le", fmt.Sprintf("--rate=%d", rate),
		"--channels=1", "--latency-msec=40", "-d", sink+".monitor")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	m.captures[key] = cmd
	view, world := src.view, src.world(ext.ID)
	go func() {
		r := bufio.NewReaderSize(stdout, 1<<16)
		buf := make([]byte, 4096*4)
		deadline := time.Now().Add(20 * time.Minute)
		for time.Now().Before(deadline) {
			n, err := readFull(r, buf)
			if n > 0 {
				chunk := base64.StdEncoding.EncodeToString(buf[:n])
				coreglib.IdleAdd(func() {
					m.evalIn(view, world, "window.__fastyAudio&&window.__fastyAudio("+jsq(id)+","+jsq(chunk)+")")
				})
			}
			if err != nil {
				break
			}
		}
		coreglib.IdleAdd(func() { m.stopCapture(key) })
	}()
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		k, err := r.Read(buf[n:])
		n += k
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (m *ExtensionManager) stopCapture(key string) {
	if cmd := m.captures[key]; cmd != nil {
		delete(m.captures, key)
		cmd.Process.Kill()
		go cmd.Wait()
	}
}

func (m *ExtensionManager) stopAllCaptures() {
	for k := range m.captures {
		m.stopCapture(k)
	}
}
