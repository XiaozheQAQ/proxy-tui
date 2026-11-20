package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/storage"
)

func TestAppBackendExposesConfigInfo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	application, err := app.New(paths)
	if err != nil {
		t.Fatalf("app: %v", err)
	}
	backend := NewAppBackend(application)
	if _, err := invokeFacade(context.Background(), backend.Callable(), "ConfigInfo", nil); err != nil {
		t.Fatalf("ConfigInfo: %v", err)
	}
}

func TestServeAppBackendConfigInfoRPC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	application, err := app.New(paths)
	if err != nil {
		t.Fatalf("app: %v", err)
	}
	endpoint, cancel, done := startServer(t, paths, NewAppBackend(application))
	defer func() {
		cancel()
		<-done
	}()
	client, err := ipc.Dial(context.Background(), endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	var info app.ConfigInfo
	if err := client.Call(context.Background(), ipc.OpAppCall, app.AppCallPayload{Method: "ConfigInfo"}, &info); err != nil {
		t.Fatalf("ConfigInfo: %v", err)
	}
}
