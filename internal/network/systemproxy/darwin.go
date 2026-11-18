package systemproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/execx"
)

// Darwin controls the macOS system proxy through networksetup.
type Darwin struct {
	runner execx.Runner
}

// NewDarwin builds the macOS adapter.
func NewDarwin(runner execx.Runner) *Darwin { return &Darwin{runner: runner} }

// Name implements network.Adapter.
func (d *Darwin) Name() string { return "darwin-networksetup" }

type darwinProxy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type darwinService struct {
	Name   string      `json:"name"`
	Web    darwinProxy `json:"web"`
	Secure darwinProxy `json:"secure"`
	Socks  darwinProxy `json:"socks"`
	Bypass []string    `json:"bypass,omitempty"`
}

type darwinState struct {
	Services []darwinService `json:"services"`
}

// Capabilities implements network.Adapter.
func (d *Darwin) Capabilities(ctx context.Context) network.Capabilities {
	caps := network.Capabilities{Platform: "darwin", Method: "networksetup"}
	stdout, stderr, err := d.runner.Run(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		caps.Reason = "networksetup is not available: " + firstNonEmpty(stderr, err.Error())
		return caps
	}
	if strings.TrimSpace(stdout) == "" {
		caps.Reason = "networksetup listed no network services"
		return caps
	}
	caps.Supported = true
	return caps
}

func (d *Darwin) serviceNames(ctx context.Context) ([]string, error) {
	stdout, stderr, err := d.runner.Run(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, fmt.Errorf("list network services: %s", firstNonEmpty(stderr, err.Error()))
	}
	var services []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func (d *Darwin) readProxy(ctx context.Context, service, flag string) darwinProxy {
	stdout, _, err := d.runner.Run(ctx, "networksetup", flag, service)
	if err != nil {
		return darwinProxy{}
	}
	return parseNetworksetupProxy(stdout)
}

func (d *Darwin) readBypass(ctx context.Context, service string) []string {
	stdout, _, err := d.runner.Run(ctx, "networksetup", "-getproxybypassdomains", service)
	if err != nil {
		return nil
	}
	return parseBypassDomains(stdout)
}

func (d *Darwin) snapshotState(ctx context.Context) (darwinState, error) {
	services, err := d.serviceNames(ctx)
	if err != nil {
		return darwinState{}, err
	}
	state := darwinState{Services: make([]darwinService, 0, len(services))}
	for _, name := range services {
		state.Services = append(state.Services, darwinService{
			Name:   name,
			Web:    d.readProxy(ctx, name, "-getwebproxy"),
			Secure: d.readProxy(ctx, name, "-getsecurewebproxy"),
			Socks:  d.readProxy(ctx, name, "-getsocksfirewallproxy"),
			Bypass: d.readBypass(ctx, name),
		})
	}
	return state, nil
}

// GetState implements network.Adapter.
func (d *Darwin) GetState(ctx context.Context) (network.ProxyState, error) {
	state, err := d.snapshotState(ctx)
	if err != nil {
		return network.ProxyState{}, err
	}
	result := network.ProxyState{}
	for _, service := range state.Services {
		entry := network.ServiceProxy{Name: service.Name, Enabled: service.Web.Enabled, Host: service.Web.Server, Port: service.Web.Port}
		result.Services = append(result.Services, entry)
	}
	if len(result.Services) > 0 {
		primary := state.Services[0]
		result.Enabled = primary.Web.Enabled
		result.Host = primary.Web.Server
		result.Port = primary.Web.Port
		result.Protocol = "http"
		result.Bypass = strings.Join(primary.Bypass, ",")
	}
	if len(result.Services) > 1 {
		result.Detail = fmt.Sprintf("%d network services", len(result.Services))
	}
	return result, nil
}

// Snapshot implements network.Adapter.
func (d *Darwin) Snapshot(ctx context.Context) (network.Snapshot, error) {
	state, err := d.snapshotState(ctx)
	if err != nil {
		return network.Snapshot{}, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return network.Snapshot{}, err
	}
	proxy, err := d.GetState(ctx)
	if err != nil {
		return network.Snapshot{}, err
	}
	return network.Snapshot{Adapter: d.Name(), Taken: time.Now(), Proxy: proxy, Data: data}, nil
}

// Apply implements network.Adapter.
func (d *Darwin) Apply(ctx context.Context, desired network.Desired) error {
	services, err := d.serviceNames(ctx)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("no macOS network services were found")
	}
	port := strconv.Itoa(desired.Port)
	bypass := splitBypass(desired.Bypass)
	for _, service := range services {
		steps := [][]string{
			{"-setwebproxy", service, desired.Host, port},
			{"-setwebproxystate", service, "on"},
			{"-setsecurewebproxy", service, desired.Host, port},
			{"-setsecurewebproxystate", service, "on"},
		}
		if desired.Protocol == "socks5" {
			steps = append(steps,
				[]string{"-setsocksfirewallproxy", service, desired.Host, port},
				[]string{"-setsocksfirewallproxystate", service, "on"},
			)
		}
		steps = append(steps, []string{"-setproxybypassdomains", service, strings.Join(bypassArgs(bypass), " ")})
		for _, step := range steps {
			// The bypass step is passed as one argument on purpose.
			args := step
			if step[0] == "-setproxybypassdomains" {
				args = []string{"-setproxybypassdomains", service}
				args = append(args, bypassArgs(bypass)...)
			}
			if _, stderr, err := d.runner.Run(ctx, "networksetup", args...); err != nil {
				return fmt.Errorf("networksetup %s: %s", step[0], firstNonEmpty(stderr, err.Error()))
			}
		}
	}
	return nil
}

