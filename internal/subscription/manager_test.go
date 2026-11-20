package subscription

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

const nodesYAML = `
proxies:
  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
  - {name: B, type: trojan, server: 5.6.7.8, port: 443, password: pw}
`

const fullMihomoYAML = `
mixed-port: 7890
proxies:
  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}
proxy-groups:
  - {name: PROXY, type: select, proxies: [A]}
rules:
  - MATCH,PROXY
`

func newTestManager(t *testing.T) (*Manager, storage.Paths) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PROXY_TUI_HOME", dir)
	paths, err := storage.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	manager, err := NewManager(paths, NewFetcher("proxy-tui-test"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, paths
}

func TestImportURLStoresProfile(t *testing.T) {
	manager, paths := newTestManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "proxy-tui-test" {
			t.Errorf("user agent = %q", got)
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, nodesYAML)
	}))
	defer server.Close()

	outcomes, err := manager.Import(context.Background(), ImportRequest{
		Core:  model.CoreMihomo,
		Input: server.URL + "/api/v1/client/subscribe?token=secret-token",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d", len(outcomes))
	}
	if outcomes[0].Err != nil {
		t.Fatalf("import failed: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile
	if profile.Core != model.CoreMihomo || profile.Format != model.FormatMihomoYAML {
		t.Errorf("profile = %+v", profile)
	}
	if profile.NodeCount != 2 {
		t.Errorf("node count = %d", profile.NodeCount)
	}
	if !profile.Activatable {
		t.Errorf("profile should be activatable, warnings: %v", profile.Warnings)
	}
	if outcomes[0].Source == "" || strings.Contains(outcomes[0].Source, "secret-token") {
		t.Errorf("source is not redacted: %q", outcomes[0].Source)
	}

	active, activePath, err := manager.ActiveConfig(profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig: %v", err)
	}
	if !strings.HasSuffix(activePath, "active.yaml") {
		t.Errorf("active path = %s", activePath)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(active, &doc); err != nil {
		t.Fatalf("active config is not valid YAML: %v", err)
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != 2 {
		t.Errorf("rendered proxies = %d, want 2", len(proxies))
	}
	groups, _ := doc["proxy-groups"].([]any)
	if len(groups) != 2 {
		t.Errorf("rendered groups = %d, want 2", len(groups))
	}

	// The index must survive a restart.
	reopened, err := NewManager(paths, NewFetcher("proxy-tui-test"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(reopened.List()) != 1 {
		t.Fatalf("profiles after restart = %d", len(reopened.List()))
	}
	if reopened.List()[0].Name != profile.Name {
		t.Errorf("name after restart = %q", reopened.List()[0].Name)
	}
}

func TestImportDuplicateIsRejected(t *testing.T) {
	manager, _ := newTestManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, nodesYAML)
	}))
	defer server.Close()

	req := ImportRequest{Core: model.CoreMihomo, Input: server.URL + "/sub"}
	if _, err := manager.Import(context.Background(), req); err != nil {
		t.Fatalf("first import: %v", err)
	}
	outcomes, err := manager.Import(context.Background(), req)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if outcomes[0].Err == nil {
		t.Fatal("duplicate import should fail")
	}
	if !strings.Contains(outcomes[0].Err.Error(), "already imported") {
		t.Errorf("unexpected error: %v", outcomes[0].Err)
	}
	if len(manager.List()) != 1 {
		t.Errorf("profile count = %d, want 1", len(manager.List()))
	}

	// The same URL for the other core is a different profile.
	if _, err := manager.Import(context.Background(), ImportRequest{Core: model.CoreSingBox, Input: server.URL + "/sub"}); err != nil {
		t.Fatalf("sing-box import: %v", err)
	}
	if len(manager.List()) != 2 {
		t.Errorf("profile count = %d, want 2", len(manager.List()))
	}
}

func TestImportInvalidPayloadKeepsStoreClean(t *testing.T) {
	manager, _ := newTestManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "this is not a subscription at all")
	}))
	defer server.Close()

	outcomes, err := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: server.URL + "/sub"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if outcomes[0].Err == nil {
		t.Fatal("expected an error for an invalid payload")
	}
	if len(manager.List()) != 0 {
		t.Errorf("profiles = %d, want 0", len(manager.List()))
	}
}

