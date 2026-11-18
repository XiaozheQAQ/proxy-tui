package subscription

import (
	"encoding/base64"
	"testing"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func TestDetectMihomoYAML(t *testing.T) {
	payload := []byte("mixed-port: 7890\nproxies:\n  - {name: A, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: p}\nrules:\n  - MATCH,PROXY\n")
	detection, err := Detect(payload)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detection.Core != model.CoreMihomo || detection.Format != model.FormatMihomoYAML {
		t.Fatalf("detection = %+v", detection)
	}
}

func TestDetectSingBoxJSON(t *testing.T) {
	payload := []byte(`{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}],"inbounds":[]}`)
	detection, err := Detect(payload)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detection.Core != model.CoreSingBox || detection.Format != model.FormatSingBoxJSON {
		t.Fatalf("detection = %+v", detection)
	}
}

func TestDetectNodeList(t *testing.T) {
	payload := []byte("ss://YWVzLTEyOC1nY206cA@1.2.3.4:8388#A\nvmess://eyJ2IjoiMiIsInBzIjoiQiIsImFkZCI6IjUuNi43LjgiLCJwb3J0IjoiNDQzIiwiaWQiOiJ1In0=\n")
	detection, err := Detect(payload)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detection.Format != model.FormatNodeList {
		t.Fatalf("detection = %+v", detection)
	}
	if detection.Core != "" {
		t.Errorf("node lists are core agnostic, got core %q", detection.Core)
	}
}

func TestDetectBase64NodeList(t *testing.T) {
	plain := "ss://YWVzLTEyOC1nY206cA@1.2.3.4:8388#A\ntrojan://pw@example.com:443#B\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	detection, err := Detect([]byte(encoded))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if detection.Format != model.FormatNodeList || detection.Encoding != model.EncodingBase64 {
		t.Fatalf("detection = %+v", detection)
	}
}

func TestDetectRejectsGarbage(t *testing.T) {
	if _, err := Detect([]byte("hello world, this is not a subscription")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Detect([]byte("   ")); err == nil {
		t.Fatal("expected an error for whitespace")
	}
}
