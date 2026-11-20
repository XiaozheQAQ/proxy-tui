// Package singbox adapts the sing-box core.
package singbox

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

// Adapter implements core.Core for sing-box.
type Adapter struct {
	paths storage.Paths
	opts  core.RuntimeOptions
	logf  func(level, msg string)
	proc  *core.Process

	mu         sync.Mutex
	client     *clashapi.Client
	cancel     context.CancelFunc
	configPath string

	trafficMu sync.Mutex
	upRate    int64
	downRate  int64
}

// New builds a sing-box adapter.
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
func (a *Adapter) Kind() model.CoreKind { return model.CoreSingBox }

// Capabilities reports the sing-box feature set. Proxy and connection data is
// only available through the injected Clash compatible API.
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

// Status separates process identity from API availability. A verified
// process whose API is temporarily unreachable is DEGRADED, not deleted and
// not reported as STOPPED. Only a genuinely gone process clears the state.
func (a *Adapter) Status(ctx context.Context) core.Status {
	status := core.Status{Core: model.CoreSingBox}
	if version, binary, ok := core.NewLayout(a.paths, model.CoreSingBox).Installed(); ok {
		status.Installed = true
		status.Version = version
		status.BinaryPath = binary
	}
	if a.proc.Running() {
		a.mu.Lock()
		configPath := a.configPath
		a.mu.Unlock()
		status.Running = true
		status.PID = a.proc.PID()
		status.StartedAt = a.proc.StartedAt()
		status.ConfigPath = configPath
		status.APIAddr = a.proc.APIAddr()
		status.LastError = a.proc.LastError()
		return status
	}
	state, ok := core.ReadRuntimeState(a.paths, model.CoreSingBox)
	if !ok {
		status.LastError = a.proc.LastError()
		return status
	}
	_, verdict, verifyErr := core.VerifyRuntimeState(state, false)
	if verdict == core.ProcessGone {
		_ = core.RemoveRuntimeState(a.paths, model.CoreSingBox)
		status.LastError = "core process exited"
		return status
	}
	status.PID = state.PID
	status.StartedAt = state.ProcessStartedAt
	status.APIAddr = state.APIAddr
	status.ConfigPath = state.ConfigPath
	if verdict != core.IdentityVerified || verifyErr != nil {
		status.Degraded = true
		if verifyErr != nil {
			status.LastError = "core identity unavailable: " + verifyErr.Error()
		} else {
			status.LastError = "core identity cannot be verified (" + string(verdict) + "); refusing to manage this process"
		}
		return status
	}
	status.Running = true
	client := clashapi.New(state.APIAddr, state.Secret)
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	_, apiErr := client.Version(probeCtx)
	cancel()
	if apiErr != nil {
		status.Degraded = true
		status.LastError = "core API temporarily unavailable: " + apiErr.Error()
		return status
	}
	a.adopt(client)
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

// Install downloads the newest sing-box release.
func (a *Adapter) Install(ctx context.Context, progress func(string)) (core.InstallResult, error) {
	result, err := core.Install(ctx, a.paths, model.CoreSingBox, core.SingBoxRepo, nil, progress)
	if err != nil {
		return result, err
	}
	a.logf("info", "installed sing-box "+result.Version)
	return result, nil
}

// ValidateConfig runs "sing-box check".
func (a *Adapter) ValidateConfig(ctx context.Context, name string, data []byte) error {
	_, binary, ok := core.NewLayout(a.paths, model.CoreSingBox).Installed()
	if !ok {
		return core.ErrNotInstalled
	}
	runtimeConfig, err := core.PrepareSingBoxConfig(data, core.RuntimeOptions{
		ProxyPort: a.opts.ProxyPort,
		APIPort:   a.opts.APIPort,
		Secret:    "validation-only",
		LogLevel:  a.opts.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrInvalidConfig, err)
	}
	dir := filepath.Join(a.paths.RuntimeDir(), "sing-box-validate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	if err := storage.WriteFileAtomic(path, runtimeConfig, 0o600); err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(path)
		_ = os.Remove(dir)
	}()

	runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binary, "check", "-c", path)
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

// Start writes the runtime config and launches sing-box.
func (a *Adapter) Start(ctx context.Context, opts core.StartOptions) error {
	if a.proc.Running() {
		return core.ErrAlreadyRunning
	}
	// A core started by a previous process also counts as running.
	if status := a.Status(ctx); status.Running {
		return core.ErrAlreadyRunning
	}
	if state, ok := core.ReadRuntimeState(a.paths, model.CoreSingBox); ok {
		_, verdict, _ := core.VerifyRuntimeState(state, false)
		if verdict != core.ProcessGone {
			return fmt.Errorf("%w: existing runtime state for PID %d (%s); run 'proxy core stop' or inspect %s",
				core.ErrCoreIdentityMismatch, state.PID, verdict, core.StatePath(a.paths, model.CoreSingBox))
		}
		_ = core.RemoveRuntimeState(a.paths, model.CoreSingBox)
	}
	version, binary, ok := core.NewLayout(a.paths, model.CoreSingBox).Installed()
	if !ok {
		return core.ErrNotInstalled
	}
	secret := core.RandomSecret()
	runtimeConfig, err := core.PrepareSingBoxConfig(opts.Config, core.RuntimeOptions{
		ProxyPort: a.opts.ProxyPort,
		APIPort:   a.opts.APIPort,
		Secret:    secret,
		LogLevel:  a.opts.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("prepare sing-box config: %w", err)
	}
	dir := filepath.Join(a.paths.RuntimeDir(), "sing-box")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := storage.WriteFileAtomic(configPath, runtimeConfig, 0o600); err != nil {
		return err
	}
	apiAddr := fmt.Sprintf("127.0.0.1:%d", a.opts.APIPort)
	if err := a.proc.Start(binary, []string{"run", "-c", configPath}, nil, dir, apiAddr); err != nil {
		return err
	}
	client := clashapi.New(apiAddr, secret)
	a.mu.Lock()
	a.client = client
	a.mu.Unlock()

	a.mu.Lock()
	a.configPath = configPath
	a.mu.Unlock()
	if err := a.waitForAPI(ctx, client); err != nil {
		_ = a.proc.Stop(3 * time.Second)
		a.mu.Lock()
		a.client = nil
		a.mu.Unlock()
		return err
	}
	identity, identityErr := core.ReadProcessIdentity(a.proc.PID())
	if identityErr != nil {
		_ = a.proc.Stop(3 * time.Second)
		return fmt.Errorf("read core process identity: %w", identityErr)
	}
	binarySHA := core.HashFile(identity.Executable)
	if binarySHA == "" {
		_ = a.proc.Stop(3 * time.Second)
		return fmt.Errorf("hash core binary %s: unreadable", identity.Executable)
	}
	configSHA := core.HashBytes(runtimeConfig)
	a.startTrafficWatch(ctx, client)
	state := core.RuntimeState{
		Core:             model.CoreSingBox,
		PID:              a.proc.PID(),
		APIAddr:          apiAddr,
		Secret:           secret,
		ProxyPort:        a.opts.ProxyPort,
		ConfigPath:       configPath,
		Version:          version,
		StartedAt:        a.proc.StartedAt(),
		ProcessStartedAt: identity.StartedAt,
		BinaryPath:       identity.Executable,
		BinarySHA256:     binarySHA,
		ConfigSHA256:     configSHA,
	}
	if err := core.WriteRuntimeState(a.paths, state); err != nil {
		a.logf("warn", "persist runtime state: "+err.Error())
	}
	a.logf("info", fmt.Sprintf("sing-box started, proxy on 127.0.0.1:%d, API on %s", a.opts.ProxyPort, apiAddr))
	return nil
}

func (a *Adapter) waitForAPI(ctx context.Context, client *clashapi.Client) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := client.Version(probeCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sing-box Clash API did not become ready (experimental.clash_api must be enabled): %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
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

// Stop terminates the core and the traffic stream. For an adopted process it
// verifies PID start time, executable, binary hash and config hash before any
// kill. Unverified or mismatched state is never killed and the state file is
// kept for manual inspection.
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
		if err := a.proc.Stop(8 * time.Second); err != nil {
			// Keep the state if the owned child could not be stopped.
			return err
		}
		return core.RemoveRuntimeState(a.paths, model.CoreSingBox)
	}
	state, ok := core.ReadRuntimeState(a.paths, model.CoreSingBox)
	if !ok {
		return nil
	}
	_, verdict, verifyErr := core.VerifyRuntimeState(state, true)
	if verifyErr != nil {
		return fmt.Errorf("verify core identity before stop: %w", verifyErr)
	}
	switch verdict {
	case core.ProcessGone:
		return core.RemoveRuntimeState(a.paths, model.CoreSingBox)
	case core.IdentityUnverified, core.IdentityMismatch:
		return fmt.Errorf("%w: refusing to kill PID %d (%s); state kept at %s",
			core.ErrCoreIdentityMismatch, state.PID, verdict, core.StatePath(a.paths, model.CoreSingBox))
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	_, apiErr := clashapi.New(state.APIAddr, state.Secret).Version(probeCtx)
	cancelProbe()
	if apiErr != nil && (strings.Contains(apiErr.Error(), "HTTP 401") || strings.Contains(apiErr.Error(), "HTTP 403")) {
		return fmt.Errorf("%w: core API rejected the stored secret for PID %d", core.ErrCoreIdentityMismatch, state.PID)
	}
	if _, again, err := core.VerifyRuntimeState(state, false); err != nil || again != core.IdentityVerified {
		return fmt.Errorf("%w: core identity changed while stopping PID %d", core.ErrCoreIdentityMismatch, state.PID)
	}
	if err := core.KillPID(state.PID); err != nil {
		return err
	}
	if !core.WaitProcessExit(state.PID, 5*time.Second) {
		return fmt.Errorf("core PID %d did not exit; runtime state kept at %s", state.PID, core.StatePath(a.paths, model.CoreSingBox))
	}
	return core.RemoveRuntimeState(a.paths, model.CoreSingBox)
}

func (a *Adapter) activeClient() (*clashapi.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, core.ErrNotRunning
	}
	return a.client, nil
}

// Proxies lists outbound groups through the Clash API.
func (a *Adapter) Proxies(ctx context.Context) ([]core.Proxy, error) {
	client, err := a.activeClient()
	if err != nil {
		return nil, err
	}
	return client.Proxies(ctx)
}

// SelectProxy switches a selector group.
func (a *Adapter) SelectProxy(ctx context.Context, group, name string) error {
	client, err := a.activeClient()
	if err != nil {
		return err
	}
	return client.Select(ctx, group, name)
}

// TestDelay measures latency for one outbound.
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
