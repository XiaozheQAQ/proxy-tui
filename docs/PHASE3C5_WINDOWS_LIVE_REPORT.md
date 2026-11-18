# Phase 3C.5 Windows Live TUN Acceptance Report

- Date: 2026-11-18 23:05 (Asia/Shanghai)
- Version: 0.2.0-dev
- Branch: `test/phase3c5-windows-live`
- Evidence: `%TEMP%\proxy-tui-tun-20261118-225700`
- Result: **Mihomo and sing-box real TUN tested on Windows; both PASS for the tested scope.**

## Environment

| Item | Value |
|---|---|
| Windows | Windows 11 专业工作站版 10.0.26200 build 26200 |
| Administrator | true |
| Mihomo | 1.19.34 (test home `%TEMP%\pt-tun-live`) |
| sing-box | 1.14.4 (test home) |
| Original egress | WLAN → 192.168.66.1; no IPv6 default route |
| DNS | WLAN DHCP 192.168.66.1 |
| WinINet | ProxyEnable=0; ProxyServer string retained `127.0.0.1:10808` |
| WinHTTP | Direct |
| Third-party | v2rayN/Xray stopped before this round; `xray_tun` absent |
| Direct internet | unavailable from this host; proxied access works |

Profiles: Mihomo `Real Upstream` (99 nodes), sing-box `Real sing-box` (5 nodes:
4 vless + 1 hysteria2). Both real `mihomo -t` / `sing-box check` passed.

## Core test matrix

| Test item | Mihomo 1.19.34 | sing-box 1.14.4 |
|---|---|---|
| Preflight | PASS (activation ready) | PASS (activation ready, 1 expected warning) |
| Activation Ready | YES | YES |
| Config Validation | PASS | PASS |
| TUN Adapter | PASS (`Meta`, ifIndex 60) | PASS (`tun0`, ifIndex 61) |
| Default Route | PASS (`198.18.0.2` via Meta) | PASS (`172.19.0.2` via tun0) |
| IPv4 | PASS (egress 128.1.186.57) | PASS (egress 152.69.205.105) |
| IPv6 | NOT APPLICABLE (no IPv6 default/stack; TUN ipv6=false) | NOT APPLICABLE |
| DNS | PASS (Meta DNS 198.18.0.2; A + TCP queries resolved) | PASS (tun0 DNS 172.19.0.2; A queries resolved) |
| TCP | PASS (HTTPS 200, twice) | PASS (HTTPS 200, twice) |
| UDP | PASS (DNS over UDP via TUN) | PASS (DNS over UDP via TUN) |
| QUIC | NOT TESTED (curl has no HTTP/3) | NOT TESTED |
| LAN | PASS (ping gateway) | PASS (ping gateway) |
| Normal Stop | PASS | PASS |
| Interface Released | PASS | PASS |
| TUI Crash Recovery | NOT TESTED (no interactive TUI in this harness) | NOT TESTED |
| Core Crash Recovery | PASS (killed owned PID; adapter disappeared; next invocation recovered) | PASS (same) |

### Mihomo details

- Enable: PASS, "TUN is active"; adapter Meta, default route via 198.18.0.2.
- `curl --noproxy "*"` (non-proxy-aware) → 200 with egress **128.1.186.57**, matching
  the plain-mode proxy egress (direct was unavailable), twice.
- DNS: Windows client resolved api.ipify.org A records and a TCP DNS query succeeded;
  Meta interface DNS was 198.18.0.2 (core-managed).
- LAN: ping 192.168.66.1 2/2 replies.
- Disable: PASS, "TUN disabled; core resources released"; Meta removed, default route back
  to WLAN, DNS restored, core stopped, no tun-state.

### sing-box details

- Enable: PASS; preflight warning that a DNS hijack rule was prepended (expected).
- Adapter tun0, default route via 172.19.0.2; tun0 DNS 172.19.0.2.
- `curl --noproxy "*"` → 200 with egress **152.69.205.105**, matching plain-mode proxy
  egress (Hysteria2 via URLTest auto), twice.
- DNS A queries resolved; LAN ping 2/2.
- Disable: PASS; tun0 removed, routes/DNS restored, core stopped, no tun-state.

### Crash recovery

- Mihomo: enabled TUN, killed only the ProxyTUI-owned mihomo PID 44804. Wintun adapter
  disappeared, routes returned to WLAN. The next ProxyTUI invocation performed startup
  recovery and cleared the state; explicit `recover` reported "nothing to recover".
- sing-box: same procedure with PID 1108; tun0 removed and state cleared by startup
  recovery.

### Defect found and fixed

Live testing exposed stale DNS from removed adapters: the Windows probe read every
TCP/IP interface registry subkey, so DNS of deleted TUN adapters (198.18.0.2,
172.19.0.2) and the old xray_tun (1.1.1.1, 8.8.8.8) appeared in preflight. Fixed
`windowsDNS` to consider only interfaces that currently exist, with a live regression test.
After the fix, preflight reports only the active WLAN DNS (192.168.66.1).

## Network state comparison

| Item | Before | Mihomo TUN | After Mihomo | sing-box TUN | After sing-box |
|---|---|---|---|---|---|
| IPv4 default | WLAN 192.168.66.1 | Meta 198.18.0.2 | WLAN | tun0 172.19.0.2 | WLAN |
| TUN adapter | none | Meta (60) | none | tun0 (61) | none |
| DNS (active) | WLAN 192.168.66.1 | + Meta 198.18.0.2 | WLAN | + tun0 172.19.0.2 | WLAN |
| WinINet | disabled | unchanged | unchanged | unchanged | unchanged |
| Core processes | 0 | 1 | 0 | 1 | 0 |
| TUN state file | none | yes | none | yes | none |

No route table flush, adapter deletion, DNS reset or third-party process stop was used.

## Judgment

1. Mihomo TUN really usable? **YES** (tested scope).
2. sing-box TUN really usable? **YES** (tested scope).
3. TCP / UDP normal? **YES**; QUIC not tested (no HTTP/3 client).
4. DNS leak? No leak observed: DNS was served through the TUN interface DNS and resolved;
   packet-capture-level proof was not performed.
5. IPv4 leak? No leak observed: non-proxy-aware traffic exited via the real node; direct
   egress was unavailable. IPv6: not applicable (no IPv6 default route, TUN IPv6 disabled).
6. Normal stop releases interfaces? **YES**, both cores.
7. TUI crash recovery? **NOT TESTED** (no interactive TUI in this harness; cross-process
   state adoption and startup recovery were exercised by separate CLI invocations).
8. Core crash recovery? **YES**, both cores (owned PID killed; recovered on next invocation).
9. Original environment restored? **YES**: WLAN default route, WLAN DNS, proxy disabled,
   xray_tun absent, no ProxyTUI processes or TUN state.
10. Phase 3D entry? Core/TUN acceptance passed except TUI-crash and QUIC coverage; recommend
    one interactive TUI crash test before formally starting Phase 3D.
