package subscription

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser"
)

// Validator checks a rendered config with the target core. When the core is
// not installed yet the validator returns ErrCoreUnavailable so imports keep
// working on a clean machine.
type Validator func(ctx context.Context, core model.CoreKind, name string, data []byte) error

// ErrCoreUnavailable marks validation as skipped because the binary is absent.
var ErrCoreUnavailable = errors.New("proxy core is not installed yet")

// Manager owns the profile index and every profile file on disk.
type Manager struct {
	paths     storage.Paths
	fetcher   *Fetcher
	validator Validator
	logf      func(level, msg string)

	mu              sync.RWMutex
	index           model.Index
	userAgentValue  string
	defaultInterval int
}

// NewManager loads profiles.json (creating it lazily).
func NewManager(paths storage.Paths, fetcher *Fetcher) (*Manager, error) {
	if fetcher == nil {
		fetcher = NewFetcher("")
	}
	m := &Manager{
		paths:   paths,
		fetcher: fetcher,
		index:   model.NewIndex(),
		logf:    func(string, string) {},
	}
	if err := storage.ReadJSON(paths.ProfileIndexFile(), &m.index); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if m.index.Active == nil {
		m.index.Active = map[model.CoreKind]string{}
	}
	if m.index.Version == 0 {
		m.index.Version = 1
	}
	m.userAgentValue = fetcher.userAgent
	return m, nil
}

// SetValidator installs the config validator (wired from the core manager).
func (m *Manager) SetValidator(v Validator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validator = v
}

// SetLogger installs a log sink.
func (m *Manager) SetLogger(fn func(level, msg string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn != nil {
		m.logf = fn
	}
}

// Paths exposes the storage layout.
func (m *Manager) Paths() storage.Paths { return m.paths }

// SetDefaultInterval sets the refresh interval applied to imported profiles.
func (m *Manager) SetDefaultInterval(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if seconds < 0 {
		seconds = 0
	}
	m.defaultInterval = seconds
}

// List returns a snapshot of every profile.
func (m *Manager) List() []model.Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Profile, len(m.index.Profiles))
	copy(out, m.index.Profiles)
	return out
}

// ByCore returns the profiles belonging to one core.
func (m *Manager) ByCore(core model.CoreKind) []model.Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Profile, 0, len(m.index.Profiles))
	for _, p := range m.index.Profiles {
		if p.Core == core {
			out = append(out, p)
		}
	}
	return out
}

// Get returns one profile by ID.
func (m *Manager) Get(id string) (model.Profile, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.getLocked(id); ok {
		return p, true
	}
	return model.Profile{}, false
}

// ActiveFor returns the active profile of a core.
func (m *Manager) ActiveFor(core model.CoreKind) (model.Profile, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.index.Active[core]
	if !ok {
		return model.Profile{}, false
	}
	return m.getLocked(id)
}

func (m *Manager) getLocked(id string) (model.Profile, bool) {
	for _, p := range m.index.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return model.Profile{}, false
}

// ActiveConfig loads the runtime config for a profile.
func (m *Manager) ActiveConfig(id string) ([]byte, string, error) {
	profile, ok := m.Get(id)
	if !ok {
		return nil, "", fmt.Errorf("profile %q not found", id)
	}
	if !profile.Activatable {
		return nil, "", fmt.Errorf("profile %q cannot be activated for %s", profile.Label(), profile.Core.DisplayName())
	}
	path := m.ActivePath(profile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read active config: %w", err)
	}
	return data, path, nil
}

// SetActive marks a profile as the active one for its core.
func (m *Manager) SetActive(id string) (model.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.getLocked(id)
	if !ok {
		return model.Profile{}, fmt.Errorf("profile %q not found", id)
	}
	if !profile.Activatable {
		return model.Profile{}, fmt.Errorf("profile %q cannot be activated for %s: %s", profile.Label(), profile.Core.DisplayName(), strings.Join(profile.Warnings, "; "))
	}
	if _, err := os.Stat(m.ActivePath(profile)); err != nil {
		return model.Profile{}, fmt.Errorf("active config for %q is missing: %w", profile.Label(), err)
	}
	m.index.Active[profile.Core] = id
	if err := m.saveLocked(); err != nil {
		return model.Profile{}, err
	}
	return profile, nil
}

