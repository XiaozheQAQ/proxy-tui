package app

import (
	"context"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// SettingsSnapshot returns the supervisor-owned settings.
func (a *App) SettingsSnapshot(context.Context) storage.Settings { return a.Settings }

// UpdateSettings validates, persists and publishes shared settings. The
// in-memory copy changes only after the atomic write succeeds.
func (a *App) UpdateSettings(_ context.Context, settings storage.Settings) error {
	settings.Normalize()
	if err := storage.SaveSettings(a.Paths, settings); err != nil {
		return err
	}
	a.Settings = settings
	a.Subs.SetDefaultInterval(settings.UpdateIntervalSeconds)
	return nil
}
