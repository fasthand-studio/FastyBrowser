//go:build windows

package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

//go:embed voice/voice.ps1
var voiceScript []byte

// voiceSession runs the speech helper and forwards what it hears to the page that asked.
type voiceSession struct {
	cmd  *exec.Cmd
	tab  *tab
	done chan struct{}
	once sync.Once
}

type voiceEvent struct {
	T string  `json:"t"`
	V any     `json:"v"`
	C float64 `json:"c"`
	L string  `json:"lang"`
}

// startVoice begins listening for the given tab. Results reach the page as
// window.fastyVoice({t, v, ...}) and arrive on the UI thread.
func (a *app) startVoice(t *tab, lang string) {
	a.stopVoice()
	dir := filepath.Join(os.TempDir(), "fasty-voice")
	_ = os.MkdirAll(dir, 0o700)
	script := filepath.Join(dir, "voice.ps1")
	if err := os.WriteFile(script, voiceScript, 0o600); err != nil {
		a.voiceTo(t, voiceEvent{T: "error", V: "Could not start voice search: " + err.Error()})
		return
	}
	if lang == "" {
		lang = "en-US"
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Lang", lang)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.StdoutPipe()
	if err != nil {
		a.voiceTo(t, voiceEvent{T: "error", V: err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		a.voiceTo(t, voiceEvent{T: "error", V: "Could not start voice search: " + err.Error()})
		return
	}
	vs := &voiceSession{cmd: cmd, tab: t, done: make(chan struct{})}
	a.voice = vs
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var ev voiceEvent
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			ev2 := ev
			a.runOnUI(func() {
				if a.voice == vs {
					a.voiceTo(t, ev2)
				}
			})
		}
		_ = cmd.Wait()
		close(vs.done)
		a.runOnUI(func() {
			if a.voice == vs {
				a.voice = nil
				a.voiceTo(t, voiceEvent{T: "end"})
			}
		})
	}()
	// A recognizer that never says "ready" (no microphone, busy device) must not hang the page.
	go func() {
		select {
		case <-vs.done:
		case <-time.After(15 * time.Second):
		}
	}()
}

func (a *app) stopVoice() {
	vs := a.voice
	if vs == nil {
		return
	}
	a.voice = nil
	vs.once.Do(func() {
		if vs.cmd != nil && vs.cmd.Process != nil {
			_ = vs.cmd.Process.Kill()
		}
	})
}

func (a *app) voiceTo(t *tab, ev voiceEvent) {
	if t == nil || a.indexOf(t) < 0 || !t.ready || a.quitting {
		return
	}
	b, _ := json.Marshal(ev)
	t.view.Eval("window.fastyVoice&&window.fastyVoice(" + string(b) + ")")
}

// ---- running code on the UI thread from other goroutines ----

var (
	uiMu    sync.Mutex
	uiQueue []func()
)

func (a *app) runOnUI(f func()) {
	uiMu.Lock()
	uiQueue = append(uiQueue, f)
	uiMu.Unlock()
	procPostMessageW.Call(a.hwnd, wmRun, 0, 0)
}

func (a *app) drainUI() {
	uiMu.Lock()
	q := uiQueue
	uiQueue = nil
	uiMu.Unlock()
	for _, f := range q {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("ui task panicked:", r)
				}
			}()
			f()
		}()
	}
}
