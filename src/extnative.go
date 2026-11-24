package main

// Native messaging: extensions talk to a local program that registered a
// host manifest. Messages are a 4-byte native-endian length and a JSON body.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

type nativeConn struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
}

func nativeDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".config/google-chrome/NativeMessagingHosts"),
		filepath.Join(home, ".config/chromium/NativeMessagingHosts"),
		filepath.Join(home, ".config/BraveSoftware/Brave-Browser/NativeMessagingHosts"),
		filepath.Join(home, ".mozilla/native-messaging-hosts"),
		"/etc/opt/chrome/native-messaging-hosts",
		"/etc/chromium/native-messaging-hosts",
		"/usr/lib/mozilla/native-messaging-hosts",
		"/usr/lib64/mozilla/native-messaging-hosts",
		"/usr/share/mozilla/native-messaging-hosts",
		"/usr/lib/chromium/native-messaging-hosts",
	}
}

func (m *ExtensionManager) startNative(ext *Extension, name string) (*nativeConn, string) {
	granted := false
	for _, p := range append(append([]string{}, ext.Manifest.Permissions...), ext.Manifest.OptPermissions...) {
		if p == "nativeMessaging" {
			granted = true
		}
	}
	if !granted || strings.ContainsAny(name, "/\\ ") {
		return nil, "Access to the specified native messaging host is forbidden."
	}
	var manifestPath string
	var hm struct {
		Path       string   `json:"path"`
		Origins    []string `json:"allowed_origins"`
		Extensions []string `json:"allowed_extensions"`
	}
	for _, dir := range nativeDirs() {
		p := filepath.Join(dir, name+".json")
		if data, err := os.ReadFile(p); err == nil && json.Unmarshal(data, &hm) == nil && hm.Path != "" {
			manifestPath = p
			break
		}
	}
	if manifestPath == "" {
		return nil, "Specified native messaging host not found."
	}
	origin := "chrome-extension://" + ext.ID + "/"
	ok := len(hm.Origins) == 0 && len(hm.Extensions) == 0
	for _, o := range hm.Origins {
		if o == origin {
			ok = true
		}
	}
	for _, id := range hm.Extensions {
		if id == ext.realID() {
			ok = true
		}
	}
	if !ok {
		return nil, "Access to the specified native messaging host is forbidden."
	}
	args := []string{origin}
	if ext.Source == "firefox" {
		args = []string{manifestPath, ext.realID()}
	}
	cmd := exec.Command(hm.Path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "Failed to start native messaging host."
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, "Failed to start native messaging host."
	}
	go cmd.Wait()
	return &nativeConn{cmd: cmd, stdin: stdin, out: bufio.NewReaderSize(stdout, 1<<16)}, ""
}

func (c *nativeConn) write(msg json.RawMessage) error {
	var hdr [4]byte
	binary.NativeEndian.PutUint32(hdr[:], uint32(len(msg)))
	if _, err := c.stdin.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.stdin.Write(msg)
	return err
}

func (c *nativeConn) read() (json.RawMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.out, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.NativeEndian.Uint32(hdr[:])
	if n > 1<<20 {
		return nil, fmt.Errorf("native message too large")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.out, buf); err != nil {
		return nil, err
	}
	return json.RawMessage(buf), nil
}

func (c *nativeConn) kill() {
	c.stdin.Close()
	c.cmd.Process.Kill()
}

func (m *ExtensionManager) handleNative(ext *Extension, src extSource, typ string, payload json.RawMessage, done func(any, string)) {
	var p struct {
		App     string          `json:"app"`
		ID      string          `json:"id"`
		Message json.RawMessage `json:"message"`
	}
	json.Unmarshal(payload, &p)
	switch typ {
	case "native.send":
		conn, e := m.startNative(ext, p.App)
		if e != "" {
			done(nil, e)
			return
		}
		go func() {
			defer conn.kill()
			var reply json.RawMessage
			err := conn.write(p.Message)
			if err == nil {
				ch := make(chan struct{})
				go func() { reply, err = conn.read(); close(ch) }()
				select {
				case <-ch:
				case <-time.After(30 * time.Second):
					err = fmt.Errorf("native host timed out")
				}
			}
			coreglib.IdleAdd(func() {
				if err != nil {
					done(nil, "Native host has exited.")
				} else {
					done(reply, "")
				}
			})
		}()
	case "native.open":
		conn, e := m.startNative(ext, p.App)
		if e != "" {
			m.deliver(src.view, src.world(ext.ID), "nativeClose", map[string]any{"id": p.ID, "error": e})
			return
		}
		key := ext.ID + ":" + p.ID
		m.natives[key] = conn
		view, world := src.view, src.world(ext.ID)
		go func() {
			for {
				msg, err := conn.read()
				if err != nil {
					coreglib.IdleAdd(func() {
						delete(m.natives, key)
						m.deliver(view, world, "nativeClose", map[string]any{"id": p.ID, "error": "Native host has exited."})
					})
					return
				}
				coreglib.IdleAdd(func() { m.deliver(view, world, "nativeMsg", map[string]any{"id": p.ID, "msg": msg}) })
			}
		}()
	case "native.msg":
		if c := m.natives[ext.ID+":"+p.ID]; c != nil {
			go c.write(p.Message)
		}
	case "native.close":
		if c := m.natives[ext.ID+":"+p.ID]; c != nil {
			delete(m.natives, ext.ID+":"+p.ID)
			c.kill()
		}
	}
}
