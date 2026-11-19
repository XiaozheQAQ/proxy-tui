package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// AppCallPayload is the generic frontend invocation envelope.
type AppCallPayload struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args,omitempty"`
}

type remote struct {
	mu       sync.Mutex
	client   *ipc.Client
	endpoint string
	sequence atomic.Uint64
}

var reconnectDelays = []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second}

// NewRemote builds a frontend backed by the supervisor IPC connection.
func NewRemote(ctx context.Context, client *ipc.Client) (*Frontend, error) {
	r := &remote{client: client}
	var info ConfigInfo
	if err := r.invoke(ctx, "ConfigInfo", nil, &info); err != nil {
		return nil, err
	}
	endpoint, err := ipc.EndpointPath(info.Paths.RuntimeDir())
	if err != nil {
		return nil, err
	}
	r.endpoint = endpoint
	frontend := &Frontend{
		Cores:    &remoteCores{r: r},
		Subs:     &remoteSubs{r: r},
		TUN:      &remoteTUN{r: r},
		Logs:     &remoteLogs{r: r},
		Paths:    info.Paths,
		Settings: info.Settings,
		api:      &remoteAPI{r: r},
	}
	return frontend, nil
}

func (r *remote) invoke(ctx context.Context, method string, args []any, out any) error {
	rawArgs := make([]json.RawMessage, 0, len(args))
	for _, arg := range args {
		data, err := json.Marshal(arg)
		if err != nil {
			return err
		}
		rawArgs = append(rawArgs, data)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	operationID := ""
	if isRemoteMutation(method) {
		operationID = fmt.Sprintf("op-%d-%d", time.Now().UnixNano(), r.sequence.Add(1))
	}
	var lastErr error
	for attempt := 0; attempt <= len(reconnectDelays); attempt++ {
		if attempt > 0 {
			delay := reconnectDelays[attempt-1]
			if deadline, ok := ctx.Deadline(); ok && time.Now().Add(delay).After(deadline) {
				return lastErr
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			if err := r.reconnect(ctx); err != nil {
				lastErr = err
				continue
			}
		}
		var result json.RawMessage
		err := r.client.CallWithOperationID(ctx, ipc.OpAppCall, AppCallPayload{Method: method, Args: rawArgs}, &result, operationID)
		if err == nil {
			if out == nil || len(result) == 0 {
				return nil
			}
			return json.Unmarshal(result, out)
		}
		if _, definitive := err.(*ipc.RPCError); definitive {
			return err
		}
		lastErr = err
		_ = r.client.Close()
	}
	return lastErr
}

func (r *remote) reconnect(ctx context.Context) error {
	if r.endpoint == "" {
		return fmt.Errorf("supervisor endpoint unknown")
	}
	client, err := ipc.Dial(ctx, r.endpoint, 30*time.Second)
	if err != nil {
		return err
	}
	r.client = client
	return nil
}

func isRemoteMutation(method string) bool {
	switch method {
	case "CoreInstall", "CoreStart", "CoreStop", "CoreRestart", "CoreSelect",
		"SubsSetActive", "SubsDelete", "SubsUpdate", "SubsUpdateAll", "SubsSetDefaultInterval",
		"UpdateSettings", "LogsClear", "Import", "ApplyTrafficMode", "RecoverTUN",
		"RecoverNetwork", "TUNDisable":
		return true
	}
	return false
}

func callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

type remoteCores struct{ r *remote }

func (c *remoteCores) Kinds() []model.CoreKind {
	ctx, cancel := callContext()
	defer cancel()
	var out []model.CoreKind
	_ = c.r.invoke(ctx, "CoreKinds", nil, &out)
	return out
}
func (c *remoteCores) Status(ctx context.Context, kind model.CoreKind) core.Status {
	var out core.Status
	_ = c.r.invoke(ctx, "CoreStatus", []any{kind}, &out)
	return out
}
func (c *remoteCores) Install(ctx context.Context, kind model.CoreKind, progress func(string)) (core.InstallResult, error) {
	var out core.InstallResult
	err := c.r.invoke(ctx, "CoreInstall", []any{kind}, &out)
	return out, err
}
func (c *remoteCores) Start(ctx context.Context, kind model.CoreKind, _ core.StartOptions) error {
	return c.r.invoke(ctx, "CoreStart", []any{kind}, nil)
}
func (c *remoteCores) Stop(ctx context.Context, kind model.CoreKind) error {
	return c.r.invoke(ctx, "CoreStop", []any{kind}, nil)
}
func (c *remoteCores) Proxies(ctx context.Context, kind model.CoreKind) ([]core.Proxy, error) {
	var out []core.Proxy
	err := c.r.invoke(ctx, "CoreProxies", []any{kind}, &out)
	return out, err
}
func (c *remoteCores) SelectProxy(ctx context.Context, kind model.CoreKind, group, name string) error {
	return c.r.invoke(ctx, "CoreSelect", []any{kind, group, name}, nil)
}
func (c *remoteCores) TestDelay(ctx context.Context, kind model.CoreKind, name string, timeout time.Duration) (int, error) {
	var out int
	err := c.r.invoke(ctx, "CoreTestDelay", []any{kind, name, timeout}, &out)
	return out, err
}
func (c *remoteCores) Connections(ctx context.Context, kind model.CoreKind) ([]core.Connection, error) {
	var out []core.Connection
	err := c.r.invoke(ctx, "CoreConnections", []any{kind}, &out)
	return out, err
}
func (c *remoteCores) Traffic(ctx context.Context, kind model.CoreKind) (core.Traffic, error) {
	var out core.Traffic
	err := c.r.invoke(ctx, "CoreTraffic", []any{kind}, &out)
	return out, err
}

type remoteSubs struct{ r *remote }

func (s *remoteSubs) List() []model.Profile {
	ctx, cancel := callContext()
	defer cancel()
	var out []model.Profile
	_ = s.r.invoke(ctx, "SubsList", nil, &out)
	return out
}
func (s *remoteSubs) Get(id string) (model.Profile, bool) {
	ctx, cancel := callContext()
	defer cancel()
	var out ProfileLookup
	_ = s.r.invoke(ctx, "SubsGetLookup", []any{id}, &out)
	return out.Profile, out.Found
}
func (s *remoteSubs) ActiveFor(kind model.CoreKind) (model.Profile, bool) {
	ctx, cancel := callContext()
	defer cancel()
	var out ProfileLookup
	_ = s.r.invoke(ctx, "SubsActiveLookup", []any{kind}, &out)
	return out.Profile, out.Found
}
func (s *remoteSubs) ActiveConfig(id string) ([]byte, string, error) {
	ctx, cancel := callContext()
	defer cancel()
	var out ActiveConfigInfo
	err := s.r.invoke(ctx, "SubsActiveConfigInfo", []any{id}, &out)
	return out.Data, out.Format, err
}
func (s *remoteSubs) SetActive(id string) (model.Profile, error) {
	ctx, cancel := callContext()
	defer cancel()
	var out model.Profile
	err := s.r.invoke(ctx, "SubsSetActive", []any{id}, &out)
	return out, err
}
func (s *remoteSubs) Delete(id string) error {
	ctx, cancel := callContext()
	defer cancel()
	return s.r.invoke(ctx, "SubsDelete", []any{id}, nil)
}
func (s *remoteSubs) Update(ctx context.Context, id string) (model.Profile, error) {
	var out model.Profile
	err := s.r.invoke(ctx, "SubsUpdate", []any{id}, &out)
	return out, err
}
func (s *remoteSubs) UpdateAll(ctx context.Context) []subscription.UpdateOutcome {
	var out []subscription.UpdateOutcome
	_ = s.r.invoke(ctx, "SubsUpdateAll", nil, &out)
	return out
}
func (s *remoteSubs) Due(now time.Time) []model.Profile {
	ctx, cancel := callContext()
	defer cancel()
	var out []model.Profile
	_ = s.r.invoke(ctx, "SubsDue", []any{now}, &out)
	return out
}
func (s *remoteSubs) SetDefaultInterval(seconds int) {
	ctx, cancel := callContext()
	defer cancel()
	_ = s.r.invoke(ctx, "SubsSetDefaultInterval", []any{seconds}, nil)
}
func (s *remoteSubs) Snapshots(id string) ([]model.Snapshot, error) {
	ctx, cancel := callContext()
	defer cancel()
	var out []model.Snapshot
	err := s.r.invoke(ctx, "SubsSnapshots", []any{id}, &out)
	return out, err
}

type remoteTUN struct{ r *remote }

func (t *remoteTUN) Disable(ctx context.Context) (tun.DisableResult, error) {
	var out tun.DisableResult
	err := t.r.invoke(ctx, "TUNDisable", nil, &out)
	return out, err
}

type remoteLogs struct{ r *remote }

func (l *remoteLogs) Tail(n int) []logging.Entry {
	ctx, cancel := callContext()
	defer cancel()
	var out []logging.Entry
	_ = l.r.invoke(ctx, "LogsTail", []any{n}, &out)
	return out
}
func (l *remoteLogs) Clear() {
	ctx, cancel := callContext()
	defer cancel()
	_ = l.r.invoke(ctx, "LogsClear", nil, nil)
}

type remoteAPI struct{ r *remote }

func (a *remoteAPI) StartCore(ctx context.Context, kind model.CoreKind) error {
	return a.r.invoke(ctx, "CoreStart", []any{kind}, nil)
}
func (a *remoteAPI) StopCore(ctx context.Context, kind model.CoreKind) error {
	return a.r.invoke(ctx, "CoreStop", []any{kind}, nil)
}
func (a *remoteAPI) RestartCore(ctx context.Context, kind model.CoreKind) error {
	return a.r.invoke(ctx, "CoreRestart", []any{kind}, nil)
}
func (a *remoteAPI) Import(ctx context.Context, req subscription.ImportRequest) ([]subscription.ImportOutcome, error) {
	var out []subscription.ImportOutcome
	err := a.r.invoke(ctx, "Import", []any{req}, &out)
	return out, err
}
func (a *remoteAPI) DefaultTunCore(ctx context.Context) model.CoreKind {
	var out model.CoreKind
	_ = a.r.invoke(ctx, "DefaultTunCore", nil, &out)
	return out
}
func (a *remoteAPI) ApplyTrafficMode(ctx context.Context, mode network.Mode, kind model.CoreKind, settings tun.Settings) (TrafficResult, error) {
	var out TrafficResult
	err := a.r.invoke(ctx, "ApplyTrafficMode", []any{mode, kind, settings}, &out)
	return out, err
}
func (a *remoteAPI) NetworkReport(ctx context.Context) (network.Report, error) {
	var out network.Report
	err := a.r.invoke(ctx, "NetworkReport", nil, &out)
	return out, err
}
func (a *remoteAPI) TUNStatus(ctx context.Context) (tun.Status, error) {
	var out tun.Status
	err := a.r.invoke(ctx, "TUNStatus", nil, &out)
	return out, err
}
func (a *remoteAPI) TUNPreflight(ctx context.Context, kind model.CoreKind, settings tun.Settings) (tun.Report, error) {
	var out tun.Report
	err := a.r.invoke(ctx, "TUNPreflight", []any{kind, settings}, &out)
	return out, err
}
func (a *remoteAPI) RecoverTUN(ctx context.Context) (tun.RecoveryResult, error) {
	var out tun.RecoveryResult
	err := a.r.invoke(ctx, "RecoverTUN", nil, &out)
	return out, err
}
func (a *remoteAPI) RecoverNetwork(ctx context.Context) (network.RecoveryResult, error) {
	var out network.RecoveryResult
	err := a.r.invoke(ctx, "RecoverNetwork", nil, &out)
	return out, err
}
func (a *remoteAPI) Doctor(ctx context.Context) []string {
	var out []string
	_ = a.r.invoke(ctx, "Doctor", nil, &out)
	return out
}
func (a *remoteAPI) TunPlatform() string {
	var out string
	_ = a.r.invoke(context.Background(), "TunPlatform", nil, &out)
	return out
}
func (a *remoteAPI) OwnProxyAddress() string {
	var out string
	_ = a.r.invoke(context.Background(), "OwnProxyAddress", nil, &out)
	return out
}
func (a *remoteAPI) VerifyProxyConnectivity(ctx context.Context) ProxyConnectivity {
	var out ProxyConnectivity
	_ = a.r.invoke(ctx, "VerifyProxyConnectivity", nil, &out)
	return out
}
func (a *remoteAPI) Health(ctx context.Context) error {
	return a.r.invoke(ctx, "Ping", nil, nil)
}
func (a *remoteAPI) SettingsSnapshot(ctx context.Context) storage.Settings {
	var out storage.Settings
	_ = a.r.invoke(ctx, "SettingsSnapshot", nil, &out)
	return out
}
func (a *remoteAPI) UpdateSettings(ctx context.Context, settings storage.Settings) error {
	return a.r.invoke(ctx, "UpdateSettings", []any{settings}, nil)
}
func (a *remoteAPI) CoreBinaryPath(kind model.CoreKind) string {
	var out string
	_ = a.r.invoke(context.Background(), "CoreBinaryPath", []any{kind}, &out)
	return out
}

var _ = fmt.Sprintf
var _ = storage.Paths{}