func TestImportInlineYAMLPreservesIndentation(t *testing.T) {
	manager, _ := newTestManager(t)
	input := "mixed-port: 7890\n" +
		"dns:\n" +
		"  enable: true\n" +
		"  nameserver:\n" +
		"    - 223.5.5.5\n" +
		"proxies:\n" +
		"  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}\n" +
		"proxy-groups:\n" +
		"  - {name: PROXY, type: select, proxies: [A]}\n" +
		"rules:\n" +
		"  - MATCH,PROXY\n"
	outcomes, err := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: input})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if outcomes[0].Err != nil {
		t.Fatalf("import failed: %v", outcomes[0].Err)
	}
	active, _, err := manager.ActiveConfig(outcomes[0].Profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig: %v", err)
	}
	if strings.TrimRight(string(active), "\r\n") != strings.TrimRight(input, "\r\n") {
		t.Fatalf("stored YAML changed:\n--- want ---\n%s\n--- got ---\n%s", input, active)
	}
	if !strings.Contains(string(active), "dns:\n  enable: true") {
		t.Errorf("nested dns settings lost their indentation:\n%s", active)
	}
}

func TestImportInlineNodeListForSingBox(t *testing.T) {
	manager, _ := newTestManager(t)
	input := strings.Join([]string{
		"ss://YWVzLTEyOC1nY206cA@1.2.3.4:8388#NodeA",
		"trojan://pw@example.com:443#NodeB",
	}, "\n")
	outcomes, err := manager.Import(context.Background(), ImportRequest{Core: model.CoreSingBox, Input: input})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if outcomes[0].Err != nil {
		t.Fatalf("import failed: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile
	if profile.Format != model.FormatNodeList {
		t.Errorf("format = %q", profile.Format)
	}
	if profile.NodeCount != 2 || !profile.Activatable {
		t.Errorf("profile = %+v", profile)
	}

	active, activePath, err := manager.ActiveConfig(profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig: %v", err)
	}
	if !strings.HasSuffix(activePath, "active.json") {
		t.Errorf("active path = %s", activePath)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(active, &doc); err != nil {
		t.Fatalf("active sing-box config is not valid JSON: %v", err)
	}
	outbounds, _ := doc["outbounds"].([]any)
	if len(outbounds) < 4 {
		t.Fatalf("outbounds = %d", len(outbounds))
	}
	first, _ := outbounds[0].(map[string]any)
	if first["type"] != "selector" || first["tag"] != "proxy" {
		t.Errorf("first outbound = %#v", first)
	}
}

func TestImportCrossCoreIsStoredButNotActivatable(t *testing.T) {
	manager, _ := newTestManager(t)
	outcomes, err := manager.Import(context.Background(), ImportRequest{Core: model.CoreSingBox, Input: fullMihomoYAML})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if outcomes[0].Err != nil {
		t.Fatalf("cross core import should be stored: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile
	if profile.Activatable {
		t.Fatal("a Mihomo config with rules must not be activatable for sing-box")
	}
	if len(profile.Warnings) == 0 {
		t.Error("expected a conversion warning")
	}
	if _, _, err := manager.ActiveConfig(profile.ID); err == nil {
		t.Error("ActiveConfig should fail for a non activatable profile")
	}
	if _, err := manager.SetActive(profile.ID); err == nil {
		t.Error("SetActive should fail for a non activatable profile")
	}
}

func TestUpdateKeepsOldRevisionOnFailure(t *testing.T) {
	manager, _ := newTestManager(t)
	mode := "good"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "good":
			_, _ = io.WriteString(w, nodesYAML)
		case "expanded":
			_, _ = io.WriteString(w, nodesYAML+"  - {name: C, type: ss, server: 9.9.9.9, port: 8388, cipher: aes-128-gcm, password: p}\n")
		default:
			_, _ = io.WriteString(w, "broken payload that is not a subscription")
		}
	}))
	defer server.Close()

	outcomes, _ := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: server.URL + "/sub"})
	if outcomes[0].Err != nil {
		t.Fatalf("import: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile

	mode = "expanded"
	updated, err := manager.Update(context.Background(), profile.ID)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Revision != 2 || updated.NodeCount != 3 {
		t.Errorf("updated = %+v", updated)
	}

	activeBefore, _, err := manager.ActiveConfig(profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig: %v", err)
	}

	mode = "bad"
	if _, err := manager.Update(context.Background(), profile.ID); err == nil {
		t.Fatal("expected update to fail on a broken payload")
	}
	after, _ := manager.Get(profile.ID)
	if after.Revision != 2 {
		t.Errorf("revision changed on failed update: %d", after.Revision)
	}
	if after.LastUpdate == nil || after.LastUpdate.OK {
		t.Errorf("last update should be recorded as failed: %+v", after.LastUpdate)
	}
	activeAfter, _, err := manager.ActiveConfig(profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig after failure: %v", err)
	}
	if string(activeBefore) != string(activeAfter) {
		t.Error("active config was replaced by a failed update")
	}
}

func TestUpdateNotModified(t *testing.T) {
	manager, _ := newTestManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, nodesYAML)
	}))
	defer server.Close()

	outcomes, _ := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: server.URL + "/sub"})
	if outcomes[0].Err != nil {
		t.Fatalf("import: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile
	if profile.ETag != `"v1"` {
		t.Errorf("etag = %q", profile.ETag)
	}

	updated, err := manager.Update(context.Background(), profile.ID)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Revision != profile.Revision {
		t.Errorf("revision changed on 304: %d -> %d", profile.Revision, updated.Revision)
	}
	if updated.LastUpdate == nil || !updated.LastUpdate.OK {
		t.Errorf("last update = %+v", updated.LastUpdate)
	}
}

func TestRollbackRestoresSnapshot(t *testing.T) {
	manager, _ := newTestManager(t)
	content := "good"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if content == "good" {
			_, _ = io.WriteString(w, nodesYAML)
			return
		}
		_, _ = io.WriteString(w, nodesYAML+"  - {name: C, type: ss, server: 9.9.9.9, port: 8388, cipher: aes-128-gcm, password: p}\n")
	}))
	defer server.Close()

	outcomes, _ := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: server.URL + "/sub"})
	if outcomes[0].Err != nil {
		t.Fatalf("import: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile

	content = "expanded"
	if _, err := manager.Update(context.Background(), profile.ID); err != nil {
		t.Fatalf("update: %v", err)
	}
	snapshots, err := manager.Snapshots(profile.ID)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snapshots) < 2 {
		t.Fatalf("snapshots = %d, want at least 2", len(snapshots))
	}

	restored, err := manager.Rollback(profile.ID, 1)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if restored.Revision != 1 {
		t.Errorf("revision after rollback = %d", restored.Revision)
	}
	active, _, err := manager.ActiveConfig(profile.ID)
	if err != nil {
		t.Fatalf("ActiveConfig: %v", err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(active, &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != 2 {
		t.Errorf("proxies after rollback = %d, want 2", len(proxies))
	}
}

func TestDeleteRemovesFiles(t *testing.T) {
	manager, paths := newTestManager(t)
	outcomes, _ := manager.Import(context.Background(), ImportRequest{Core: model.CoreMihomo, Input: nodesYAML})
	if outcomes[0].Err != nil {
		t.Fatalf("import: %v", outcomes[0].Err)
	}
	profile := outcomes[0].Profile
	if _, err := manager.SetActive(profile.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := manager.Delete(profile.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(manager.List()) != 0 {
		t.Errorf("profiles after delete = %d", len(manager.List()))
	}
	if _, err := os.Stat(filepath.Join(paths.ProfileDir(profile.ID))); !os.IsNotExist(err) {
		t.Errorf("profile directory still exists: %v", err)
	}
	if _, ok := manager.ActiveFor(model.CoreMihomo); ok {
		t.Error("active profile survived deletion")
	}
}

func TestImportFailureRedactsURLCanary(t *testing.T) {
	manager, _ := newTestManager(t)
	const canary = "PROXYTUI_TEST_SECRET_SUB_91X"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	outcomes, err := manager.Import(context.Background(), ImportRequest{
		Core:  model.CoreMihomo,
		Input: server.URL + "/subscribe/" + canary + "?token=" + canary,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	combined := outcomes[0].Source + " " + outcomes[0].Err.Error()
	if strings.Contains(combined, canary) {
		t.Fatalf("canary leaked in import failure: %s", combined)
	}
}
