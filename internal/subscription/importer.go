package subscription

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/security"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/subscription/parser"
	"github.com/proxytui/proxy-tui/internal/subscription/parser/mihomo"
	"github.com/proxytui/proxy-tui/internal/subscription/parser/shareuri"
	"github.com/proxytui/proxy-tui/internal/subscription/parser/singbox"
)

// ImportRequest describes one Add Subscription submission.
type ImportRequest struct {
	Core           model.CoreKind
	Name           string
	Input          string
	AllowDuplicate bool
}

// ImportOutcome reports the result for one imported source.
type ImportOutcome struct {
	Profile   model.Profile
	Detection Detection
	Warnings  []string
	Source    string
	Err       error
}

// ImportPayload is one already fetched or pasted subscription body.
type ImportPayload struct {
	Body         []byte
	Source       model.Source
	ETag         string
	LastModified string
}

// Import resolves, parses, validates and stores the pasted input.
func (m *Manager) Import(ctx context.Context, req ImportRequest) ([]ImportOutcome, error) {
	if !req.Core.Valid() {
		return nil, fmt.Errorf("invalid core %q", req.Core)
	}
	urls, inline := classifyImportInput(req.Input)
	if len(urls) == 0 && inline == "" {
		return nil, fmt.Errorf("paste a subscription URL, a share-link list or a YAML/JSON config first")
	}

	outcomes := make([]ImportOutcome, 0, len(urls)+1)
	if inline != "" {
		outcomes = append(outcomes, m.importPayload(ctx, req, ImportPayload{
			Body:   []byte(inline),
			Source: model.Source{Kind: model.SourcePaste},
		}))
	}
	for _, rawURL := range urls {
		endpoint, err := NormalizeSubscriptionURL(rawURL)
		if err != nil {
			outcomes = append(outcomes, ImportOutcome{Source: security.RedactURL(rawURL), Err: fmt.Errorf("%s", security.Redact(err.Error()))})
			continue
		}
		if !req.AllowDuplicate {
			if dup, ok := m.findByURL(req.Core, endpoint); ok {
				outcomes = append(outcomes, ImportOutcome{
					Profile: dup,
					Source:  security.RedactURL(endpoint),
					Err:     fmt.Errorf("already imported as %q", dup.Label()),
				})
				continue
			}
		}
		fetched, err := m.fetcher.Fetch(ctx, endpoint, FetchOptions{})
		if err != nil {
			outcomes = append(outcomes, ImportOutcome{Source: security.RedactURL(endpoint), Err: err})
			continue
		}
		outcomes = append(outcomes, m.importPayload(ctx, req, ImportPayload{
			Body:         fetched.Body,
			Source:       model.Source{Kind: model.SourceURL, URL: endpoint},
			ETag:         fetched.ETag,
			LastModified: fetched.LastModified,
		}))
	}
	return outcomes, nil
}

func (m *Manager) importPayload(ctx context.Context, req ImportRequest, payload ImportPayload) ImportOutcome {
	outcome := ImportOutcome{Source: displaySource(payload.Source)}
	if len(payload.Body) == 0 {
		outcome.Err = fmt.Errorf("subscription payload is empty")
		return outcome
	}
	detection, err := Detect(payload.Body)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	outcome.Detection = detection

	res, err := parsePayload(payload.Body, detection)
	if err != nil {
		outcome.Err = err
		return outcome
	}

	active, renderErr := RenderConfig(res, req.Core)
	warnings := append([]string{}, res.Warnings...)
	activatable := renderErr == nil && len(active) > 0
	if renderErr != nil {
		warnings = append(warnings, fmt.Sprintf("stored but not activatable for %s: %v", req.Core.DisplayName(), renderErr))
	}
	if activatable {
		validator := m.getValidator()
		if validator != nil {
			switch err := validator(ctx, req.Core, "", active); {
			case err == nil:
			case errors.Is(err, ErrCoreUnavailable):
				warnings = append(warnings, "config was not validated: the "+req.Core.DisplayName()+" binary is not installed yet")
			default:
				outcome.Err = fmt.Errorf("config validation failed: %w", err)
				outcome.Warnings = warnings
				return outcome
			}
		}
	}

	baseName := parser.SanitizeName(req.Name)
	if baseName == "" {
		baseName = deriveImportName(payload.Source, detection)
	}

	m.mu.Lock()
	name := m.uniqueNameLocked(baseName)
	profile := model.Profile{
		ID:                    m.uniqueIDLocked(name),
		Name:                  name,
		Core:                  req.Core,
		Format:                detection.Format,
		Encoding:              detection.Encoding,
		Source:                payload.Source,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
		ETag:                  payload.ETag,
		LastModified:          payload.LastModified,
		Revision:              1,
		NodeCount:             len(parseNodes(res)),
		GroupCount:            res.Groups,
		RuleCount:             res.Rules,
		SizeBytes:             int64(len(payload.Body)),
		Activatable:           activatable,
		Warnings:              warnings,
		Unsupported:           res.UnsupportedSummaries(20),
		UpdateIntervalSeconds: m.defaultInterval,
	}
	persisted, err := m.persistProfile(profile, payload.Body, active)
	if err != nil {
		m.mu.Unlock()
		outcome.Err = err
		return outcome
	}
	m.index.Profiles = append(m.index.Profiles, persisted)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		outcome.Err = err
		return outcome
	}
	m.mu.Unlock()

	outcome.Profile = persisted
	if len(warnings) > 0 {
		m.logf("warn", fmt.Sprintf("imported %q with %d warning(s)", persisted.Label(), len(warnings)))
	} else {
		m.logf("info", fmt.Sprintf("imported %q (%d nodes)", persisted.Label(), persisted.NodeCount))
	}
	if len(res.Unsupported) > 0 {
		outcome.Warnings = append(outcome.Warnings, fmt.Sprintf("%d entry/entries were not imported", len(res.Unsupported)))
	}
	return outcome
}

