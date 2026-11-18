package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/storage"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	buffer := new(bytes.Buffer)
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs(args)
	err := root.Execute()
	return buffer.String(), err
}

const cliFixture = `
proxies:
  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
  - {name: B, type: trojan, server: 5.6.7.8, port: 443, password: pw}
rules:
  - MATCH,PROXY
`

func TestCLIProfileLifecycle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	fixture := filepath.Join(dir, "sub.yaml")
	if err := os.WriteFile(fixture, []byte(cliFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out, err := runCLI(t, "profile", "add", "--core", "mihomo", "--name", "Fixture", fixture)
	if err != nil {
		t.Fatalf("profile add: %v (%s)", err, out)
	}
	if !strings.Contains(out, "✓ Fixture") {
		t.Errorf("add output = %q", out)
	}

	out, err = runCLI(t, "profile", "list")
	if err != nil {
		t.Fatalf("profile list: %v", err)
	}
	if !strings.Contains(out, "Fixture") || !strings.Contains(out, "Mihomo") {
		t.Errorf("list output = %q", out)
	}

	application, err := app.New(mustPaths(t))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	profiles := application.Subs.List()
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d", len(profiles))
	}
	profile := profiles[0]

	out, err = runCLI(t, "profile", "use", profile.ID)
	if err != nil {
		t.Fatalf("profile use: %v", err)
	}
	if !strings.Contains(out, "active profile") {
		t.Errorf("use output = %q", out)
	}

	// A config with its own rules is not activatable for sing-box but can be
	// imported and inspected.
	out, err = runCLI(t, "profile", "show", profile.ID)
	if err != nil {
		t.Fatalf("profile show: %v", err)
	}
	if !strings.Contains(out, "format:      Mihomo YAML") {
		t.Errorf("show output = %q", out)
	}

	out, err = runCLI(t, "profile", "delete", profile.ID)
	if err != nil {
		t.Fatalf("profile delete: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("delete output = %q", out)
	}

	out, err = runCLI(t, "profile", "list")
	if err != nil {
		t.Fatalf("profile list after delete: %v", err)
	}
	if !strings.Contains(out, "no profiles yet") {
		t.Errorf("empty list output = %q", out)
	}
}

func TestCLIVersionAndCoreList(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)

	out, err := runCLI(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out, "proxy-tui") {
		t.Errorf("version output = %q", out)
	}

	out, err = runCLI(t, "core", "list")
	if err != nil {
		t.Fatalf("core list: %v", err)
	}
	if !strings.Contains(out, "Mihomo") || !strings.Contains(out, "NOT INSTALLED") {
		t.Errorf("core list output = %q", out)
	}
}

func TestCLIStatusAndDoctor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)

	out, err := runCLI(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "active profile: none") {
		t.Errorf("status output = %q", out)
	}

	out, err = runCLI(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	for _, want := range []string{"proxy-tui", "platform:", "profiles:", "not implemented"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestCLINetworkStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)

	out, err := runCLI(t, "network", "status")
	if err != nil {
		t.Fatalf("network status: %v", err)
	}
	for _, want := range []string{"platform:", "mode:", "desktop:", "shell:", "backup:"} {
		if !strings.Contains(out, want) {
			t.Errorf("network status is missing %q:\n%s", want, out)
		}
	}
}

func TestCLISystemProxyRequiresCore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	if _, err := runCLI(t, "network", "system-proxy", "enable"); err == nil {
		t.Fatal("expected an error when no core is running")
	} else if !strings.Contains(err.Error(), "no proxy core is running") {
		t.Errorf("error = %v", err)
	}
}

func TestCLINetworkModeParsing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	if _, err := runCLI(t, "network", "mode", "nonsense"); err == nil {
		t.Fatal("expected an error for an unknown traffic mode")
	}
	if _, err := runCLI(t, "network", "system-proxy", "sideways"); err == nil {
		t.Fatal("expected an error for an unknown system-proxy action")
	}
}

func TestCLITUNStatusAndPreflight(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)

	out, err := runCLI(t, "network", "tun", "status")
	if err != nil {
		t.Fatalf("tun status: %v", err)
	}
	for _, want := range []string{"platform:", "tun:", "settings:", "interface:"} {
		if !strings.Contains(out, want) {
			t.Errorf("tun status missing %q:\n%s", want, out)
		}
	}

	// Preflight completes even when activation is blocked; enable still refuses.
	out, err = runCLI(t, "network", "tun", "preflight")
	if err != nil {
		t.Fatalf("preflight should complete without an execution error: %v\n%s", err, out)
	}
	for _, want := range []string{"preflight: completed=true", "activation ready: no", "blocking issues:"} {
		if !strings.Contains(out, want) {
			t.Errorf("preflight output missing %q:\n%s", want, out)
		}
	}
	if _, err := runCLI(t, "network", "tun", "enable"); err == nil {
		t.Error("enable must fail without an installed core")
	}
}

func TestCLIRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)

	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := runCLI(t, "profile", "add", "--core", "mihomo", empty); err == nil {
		t.Fatal("expected an error for an empty file")
	}
	if _, err := runCLI(t, "profile", "add", "--core", "nonsense", empty); err == nil {
		t.Fatal("expected an error for an unknown core")
	}
}

func mustPaths(t *testing.T) storage.Paths {
	t.Helper()
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	return paths
}
