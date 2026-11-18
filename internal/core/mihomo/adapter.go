// Package mihomo adapts the Mihomo (Clash Meta) core.
package mihomo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/core/clashapi"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Adapter implements core.Core for Mihomo.
type Adapter struct {
	paths storage.Paths
	opts  core.RuntimeOptions
	logf  func(level, msg string)
	proc  *core.Process

	mu     sync.Mutex
	client *clashapi.Client
	cancel context.CancelFunc

	trafficMu sync.Mutex
	upRate    int64
	downRate  int64
}

// New builds a Mihomo adapter.
func New(paths storage.Paths, opts core.RuntimeOptions, logf func(level, msg string)) *Adapter {
	if opts.ProxyPort == 0 {
		opts.ProxyPort = 7890
	}
	if opts.APIPort == 0 {
		opts.APIPort = 9090
	}
	if logf == nil {
		logf = func(string, string) {}
	}
	return &Adapter{paths: paths, opts: opts, logf: logf, proc: core.NewProcess(logf)}
}

// Kind identifies the core.
func (a *Adapter) Kind() model.CoreKind { return model.CoreMihomo }

// Capabilities reports the Mihomo feature set.
func (a *Adapter) Capabilities() core.Capabilities {
	return core.Capabilities{
		Proxies:        true,
		ProxyGroups:    true,
		Connections:    true,
		Traffic:        true,
		ValidateConfig: true,
		Install:        true,
		ClashAPI:       true,
	}
}

// Status reports the installed version and running state. A core started by a
// previous process is adopted when its control API still answers.
func (a *Adapter) Status(ctx context.Context) core.Status {
	status := core.Status{Core: model.CoreMihomo}
	if version, binary, ok := core.NewLayout(a.paths, model.CoreMihomo).Installed(); ok {
		status.Installed = true
		status.Version = version
		status.BinaryPath = binary
	}
	if a.proc.Running() {
		status.Running = true
		status.PID = a.proc.PID()
		status.StartedAt = a.proc.StartedAt()
		status.ConfigPath = a.proc.ConfigPath()
		status.APIAddr = a.proc.APIAddr()
		status.LastError = a.proc.LastError()
		return status
	}
	if state, ok := core.ReadRuntimeState(a.paths, model.CoreMihomo); ok {
		client := clashapi.New(state.APIAddr, state.Secret)
		probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		_, err := client.Version(probeCtx)
		cancel()
		if err == nil {
			status.Running = true
			status.PID = state.PID
			status.StartedAt = state.StartedAt
			status.APIAddr = state.APIAddr
			status.ConfigPath = state.ConfigPath
			a.adopt(client)
			return status
		}
		_ = core.RemoveRuntimeState(a.paths, model.CoreMihomo)
	}
	status.LastError = a.proc.LastError()
	return status
}

// adopt takes over a core that was started by another proxy-tui process.
func (a *Adapter) adopt(client *clashapi.Client) {
	a.mu.Lock()
	a.client = client
	active := a.cancel != nil
	a.mu.Unlock()
	if !active {
		a.startTrafficWatch(context.Background(), client)
	}
}

// Install downloads the newest Mihomo release.
func (a *Adapter) Install(ctx context.Context, progress func(string)) (core.InstallResult, error) {
	result, err := core.Install(ctx, a.paths, model.CoreMihomo, core.MihomoRepo, nil, progress)
	if err != nil {
		return result, err
	}
	a.logf("info", "installed Mihomo "+result.Version)
	return result, nil
}

// ValidateConfig runs "mihomo -t" against the rendered config.
func (a *Adapter) ValidateConfig(ctx context.Context, name string, data []byte) error {
	_, binary, ok := core.NewLayout(a.paths, model.CoreMihomo).Installed()
	if !ok {
		return core.ErrNotInstalled
	}
	runtimeConfig, err := core.PrepareMihomoConfig(data, core.RuntimeOptions{
		ProxyPort: a.opts.ProxyPort,
		APIPort:   a.opts.APIPort,
		Secret:    "validation-only",
		LogLevel:  a.opts.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrInvalidConfig, err)
	}
	dir := filepath.Join(a.paths.RuntimeDir(), "mihomo-validate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.yaml")
	if err := storage.WriteFileAtomic(path, runtimeConfig, 0o600); err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(path)
		_ = os.Remove(dir)
	}()

	runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binary, "-t", "-f", path, "-d", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%w: %s", core.ErrInvalidConfig, message)
	}
	return nil
}

