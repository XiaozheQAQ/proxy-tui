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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buffer.Bytes()
}

func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	gz := gzip.NewWriter(buffer)
	writer := tar.NewWriter(gz)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buffer.Bytes()
}

// platformAssetName builds a zip asset name that SelectAsset accepts on the
// current platform, so the installer tests run on every OS.
func platformAssetName(core, variant, version string) string {
	return fmt.Sprintf("%s-%s-%s-%s-v%s.zip", core, runtime.GOOS, runtime.GOARCH, variant, version)
}

func TestSelectAsset(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "mihomo-windows-amd64-compatible-v1.19.0.zip"},
		{Name: "mihomo-windows-amd64-v3-v1.19.0.zip"},
		{Name: "mihomo-linux-amd64-v1.19.0.gz"},
		{Name: "mihomo-darwin-arm64-v1.19.0.gz"},
		{Name: "mihomo-android-arm64-v1.19.0.gz"},
		{Name: "mihomo-linux-amd64-v1.19.0.deb"},
	}
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"windows", "amd64", "mihomo-windows-amd64-compatible-v1.19.0.zip"},
		{"linux", "amd64", "mihomo-linux-amd64-v1.19.0.gz"},
		{"darwin", "arm64", "mihomo-darwin-arm64-v1.19.0.gz"},
	}
	for _, testCase := range cases {
		asset, err := SelectAsset(assets, testCase.goos, testCase.goarch)
		if err != nil {
			t.Fatalf("%s/%s: %v", testCase.goos, testCase.goarch, err)
		}
		if asset.Name != testCase.want {
			t.Errorf("%s/%s selected %q, want %q", testCase.goos, testCase.goarch, asset.Name, testCase.want)
		}
	}
	if _, err := SelectAsset(assets, "plan9", "amd64"); err == nil {
		t.Error("expected an error for an unsupported OS")
	}
	if _, err := SelectAsset([]ReleaseAsset{{Name: "mihomo-linux-amd64.deb"}}, "linux", "amd64"); err == nil {
		t.Error("expected an error when only non archive assets exist")
	}
}

func TestParseChecksums(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := []byte(hash + "  mihomo.zip\n" +
		"not-a-hash  other.zip\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *sing-box.tar.gz\n")
	sums := parseChecksums(data)
	if sums["mihomo.zip"] != hash {
		t.Errorf("mihomo.zip = %q", sums["mihomo.zip"])
	}
	if sums["sing-box.tar.gz"] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("sing-box.tar.gz = %q", sums["sing-box.tar.gz"])
	}
	if _, ok := sums["other.zip"]; ok {
		t.Error("invalid hash line was accepted")
	}
}

func TestExtractBinary(t *testing.T) {
	zipData := makeZip(t, map[string][]byte{"dir/mihomo.exe": []byte("zip-binary")})
	got, err := ExtractBinary(zipData, "mihomo.zip", model.CoreMihomo)
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	if string(got) != "zip-binary" {
		t.Errorf("zip content = %q", got)
	}

	tarData := makeTarGz(t, map[string][]byte{"sing-box-1.11.0-linux-amd64/sing-box": []byte("tar-binary")})
	got, err = ExtractBinary(tarData, "sing-box.tar.gz", model.CoreSingBox)
	if err != nil {
		t.Fatalf("tar.gz: %v", err)
	}
	if string(got) != "tar-binary" {
		t.Errorf("tar content = %q", got)
	}

	gzData := makeTarGz(t, map[string][]byte{"mihomo": []byte("ignored")})
	_ = gzData
	plain := new(bytes.Buffer)
	writer := gzip.NewWriter(plain)
	_, _ = writer.Write([]byte("gz-binary"))
	_ = writer.Close()
	got, err = ExtractBinary(plain.Bytes(), "mihomo-linux-amd64-v1.19.0.gz", model.CoreMihomo)
	if err != nil {
		t.Fatalf("gz: %v", err)
	}
	if string(got) != "gz-binary" {
		t.Errorf("gz content = %q", got)
	}

	if _, err := ExtractBinary(zipData, "mihomo.zip", model.CoreSingBox); err == nil {
		t.Error("expected an error when the archive lacks the requested binary")
	}
}

