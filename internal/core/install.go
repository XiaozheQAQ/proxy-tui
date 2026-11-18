package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// Release repositories.
const (
	MihomoRepo  = "MetaCubeX/mihomo"
	SingBoxRepo = "SagerNet/sing-box"

	// maxDownloadBytes caps a core archive download at 200 MiB.
	maxDownloadBytes int64 = 200 << 20
)

// APIBase is the GitHub API root, overridable for tests.
var APIBase = "https://api.github.com"

// goVariantPattern matches legacy per-Go-version assets such as
// "mihomo-windows-amd64-v1-go120-v1.19.34.zip".
var goVariantPattern = regexp.MustCompile(`-god+-`)

// ReleaseAsset is one downloadable file of a release.
type ReleaseAsset struct {
	Name string
	URL  string
	Size int64
}

// Release is the subset of the GitHub release payload ProxyTUI needs.
type Release struct {
	Version   string
	Assets    []ReleaseAsset
	Checksums map[string]string
}

// InstallMeta is written next to an installed binary.
type InstallMeta struct {
	Version     string    `json:"version"`
	Asset       string    `json:"asset"`
	SHA256      string    `json:"sha256"`
	Verified    bool      `json:"verified"`
	SourceURL   string    `json:"source_url"`
	InstalledAt time.Time `json:"installed_at"`
	Notes       []string  `json:"notes,omitempty"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// FetchRelease loads the newest stable release for repo.
func FetchRelease(ctx context.Context, client HTTPDoer, repo string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(APIBase, "/"), repo)
	body, err := httpGet(ctx, client, url, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("query %s releases: %w", repo, err)
	}
	var payload githubRelease
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse %s release metadata: %w", repo, err)
	}
	release := &Release{Version: strings.TrimPrefix(payload.TagName, "v")}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets, ReleaseAsset{Name: asset.Name, URL: asset.BrowserDownloadURL, Size: asset.Size})
	}
	for _, asset := range release.Assets {
		if isChecksumAsset(asset.Name) {
			if data, err := httpGet(ctx, client, asset.URL, 2<<20); err == nil {
				release.Checksums = parseChecksums(data)
			}
			break
		}
	}
	return release, nil
}

// SelectAsset picks the archive that fits the current platform.
func SelectAsset(assets []ReleaseAsset, goos, goarch string) (ReleaseAsset, error) {
	type scored struct {
		asset ReleaseAsset
		score int
	}
	osTokens := map[string][]string{
		"windows": {"windows"},
		"darwin":  {"darwin", "macos"},
		"linux":   {"linux"},
	}
	archTokens := map[string][]string{
		"amd64": {"amd64", "x86_64", "x64"},
		"arm64": {"arm64", "aarch64"},
		"386":   {"386", "x86"},
	}
	wantedOS, ok := osTokens[goos]
	if !ok {
		return ReleaseAsset{}, fmt.Errorf("unsupported operating system %q", goos)
	}
	wantedArch, ok := archTokens[goarch]
	if !ok {
		return ReleaseAsset{}, fmt.Errorf("unsupported architecture %q", goarch)
	}

	var candidates []scored
	for _, asset := range assets {
		lower := strings.ToLower(asset.Name)
		if !hasAnyToken(lower, wantedOS) || !hasAnyToken(lower, wantedArch) {
			continue
		}
		if strings.Contains(lower, "android") || strings.Contains(lower, "freebsd") || strings.Contains(lower, "openbsd") || strings.Contains(lower, "armv7") {
			continue
		}
		if !(strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".gz")) {
			continue
		}
		// Mihomo publishes several builds of the same version: a portable
		// "compatible" build, a plain build, GOAMD64 level builds (v1/v2/v3)
		// and legacy per-Go-version builds. Prefer the most portable asset.
		score := 0
		switch {
		case strings.Contains(lower, "compatible"):
			score = 10
		case strings.Contains(lower, "legacy"):
			score = 0
		case goVariantPattern.MatchString(lower):
			score = 1
		case strings.Contains(lower, "glibc"), strings.Contains(lower, "musl"):
			score = 2
		case strings.Contains(lower, "-v1-"), strings.Contains(lower, "-v2-"), strings.Contains(lower, "-v3-"):
			score = 8
		default:
			score = 9
		}
		if strings.HasSuffix(lower, ".zip") {
			score++
		}
		candidates = append(candidates, scored{asset: asset, score: score})
	}
	if len(candidates) == 0 {
		names := make([]string, 0, len(assets))
		for _, asset := range assets {
			names = append(names, asset.Name)
		}
		sort.Strings(names)
		return ReleaseAsset{}, fmt.Errorf("no %s/%s asset found in this release (available: %s)", goos, goarch, strings.Join(names, ", "))
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].asset.Name < candidates[j].asset.Name
	})
	return candidates[0].asset, nil
}

func hasAnyToken(name string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func isChecksumAsset(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".txt") && !strings.HasSuffix(lower, ".sha256") && !strings.Contains(lower, "checksum") && !strings.Contains(lower, "sha256") {
		return false
	}
	return strings.Contains(lower, "checksum") || strings.Contains(lower, "sha256") || strings.Contains(lower, "hash")
}

// parseChecksums reads "hash  filename" lines.
func parseChecksums(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		name := strings.TrimPrefix(strings.Join(fields[1:], " "), "*")
		out[name] = sum
	}
	return out
}

// Install downloads and unpacks a core release into the data directory.
func Install(ctx context.Context, paths storage.Paths, kind model.CoreKind, repo string, client HTTPDoer, progress func(string)) (InstallResult, error) {
	if progress == nil {
		progress = func(string) {}
	}
	layout := NewLayout(paths, kind)
	if client == nil {
		client = DefaultHTTPClient()
	}

	progress("querying latest " + repo + " release")
	release, err := FetchRelease(ctx, client, repo)
	if err != nil {
		return InstallResult{}, err
	}
	asset, err := SelectAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return InstallResult{}, err
	}

	progress(fmt.Sprintf("downloading %s (%s)", asset.Name, format.Bytes(asset.Size)))
	archive, err := httpGet(ctx, client, asset.URL, maxDownloadBytes)
	if err != nil {
		return InstallResult{}, fmt.Errorf("download %s: %w", asset.Name, err)
	}

	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	result := InstallResult{Version: release.Version}
	if expected, ok := release.Checksums[asset.Name]; ok {
		if !strings.EqualFold(expected, digest) {
			return InstallResult{}, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset.Name, expected, digest)
		}
		result.Verified = true
	} else {
		result.Notes = append(result.Notes, "the release publishes no checksum for this asset; integrity was not verified")
	}

	progress("extracting core binary")
	binary, err := ExtractBinary(archive, asset.Name, kind)
	if err != nil {
		return InstallResult{}, err
	}

	versionDir := filepath.Join(layout.Dir(), release.Version)
	if err := storage.WriteFileAtomic(filepath.Join(versionDir, BinaryName(kind)), binary, 0o755); err != nil {
		return InstallResult{}, err
	}
	if err := layout.SetCurrent(release.Version); err != nil {
		return InstallResult{}, err
	}
	result.BinaryPath = layout.BinaryPath(release.Version)
	meta := InstallMeta{
		Version:     release.Version,
		Asset:       asset.Name,
		SHA256:      digest,
		Verified:    result.Verified,
		SourceURL:   asset.URL,
		InstalledAt: time.Now(),
		Notes:       result.Notes,
	}
	if err := storage.WriteJSONAtomic(layout.MetaPath(), meta, 0o600); err != nil {
		return InstallResult{}, err
	}
	progress("installed " + kind.DisplayName() + " " + release.Version)
	return result, nil
}

// ExtractBinary unpacks the core executable from a zip, tar.gz or gz archive.
func ExtractBinary(archive []byte, name string, kind model.CoreKind) ([]byte, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archive, kind)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archive, kind)
	case strings.HasSuffix(lower, ".gz"):
		return gunzip(archive)
	}
	return nil, fmt.Errorf("unsupported archive %q", name)
}

// matchesBinaryName accepts the executable names used by the cores:
// "mihomo.exe", "mihomo-windows-amd64-compatible.exe" (Mihomo publishes
// platform suffixed binaries inside its archives) and "sing-box".
func matchesBinaryName(name string, kind model.CoreKind) bool {
	base := strings.ToLower(filepath.Base(name))
	base = strings.TrimSuffix(base, ".exe")
	core := strings.ToLower(string(kind))
	if base == core {
		return true
	}
	return strings.HasPrefix(base, core+"-")
}

func extractZip(archive []byte, kind model.CoreKind) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !matchesBinaryName(file.Name, kind) {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", file.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain a %s binary", kind.DisplayName())
}

func extractTarGz(archive []byte, kind model.CoreKind) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if header.Typeflag != tar.TypeReg || !matchesBinaryName(header.Name, kind) {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", header.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain a %s binary", kind.DisplayName())
}

func gunzip(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return data, nil
}

// Uninstall removes every installed version of a core.
func Uninstall(paths storage.Paths, kind model.CoreKind) error {
	layout := NewLayout(paths, kind)
	return removeAll(layout.Dir())
}
