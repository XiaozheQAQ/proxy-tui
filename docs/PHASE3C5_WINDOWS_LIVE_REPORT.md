# Phase 3C.5 Windows Live TUN Acceptance Report

- Date: 2026-11-18 22:54 (Asia/Shanghai)
- Version: 0.2.0-dev
- Branch: `test/phase3c5-windows-live`
- Evidence: `%TEMP%\proxy-tui-upstream-20261118-224241` (Mihomo),
  `%TEMP%\proxy-tui-singbox-20261118-225156` (sing-box)
- Stage: **upstream qualification complete for both cores; TUN not started.**

## Environment

| Item | Value |
|---|---|
| Windows | Windows 11 专业工作站版 10.0.26200 build 26200 |
| Administrator | true |
| Mihomo | 1.19.34 (test home `%TEMP%\pt-tun-live`) |
| sing-box | 1.14.4 (test home) |
| Egress | WLAN → 192.168.66.1; no IPv6 default route |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 |
| WinINet | ProxyEnable=0; ProxyServer string retained `127.0.0.1:10808` |
| WinHTTP | Direct |
| Third-party | v2rayN/Xray not running; `xray_tun` absent |
| Direct internet | **unavailable** from this host; proxied access works |

## Mihomo 1.19.34

- Real subscription imported via stdin from the user's local file: **99 nodes**, Mihomo YAML;
  URL/token never printed or passed as a command-line argument.
- Explicit `mihomo -t` on the stored active config: **exit 0**.
- Plain-mode start: RUNNING, PID 40532, API 127.0.0.1:9090.
- Listeners: HTTP `7890`, SOCKS `7891` (both owned by this Mihomo process; the mixed-port
  bind logged a fallback error, so the ports are reported as observed).
- Real egress:
  - HTTP proxy → `https://api.ipify.org`: **200**, egress IP **128.1.186.57** (twice).
  - SOCKS5 hostname → **200**, same IP (proxy-side DNS resolution).
  - Clash chain: `CF移动优选23 → 自动选择 → 节点选择 → 漏网之鱼`, selected node **Vless**.
  - Core DNS `/dns/query` returned A records for `api.ipify.org`.
- Normal stop: PASS; 0 mihomo processes; 7890/7891/9090 closed.
- No TUN, route, DNS or system-proxy change.

## sing-box 1.14.4

- User-provided node URI list: **5 nodes** (4 vless, 1 hysteria2), imported as a node list.
- Explicit `sing-box check` on the stored active config: **exit 0**.
- Plain-mode start: RUNNING, PID 38348, API 127.0.0.1:9090.
- Listener: mixed `127.0.0.1:7890`.
- Real egress:
  - HTTP proxy → `https://api.ipify.org`: **200**, egress IP **152.69.205.105** (twice).
  - SOCKS5 hostname → **200**, same IP.
  - Clash API: selector `proxy → auto`, URLTest `auto → Hysteria2`.
- Normal stop: PASS; 0 sing-box processes; 7890/9090 closed.
- No TUN, route, DNS or system-proxy change.

## Core test matrix

| Test item | Mihomo 1.19.34 | sing-box 1.14.4 |
|---|---|---|
| Profile Import | PASS (99 nodes) | PASS (5 nodes) |
| Config Validation (real binary) | PASS | PASS |
| Core Start | PASS | PASS |
| Proxy Listener | PASS (7890/7891) | PASS (7890) |
| Real Node Reachability | PASS (Vless) | PASS (Hysteria2 via URLTest) |
| TCP Egress | PASS (HTTP 200 ×2) | PASS (HTTP 200 ×2) |
| DNS | PASS (core resolver); system takeover N/A in plain mode | PASS (SOCKS hostname; core resolves) |
| Normal Stop | PASS | PASS |
| Process Cleanup | PASS | PASS |
| TUN / Route / Leak | NOT TESTED (TUN not started) | NOT TESTED |

## Network state comparison

| Item | Before | After |
|---|---|---|
| IPv4 default | WLAN → 192.168.66.1 | unchanged |
| IPv6 default | none | unchanged |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 | unchanged |
| WinINet proxy | disabled (string retained) | unchanged |
| WinHTTP | Direct | unchanged |
| TUN adapters | 0 | 0 |
| ProxyTUI TUN state | none | none |
| ProxyTUI core processes | 0 | 0 |

## Judgment

1. Mihomo has a real working upstream in plain mode: **YES**.
2. sing-box has a real working upstream in plain mode: **YES**.
3. TUN has **not** been tested; no route/DNS takeover occurred.
4. Direct internet is unavailable on this host, so TUN leak tests will be evaluated against
   the proxied path and the preserved physical route, not against a direct egress baseline.

## Proposed next stage (requires explicit authorization)

TUN acceptance for Mihomo first, then restore, then sing-box, then crash tests. Planned
checks: preflight, TUN adapter, default route via TUN, IPv4 through the real node, IPv6
(host has none), DNS hijack/leak evidence, TCP/UDP/QUIC where tooling permits, LAN, normal
stop/interface release, TUI/core crash recovery, and final restoration of the original
proxy client state. No TUN will be started without explicit confirmation.