// Verify implements network.Adapter.
func (d *Darwin) Verify(ctx context.Context, desired network.Desired) error {
	state, err := d.GetState(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return fmt.Errorf("the web proxy is still disabled")
	}
	if state.Host != desired.Host || state.Port != desired.Port {
		return fmt.Errorf("the web proxy is %s, want %s", state.Address(), desired.Address())
	}
	return nil
}

// Restore implements network.Adapter.
func (d *Darwin) Restore(ctx context.Context, snapshot network.Snapshot) error {
	state := darwinState{}
	if err := json.Unmarshal(snapshot.Data, &state); err != nil {
		return fmt.Errorf("decode macOS proxy snapshot: %w", err)
	}
	for _, service := range state.Services {
		if err := d.restoreProxy(ctx, service.Name, service.Web, "-setwebproxy", "-setwebproxystate"); err != nil {
			return err
		}
		if err := d.restoreProxy(ctx, service.Name, service.Secure, "-setsecurewebproxy", "-setsecurewebproxystate"); err != nil {
			return err
		}
		if err := d.restoreProxy(ctx, service.Name, service.Socks, "-setsocksfirewallproxy", "-setsocksfirewallproxystate"); err != nil {
			return err
		}
		args := []string{"-setproxybypassdomains", service.Name}
		if len(service.Bypass) == 0 {
			args = append(args, "Empty")
		} else {
			args = append(args, service.Bypass...)
		}
		if _, stderr, err := d.runner.Run(ctx, "networksetup", args...); err != nil {
			return fmt.Errorf("restore bypass domains for %s: %s", service.Name, firstNonEmpty(stderr, err.Error()))
		}
	}
	return nil
}

func (d *Darwin) restoreProxy(ctx context.Context, service string, proxy darwinProxy, setFlag, stateFlag string) error {
	if proxy.Enabled && proxy.Server != "" {
		if _, stderr, err := d.runner.Run(ctx, "networksetup", setFlag, service, proxy.Server, strconv.Itoa(proxy.Port)); err != nil {
			return fmt.Errorf("restore %s for %s: %s", setFlag, service, firstNonEmpty(stderr, err.Error()))
		}
	}
	state := "off"
	if proxy.Enabled {
		state = "on"
	}
	if _, stderr, err := d.runner.Run(ctx, "networksetup", stateFlag, service, state); err != nil {
		return fmt.Errorf("restore %s for %s: %s", stateFlag, service, firstNonEmpty(stderr, err.Error()))
	}
	return nil
}

func parseNetworksetupProxy(output string) darwinProxy {
	proxy := darwinProxy{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enabled":
			proxy.Enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "server":
			proxy.Server = strings.TrimSpace(value)
		case "port":
			proxy.Port, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	return proxy
}

func parseBypassDomains(output string) []string {
	var domains []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "there aren't any bypass") {
			continue
		}
		domains = append(domains, line)
	}
	return domains
}

func splitBypass(value string) []string {
	var out []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' }) {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func bypassArgs(bypass []string) []string {
	if len(bypass) == 0 {
		return []string{"Empty"}
	}
	return bypass
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
