package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func startIdentityHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(executable, "-test.run=TestProcessHelper$")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func TestProcessIdentityVerify(t *testing.T) {
	helper := startIdentityHelper(t)
	identity, err := ReadProcessIdentity(helper.Process.Pid)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if identity.StartedAt.IsZero() || identity.Executable == "" {
		t.Fatalf("incomplete identity: %+v", identity)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("runtime-config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	state := RuntimeState{
		PID:              helper.Process.Pid,
		ProcessStartedAt: identity.StartedAt,
		BinaryPath:       identity.Executable,
		BinarySHA256:     HashFile(identity.Executable),
		ConfigPath:       configPath,
		ConfigSHA256:     HashBytes([]byte("runtime-config")),
	}
	if _, verdict, err := VerifyRuntimeState(state, true); err != nil || verdict != IdentityVerified {
		t.Fatalf("verify = %s, %v", verdict, err)
	}

	badStart := state
	badStart.ProcessStartedAt = identity.StartedAt.Add(10 * time.Second)
	if _, verdict, _ := VerifyRuntimeState(badStart, true); verdict != IdentityMismatch {
		t.Fatalf("start-time verdict = %s", verdict)
	}
	badBinary := state
	badBinary.BinarySHA256 = "deadbeef"
	if _, verdict, _ := VerifyRuntimeState(badBinary, true); verdict != IdentityMismatch {
		t.Fatalf("binary verdict = %s", verdict)
	}
	badConfig := state
	badConfig.ConfigSHA256 = "deadbeef"
	if _, verdict, _ := VerifyRuntimeState(badConfig, true); verdict != IdentityMismatch {
		t.Fatalf("config verdict = %s", verdict)
	}
	legacy := state
	legacy.ProcessStartedAt = time.Time{}
	legacy.BinarySHA256 = ""
	legacy.ConfigSHA256 = ""
	if _, verdict, _ := VerifyRuntimeState(legacy, true); verdict != IdentityUnverified {
		t.Fatalf("legacy verdict = %s", verdict)
	}
}

func TestProcessIdentityGoneAndWait(t *testing.T) {
	helper := startIdentityHelper(t)
	pid := helper.Process.Pid
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	_ = helper.Wait()
	if _, err := ReadProcessIdentity(pid); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("identity after exit = %v", err)
	}
	if !WaitProcessExit(pid, time.Second) {
		t.Fatal("WaitProcessExit did not observe the exit")
	}
}
