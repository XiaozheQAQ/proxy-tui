# Phase 3C.5 Windows Live TUN Acceptance Report

- Date: 2026-11-18 22:47 (Asia/Shanghai)
- Version: 0.2.0-dev
- Branch: `test/phase3c5-windows-live`
- Evidence: `%TEMP%\proxy-tui-upstream-20261118-224241`

## Current stage: upstream qualification (no TUN)

This round imported and tested real upstreams in plain mode. **No TUN was started, no route,
DNS or system proxy was modified.**

### Environment

| Item | Value |
|---|---|
| Windows | Windows 11 专业工作站版 10.0.26200 build 26200 |
| Administrator | **true** |
| Mihomo | 1.19.34 (test home `%TEMP%\pt-tun-live`) |
| sing-box | 1.14.4 (test home) |
| Original egress | WLAN → 192.168.66.1; no IPv6 default route |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 |
| WinINet | ProxyEnable=0; ProxyServer string retained `127.0.0.1:10808` |
| WinHTTP | Direct |
| xray_tun / v2rayN / Xray | absent / not running |
| Direct internet | **unavailable from this host** (curl to api.ipify.org times out); proxied access works |

### Mihomo real upstream result

- Import: real subscription from user's local file via stdin, **99 nodes**, Mihomo YAML,
  real `mihomo -t` passed. URL/token never printed or passed as a command-line argument.
- Activated as `Real Upstream`.
- Plain-mode start: `Mihomo RUNNING`, PID 40532, API 127.0.0.1:9090.
- Listeners: HTTP on 7890, SOCKS on 7891 (mixed-port bind logged a fallback error; both
  listeners belong to the same Mihomo PID).
- Real node egress:
  - HTTP proxy: `https://api.ipify.org` → HTTP 200, egress IP **128.1.186.57** (repeated twice).
  - SOCKS5 hostname: HTTP 200, same egress IP (proves proxy-side name resolution).
  - Clash API connection chain: `CF移动优选23 → 自动选择 → 节点选择 → 漏网之鱼`, selected
    node type `Vless` — not DIRECT.
  - Core DNS resolver: `/dns/query` for `api.ipify.org` returned A records (172.67.74.152,
    104.26.13.205, 104.26.12.205).
- Normal stop: `Mihomo stopped`; 0 mihomo processes; ports 7890/7891/9090 have no listeners.
- Cleanup: no TUN adapter, default route still WLAN, DNS unchanged, system proxy unchanged,
  no `tun-state.json`, no xray_tun.

Direct egress is unavailable in this network, so a "direct vs proxied IP differs" check
cannot be used as evidence; the SOCKS-hostname request plus the Clash connection chain and
the selected Vless node are the evidence that traffic traversed the real node.

### sing-box result

- The only provided sing-box input was an empty JSON file and a node file containing the
  same subscription URL. Importing that URL for sing-box stored a **Mihomo YAML** profile
  and correctly warned it is **not activatable for sing-box**.
- Per the rule against converting complex Mihomo YAML, the profile was deleted and
  sing-box remains **BLOCKED**: no native sing-box JSON or supported node URI list.
- No sing-box core was started; no listener or egress test was performed.

## Core test matrix

| Test item | Mihomo 1.19.34 | sing-box 1.14.4 |
|---|---|---|
| Profile Import | PASS (99 nodes, real) | BLOCKED (no native input; Mihomo YAML rejected) |
| Config Validation | PASS (real `mihomo -t`) | PASS for generated test config only; no profile |
| Core Start | PASS | NOT TESTED |
| Proxy Listener | PASS (HTTP 7890, SOCKS 7891) | NOT TESTED |
| Real Node Reachability | PASS (Vless node, HTTP 200) | NOT TESTED |
| TCP Egress | PASS (HTTP 200 twice, SOCKS 200) | NOT TESTED |
| DNS | PASS (core resolver via API); system-wide takeover N/A in plain mode | NOT TESTED |
| Normal Stop | PASS | NOT TESTED |
| Process Cleanup | PASS (0 processes, ports closed) | NOT TESTED |
| TUN Adapter / Route / Leak | NOT TESTED — TUN not started this round | NOT TESTED |

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

## Final judgment for this stage

1. Mihomo has a real, working upstream in plain mode: **YES**.
2. sing-box has a real, working upstream in plain mode: **NO (BLOCKED)**.
3. TUN acceptance has **not** started and is not requested yet: the two-core upstream
   prerequisite is only half met.
4. Direct internet is unavailable on this host; future TUN leak tests must account for that.

## Next input needed

A native sing-box configuration or a supported node URI list (not a Mihomo YAML
subscription), placed at `%TEMP%\proxy-tui-singbox.json` or
`%TEMP%\proxy-tui-nodes.txt`. After sing-box passes the same plain-mode tests, the TUN
phase can be proposed with explicit authorization.

The temporary subscription input files were removed after import to reduce secret exposure.