// Start writes the runtime config and launches Mihomo.
func (a *Adapter) Start(ctx context.Context, opts core.StartOptions) error {
	if a.proc.Running() {
		return core.ErrAlreadyRunning
	}
	// A core started by a previous process also counts as running.
	if status := a.Status(ctx); status.Running {
		return core.ErrAlreadyRunning
	}
	version, binary, ok := core.NewLayout(a.paths, model.CoreMihomo).Installed()
	if !ok {
		return core.ErrNotInstalled
	}
	secret := core.RandomSecret()
	runtimeConfig, err := core.PrepareMihomoConfig(opts.Config, core.RuntimeOptions{
		ProxyPort: a.opts.ProxyPort,
		APIPort:   a.opts.APIPort,
		Secret:    secret,
		LogLevel:  a.opts.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("prepare mihomo config: %w", err)
	}
	dir := filepath.Join(a.paths.RuntimeDir(), "mihomo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	if err := storage.WriteFileAtomic(configPath, runtimeConfig, 0o600); err != nil {
		return err
	}
	apiAddr := fmt.Sprintf("127.0.0.1:%d", a.opts.APIPort)
	if err := a.proc.Start(binary, []string{"-d", dir, "-f", configPath}, nil, dir, apiAddr); err != nil {
		return err
	}
	client := clashapi.New(apiAddr, secret)
	a.mu.Lock()
	a.client = client
	a.mu.Unlock()

	if err := a.waitForAPI(ctx, client); err != nil {
		_ = a.proc.Stop(3 * time.Second)
		a.mu.Lock()
		a.client = nil
		a.mu.Unlock()
		return err
	}
	a.startTrafficWatch(ctx, client)
	state := core.RuntimeState{
		Core:       model.CoreMihomo,
		PID:        a.proc.PID(),
		APIAddr:    apiAddr,
		Secret:     secret,
		ProxyPort:  a.opts.ProxyPort,
		ConfigPath: configPath,
		Version:    version,
		StartedAt:  a.proc.StartedAt(),
	}
	if err := core.WriteRuntimeState(a.paths, state); err != nil {
		a.logf("warn", "persist runtime state: "+err.Error())
	}
	a.logf("info", fmt.Sprintf("Mihomo started, proxy on 127.0.0.1:%d, API on %s", a.opts.ProxyPort, apiAddr))
	return nil
}

func (a *Adapter) waitForAPI(ctx context.Context, client *clashapi.Client) error {
	deadline := time.Now().Add(8 * time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := client.Version(probeCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Mihomo API did not become ready: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (a *Adapter) startTrafficWatch(ctx context.Context, client *clashapi.Client) {
	watchCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.cancel = cancel
	a.mu.Unlock()

	go func() {
		body, err := client.OpenTraffic(watchCtx)
		if err != nil {
			return
		}
		defer func() { _ = body.Close() }()
		_ = clashapi.ReadTraffic(body, func(up, down int64) {
			a.trafficMu.Lock()
			a.upRate = up
			a.downRate = down
			a.trafficMu.Unlock()
		})
	}()
}

// Stop terminates the core and the traffic stream, including a core adopted
// from a previous process.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.client = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.trafficMu.Lock()
	a.upRate, a.downRate = 0, 0
	a.trafficMu.Unlock()

	if a.proc.Running() {
		err := a.proc.Stop(8 * time.Second)
		_ = core.RemoveRuntimeState(a.paths, model.CoreMihomo)
		return err
	}
	if state, ok := core.ReadRuntimeState(a.paths, model.CoreMihomo); ok {
		if err := core.KillPID(state.PID); err != nil {
			return err
		}
	}
	return core.RemoveRuntimeState(a.paths, model.CoreMihomo)
}

func (a *Adapter) activeClient() (*clashapi.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, core.ErrNotRunning
	}
	return a.client, nil
}

// Proxies lists nodes and groups.
func (a *Adapter) Proxies(ctx context.Context) ([]core.Proxy, error) {
	client, err := a.activeClient()
	if err != nil {
		return nil, err
	}
	return client.Proxies(ctx)
}

// SelectProxy switches a group.
func (a *Adapter) SelectProxy(ctx context.Context, group, name string) error {
	client, err := a.activeClient()
	if err != nil {
		return err
	}
	return client.Select(ctx, group, name)
}

// TestDelay measures latency for one proxy.
func (a *Adapter) TestDelay(ctx context.Context, name string, timeout time.Duration) (int, error) {
	client, err := a.activeClient()
	if err != nil {
		return 0, err
	}
	return client.TestDelay(ctx, name, timeout)
}

// Connections lists live connections.
func (a *Adapter) Connections(ctx context.Context) ([]core.Connection, error) {
	client, err := a.activeClient()
	if err != nil {
		return nil, err
	}
	connections, _, err := client.Connections(ctx)
	return connections, err
}

// Traffic reports live throughput and cumulative totals.
func (a *Adapter) Traffic(ctx context.Context) (core.Traffic, error) {
	client, err := a.activeClient()
	if err != nil {
		return core.Traffic{}, err
	}
	_, traffic, err := client.Connections(ctx)
	if err != nil {
		return core.Traffic{}, err
	}
	a.trafficMu.Lock()
	traffic.UpRate = a.upRate
	traffic.DownRate = a.downRate
	a.trafficMu.Unlock()
	return traffic, nil
}
