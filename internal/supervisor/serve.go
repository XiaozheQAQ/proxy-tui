package supervisor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/storage"
)

// Serve binds the local endpoint, publishes state and serves until ctx ends.
func Serve(ctx context.Context, paths storage.Paths, version string, backend Backend, logger *log.Logger) error {
	endpoint, err := ipc.EndpointPath(paths.RuntimeDir())
	if err != nil {
		return err
	}
	listener, err := ipc.Listen(endpoint)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	state := State{InstanceID: NewInstanceID(), PID: os.Getpid(), StartedAt: time.Now(), Endpoint: endpoint, Version: version, Protocol: ipc.Version}
	persistStop := func() error {
		return SaveLifecycle(paths, LifecycleRecord{
			Version: 1, InstanceID: state.InstanceID, Status: LifecycleStopped,
			PID: state.PID, StartedAt: state.StartedAt, Source: "supervisor.stop",
		})
	}
	h := NewHandler(backend, endpoint, version, filepath.Join(paths.LogsDir(), "supervisor.log"), cancel, persistStop)
	server := ipc.NewServer(listener, h.handle)
	defer server.Close()

	if err := SaveState(paths, state); err != nil {
		return fmt.Errorf("persist supervisor state: %w", err)
	}
	defer RemoveState(paths)
	if err := SaveLifecycle(paths, LifecycleRecord{Version: 1, InstanceID: state.InstanceID, Status: LifecycleRunning, PID: state.PID, StartedAt: state.StartedAt, Source: "supervisor.start"}); err != nil {
		return fmt.Errorf("persist supervisor lifecycle: %w", err)
	}
	defer func() {
		if h.Stopped() {
			_ = persistStop()
		}
	}()
	if logger != nil {
		logger.Printf("supervisor listening on %s (pid %d)", endpoint, os.Getpid())
	}

	err = server.Serve(runCtx)
	if logger != nil {
		logger.Printf("supervisor stopped: %v", err)
	}
	return err
}

// Run starts the supervisor with a real application backend.
func Run(ctx context.Context, paths storage.Paths, version string) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(paths.LogsDir(), "supervisor.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open supervisor log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logger := log.New(logFile, "supervisor ", log.LstdFlags|log.Lmicroseconds)

	application, err := app.New(paths)
	if err != nil {
		return err
	}
	return Serve(ctx, paths, version, NewAppBackend(application), logger)
}
