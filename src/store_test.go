package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Private data must be readable by this user only, including data an older
// version left open to everyone.
func TestDataIsPrivate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	// an older install: folder open to all, files readable by all
	old := filepath.Join(root, "data", appName)
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "history.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	hardenFiles()
	if fi, _ := os.Stat(old); fi.Mode().Perm() != 0o700 {
		t.Errorf("data folder is %o, want 700", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Join(old, "history.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("old history file is %o, want 600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(cacheDir()); fi.Mode().Perm() != 0o700 {
		t.Errorf("cache folder is %o, want 700", fi.Mode().Perm())
	}
	if err := writeJSON("settings.json", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dataPath("settings.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file is %o, want 600", fi.Mode().Perm())
	}
}
