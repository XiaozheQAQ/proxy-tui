package supervisor

import (
	"context"
	"errors"
	"fmt"
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
	// Keep temporary paths short; some Windows IPC names and test directories
	// have practical length limits.
	dir, err := os.MkdirTemp("", "pt-sup-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// Isolate the Windows Named Pipe from a real user supervisor while still
	// exercising the production endpoint code.
	t.Setenv("PROXY_TUI_HOME", dir)
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
	endpoint, err := ipc.EndpointPath(paths.RuntimeDir())
	if err != nil {
		cancel()
		t.Fatalf("endpoint: %v", err)
	}
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

func TestStopIntentPersistedBeforeShutdown(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
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
	if err := client.Call(context.Background(), ipc.OpSupervisorStop, nil, nil); err != nil {
		t.Fatalf("stop: %v", err)
	}
	record, err := LoadLifecycle(paths)
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	if record == nil || record.Status != LifecycleStopped {
		t.Fatalf("stop intent was not persisted before shutdown: %+v", record)
	}
	if record.InstanceID == "" || record.Version != 1 || record.Source != "supervisor.stop" {
		t.Fatalf("lifecycle metadata incomplete: %+v", record)
	}
}

func TestStopRefusedWhenIntentCannotPersist(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := NewHandler(backend, "endpoint", "test", paths.LogsDir()+"/supervisor.log", cancel, func() error {
		return errors.New("disk full")
	})
	request := ipc.Request{Version: ipc.Version, RequestID: "req-stop", Operation: ipc.OpSupervisorStop}
	response := h.handle(ctx, request)
	if response.Success {
		t.Fatal("stop must fail when the stop intent cannot be persisted")
	}
	if h.Stopped() {
		t.Fatal("handler must not mark itself stopped after a failed persist")
	}
}

func TestVersionMismatch(t *testing.T) {
	paths := testPaths(t)
	_, cancel, done := startServer(t, paths, newFakeBackend())
	defer func() {
		cancel()
		<-done
	}()
	endpoint, err := ipc.EndpointPath(paths.RuntimeDir())
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	conn, err := ipc.DialConn(context.Background(), endpoint)
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

func TestConcurrentOperationIDExecutesOnce(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	facade := &fakeFacade{}
	backend.facade = facade
	endpoint, cancel, done := startServer(t, paths, backend)
	defer func() {
		cancel()
		<-done
	}()
	const clients = 32
	results := make(chan error, clients)
	for i := 0; i < clients; i++ {
		go func() {
			client, err := ipc.Dial(context.Background(), endpoint, time.Second)
			if err != nil {
				results <- err
				return
			}
			defer client.Close()
			results <- client.CallWithOperationID(context.Background(), ipc.OpAppCall,
				map[string]any{"method": "CoreRestart"}, nil, "same-operation")
		}()
	}
	successes := 0
	for i := 0; i < clients; i++ {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		if rpcErr, ok := err.(*ipc.RPCError); ok && rpcErr.Code == ipc.CodeOperationInProgress {
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if successes == 0 {
		t.Fatal("no caller observed the completed operation")
	}
	if got := atomic.LoadInt32(&facade.calls); got != 1 {
		t.Fatalf("operation executed %d times, want 1", got)
	}
}

func TestOperationIDFingerprintConflict(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
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
	if err := client.CallWithOperationID(context.Background(), ipc.OpCoreStart,
		map[string]string{"core": "mihomo"}, nil, "shared-id"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	err = client.CallWithOperationID(context.Background(), ipc.OpCoreStart,
		map[string]string{"core": "sing-box"}, nil, "shared-id")
	if err == nil {
		t.Fatal("reusing an operation id for a different payload must fail")
	}
	if rpcErr, ok := err.(*ipc.RPCError); !ok || rpcErr.Code != ipc.CodeBadRequest {
		t.Fatalf("error = %v, want BAD_REQUEST", err)
	}
	backend.mu.Lock()
	starts := backend.starts
	backend.mu.Unlock()
	if starts != 1 {
		t.Fatalf("Starts = %d, want 1", starts)
	}
}

type blockingFacade struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   int32
}

func (b *blockingFacade) CoreRestart(context.Context) error {
	atomic.AddInt32(&b.calls, 1)
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func TestOperationStatusRunningThenSucceeded(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	facade := &blockingFacade{entered: make(chan struct{}), release: make(chan struct{})}
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
	queryClient, err := ipc.Dial(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial query: %v", err)
	}
	defer queryClient.Close()
	result := make(chan error, 1)
	go func() {
		result <- client.CallWithOperationID(context.Background(), ipc.OpAppCall,
			map[string]any{"method": "CoreRestart"}, nil, "op-running")
	}()
	select {
	case <-facade.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not start")
	}
	var running OperationStatusPayload
	if err := queryClient.Call(context.Background(), ipc.OpOperationGet,
		map[string]string{"operation_id": "op-running"}, &running); err != nil {
		t.Fatalf("operation.get: %v", err)
	}
	if running.Status != "RUNNING" {
		t.Fatalf("status = %q, want RUNNING", running.Status)
	}
	// A retry while RUNNING must not execute the operation again.
	err = queryClient.CallWithOperationID(context.Background(), ipc.OpAppCall,
		map[string]any{"method": "CoreRestart"}, nil, "op-running")
	if rpcErr, ok := err.(*ipc.RPCError); !ok || rpcErr.Code != ipc.CodeOperationInProgress {
		t.Fatalf("retry error = %v, want OPERATION_IN_PROGRESS", err)
	}
	close(facade.release)
	if err := <-result; err != nil {
		t.Fatalf("operation: %v", err)
	}
	var completed OperationStatusPayload
	if err := queryClient.Call(context.Background(), ipc.OpOperationGet,
		map[string]string{"operation_id": "op-running"}, &completed); err != nil {
		t.Fatalf("operation.get after completion: %v", err)
	}
	if completed.Status != "SUCCEEDED" {
		t.Fatalf("status = %q, want SUCCEEDED", completed.Status)
	}
	if got := atomic.LoadInt32(&facade.calls); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestOperationTerminalExpiry(t *testing.T) {
	oldTTL := operationCacheTTL
	operationCacheTTL = 25 * time.Millisecond
	defer func() { operationCacheTTL = oldTTL }()

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
	if err := client.CallWithOperationID(context.Background(), ipc.OpAppCall,
		map[string]any{"method": "CoreRestart"}, nil, "op-expire"); err != nil {
		t.Fatalf("call: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	var status OperationStatusPayload
	if err := client.Call(context.Background(), ipc.OpOperationGet,
		map[string]string{"operation_id": "op-expire"}, &status); err != nil {
		t.Fatalf("operation.get: %v", err)
	}
	if status.Status != "EXPIRED" {
		t.Fatalf("status = %q, want EXPIRED", status.Status)
	}
}

func TestOperationTimeoutClassification(t *testing.T) {
	backend := newFakeBackend()
	h := NewHandler(backend, "endpoint", "test", "log", func() {}, nil)
	if got := h.timeoutFor(ipc.Request{Operation: ipc.OpPing}); got != readOperationTimeout {
		t.Fatalf("read timeout = %s", got)
	}
	if got := h.timeoutFor(ipc.Request{Operation: ipc.OpCoreStart, Payload: []byte(`{"core":"mihomo"}`)}); got != mutationTimeout {
		t.Fatalf("mutation timeout = %s", got)
	}
}

func TestStopRejectsNewMutations(t *testing.T) {
	backend := newFakeBackend()
	h := NewHandler(backend, "endpoint", "test", "log", func() {}, nil)
	stop := h.handle(context.Background(), ipc.Request{Version: ipc.Version, RequestID: "stop", Operation: ipc.OpSupervisorStop})
	if !stop.Success {
		t.Fatalf("stop: %+v", stop)
	}
	start := h.handle(context.Background(), ipc.Request{Version: ipc.Version, RequestID: "start", Operation: ipc.OpCoreStart, Payload: []byte(`{"core":"mihomo"}`)})
	if start.Success {
		t.Fatal("mutation accepted while stopping")
	}
	if start.Error == nil || start.Error.Code != ipc.CodeNetworkBlocked {
		t.Fatalf("error = %+v, want NETWORK_BLOCKED", start.Error)
	}
}

func TestLifecycleOwnerCannotBeOverwritten(t *testing.T) {
	paths := testPaths(t)
	first := LifecycleRecord{Version: 1, InstanceID: "first", Status: LifecycleRunning, PID: 1}
	if err := SaveLifecycle(paths, first); err != nil {
		t.Fatalf("save: %v", err)
	}
	second := LifecycleRecord{Version: 1, InstanceID: "second", Status: LifecycleStopped, PID: 2}
	if err := SaveLifecycleOwner(paths, second); !errors.Is(err, ErrLifecycleOwnedByOther) {
		t.Fatalf("owner write error = %v", err)
	}
	record, err := LoadLifecycle(paths)
	if err != nil || record == nil || record.InstanceID != "first" {
		t.Fatalf("record = %+v, err=%v", record, err)
	}
}

func TestOperationCacheKeepsRunningEntryAndBoundsTerminal(t *testing.T) {
	backend := newFakeBackend()
	h := NewHandler(backend, "endpoint", "test", "log", func() {}, nil)
	running := ipc.Request{Version: ipc.Version, RequestID: "r", OperationID: "running", Operation: ipc.OpCoreStart, Payload: []byte(`{"core":"mihomo"}`)}
	if _, claimed, err := h.claimOperation(running, requestFingerprint(running)); err != nil || !claimed {
		t.Fatalf("claim running: claimed=%v err=%v", claimed, err)
	}
	for i := 0; i < operationCacheMax+40; i++ {
		request := ipc.Request{Version: ipc.Version, RequestID: "r", OperationID: fmt.Sprintf("op-%d", i), Operation: ipc.OpCoreStop, Payload: []byte(`{"core":"mihomo"}`)}
		fingerprint := requestFingerprint(request)
		if _, claimed, err := h.claimOperation(request, fingerprint); err != nil || !claimed {
			t.Fatalf("claim %d: claimed=%v err=%v", i, claimed, err)
		}
		h.finishOperation(request.OperationID, fingerprint, ipc.Response{Version: ipc.Version, RequestID: "r", Success: true})
	}
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	if len(h.cache) > operationCacheMax {
		t.Fatalf("cache size = %d, want <= %d", len(h.cache), operationCacheMax)
	}
	if entry, ok := h.cache["running"]; !ok || !entry.running {
		t.Fatal("RUNNING entry was evicted")
	}
}

func TestOperationStatusFailed(t *testing.T) {
	paths := testPaths(t)
	backend := newFakeBackend()
	backend.facade = &failingFacade{}
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
	err = client.CallWithOperationID(context.Background(), ipc.OpAppCall,
		map[string]any{"method": "CoreRestart"}, nil, "op-failed")
	if err == nil {
		t.Fatal("operation should fail")
	}
	var status OperationStatusPayload
	if err := client.Call(context.Background(), ipc.OpOperationGet,
		map[string]string{"operation_id": "op-failed"}, &status); err != nil {
		t.Fatalf("operation.get: %v", err)
	}
	if status.Status != "FAILED" || status.Error == nil {
		t.Fatalf("status = %+v", status)
	}
}

type failingFacade struct{}

func (f *failingFacade) CoreRestart(context.Context) error { return errors.New("boom") }

type panicBackend struct{ *fakeBackend }

func (p *panicBackend) Statuses(context.Context) map[model.CoreKind]core.Status {
	panic("backend boom")
}

func TestHandlerPanicBecomesFailedResponse(t *testing.T) {
	h := NewHandler(&panicBackend{fakeBackend: newFakeBackend()}, "endpoint", "test", "log", func() {}, nil)
	response := h.handle(context.Background(), ipc.Request{Version: ipc.Version, RequestID: "panic", Operation: ipc.OpCoreStatus})
	if response.Success {
		t.Fatal("panic was reported as success")
	}
	if response.Error == nil || response.Error.Code != ipc.CodeInternal {
		t.Fatalf("error = %+v", response.Error)
	}
}
