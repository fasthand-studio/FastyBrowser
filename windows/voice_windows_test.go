//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Synthesizes a sentence with Windows' own voice, then lets the voice helper "listen" to the
// recording. This exercises the real recognizer and the helper's JSON events end to end.
func TestVoiceHelperRecognizesSpeech(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "said.wav")
	say := `Add-Type -AssemblyName System.Speech; $s=New-Object System.Speech.Synthesis.SpeechSynthesizer; ` +
		`$s.SetOutputToWaveFile('` + wav + `'); $s.Speak('search for cheap flights to london'); $s.Dispose()`
	if out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", say).CombinedOutput(); err != nil {
		t.Skipf("no speech synthesizer here: %v %s", err, out)
	}
	script := filepath.Join(dir, "voice.ps1")
	if err := os.WriteFile(script, voiceScript, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Lang", "en-US", "-Wave", wav)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	got := make(chan voiceEvent, 64)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			var ev voiceEvent
			if json.Unmarshal(sc.Bytes(), &ev) == nil {
				got <- ev
			}
		}
		close(got)
	}()
	sawReady, sawInterim := false, false
	deadline := time.After(40 * time.Second)
	for {
		select {
		case ev, ok := <-got:
			if !ok {
				t.Fatalf("helper ended without a final result (ready=%v interim=%v)", sawReady, sawInterim)
			}
			switch ev.T {
			case "error":
				t.Skipf("recognizer unavailable: %v", ev.V)
			case "ready":
				sawReady = true
			case "interim":
				sawInterim = true
			case "final":
				text, _ := ev.V.(string)
				if !strings.Contains(strings.ToLower(text), "cheap flights") || !strings.Contains(strings.ToLower(text), "london") {
					t.Fatalf("recognized %q", text)
				}
				if !sawReady || !sawInterim {
					t.Errorf("expected a ready and interim events first (ready=%v interim=%v)", sawReady, sawInterim)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the recognizer")
		}
	}
}
