package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "proxy-tui"

// Paths holds every filesystem location ProxyTUI writes to.
// Config holds user-editable settings, Data holds downloaded cores and
// profile snapshots, State holds logs, runtime files and caches.
type Paths struct {
	Config string
	Data   string
	State  string
}

// DefaultPaths resolves the platform-native locations. Setting PROXY_TUI_HOME
// redirects everything into a single directory, which tests and portable
// installs rely on.
func DefaultPaths() (Paths, error) {
	if root := os.Getenv("PROXY_TUI_HOME"); root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve PROXY_TUI_HOME: %w", err)
		}
		return Paths{
			Config: filepath.Join(abs, "config"),
			Data:   filepath.Join(abs, "data"),
			State:  filepath.Join(abs, "state"),
		}, nil
	}

	cfgBase, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config dir: %w", err)
	}
	p := Paths{Config: filepath.Join(cfgBase, appDirName)}

	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = cfgBase
		}
		p.Data = filepath.Join(base, appDirName)
		p.State = filepath.Join(base, appDirName, "state")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home dir: %w", err)
		}
		p.Data = filepath.Join(home, "Library", "Application Support", appDirName)
		p.State = filepath.Join(home, "Library", "Logs", appDirName)
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home dir: %w", err)
		}
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(home, ".local", "state")
		}
		p.Data = filepath.Join(dataHome, appDirName)
		p.State = filepath.Join(stateHome, appDirName)
	}
	return p, nil
}

// Ensure creates every managed directory with user-only permissions.
func (p Paths) Ensure() error {
	dirs := []string{
		p.Config,
		p.Data,
		p.State,
		p.ProfilesDir(),
		p.CoresDir(),
		p.LogsDir(),
		p.CacheDir(),
		p.RuntimeDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func (p Paths) ConfigFile() string          { return filepath.Join(p.Config, "config.json") }
func (p Paths) KeymapFile() string          { return filepath.Join(p.Config, "keymap.json") }
func (p Paths) ProfilesDir() string         { return filepath.Join(p.Data, "profiles") }
func (p Paths) ProfileIndexFile() string    { return filepath.Join(p.Data, "profiles.json") }
func (p Paths) ProfileDir(id string) string { return filepath.Join(p.ProfilesDir(), id) }
func (p Paths) CoresDir() string            { return filepath.Join(p.Data, "cores") }
func (p Paths) CoreDir(name string) string  { return filepath.Join(p.CoresDir(), name) }
func (p Paths) LogsDir() string             { return filepath.Join(p.State, "logs") }
func (p Paths) CacheDir() string            { return filepath.Join(p.State, "cache") }
func (p Paths) RuntimeDir() string          { return filepath.Join(p.State, "runtime") }
func (p Paths) AppLogFile() string          { return filepath.Join(p.LogsDir(), "proxy-tui.log") }
