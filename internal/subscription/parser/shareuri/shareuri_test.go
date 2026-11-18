package shareuri

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestParseSS_SIP002(t *testing.T) {
	link := "ss://" + b64("aes-256-gcm:secret") + "@1.2.3.4:8388#Tokyo"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Protocol != "ss" {
		t.Errorf("protocol = %q, want ss", node.Protocol)
	}
	if node.Method != "aes-256-gcm" {
		t.Errorf("method = %q", node.Method)
	}
	if node.Password != "secret" {
		t.Errorf("password = %q", node.Password)
	}
	if node.Server != "1.2.3.4" || node.Port != 8388 {
		t.Errorf("server = %s:%d", node.Server, node.Port)
	}
	if node.Name != "Tokyo" {
		t.Errorf("name = %q", node.Name)
	}
}

func TestParseSS_LegacyBase64AndPlugin(t *testing.T) {
	link := "ss://" + b64("chacha20-ietf-poly1305:pw@example.com:443") + "?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dcdn.example.com#Legacy"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Server != "example.com" || node.Port != 443 {
		t.Errorf("server = %s:%d", node.Server, node.Port)
	}
	if node.Method != "chacha20-ietf-poly1305" || node.Password != "pw" {
		t.Errorf("credentials = %q/%q", node.Method, node.Password)
	}
	if node.Plugin != "obfs-local" {
		t.Errorf("plugin = %q", node.Plugin)
	}
	if node.PluginOpts["obfs"] != "http" || node.PluginOpts["obfs-host"] != "cdn.example.com" {
		t.Errorf("plugin opts = %#v", node.PluginOpts)
	}
}

func TestParseVMess(t *testing.T) {
	payload := `{"v":"2","ps":"HK-1","add":"5.6.7.8","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","type":"none","host":"cdn.example.com","path":"/ws","tls":"tls","sni":"cdn.example.com"}`
	link := "vmess://" + b64(payload)
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Name != "HK-1" || node.Server != "5.6.7.8" || node.Port != 443 {
		t.Errorf("node = %+v", node)
	}
	if node.UUID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("uuid = %q", node.UUID)
	}
	if !node.TLS || node.Network != "ws" || node.Path != "/ws" || node.Host != "cdn.example.com" {
		t.Errorf("transport = %+v", node)
	}
}

func TestParseVLESSReality(t *testing.T) {
	link := "vless://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@1.2.3.4:443?encryption=none&security=reality&sni=www.example.com&fp=chrome&pbk=PUBKEY&sid=abcd&type=grpc&serviceName=grpcsvc&flow=xtls-rprx-vision#Reality"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Protocol != "vless" || !node.TLS {
		t.Errorf("protocol/tls = %q/%v", node.Protocol, node.TLS)
	}
	if node.RealityPublicKey != "PUBKEY" || node.RealityShortID != "abcd" {
		t.Errorf("reality = %q/%q", node.RealityPublicKey, node.RealityShortID)
	}
	if node.Network != "grpc" || node.ServiceName != "grpcsvc" {
		t.Errorf("network = %q service = %q", node.Network, node.ServiceName)
	}
	if node.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %q", node.Flow)
	}
	if node.Name != "Reality" {
		t.Errorf("name = %q", node.Name)
	}
}

func TestParseTrojan(t *testing.T) {
	link := "trojan://p%40ss@example.com:443?sni=example.com&type=ws&path=%2Ftrojan&host=cdn.example.com#TrojanWS"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Password != "p@ss" {
		t.Errorf("password = %q", node.Password)
	}
	if node.Network != "ws" || node.Path != "/trojan" || node.Host != "cdn.example.com" {
		t.Errorf("transport = %+v", node)
	}
	if !node.TLS {
		t.Error("trojan must default to TLS")
	}
}

