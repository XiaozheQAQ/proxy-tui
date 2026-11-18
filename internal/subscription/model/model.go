// Package model holds the domain types shared by the subscription layer,
// the core adapters and the TUI.
package model

import (
	"fmt"
	"strings"
	"time"
)

// CoreKind identifies a proxy core implementation.
type CoreKind string

const (
	CoreMihomo  CoreKind = "mihomo"
	CoreSingBox CoreKind = "sing-box"
)

// AllCores lists the supported cores in navigation order.
var AllCores = []CoreKind{CoreMihomo, CoreSingBox}

// DisplayName is the human facing label.
func (c CoreKind) DisplayName() string {
	switch c {
	case CoreMihomo:
		return "Mihomo"
	case CoreSingBox:
		return "sing-box"
	default:
		return string(c)
	}
}

// Valid reports whether the value is a supported core.
func (c CoreKind) Valid() bool { return c == CoreMihomo || c == CoreSingBox }

// ParseCoreKind accepts the CLI spellings used for cores.
func ParseCoreKind(s string) (CoreKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mihomo", "clash", "clash-meta", "meta":
		return CoreMihomo, nil
	case "sing-box", "singbox", "sing_box", "box":
		return CoreSingBox, nil
	}
	return "", fmt.Errorf("unknown core %q (want mihomo or sing-box)", s)
}

// Format is the detected payload format of a subscription.
type Format string

const (
	FormatMihomoYAML  Format = "mihomo-yaml"
	FormatSingBoxJSON Format = "sing-box-json"
	FormatNodeList    Format = "node-list"
)

// DisplayName renders the format for the UI.
func (f Format) DisplayName() string {
	switch f {
	case FormatMihomoYAML:
		return "Mihomo YAML"
	case FormatSingBoxJSON:
		return "sing-box JSON"
	case FormatNodeList:
		return "node list"
	default:
		return string(f)
	}
}

// Encoding records whether the payload was wrapped in base64.
type Encoding string

const (
	EncodingPlain  Encoding = "plain"
	EncodingBase64 Encoding = "base64"
)

// SourceKind distinguishes where a profile originally came from.
type SourceKind string

const (
	SourceURL   SourceKind = "url"
	SourcePaste SourceKind = "paste"
	SourceFile  SourceKind = "file"
)

// Source describes the origin of a profile.
type Source struct {
	Kind SourceKind `json:"kind"`
	URL  string     `json:"url,omitempty"`
}

// UpdateResult records the outcome of the last refresh attempt.
type UpdateResult struct {
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Nodes    int       `json:"nodes,omitempty"`
	Revision int       `json:"revision,omitempty"`
}

// Profile is one imported subscription plus the generated runtime config.
type Profile struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Core     CoreKind `json:"core"`
	Format   Format   `json:"format"`
	Encoding Encoding `json:"encoding,omitempty"`
	Source   Source   `json:"source"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`

	UpdateIntervalSeconds int           `json:"update_interval_seconds,omitempty"`
	LastUpdate            *UpdateResult `json:"last_update,omitempty"`

	// ActiveSnapshot is the file name of the config currently in use.
	ActiveSnapshot string `json:"active_snapshot,omitempty"`
	Revision       int    `json:"revision"`

	NodeCount  int   `json:"node_count"`
	GroupCount int   `json:"group_count"`
	RuleCount  int   `json:"rule_count"`
	SizeBytes  int64 `json:"size_bytes"`
	// Activatable is false when the payload cannot be converted into the
	// target core's config format. The profile is still stored verbatim.
	Activatable bool `json:"activatable"`

	Warnings    []string `json:"warnings,omitempty"`
	Unsupported []string `json:"unsupported,omitempty"`
}

// Label returns a short description used in lists.
func (p Profile) Label() string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// Index is the persisted profile database (profiles.json).
type Index struct {
	Version  int                 `json:"version"`
	Active   map[CoreKind]string `json:"active"`
	Profiles []Profile           `json:"profiles"`
}

// NewIndex returns an empty index with maps initialised.
func NewIndex() Index {
	return Index{Version: 1, Active: map[CoreKind]string{}}
}

// Snapshot describes a stored config revision on disk.
type Snapshot struct {
	Revision int       `json:"revision"`
	At       time.Time `json:"at"`
	File     string    `json:"file"`
	Size     int64     `json:"size"`
	Nodes    int       `json:"nodes"`
	Note     string    `json:"note,omitempty"`
}
