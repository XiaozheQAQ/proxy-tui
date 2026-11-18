package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/security"
)

const (
	// DefaultMaxBytes caps subscription responses at 8 MiB.
	DefaultMaxBytes int64 = 8 << 20
	// maxRedirects is the redirect budget for subscription fetches.
	maxRedirects = 5
	// defaultFetchTimeout bounds a single HTTP request.
	defaultFetchTimeout = 25 * time.Second
)

// FetchOptions tunes a single fetch.
type FetchOptions struct {
	ETag         string
	LastModified string
	UserAgent    string
	MaxBytes     int64
}

// FetchResult is the outcome of one subscription request.
type FetchResult struct {
	Body         []byte
	FinalURL     string
	StatusCode   int
	ETag         string
	LastModified string
	ContentType  string
	NotModified  bool
}

// Fetcher downloads subscriptions with sane limits.
type Fetcher struct {
	client    *http.Client
	userAgent string
	maxBytes  int64
}

// NewFetcher builds a fetcher with timeout, redirect and size limits.
func NewFetcher(userAgent string) *Fetcher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	client := &http.Client{
		Timeout:   defaultFetchTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	return &Fetcher{client: client, userAgent: userAgent, maxBytes: DefaultMaxBytes}
}

// Fetch downloads one subscription URL.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, opts FetchOptions) (*FetchResult, error) {
	endpoint, err := NormalizeSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = f.userAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	if opts.ETag != "" {
		req.Header.Set("If-None-Match", opts.ETag)
	}
	if opts.LastModified != "" {
		req.Header.Set("If-Modified-Since", opts.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %s", security.RedactURL(endpoint), describeHTTPError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	result := &FetchResult{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
	}
	if resp.Request != nil && resp.Request.URL != nil {
		result.FinalURL = resp.Request.URL.String()
	}
	if resp.StatusCode == http.StatusNotModified {
		result.NotModified = true
		result.ETag = opts.ETag
		result.LastModified = opts.LastModified
		return result, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d %s", security.RedactURL(endpoint), resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = f.maxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %s", describeHTTPError(err))
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response exceeds the %s limit", format.Bytes(maxBytes))
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("subscription returned an empty body")
	}
	result.Body = body
	return result, nil
}

// NormalizeSubscriptionURL validates that rawURL is a plain HTTP(S) endpoint.
func NormalizeSubscriptionURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("subscription URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid subscription URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("subscription URL has no host")
	}
	return parsed.String(), nil
}

// IsSubscriptionURL reports whether a line should be fetched over HTTP.
func IsSubscriptionURL(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")
}

func describeHTTPError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	case errors.Is(err, context.Canceled):
		return "request cancelled"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
