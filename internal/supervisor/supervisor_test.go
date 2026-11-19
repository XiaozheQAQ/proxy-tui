package supervisor

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
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

type fakeFacade struct {
	active int32
	max    int32
	calls  int32
}

func (f *fakeFacade) CoreRestart(ctx context.Context) error {
	atomic.AddInt32(&f.calls, 1)
	current := atomic.AddInt32(&f.active, 1)
	for {
		observed := atomic.LoadInt32(&f.max)
		if current <= observed || atomic.CompareAndSwapInt32(&f.max, observed, current) {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	atomic.AddInt32(&f.active, -1)
	return nil
}

type fakeBackend struct {
	mu       sync.Mutex
	statuses map[model.CoreKind]core.Status
	starts   int
	stops    int
	tunOn    bool
	backup   bool
	facade   any
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
func (b *fakeBackend) Callable() any                { return b.facade }

func testPaths(t *testing.T) storage.Paths {
	t.Helper()
	// Windows AF_UNIX paths are short; t.TempDir() includes the test name and can
	// exceed the limit for long test names.
	dir, err := os.MkdirTemp("", "pt-sup-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
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

func TestFacadeCallsAreSerialized(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	facade := &fakeFacade{}
	backend.facade = facade
	endpoint, cancel, done := startServer(t, paths, backend)
	defer func() {
		cancel()
		<-done
	}()
	const clients = 6
	results := make(chan error, clients)
	for i := 0; i < clients; i++ {
		go func() {
			c, err := ipc.Dial(context.Background(), endpoint, time.Second)
			if err != nil {
				results <- err
				return
			}
			defer c.Close()
			results <- c.Call(context.Background(), ipc.OpAppCall, map[string]any{"method": "CoreRestart"}, nil)
		}()
	}
	for i := 0; i < clients; i++ {
		if err := <-results; err != nil {
			t.Fatalf("call: %v", err)
		}
	}
	if got := atomic.LoadInt32(&facade.max); got != 1 {
		t.Fatalf("concurrent facade mutations observed: max=%d", got)
	}
}

func TestOperationIDDeduplicates(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	facade := &fakeFacade{}
	backend.facade = facade
	endpoint, cancel, done := startServer(t, paths, backend)
	defer func() {
		cancel()
		<-done
	}()
	client, err := ipc.Dial(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	call := func(opID string) {
		t.Helper()
		payload := map[string]any{"method": "CoreRestart"}
		if err := client.CallWithOperationID(context.Background(), ipc.OpAppCall, payload, nil, opID); err != nil {
			t.Fatalf("call %s: %v", opID, err)
		}
	}
	call("op-1")
	call("op-1")
	if got := atomic.LoadInt32(&facade.calls); got != 1 {
		t.Fatalf("duplicate operation executed %d times", got)
	}
	var status OperationStatusPayload
	if err := client.Call(context.Background(), ipc.OpOperationGet, map[string]string{"operation_id": "op-1"}, &status); err != nil {
		t.Fatalf("operation.get: %v", err)
	}
	if status.Status != "SUCCEEDED" {
		t.Fatalf("operation status = %q", status.Status)
	}
	call("op-2")
	if got := atomic.LoadInt32(&facade.calls); got != 2 {
		t.Fatalf("distinct operation executed %d times", got)
	}
}

func TestFacadeRejectsUnknownMethod(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	backend.facade = &fakeFacade{}
	endpoint, cancel, done := startServer(t, paths, backend)
	defer func() {
		cancel()
		<-done
	}()
	client, err := ipc.Dial(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	err = client.Call(context.Background(), ipc.OpAppCall, map[string]any{"method": "Shell"}, nil)
	if err == nil {
		t.Fatal("unknown method must fail")
	}
	if rpcErr, ok := err.(*ipc.RPCError); !ok || rpcErr.Code != ipc.CodeInternal {
		t.Fatalf("error = %v", err)
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
