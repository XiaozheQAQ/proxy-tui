package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

var semverPattern = regexp.MustCompile(`v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.]+)?`)

// BinaryName returns the executable name for a core on this platform.
func BinaryName(kind model.CoreKind) string {
	if runtime.GOOS == "windows" {
		return string(kind) + ".exe"
	}
	return string(kind)
}

// Layout resolves where a core's binaries live.
type Layout struct {
	Paths storage.Paths
	Kind  model.CoreKind
}

// NewLayout builds the layout for one core.
func NewLayout(paths storage.Paths, kind model.CoreKind) Layout {
	return Layout{Paths: paths, Kind: kind}
}

// Dir is the root directory for this core.
func (l Layout) Dir() string { return l.Paths.CoreDir(string(l.Kind)) }

func (l Layout) currentFile() string { return filepath.Join(l.Dir(), "current.txt") }

// BinaryPath is where a specific version's executable is stored.
func (l Layout) BinaryPath(version string) string {
	return filepath.Join(l.Dir(), version, BinaryName(l.Kind))
}

// Installed reports the selected version and binary, if any.
func (l Layout) Installed() (version string, binary string, ok bool) {
	data, err := os.ReadFile(l.currentFile())
	if err != nil {
		return "", "", false
	}
	version = strings.TrimSpace(string(data))
	if version == "" {
		return "", "", false
	}
	binary = l.BinaryPath(version)
	if _, err := os.Stat(binary); err != nil {
		return "", "", false
	}
	return version, binary, true
}

// SetCurrent marks a version as the selected one.
func (l Layout) SetCurrent(version string) error {
	return storage.WriteFileAtomic(l.currentFile(), []byte(version+"\n"), 0o600)
}

// MetaPath is the install metadata file.
func (l Layout) MetaPath() string { return filepath.Join(l.Dir(), "meta.json") }

// ProbeVersion runs the binary to learn its version.
func ProbeVersion(ctx context.Context, binary string) (string, error) {
	candidates := [][]string{{"--version"}, {"-v"}, {"version"}}
	for _, args := range candidates {
		runCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		cmd := exec.CommandContext(runCtx, binary, args...)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		if match := semverPattern.FindString(string(out)); match != "" {
			return strings.TrimPrefix(match, "v"), nil
		}
	}
	return "", fmt.Errorf("cannot determine the version of %s", filepath.Base(binary))
}