// ClearActive removes the active marker for a core.
func (m *Manager) ClearActive(core model.CoreKind) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.index.Active, core)
	return m.saveLocked()
}

// Delete removes a profile and its files.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.getLocked(id)
	if !ok {
		return fmt.Errorf("profile %q not found", id)
	}
	remaining := make([]model.Profile, 0, len(m.index.Profiles)-1)
	for _, p := range m.index.Profiles {
		if p.ID != id {
			remaining = append(remaining, p)
		}
	}
	m.index.Profiles = remaining
	for core, activeID := range m.index.Active {
		if activeID == id {
			delete(m.index.Active, core)
		}
	}
	dir := m.paths.ProfileDir(profile.ID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove profile files: %w", err)
	}
	return m.saveLocked()
}

// Rollback restores a previous snapshot as the active config.
func (m *Manager) Rollback(id string, revision int) (model.Profile, error) {
	data, file, err := m.ReadSnapshot(id, revision)
	if err != nil {
		return model.Profile{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.getLocked(id)
	if !ok {
		return model.Profile{}, fmt.Errorf("profile %q not found", id)
	}
	if m.validator != nil {
		if err := m.validator(context.Background(), profile.Core, profile.Label(), data); err != nil && !errors.Is(err, ErrCoreUnavailable) {
			return model.Profile{}, fmt.Errorf("snapshot %s failed validation: %w", file, err)
		}
	}
	if err := storage.WriteFileAtomic(m.ActivePath(profile), data, 0o600); err != nil {
		return model.Profile{}, err
	}
	profile.ActiveSnapshot = file
	profile.Revision = revision
	profile.UpdatedAt = time.Now()
	m.replaceLocked(profile)
	if err := m.saveLocked(); err != nil {
		return model.Profile{}, err
	}
	if err := m.writeMeta(profile); err != nil {
		return model.Profile{}, err
	}
	return profile, nil
}

func (m *Manager) replaceLocked(profile model.Profile) {
	for i := range m.index.Profiles {
		if m.index.Profiles[i].ID == profile.ID {
			m.index.Profiles[i] = profile
			return
		}
	}
	m.index.Profiles = append(m.index.Profiles, profile)
}

func (m *Manager) saveLocked() error {
	if m.index.Active == nil {
		m.index.Active = map[model.CoreKind]string{}
	}
	sortProfiles(m.index.Profiles)
	return storage.WriteJSONAtomic(m.paths.ProfileIndexFile(), m.index, 0o600)
}

func sortProfiles(profiles []model.Profile) {
	// stable by creation time so the UI keeps a predictable order
	for i := 1; i < len(profiles); i++ {
		for j := i; j > 0 && profiles[j].CreatedAt.Before(profiles[j-1].CreatedAt); j-- {
			profiles[j], profiles[j-1] = profiles[j-1], profiles[j]
		}
	}
}

func (m *Manager) nameExistsLocked(name string) bool {
	for _, p := range m.index.Profiles {
		if strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

func (m *Manager) uniqueNameLocked(base string) string {
	base = parser.SanitizeName(base)
	if base == "" {
		base = "subscription"
	}
	if !m.nameExistsLocked(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s %d", base, i)
		if !m.nameExistsLocked(candidate) {
			return candidate
		}
	}
}

func (m *Manager) uniqueIDLocked(name string) string {
	for {
		id := newProfileID(name)
		if _, ok := m.getLocked(id); !ok {
			return id
		}
	}
}

func newProfileID(name string) string {
	slug := slugify(name)
	if slug == "" {
		slug = "profile"
	}
	return slug + "-" + randomHex(3)
}

func slugify(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xFFFFFF)
	}
	return hex.EncodeToString(buf)
}

func nameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "subscription"
	}
	host := parsed.Hostname()
	path := strings.Trim(strings.TrimSuffix(parsed.Path, "/"), "/")
	if path != "" {
		last := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			last = path[idx+1:]
		}
		last = strings.TrimSuffix(last, filepath.Ext(last))
		if last != "" && len(last) <= 24 {
			return host + "-" + last
		}
	}
	return host
}
