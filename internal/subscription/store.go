package subscription

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

var snapshotName = regexp.MustCompile(`^(\d{4})-\d{8}-\d{6}\.(yaml|json)$`)

// CoreExt is the config file extension used by a core.
func CoreExt(core model.CoreKind) string {
	if core == model.CoreSingBox {
		return "json"
	}
	return "yaml"
}

func formatExt(format model.Format) string {
	switch format {
	case model.FormatMihomoYAML:
		return "yaml"
	case model.FormatSingBoxJSON:
		return "json"
	default:
		return "txt"
	}
}

// SourcePath is where the raw subscription payload is cached.
func (m *Manager) SourcePath(p model.Profile) string {
	return filepath.Join(m.paths.ProfileDir(p.ID), "source."+formatExt(p.Format))
}

// ActivePath is the config handed to the proxy core.
func (m *Manager) ActivePath(p model.Profile) string {
	return filepath.Join(m.paths.ProfileDir(p.ID), "active."+CoreExt(p.Core))
}

// SnapshotDir is the directory holding previous config revisions.
func (m *Manager) SnapshotDir(id string) string {
	return filepath.Join(m.paths.ProfileDir(id), "snapshots")
}

func (m *Manager) writeSnapshotFile(id string, revision int, ext string, data []byte) (string, error) {
	dir := m.SnapshotDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}
	name := fmt.Sprintf("%04d-%s.%s", revision, time.Now().Format("20060102-150405"), ext)
	if err := storage.WriteFileAtomic(filepath.Join(dir, name), data, 0o600); err != nil {
		return "", err
	}
	return name, nil
}

// Snapshots lists the stored revisions for a profile, newest first.
func (m *Manager) Snapshots(id string) ([]model.Snapshot, error) {
	dir := m.SnapshotDir(id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshots: %w", err)
	}
	snapshots := make([]model.Snapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := snapshotName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		revision, _ := strconv.Atoi(match[1])
		var size int64
		if info, err := entry.Info(); err == nil {
			size = info.Size()
		}
		snapshots = append(snapshots, model.Snapshot{
			Revision: revision,
			File:     entry.Name(),
			Size:     size,
			At:       modificationTime(dir, entry.Name()),
		})
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Revision > snapshots[j].Revision })
	return snapshots, nil
}

func modificationTime(dir, name string) time.Time {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// ReadSnapshot returns the raw bytes of one stored revision.
func (m *Manager) ReadSnapshot(id string, revision int) ([]byte, string, error) {
	snapshots, err := m.Snapshots(id)
	if err != nil {
		return nil, "", err
	}
	for _, snapshot := range snapshots {
		if snapshot.Revision == revision {
			data, err := os.ReadFile(filepath.Join(m.SnapshotDir(id), snapshot.File))
			if err != nil {
				return nil, "", fmt.Errorf("read snapshot %s: %w", snapshot.File, err)
			}
			return data, snapshot.File, nil
		}
	}
	return nil, "", fmt.Errorf("snapshot revision %d not found", revision)
}

// pruneSnapshots keeps the newest keep revisions for a profile.
func (m *Manager) pruneSnapshots(id string, keep int) error {
	snapshots, err := m.Snapshots(id)
	if err != nil || len(snapshots) <= keep {
		return err
	}
	for _, snapshot := range snapshots[keep:] {
		_ = os.Remove(filepath.Join(m.SnapshotDir(id), snapshot.File))
	}
	return nil
}

func (m *Manager) writeMeta(p model.Profile) error {
	return storage.WriteJSONAtomic(filepath.Join(m.paths.ProfileDir(p.ID), "meta.json"), p, 0o600)
}

// snapshotNameFor picks a display name for a stored revision.
func snapshotNameFor(profile model.Profile, revision int) string {
	return fmt.Sprintf("%s revision %d", profile.Label(), revision)
}

func extList() string { return strings.Join([]string{"yaml", "json"}, "/") }
