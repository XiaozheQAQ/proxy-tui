package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsWithHomeOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if paths.Config != filepath.Join(dir, "config") {
		t.Errorf("config dir = %s", paths.Config)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, path := range []string{paths.Config, paths.Data, paths.State, paths.ProfilesDir(), paths.CoresDir()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", path)
		}
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "file.txt")
	if err := WriteFileAtomic(target, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	if err := WriteFileAtomic(target, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic overwrite: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("content = %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %d entries", len(entries))
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	settings, err := LoadSettings(paths)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Theme != ThemeDark {
		t.Errorf("default theme = %q", settings.Theme)
	}
	settings.Theme = ThemeLight
	settings.UpdateIntervalSeconds = 3600
	if err := SaveSettings(paths, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	reloaded, err := LoadSettings(paths)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if reloaded.Theme != ThemeLight || reloaded.UpdateIntervalSeconds != 3600 {
		t.Errorf("reloaded settings = %+v", reloaded)
	}
}
