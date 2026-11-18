package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/proxytui/proxy-tui/internal/format"
)

// HTTPDoer is the minimal HTTP contract the installer needs.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient is used for release metadata and archives.
func DefaultHTTPClient() HTTPDoer {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Timeout: 15 * time.Minute, Transport: transport}
}

func httpGet(ctx context.Context, client HTTPDoer, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "proxy-tui/0.1.0")
	req.Header.Set("Accept", "application/octet-stream, application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %s", format.Bytes(maxBytes))
	}
	return body, nil
}

// removeAll is a thin wrapper so tests can stub removal if needed.
func removeAll(dir string) error { return os.RemoveAll(dir) }