// parseNodes returns nodes for the profile when available.
func parseNodes(res *parser.Result) []parser.Node { return res.Nodes }

func (m *Manager) persistProfile(p model.Profile, source, active []byte) (model.Profile, error) {
	dir := m.paths.ProfileDir(p.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return p, fmt.Errorf("create profile dir: %w", err)
	}
	sourcePath := filepath.Join(dir, "source."+formatExt(p.Format))
	if err := storage.WriteFileAtomic(sourcePath, source, 0o600); err != nil {
		return p, err
	}
	if len(active) > 0 {
		snapshot, err := m.writeSnapshotFile(p.ID, 1, CoreExt(p.Core), active)
		if err != nil {
			return p, err
		}
		if err := storage.WriteFileAtomic(m.ActivePath(p), active, 0o600); err != nil {
			return p, err
		}
		p.ActiveSnapshot = snapshot
	}
	p.Revision = 1
	p.SizeBytes = int64(len(source))
	if err := m.writeMeta(p); err != nil {
		return p, err
	}
	return p, nil
}

func (m *Manager) getValidator() Validator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.validator
}

func (m *Manager) findByURL(core model.CoreKind, endpoint string) (model.Profile, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.index.Profiles {
		if p.Core != core || p.Source.Kind != model.SourceURL {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.Source.URL), strings.TrimSpace(endpoint)) {
			return p, true
		}
	}
	return model.Profile{}, false
}

// StripBOM removes a UTF-8 byte order mark left by editors or PowerShell.
func StripBOM(body []byte) []byte {
	return bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
}

func parsePayload(body []byte, detection Detection) (*parser.Result, error) {
	body = StripBOM(body)
	var (
		res *parser.Result
		err error
	)
	switch detection.Format {
	case model.FormatNodeList:
		var nodes []parser.Node
		var unsupported []parser.Unsupported
		nodes, unsupported, err = shareuri.ParseLines(string(body))
		if err != nil {
			return nil, err
		}
		res = &parser.Result{
			Format:      model.FormatNodeList,
			Encoding:    detection.Encoding,
			Nodes:       nodes,
			Unsupported: unsupported,
			Raw:         body,
		}
	case model.FormatMihomoYAML:
		res, err = mihomo.Parse(body)
	case model.FormatSingBoxJSON:
		res, err = singbox.Parse(body)
	default:
		return nil, fmt.Errorf("unsupported payload format %q", detection.Format)
	}
	if err != nil {
		return nil, err
	}
	res.Format = detection.Format
	res.Encoding = detection.Encoding
	return res, nil
}

func classifyImportInput(input string) (urls []string, inline string) {
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(input, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) == 0 {
		return nil, ""
	}
	allURLs := true
	for _, line := range lines {
		if !IsSubscriptionURL(line) || shareuri.LooksLikeNodeURI(line) {
			allURLs = false
			break
		}
	}
	if allURLs {
		return lines, ""
	}
	// Inline payloads are YAML/JSON/link lists: keep them byte for byte.
	// Trimming every line would destroy YAML indentation, which is semantic.
	return nil, strings.Trim(input, "\r\n")
}

func deriveImportName(source model.Source, detection Detection) string {
	if source.Kind == model.SourceURL && source.URL != "" {
		return nameFromURL(source.URL)
	}
	switch detection.Format {
	case model.FormatNodeList:
		return "Pasted nodes"
	case model.FormatMihomoYAML:
		return "Pasted Mihomo config"
	case model.FormatSingBoxJSON:
		return "Pasted sing-box config"
	default:
		return "Subscription"
	}
}

func displaySource(source model.Source) string {
	if source.Kind == model.SourceURL {
		return security.RedactURL(source.URL)
	}
	return "pasted content"
}
