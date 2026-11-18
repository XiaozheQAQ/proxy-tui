// Package clashapi talks to the Clash compatible REST API that Mihomo always
// exposes and sing-box exposes when experimental.clash_api is enabled.
package clashapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/core"
)

// Client is a thin Clash API client.
type Client struct {
	base   string
	secret string
	http   *http.Client
}

// New builds a client for addr (host:port) using secret.
func New(addr, secret string) *Client {
	return &Client{
		base:   "http://" + addr,
		secret: secret,
		http:   &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, fmt.Errorf("build core API request: %w", err)
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) request(ctx context.Context, method, path string, out any) error {
	req, err := c.newRequest(ctx, method, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("core API %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("core API %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode core API %s: %w", path, err)
	}
	return nil
}

// Version returns the core version reported by the API.
func (c *Client) Version(ctx context.Context) (string, error) {
	var payload struct {
		Version string `json:"version"`
		Meta    bool   `json:"meta"`
	}
	if err := c.request(ctx, http.MethodGet, "/version", &payload); err != nil {
		return "", err
	}
	return payload.Version, nil
}

type proxyEntry struct {
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	Alive   bool     `json:"alive"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

var groupTypes = map[string]bool{
	"Selector": true, "URLTest": true, "Fallback": true,
	"LoadBalance": true, "Relay": true, "Smart": true,
}

// Proxies lists every proxy and proxy group.
func (c *Client) Proxies(ctx context.Context) ([]core.Proxy, error) {
	var payload struct {
		Proxies map[string]proxyEntry `json:"proxies"`
	}
	if err := c.request(ctx, http.MethodGet, "/proxies", &payload); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(payload.Proxies))
	for name := range payload.Proxies {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]core.Proxy, 0, len(names))
	for _, name := range names {
		entry := payload.Proxies[name]
		proxy := core.Proxy{
			Name:  name,
			Type:  entry.Type,
			Now:   entry.Now,
			All:   entry.All,
			Alive: entry.Alive,
			Delay: -1,
		}
		proxy.Group = groupTypes[entry.Type] || len(entry.All) > 0
		if len(entry.History) > 0 {
			proxy.Delay = entry.History[len(entry.History)-1].Delay
		}
		out = append(out, proxy)
	}
	return out, nil
}

// Select switches a proxy group to name.
func (c *Client) Select(ctx context.Context, group, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("select proxy %q: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("select proxy %q: HTTP %d %s", name, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// TestDelay measures the latency of one proxy in milliseconds.
func (c *Client) TestDelay(ctx context.Context, name string, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	query := url.Values{}
	query.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	query.Set("url", "https://www.gstatic.com/generate_204")
	var payload struct {
		Delay int `json:"delay"`
	}
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + query.Encode()
	if err := c.request(ctx, http.MethodGet, path, &payload); err != nil {
		return 0, err
	}
	return payload.Delay, nil
}

// Connections returns the live connection table plus cumulative traffic.
func (c *Client) Connections(ctx context.Context) ([]core.Connection, core.Traffic, error) {
	var payload struct {
		DownloadTotal int64 `json:"downloadTotal"`
		UploadTotal   int64 `json:"uploadTotal"`
		Connections   []struct {
			ID       string   `json:"id"`
			Upload   int64    `json:"upload"`
			Download int64    `json:"download"`
			Start    string   `json:"start"`
			Rule     string   `json:"rule"`
			Chain    []string `json:"chains"`
			Metadata struct {
				Host          string `json:"host"`
				DestinationIP string `json:"destinationIP"`
				Network       string `json:"network"`
				Type          string `json:"type"`
				SourceIP      string `json:"sourceIP"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := c.request(ctx, http.MethodGet, "/connections", &payload); err != nil {
		return nil, core.Traffic{}, err
	}
	connections := make([]core.Connection, 0, len(payload.Connections))
	for _, entry := range payload.Connections {
		start, _ := time.Parse(time.RFC3339Nano, entry.Start)
		connections = append(connections, core.Connection{
			ID:            entry.ID,
			Host:          entry.Metadata.Host,
			DestinationIP: entry.Metadata.DestinationIP,
			Network:       entry.Metadata.Network,
			Type:          entry.Metadata.Type,
			Rule:          entry.Rule,
			Chain:         entry.Chain,
			Upload:        entry.Upload,
			Download:      entry.Download,
			Start:         start,
		})
	}
	traffic := core.Traffic{UpTotal: payload.UploadTotal, DownTotal: payload.DownloadTotal}
	return connections, traffic, nil
}

// OpenTraffic subscribes to the /traffic stream. The caller closes the body.
func (c *Client) OpenTraffic(ctx context.Context) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/traffic", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open traffic stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("traffic stream returned HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// ReadTraffic decodes newline delimited traffic samples from body.
func ReadTraffic(body io.Reader, onSample func(up, down int64)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 16*1024), 256*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var sample struct {
			Up   int64 `json:"up"`
			Down int64 `json:"down"`
		}
		if err := json.Unmarshal(line, &sample); err != nil {
			continue
		}
		onSample(sample.Up, sample.Down)
	}
	return scanner.Err()
}
