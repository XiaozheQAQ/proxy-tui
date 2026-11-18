package core

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProcessHelper is the child process used by TestProcessLifecycle.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		t.Skip("helper process only")
	}
	fmt.Println("helper core started")
	time.Sleep(30 * time.Second)
}

func TestProcessLifecycle(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	var mu sync.Mutex
	var logs []string
	process := NewProcess(func(level, msg string) {
		mu.Lock()
		logs = append(logs, msg)
		mu.Unlock()
	})
	defer func() { _ = process.Stop(2 * time.Second) }()

	workDir := t.TempDir()
	env := []string{"GO_WANT_HELPER_PROCESS=1"}
	if err := process.Start(executable, []string{"-test.run=TestProcessHelper$"}, env, workDir, "127.0.0.1:9999"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !process.Running() {
		t.Fatal("process should be running")
	}
	if process.PID() == 0 {
		t.Fatal("pid is zero")
	}
	if process.StartedAt().IsZero() {
		t.Fatal("started at is zero")
	}
	if process.ConfigPath() != workDir {
		t.Errorf("config path = %q", process.ConfigPath())
	}
	if process.APIAddr() != "127.0.0.1:9999" {
		t.Errorf("api addr = %q", process.APIAddr())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		joined := strings.Join(logs, "\n")
		mu.Unlock()
		if strings.Contains(joined, "helper core started") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	joined := strings.Join(logs, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "helper core started") {
		t.Fatalf("stdout was not streamed into the sink: %q", joined)
	}

	if err := process.Start(executable, nil, nil, "", ""); err != ErrAlreadyRunning {
		t.Fatalf("second start error = %v, want ErrAlreadyRunning", err)
	}

	start := time.Now()
	if err := process.Stop(3 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("stop took %s", elapsed)
	}
	if process.Running() {
		t.Error("process still running after Stop")
	}
	if err := process.Stop(time.Second); err != nil {
		t.Errorf("stopping twice should be a no-op: %v", err)
	}
}
