// Package core installs, configures and supervises proxy core binaries.
package core

import (
	"context"
	"errors"
	"time"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Adapter errors shared by every core implementation.
var (
	ErrNotInstalled   = errors.New("core binary is not installed")
	ErrUnsupported    = errors.New("feature is not supported by this core")
	ErrNotRunning     = errors.New("core is not running")
	ErrAlreadyRunning = errors.New("core is already running")
	ErrInvalidConfig  = errors.New("core rejected the configuration")
)

// Capabilities documents what an adapter can actually do.
type Capabilities struct {
	Proxies        bool
	ProxyGroups    bool
	Connections    bool
	Traffic        bool
	ValidateConfig bool
	Install        bool
	ClashAPI       bool
}

// Status is the observed state of one core.
type Status struct {
	Core       model.CoreKind
	Installed  bool
	Version    string
	BinaryPath string
	Running    bool
	PID        int
	StartedAt  time.Time
	ConfigPath string
	APIAddr    string
	LastError  string
}

// Label renders a compact status for the header bar.
func (s Status) Label() string {
	switch {
	case s.Running:
		return "RUNNING"
	case s.Installed:
		return "STOPPED"
	default:
		return "NOT INSTALLED"
	}
}

// Proxy is one node or proxy group reported by the core API.
type Proxy struct {
	Name  string
	Type  string
	Group bool
	Now   string
	All   []string
	Delay int
	Alive bool
}

// Connection is one live connection entry.
type Connection struct {
	ID            string
	Host          string
	DestinationIP string
	Network       string
	Type          string
	Rule          string
	Chain         []string
	Upload        int64
	Download      int64
	Start         time.Time
}

// Traffic is the current throughput counters reported by the core.
type Traffic struct {
	UpRate    int64
	DownRate  int64
	UpTotal   int64
	DownTotal int64
}

// StartOptions carries the config the core should run with.
type StartOptions struct {
	ProfileID   string
	ProfileName string
	Config      []byte
}

// InstallResult describes a completed installation.
type InstallResult struct {
	Version    string
	BinaryPath string
	Verified   bool
	Notes      []string
}

// Core is the adapter contract. Adapters return ErrUnsupported instead of
// pretending a feature works.
type Core interface {
	Kind() model.CoreKind
	Capabilities() Capabilities

	Status(ctx context.Context) Status
	Install(ctx context.Context, progress func(string)) (InstallResult, error)
	Start(ctx context.Context, opts StartOptions) error
	Stop(ctx context.Context) error
	ValidateConfig(ctx context.Context, name string, data []byte) error

	Proxies(ctx context.Context) ([]Proxy, error)
	SelectProxy(ctx context.Context, group, name string) error
	TestDelay(ctx context.Context, name string, timeout time.Duration) (int, error)

	Connections(ctx context.Context) ([]Connection, error)
	Traffic(ctx context.Context) (Traffic, error)
}
