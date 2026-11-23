package main

// GPU video decoding on NVIDIA cards (Settings > General, off by default).
//
// By default Fasty decodes video on the CPU (see pickVideoDecoders). WebKit
// decodes video inside its sandboxed web process with GStreamer, and NVIDIA's
// decoder (NVDEC) can't start there: it needs /dev/nvidia-uvm, which WebKit's
// sandbox doesn't include. Firefox has the same trouble on NVIDIA. With this
// turned on Fasty uses the open-source pieces other browsers use:
//
//   - GStreamer's va plugin (gst-plugin-va) and NVIDIA's VA-API driver
//     (libva-nvidia-driver) decode through NVDEC. The va plugin ignores that
//     driver unless GST_VA_ALL_DRIVERS is set, and caches what it found, so
//     Fasty keeps its own GStreamer registry made with that set;
//   - native/bwrap_uvm.c, preloaded into Fasty, adds the two UVM device nodes
//     to the web process's sandbox (and nothing else).
//
// Measured on an RTX 3060: 1080p YouTube took about 33% of a core instead of
// about 44%, with the decoder doing the work (nvidia-smi showed it busy).

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const gpuDecodeEnv = "FASTY_GPUDEC"

// hookPath is the preloaded helper library, installed next to the program.
func hookPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "libfasty-uvm.so")
}

func gpuRegistryPath() string { return filepath.Join(cacheDir(), "gst-registry-gpudec.bin") }

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if m, _ := filepath.Glob(p); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

func vaPluginPath() string {
	return firstExisting("/usr/lib/gstreamer-1.0/libgstva.so", "/usr/lib64/gstreamer-1.0/libgstva.so",
		"/usr/lib/*/gstreamer-1.0/libgstva.so")
}

// gpuDecodeMissing lists what GPU decoding needs that isn't here.
func gpuDecodeMissing() []string {
	var m []string
	if _, err := os.Stat("/dev/nvidia-uvm"); err != nil {
		m = append(m, "an NVIDIA card with its driver (no /dev/nvidia-uvm)")
	}
	if firstExisting("/usr/lib/dri/nvidia_drv_video.so", "/usr/lib64/dri/nvidia_drv_video.so",
		"/usr/lib/*/dri/nvidia_drv_video.so") == "" {
		m = append(m, "libva-nvidia-driver")
	}
	if vaPluginPath() == "" {
		m = append(m, "gst-plugin-va")
	}
	if _, err := exec.LookPath("gst-inspect-1.0"); err != nil {
		m = append(m, "gst-inspect-1.0")
	}
	if p := hookPath(); p == "" || func() bool { _, err := os.Stat(p); return err != nil }() {
		m = append(m, "the helper library (reinstall with gcc present)")
	}
	return m
}

func gpuDecodeSettingOn() bool {
	var s struct {
		On bool `json:"gpu_video_decode"`
	}
	b, err := os.ReadFile(dataPath("settings.json"))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return false
	}
	return s.On
}

func gpuDecodeEnvVars() []string {
	return []string{
		"GST_VA_ALL_DRIVERS=1",
		"LIBVA_DRIVER_NAME=nvidia",
		"NVD_BACKEND=direct",
		"GST_REGISTRY_1_0=" + gpuRegistryPath(),
		"GST_PLUGIN_FEATURE_RANK=vah264dec:300,vavp9dec:300,vaav1dec:300,vah265dec:300",
	}
}

// gpuDecodeStart runs first thing in main. When the setting is on and
// everything it needs is there, it sets the GStreamer variables, makes
// Fasty's own GStreamer registry the first time, and restarts the program
// once with the helper library preloaded (it has to be loaded at start-up).
func gpuDecodeStart() {
	if os.Getenv(gpuDecodeEnv) == "1" || !gpuDecodeSettingOn() {
		return
	}
	if os.Getenv("GST_PLUGIN_FEATURE_RANK") != "" {
		return // the user has set decoder ranks themselves: leave them be
	}
	if miss := gpuDecodeMissing(); len(miss) > 0 {
		log.Printf("fasty: GPU video decoding is on but needs: %s", strings.Join(miss, ", "))
		return
	}
	env := append(os.Environ(), gpuDecodeEnvVars()...)
	env = append(env, gpuDecodeEnv+"=1")

	// The registry the va plugin's NVIDIA decoders are in. It is rebuilt when
	// the plugin changes.
	reg := gpuRegistryPath()
	rebuild := true
	if ri, err := os.Stat(reg); err == nil {
		if pi, err := os.Stat(vaPluginPath()); err == nil && ri.ModTime().After(pi.ModTime()) {
			rebuild = false
		}
	}
	if rebuild {
		os.MkdirAll(filepath.Dir(reg), 0o755)
		os.Remove(reg)
		cmd := exec.Command("gst-inspect-1.0", "va")
		cmd.Env = append(os.Environ(), gpuDecodeEnvVars()...)
		done := make(chan error, 1)
		if err := cmd.Start(); err != nil {
			log.Printf("fasty: couldn't prepare GPU decoding: %v", err)
			return
		}
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(90 * time.Second):
			cmd.Process.Kill()
			log.Printf("fasty: preparing GPU decoding took too long")
			return
		}
	}

	hook := hookPath()
	pre := hook
	if old := os.Getenv("LD_PRELOAD"); old != "" {
		pre = hook + ":" + old
	}
	env = append(env, "LD_PRELOAD="+pre)
	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return
	}
	if err := syscall.Exec(exe, os.Args, env); err != nil {
		log.Printf("fasty: couldn't restart for GPU decoding: %v", err)
	}
}

// gpuDecodeActive is true in the restarted program.
func gpuDecodeActive() bool { return os.Getenv(gpuDecodeEnv) == "1" }

// gpuDecodeStatus is the line shown in Settings.
func gpuDecodeStatus() (ok bool, line string) {
	if miss := gpuDecodeMissing(); len(miss) > 0 {
		return false, fmt.Sprintf("Needs: %s", strings.Join(miss, ", "))
	}
	return true, "Off by default. Decodes video on your NVIDIA card instead of the CPU. Takes effect next launch."
}