func TestParseHysteria2(t *testing.T) {
	link := "hysteria2://secret@1.2.3.4:8443?sni=example.com&insecure=1&obfs=salamander&obfs-password=xyz&up=50&down=200#HY2"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Protocol != "hysteria2" || node.Password != "secret" {
		t.Errorf("node = %+v", node)
	}
	if node.Obfs != "salamander" || node.ObfsPassword != "xyz" {
		t.Errorf("obfs = %q/%q", node.Obfs, node.ObfsPassword)
	}
	if node.UpMbps != 50 || node.DownMbps != 200 {
		t.Errorf("bandwidth = %d/%d", node.UpMbps, node.DownMbps)
	}
	if !node.Insecure {
		t.Error("insecure should be parsed")
	}
}

func TestParseTUIC(t *testing.T) {
	link := "tuic://uuid-1:pass@1.2.3.4:443?congestion_control=bbr&alpn=h3&sni=example.com&udp_relay_mode=native#TUIC"
	node, err := ParseLine(link)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if node.Protocol != "tuic" || node.UUID != "uuid-1" || node.Password != "pass" {
		t.Errorf("node = %+v", node)
	}
	if len(node.ALPN) != 1 || node.ALPN[0] != "h3" {
		t.Errorf("alpn = %#v", node.ALPN)
	}
	if node.Extra["udp_relay_mode"] != "native" {
		t.Errorf("extra = %#v", node.Extra)
	}
}

func TestParseSocksAndHTTP(t *testing.T) {
	socks, err := ParseLine("socks5://user:pass@127.0.0.1:1080#Socks")
	if err != nil {
		t.Fatalf("socks: %v", err)
	}
	if socks.Protocol != "socks5" || socks.Username != "user" || socks.Password != "pass" {
		t.Errorf("socks = %+v", socks)
	}

	httpProxy, err := ParseLine("http://user:pass@10.0.0.1:8080#HTTP")
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	if httpProxy.Protocol != "http" || httpProxy.Port != 8080 || httpProxy.TLS {
		t.Errorf("http = %+v", httpProxy)
	}

	httpsProxy, err := ParseLine("https://user:pass@10.0.0.2:8443#HTTPS")
	if err != nil {
		t.Fatalf("https: %v", err)
	}
	if !httpsProxy.TLS {
		t.Error("https proxy must be tls")
	}
}

func TestLooksLikeNodeURI(t *testing.T) {
	cases := map[string]bool{
		"ss://abc@1.2.3.4:443#name":                             true,
		"vmess://eyJ2IjoiMiJ9":                                  true,
		"vless://uuid@host:443?type=ws":                         true,
		"trojan://pass@host:443":                                true,
		"hysteria2://pass@host:443":                             true,
		"tuic://uuid:pass@host:443":                             true,
		"socks5://user:pass@host:1080":                          true,
		"http://user:pass@1.2.3.4:8080":                         true,
		"https://example.com/api/v1/client/subscribe?token=abc": false,
		"https://sub.example.com/sub":                           false,
		"http://1.2.3.4:8080":                                   true,
		"wireguard://key@host:51820":                            false,
		"not a link":                                            false,
	}
	for input, want := range cases {
		if got := LooksLikeNodeURI(input); got != want {
			t.Errorf("LooksLikeNodeURI(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseLinesReportsUnsupported(t *testing.T) {
	blob := strings.Join([]string{
		"ss://" + b64("aes-128-gcm:pw") + "@1.2.3.4:8388#OK",
		"wireguard://something",
		"",
	}, "\n")
	nodes, unsupported, err := ParseLines(blob)
	if err != nil {
		t.Fatalf("ParseLines: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if len(unsupported) != 1 {
		t.Fatalf("unsupported = %d, want 1: %#v", len(unsupported), unsupported)
	}
	if strings.Contains(unsupported[0].Sample, "something") {
		t.Errorf("unsupported sample leaks the raw link: %q", unsupported[0].Sample)
	}
}

func TestParseLinesRejectsEmpty(t *testing.T) {
	if _, _, err := ParseLines("   \n\n"); err == nil {
		t.Fatal("expected an error for empty input")
	}
}
