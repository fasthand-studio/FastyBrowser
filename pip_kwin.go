// Keeping the pop-up on top of other windows on KDE.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// keepAbove asks KWin to keep our window titled title above the others, on
// every desktop. Elsewhere it does nothing.
func keepAbove(title string) {
	if !strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") {
		return
	}
	script := fmt.Sprintf(`for (const w of workspace.windowList()) {
  if (w.pid === %d && w.caption === %q) {
    w.keepAbove = true;
    w.onAllDesktops = true;
  }
}`, os.Getpid(), title)
	path := filepath.Join(cacheDir(), "kwin-pip.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return
	}
	call := func(object, method string, args ...string) (string, error) {
		a := append([]string{"call", "--session", "--dest", "org.kde.KWin", "--object-path", object, "--method", method}, args...)
		out, err := exec.Command("gdbus", a...).Output()
		return strings.TrimSpace(string(out)), err
	}
	const plugin = "fasty-browser-pip"
	call("/Scripting", "org.kde.kwin.Scripting.unloadScript", plugin)
	out, err := call("/Scripting", "org.kde.kwin.Scripting.loadScript", path, plugin)
	var id int
	if err != nil || func() bool { _, e := fmt.Sscanf(out, "(%d,)", &id); return e != nil }() || id < 0 {
		log.Printf("fasty: couldn't keep the video window on top: %v %s", err, out)
		return
	}
	if _, err := call(fmt.Sprintf("/Scripting/Script%d", id), "org.kde.kwin.Script.run"); err != nil {
		log.Printf("fasty: couldn't keep the video window on top: %v", err)
	}
	time.Sleep(time.Second)
	call("/Scripting", "org.kde.kwin.Scripting.unloadScript", plugin)
}