func TestInstallEndToEnd(t *testing.T) {
	binaryName := BinaryName(model.CoreMihomo)
	assetName := platformAssetName("mihomo", "compatible", "9.9.9")
	archive := makeZip(t, map[string][]byte{binaryName: []byte("fake-core-binary")})
	sum := sha256.Sum256(archive)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + assetName + "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/core/releases/latest":
			payload := map[string]any{
				"tag_name": "v9.9.9",
				"assets": []map[string]any{
					{"name": assetName, "browser_download_url": "http://" + r.Host + "/download/core.zip", "size": len(archive)},
					{"name": "checksums.txt", "browser_download_url": "http://" + r.Host + "/download/checksums.txt", "size": len(checksums)},
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		case "/download/core.zip":
			_, _ = w.Write(archive)
		case "/download/checksums.txt":
			_, _ = w.Write(checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase := APIBase
	APIBase = server.URL
	t.Cleanup(func() { APIBase = oldBase })

	base := t.TempDir()
	paths := storage.Paths{
		Config: filepath.Join(base, "config"),
		Data:   filepath.Join(base, "data"),
		State:  filepath.Join(base, "state"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	var progress []string
	result, err := Install(context.Background(), paths, model.CoreMihomo, "test/core", nil, func(status string) {
		progress = append(progress, status)
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Version != "9.9.9" {
		t.Errorf("version = %q", result.Version)
	}
	if !result.Verified {
		t.Error("checksum should have been verified")
	}
	if len(progress) == 0 {
		t.Error("no progress was reported")
	}
	binaryPath := filepath.Join(paths.CoreDir("mihomo"), "9.9.9", binaryName)
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}
	if string(data) != "fake-core-binary" {
		t.Errorf("binary content = %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(binaryPath)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("binary is not executable: %v", info.Mode())
		}
	}
	version, installedPath, ok := NewLayout(paths, model.CoreMihomo).Installed()
	if !ok || version != "9.9.9" || installedPath != binaryPath {
		t.Errorf("layout.Installed = %q %q %v", version, installedPath, ok)
	}
	meta := InstallMeta{}
	if err := storage.ReadJSON(NewLayout(paths, model.CoreMihomo).MetaPath(), &meta); err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.Version != "9.9.9" || !meta.Verified {
		t.Errorf("meta = %+v", meta)
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	binaryName := BinaryName(model.CoreMihomo)
	assetName := platformAssetName("mihomo", "compatible", "1.0.0")
	archive := makeZip(t, map[string][]byte{binaryName: []byte("payload")})
	wrong := []byte(fmt.Sprintf("%064d  %s\n", 0, assetName))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/core/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.0.0",
				"assets": []map[string]any{
					{"name": assetName, "browser_download_url": "http://" + r.Host + "/core.zip"},
					{"name": "checksums.txt", "browser_download_url": "http://" + r.Host + "/checksums.txt"},
				},
			})
		case "/core.zip":
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = w.Write(wrong)
		}
	}))
	defer server.Close()

	oldBase := APIBase
	APIBase = server.URL
	t.Cleanup(func() { APIBase = oldBase })

	base := t.TempDir()
	paths := storage.Paths{Config: filepath.Join(base, "c"), Data: filepath.Join(base, "d"), State: filepath.Join(base, "s")}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := Install(context.Background(), paths, model.CoreMihomo, "test/core", nil, nil); err == nil {
		t.Fatal("expected a checksum mismatch error")
	}
	if _, _, ok := NewLayout(paths, model.CoreMihomo).Installed(); ok {
		t.Error("a rejected install must not become current")
	}
}
