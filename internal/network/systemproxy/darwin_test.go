package systemproxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/execx"
)

func newDarwinFake() *execx.Fake {
	return execx.NewFake(func(name string, args []string) execx.Result {
		if name != "networksetup" {
			return execx.Result{Err: fmt.Errorf("unexpected command %q", name)}
		}
		joined := strings.Join(args, " ")
		switch {
		case joined == "-listallnetworkservices":
			return execx.Result{Stdout: "An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nUSB LAN\n"}
		case strings.HasPrefix(joined, "-getwebproxy "):
			if args[1] == "Wi-Fi" {
				return execx.Result{Stdout: "Enabled: Yes\nServer: 10.0.0.5\nPort: 8080\nAuthenticated Proxy Enabled: 0\n"}
			}
			return execx.Result{Stdout: "Enabled: No\nServer: \nPort: 0\nAuthenticated Proxy Enabled: 0\n"}
		case strings.HasPrefix(joined, "-getsecurewebproxy "):
			return execx.Result{Stdout: "Enabled: No\nServer: \nPort: 0\nAuthenticated Proxy Enabled: 0\n"}
		case strings.HasPrefix(joined, "-getsocksfirewallproxy "):
			return execx.Result{Stdout: "Enabled: No\nServer: \nPort: 0\n"}
		case strings.HasPrefix(joined, "-getproxybypassdomains "):
			if args[1] == "Wi-Fi" {
				return execx.Result{Stdout: "*.local\n169.254/16\n"}
			}
			return execx.Result{Stdout: "There aren't any bypass domains set on USB LAN.\n"}
		default:
			return execx.Result{}
		}
	})
}

func TestDarwinAdapterStateAndSnapshot(t *testing.T) {
	runner := newDarwinFake()
	adapter := NewDarwin(runner)
	ctx := context.Background()

	caps := adapter.Capabilities(ctx)
	if !caps.Supported {
		t.Fatalf("capabilities = %+v", caps)
	}
	state, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !state.Enabled || state.Host != "10.0.0.5" || state.Port != 8080 {
		t.Errorf("state = %+v", state)
	}
	if len(state.Services) != 2 {
		t.Errorf("services = %d", len(state.Services))
	}

	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.Contains(string(snapshot.Data), "Wi-Fi") || !strings.Contains(string(snapshot.Data), "*.local") {
		t.Errorf("snapshot data = %s", snapshot.Data)
	}
}

func TestDarwinAdapterApplyAndVerify(t *testing.T) {
	runner := newDarwinFake()
	adapter := NewDarwin(runner)
	ctx := context.Background()
	desired := network.Desired{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 7890, Bypass: "localhost,127.0.0.1"}

	if err := adapter.Apply(ctx, desired); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	commands := strings.Join(runner.Commands(), "\n")
	for _, want := range []string{
		"networksetup -setwebproxy Wi-Fi 127.0.0.1 7890",
		"networksetup -setwebproxystate Wi-Fi on",
		"networksetup -setsecurewebproxy Wi-Fi 127.0.0.1 7890",
		"networksetup -setproxybypassdomains Wi-Fi localhost 127.0.0.1",
		"networksetup -setwebproxy USB LAN 127.0.0.1 7890",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing command %q in:\n%s", want, commands)
		}
	}
	if strings.Contains(commands, "-setsocksfirewallproxy") {
		t.Error("socks proxy should not be configured in http mode")
	}

	// Verify against the fake that still reports the original state.
	if err := adapter.Verify(ctx, desired); err == nil {
		t.Error("Verify should fail because the fake state was not updated")
	}
}

func TestDarwinAdapterRestore(t *testing.T) {
	runner := newDarwinFake()
	adapter := NewDarwin(runner)
	ctx := context.Background()
	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := adapter.Restore(ctx, snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	commands := strings.Join(runner.Commands(), "\n")
	for _, want := range []string{
		"networksetup -setwebproxy Wi-Fi 10.0.0.5 8080",
		"networksetup -setwebproxystate Wi-Fi on",
		"networksetup -setwebproxystate USB LAN off",
		"networksetup -setproxybypassdomains Wi-Fi *.local 169.254/16",
		"networksetup -setproxybypassdomains USB LAN Empty",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing restore command %q in:\n%s", want, commands)
		}
	}
}

func TestDarwinCapabilitiesWithoutNetworksetup(t *testing.T) {
	runner := execx.NewFake(func(name string, args []string) execx.Result {
		return execx.Result{Err: fmt.Errorf("exec: networksetup: executable file not found")}
	})
	adapter := NewDarwin(runner)
	caps := adapter.Capabilities(context.Background())
	if caps.Supported {
		t.Fatal("capabilities should be unsupported")
	}
	if !strings.Contains(caps.Reason, "networksetup") {
		t.Errorf("reason = %q", caps.Reason)
	}
}
