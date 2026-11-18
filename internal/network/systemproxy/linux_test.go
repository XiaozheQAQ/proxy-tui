package systemproxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/execx"
)

func newLinuxFake(initial linuxState) *execx.Fake {
	state := initial
	return execx.NewFake(func(name string, args []string) execx.Result {
		if name != "gsettings" {
			return execx.Result{Err: fmt.Errorf("unexpected command %q", name)}
		}
		switch args[0] {
		case "--version":
			return execx.Result{Stdout: "gsettings 2.80.0\n"}
		case "get":
			schema, key := args[1], args[2]
			switch schema + "." + key {
			case "org.gnome.system.proxy.mode":
				return execx.Result{Stdout: gvariantString(state.Mode) + "\n"}
			case "org.gnome.system.proxy.http.host":
				return execx.Result{Stdout: gvariantString(state.HTTPHost) + "\n"}
			case "org.gnome.system.proxy.http.port":
				return execx.Result{Stdout: fmt.Sprintf("%d\n", state.HTTPPort)}
			case "org.gnome.system.proxy.https.host":
				return execx.Result{Stdout: gvariantString(state.HTTPSHost) + "\n"}
			case "org.gnome.system.proxy.https.port":
				return execx.Result{Stdout: fmt.Sprintf("%d\n", state.HTTPSPort)}
			case "org.gnome.system.proxy.socks.host":
				return execx.Result{Stdout: gvariantString(state.SocksHost) + "\n"}
			case "org.gnome.system.proxy.socks.port":
				return execx.Result{Stdout: fmt.Sprintf("%d\n", state.SocksPort)}
			case "org.gnome.system.proxy.ignore-hosts":
				return execx.Result{Stdout: gvariantStringArray(state.IgnoreHosts) + "\n"}
			}
			return execx.Result{Err: fmt.Errorf("unexpected get %s %s", schema, key)}
		case "set":
			schema, key, value := args[1], args[2], args[3]
			unquoted := parseGVariantString(value)
			switch schema + "." + key {
			case "org.gnome.system.proxy.mode":
				state.Mode = unquoted
			case "org.gnome.system.proxy.http.host":
				state.HTTPHost = unquoted
			case "org.gnome.system.proxy.http.port":
				state.HTTPPort = parseGVariantInt(value)
			case "org.gnome.system.proxy.https.host":
				state.HTTPSHost = unquoted
			case "org.gnome.system.proxy.https.port":
				state.HTTPSPort = parseGVariantInt(value)
			case "org.gnome.system.proxy.socks.host":
				state.SocksHost = unquoted
			case "org.gnome.system.proxy.socks.port":
				state.SocksPort = parseGVariantInt(value)
			case "org.gnome.system.proxy.ignore-hosts":
				state.IgnoreHosts = parseGVariantStringArray(value)
			}
			return execx.Result{}
		}
		return execx.Result{Err: fmt.Errorf("unexpected gsettings args %v", args)}
	})
}

func TestLinuxCapabilities(t *testing.T) {
	gnome := NewLinux(newLinuxFake(linuxState{Mode: "none"}), "GNOME")
	if !gnome.Capabilities(context.Background()).Supported {
		t.Fatal("GNOME should be supported")
	}
	kde := NewLinux(newLinuxFake(linuxState{Mode: "none"}), "KDE")
	caps := kde.Capabilities(context.Background())
	if caps.Supported {
		t.Fatal("KDE should be reported as unsupported for now")
	}
	if !strings.Contains(caps.Reason, "not supported") {
		t.Errorf("reason = %q", caps.Reason)
	}
}

func TestLinuxApplyAndVerify(t *testing.T) {
	runner := newLinuxFake(linuxState{Mode: "none"})
	adapter := NewLinux(runner, "GNOME")
	ctx := context.Background()
	desired := network.Desired{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 7890, Bypass: "localhost,10.0.0.0/8"}

	if err := adapter.Apply(ctx, desired); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := adapter.Verify(ctx, desired); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	commands := strings.Join(runner.Commands(), "\n")
	for _, want := range []string{
		"gsettings set org.gnome.system.proxy mode 'manual'",
		"gsettings set org.gnome.system.proxy.http host '127.0.0.1'",
		"gsettings set org.gnome.system.proxy.http port 7890",
		"gsettings set org.gnome.system.proxy.https host '127.0.0.1'",
		"gsettings set org.gnome.system.proxy ignore-hosts ['localhost', '10.0.0.0/8']",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing command %q in:\n%s", want, commands)
		}
	}
	state, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !state.Enabled || state.Host != "127.0.0.1" || state.Port != 7890 {
		t.Errorf("state = %+v", state)
	}
	if state.Bypass != "localhost,10.0.0.0/8" {
		t.Errorf("bypass = %q", state.Bypass)
	}
}

func TestLinuxRestore(t *testing.T) {
	original := linuxState{
		Mode:        "none",
		HTTPHost:    "10.1.2.3",
		HTTPPort:    3128,
		HTTPSHost:   "10.1.2.3",
		HTTPSPort:   3128,
		SocksHost:   "",
		SocksPort:   0,
		IgnoreHosts: []string{"localhost"},
	}
	runner := newLinuxFake(original)
	adapter := NewLinux(runner, "GNOME")
	ctx := context.Background()

	snapshot, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := adapter.Apply(ctx, network.Desired{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 7890}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := adapter.Restore(ctx, snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	state, err := adapter.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Enabled {
		t.Errorf("mode should be restored to none: %+v", state)
	}
	if state.Host != "10.1.2.3" || state.Port != 3128 {
		t.Errorf("host/port not restored: %+v", state)
	}
	if state.Bypass != "localhost" {
		t.Errorf("bypass not restored: %q", state.Bypass)
	}
}
