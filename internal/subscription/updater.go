package subscription

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/security"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// UpdateOutcome reports one refresh attempt.
type UpdateOutcome struct {
	Profile model.Profile
	Err     error
}

// Update re-fetches a profile, validates the new config and only then swaps it
// into place. Failures leave the previous revision untouched.
func (m *Manager) Update(ctx context.Context, id string) (model.Profile, error) {
	profile, ok := m.Get(id)
	if !ok {
		return model.Profile{}, fmt.Errorf("profile %q not found", id)
	}
	if profile.Source.Kind != model.SourceURL || profile.Source.URL == "" {
		return model.Profile{}, fmt.Errorf("profile %q has no subscription URL to refresh", profile.Label())
	}

	fetched, err := m.fetcher.Fetch(ctx, profile.Source.URL, FetchOptions{
		ETag:         profile.ETag,
		LastModified: profile.LastModified,
		UserAgent:    m.userAgent(),
	})
	if err != nil {
		m.recordUpdateFailure(id, err)
		return model.Profile{}, err
	}
	if fetched.NotModified {
		m.recordUpdateSuccess(id, profile.NodeCount, profile.Revision, fetched.ETag, fetched.LastModified)
		updated, _ := m.Get(id)
		return updated, nil
	}

	detection, err := Detect(fetched.Body)
	if err != nil {
		m.recordUpdateFailure(id, err)
		return model.Profile{}, err
	}
	res, err := parsePayload(fetched.Body, detection)
	if err != nil {
		m.recordUpdateFailure(id, err)
		return model.Profile{}, err
	}

	active, renderErr := RenderConfig(res, profile.Core)
	activatable := renderErr == nil && len(active) > 0
	warnings := append([]string{}, res.Warnings...)
	if renderErr != nil {
		warnings = append(warnings, fmt.Sprintf("stored but not activatable for %s: %v", profile.Core.DisplayName(), renderErr))
	}
	if activatable {
		if validator := m.getValidator(); validator != nil {
			switch err := validator(ctx, profile.Core, profile.Label(), active); {
			case err == nil:
			case errors.Is(err, ErrCoreUnavailable):
				warnings = append(warnings, "config was not validated: the "+profile.Core.DisplayName()+" binary is not installed yet")
			default:
				m.recordUpdateFailure(id, fmt.Errorf("config validation failed: %w", err))
				return model.Profile{}, fmt.Errorf("config validation failed, keeping revision %d: %w", profile.Revision, err)
			}
		}
	}

	m.mu.Lock()
	current, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return model.Profile{}, fmt.Errorf("profile %q disappeared during update", id)
	}
	newRevision := current.Revision + 1
	sourcePath := filepath.Join(m.paths.ProfileDir(current.ID), "source."+formatExt(detection.Format))
	if err := storage.WriteFileAtomic(sourcePath, fetched.Body, 0o600); err != nil {
		m.mu.Unlock()
		m.recordUpdateFailure(id, err)
		return model.Profile{}, err
	}
	if activatable {
		snapshot, err := m.writeSnapshotFile(current.ID, newRevision, CoreExt(current.Core), active)
		if err != nil {
			m.mu.Unlock()
			m.recordUpdateFailure(id, err)
			return model.Profile{}, err
		}
		if err := storage.WriteFileAtomic(m.ActivePath(current), active, 0o600); err != nil {
			m.mu.Unlock()
			m.recordUpdateFailure(id, err)
			return model.Profile{}, err
		}
		current.ActiveSnapshot = snapshot
	}
	current.Format = detection.Format
	current.Encoding = detection.Encoding
	current.Revision = newRevision
	current.ETag = fetched.ETag
	current.LastModified = fetched.LastModified
	current.UpdatedAt = time.Now()
	current.NodeCount = len(res.Nodes)
	current.GroupCount = res.Groups
	current.RuleCount = res.Rules
	current.SizeBytes = int64(len(fetched.Body))
	current.Activatable = activatable
	current.Warnings = warnings
	current.Unsupported = res.UnsupportedSummaries(20)
	current.LastUpdate = &model.UpdateResult{At: time.Now(), OK: true, Nodes: len(res.Nodes), Revision: newRevision}
	m.replaceLocked(current)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return model.Profile{}, err
	}
	if err := m.writeMeta(current); err != nil {
		m.mu.Unlock()
		return model.Profile{}, err
	}
	m.mu.Unlock()

	if err := m.pruneSnapshots(current.ID, 10); err != nil {
		m.logf("warn", fmt.Sprintf("prune snapshots for %q: %v", current.Label(), err))
	}
	m.logf("info", fmt.Sprintf("updated %q to revision %d (%d nodes)", current.Label(), newRevision, len(res.Nodes)))
	return current, nil
}

// UpdateAll refreshes every URL backed profile.
func (m *Manager) UpdateAll(ctx context.Context) []UpdateOutcome {
	profiles := m.List()
	outcomes := make([]UpdateOutcome, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Source.Kind != model.SourceURL || profile.Source.URL == "" {
			continue
		}
		updated, err := m.Update(ctx, profile.ID)
		outcomes = append(outcomes, UpdateOutcome{Profile: updated, Err: err})
	}
	return outcomes
}

func (m *Manager) recordUpdateFailure(id string, cause error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.getLocked(id)
	if !ok {
		return
	}
	profile.LastUpdate = &model.UpdateResult{At: time.Now(), OK: false, Error: security.Redact(cause.Error())}
	m.replaceLocked(profile)
	if err := m.saveLocked(); err != nil {
		m.logf("error", fmt.Sprintf("persist update failure: %v", err))
	}
}

func (m *Manager) recordUpdateSuccess(id string, nodes, revision int, etag, lastModified string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.getLocked(id)
	if !ok {
		return
	}
	profile.LastUpdate = &model.UpdateResult{At: time.Now(), OK: true, Nodes: nodes, Revision: revision}
	profile.UpdatedAt = time.Now()
	if etag != "" {
		profile.ETag = etag
	}
	if lastModified != "" {
		profile.LastModified = lastModified
	}
	m.replaceLocked(profile)
	if err := m.saveLocked(); err != nil {
		m.logf("error", fmt.Sprintf("persist update success: %v", err))
	}
}

// Due returns URL backed profiles whose refresh interval has elapsed.
func (m *Manager) Due(now time.Time) []model.Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var due []model.Profile
	for _, p := range m.index.Profiles {
		if p.Source.Kind != model.SourceURL || p.Source.URL == "" {
			continue
		}
		interval := p.UpdateIntervalSeconds
		if interval <= 0 {
			continue
		}
		last := p.UpdatedAt
		if p.LastUpdate != nil {
			last = p.LastUpdate.At
		}
		if now.Sub(last) >= time.Duration(interval)*time.Second {
			due = append(due, p)
		}
	}
	return due
}

// userAgent returns the configured UA under lock.
func (m *Manager) userAgent() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userAgentValue
}

// SetUserAgent stores the User-Agent used for subscription requests.
func (m *Manager) SetUserAgent(ua string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userAgentValue = ua
}
