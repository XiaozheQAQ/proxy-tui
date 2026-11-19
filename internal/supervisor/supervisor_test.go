package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

type fakeBackend struct {
	mu       sync.Mutex
	statuses map[model.CoreKind]core.Status
	starts   int
	stops    int
	tunOn    bool
	backup   bool
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{statuses: map[model.CoreKind]core.Status{
		model.CoreMihomo:  {Core: model.CoreMihomo, Installed: true},
		model.CoreSingBox: {Core: model.CoreSingBox, Installed: true},
	}}
}

func (b *fakeBackend) Statuses(context.Context) map[model.CoreKind]core.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[model.CoreKind]core.Status{}
	for kind, status := range b.statuses {
		out[kind] = status
	}
	return out
}

func (b *fakeBackend) StartCore(_ context.Context, kind model.CoreKind) error {
	b.mu.Lock()
	b.starts++
	b.statuses[kind] = core.Status{Core: kind, Installed: true, Running: true, PID: 4242}
	b.mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	return nil
}

func (b *fakeBackend) StopCore(_ context.Context, kind model.CoreKind) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
	status := b.statuses[kind]
	status.Running = false
	status.PID = 0
	b.statuses[kind] = status
	return nil
}

func (b *fakeBackend) RestartCore(ctx context.Context, kind model.CoreKind) error {
	if err := b.StopCore(ctx, kind); err != nil {
		return err
	}
	return b.StartCore(ctx, kind)
}

func (b *fakeBackend) Proxies(context.Context, model.CoreKind) ([]core.Proxy, error) {
	return []core.Proxy{{Name: "PROXY", Type: "Selector", Group: true, Now: "A", All: []string{"A", "B"}}}, nil
}

func (b *fakeBackend) SelectProxy(context.Context, model.CoreKind, string, string) error { return nil }

func (b *fakeBackend) NetworkReport(context.Context) (network.Report, error) {
	return network.Report{BackupActive: b.backup}, nil
}

func (b *fakeBackend) TUNStatus(context.Context) (tun.Status, error) {
	return tun.Status{Enabled: b.tunOn}, nil
}

func (b *fakeBackend) LogsTail(int) []logging.Entry { return nil }

func testPaths(t *testing.T) storage.Paths {
	t.Helper()
	dir := t.TempDir()
	paths := storage.Paths{
		Config: dir + "/config",
		Data:   dir + "/data",
		State:  dir + "/state",
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return paths
}

func startServer(t *testing.T, paths storage.Paths, backend Backend) (string, context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, paths, "test", backend, nil) }()
	endpoint := ipc.EndpointPath(paths.RuntimeDir())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := ipc.Dial(ctx, endpoint, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return endpoint, cancel, done
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	t.Fatal("supervisor did not start")
	return "", nil, nil
}

func TestSingleInstance(t *testing.T) {
	paths := testPaths(t)
	_, cancel, done := startServer(t, paths, newFakeBackend())
	defer func() {
		cancel()
		<-done
	}()
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- Serve(context.Background(), paths, "test", newFakeBackend(), nil)
	}()
	select {
	case err := <-secondDone:
		if err == nil {
			t.Fatal("second supervisor must fail")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second supervisor did not fail promptly")
	}
}

func TestConcurrentStartOnlyOnce(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	endpoint, cancel, done := startServer(t, paths, backend)
	defer func() {
		cancel()
		<-done
	}()
	client := func() *ipc.Client {
		c, err := ipc.Dial(context.Background(), endpoint, time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}
	const clients = 8
	results := make(chan error, clients)
	for i := 0; i < clients; i++ {
		go func() {
			c := client()
			defer c.Close()
			var action CoreAction
			results <- c.Call(context.Background(), ipc.OpCoreStart, map[string]string{"core": "mihomo"}, &action)
		}()
	}
	success := 0
	already := 0
	for i := 0; i < clients; i++ {
		err := <-results
		if err == nil {
			success++
			continue
		}
		if rpcErr, ok := err.(*ipc.RPCError); ok && rpcErr.Code == ipc.CodeCoreAlreadyRunning {
			already++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || already != clients-1 {
		t.Fatalf("success=%d already=%d", success, already)
	}
	backend.mu.Lock()
	starts := backend.starts
	backend.mu.Unlock()
	if starts != 1 {
		t.Fatalf("StartCore called %d times", starts)
	}
}

func TestStopGuards(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	endpoint, cancel, done := startServer(t, paths, backend)
	defer cancel()
	client, err := ipc.Dial(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var action CoreAction
	if err := client.Call(context.Background(), ipc.OpCoreStart, map[string]string{"core": "mihomo"}, &action); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := client.Call(context.Background(), ipc.OpSupervisorStop, nil, nil); err == nil {
		t.Fatal("stop must be refused with a running core")
	} else if rpcErr, ok := err.(*ipc.RPCError); !ok || rpcErr.Code != ipc.CodeNetworkBlocked {
		t.Fatalf("error = %v", err)
	}
	if err := client.Call(context.Background(), ipc.OpCoreStop, map[string]string{"core": "mihomo"}, &action); err != nil {
		t.Fatalf("stop core: %v", err)
	}
	backend.mu.Lock()
	backend.tunOn = true
	backend.mu.Unlock()
	if err := client.Call(context.Background(), ipc.OpSupervisorStop, nil, nil); err == nil {
		t.Fatal("stop must be refused with TUN enabled")
	}
	backend.mu.Lock()
	backend.tunOn = false
	backend.mu.Unlock()
	if err := client.Call(context.Background(), ipc.OpSupervisorStop, nil, nil); err != nil {
		t.Fatalf("final stop: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestVersionMismatch(t *testing.T) {
	paths := testPaths(t)
	_, cancel, done := startServer(t, paths, newFakeBackend())
	defer func() {
		cancel()
		<-done
	}()
	conn, err := ipc.DialConn(context.Background(), ipc.EndpointPath(paths.RuntimeDir()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := ipc.WriteMessage(conn, ipc.Request{Version: 999, RequestID: "x", Operation: ipc.OpPing}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var response ipc.Response
	if err := ipc.ReadMessage(conn, &response); err != nil {
		t.Fatalf("read: %v", err)
	}
	if response.Success || response.Error == nil || response.Error.Code != ipc.CodeVersionMismatch {
		t.Fatalf("response = %+v", response)
	}
}
