package app

import (
	"context"
	"encoding/json"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// SettingsSnapshot returns an immutable copy of the supervisor-owned settings.
func (a *App) SettingsSnapshot(context.Context) storage.Settings {
	return a.settingsCopy()
}

// UpdateSettings validates, persists and publishes shared settings. The
// in-memory copy changes only after the atomic write succeeds.
func (a *App) UpdateSettings(_ context.Context, settings storage.Settings) error {
	settings.Normalize()
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if err := storage.SaveSettings(a.Paths, settings); err != nil {
		return err
	}
	a.Settings = settings
	a.Subs.SetDefaultInterval(settings.UpdateIntervalSeconds)
	return nil
}

// PatchSettings merges named fields into the current settings. Concurrent
// clients editing different fields cannot overwrite each other; the same
// field remains a deterministic last-writer-wins value.
func (a *App) PatchSettings(_ context.Context, patch map[string]json.RawMessage) error {
	if len(patch) == 0 {
		return nil
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	next, err := storage.ApplySettingsPatch(a.Settings, patch)
	if err != nil {
		return err
	}
	if err := storage.SaveSettings(a.Paths, next); err != nil {
		return err
	}
	a.Settings = next
	a.Subs.SetDefaultInterval(next.UpdateIntervalSeconds)
	return nil
}
