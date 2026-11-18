# Phase 3C.5 Windows Live TUN Acceptance Report

- Date: 2026-11-18 21:52 (Asia/Shanghai)
- Version: 0.2.0-dev
- HEAD: f1166c9 + report commit; branch `test/phase3c5-windows-live`
- Evidence: `%TEMP%\proxy-tui-live-20261118-215206`
- Result: **BLOCKED — no valid upstream profile; TUN was not started.**
  The session is now elevated, preflight for Mihomo is activation-ready, but the only
  available profile contains a synthetic, unreachable node.

## Environment

| Item | Value |
|---|---|
| Windows | Microsoft Windows 11 专业工作站版 10.0.26200 build 26200 |
| Computer | LENOVO / 83F5 |
| User / session | 喝彩城堡\35646 |
| Administrator | **true** (verified in the elevated run) |
| Go | go1.27.4 windows/amd64 |
| Mihomo | 1.19.34 (test home `%TEMP%\pt-tun-live`) |
| sing-box | 1.14.4 (test home) |
| Original egress | WLAN (ifIndex 20 → 192.168.66.1) |
| IPv4 default | WLAN only |
| IPv6 default | none |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 |
| WinINet proxy | ProxyEnable=0; ProxyServer string retained `127.0.0.1:10808` |
| WinHTTP | Direct |
| Third-party | v2rayN/Xray not running; `xray_tun` absent |
| Host-only | `以太网 2` (VirtualBox) classified HOST_ONLY |

## Attempts

1. Non-elevated run: preflight BLOCKED on privileges; no activation.
2. Elevated run (this report): preflight **activation ready: yes** for Mihomo
   (20 READY / 0 WARNING / 0 BLOCKED / 0 UNKNOWN); sing-box still BLOCKED on missing
   profile. Profile reachability check: `1.2.3.4:8388` TCP **unreachable**.
   The only Mihomo profile (`TUN Live`) is a synthetic verification fixture; starting TUN
   with it would take over the default route and DNS without a working upstream. Per the
   acceptance rule "stop when no valid profile exists", **no TUN was started**.

## Core test matrix

| Test item | Mihomo 1.19.34 | sing-box 1.14.4 |
|---|---|---|
| Preflight | PASS (completed) | PASS (completed) |
| Activation Ready | **YES** (20/20 READY) | **NO** (no profile) |
| Config Validation | PASS (real `mihomo -t`) | PASS (real `sing-box check`) |
| Profile reachability | **FAIL** (synthetic node unreachable) | NOT APPLICABLE (no profile) |
| TUN Adapter | BLOCKED (not started) | BLOCKED |
| Default Route takeover | BLOCKED | BLOCKED |
| IPv4 | BLOCKED | BLOCKED |
| IPv6 | NOT TESTED (no IPv6 default route) | NOT TESTED |
| DNS | BLOCKED | BLOCKED |
| TCP | BLOCKED | BLOCKED |
| UDP / QUIC | BLOCKED | BLOCKED |
| LAN | BLOCKED | BLOCKED |
| Normal Stop | BLOCKED (not started) | BLOCKED |
| Interface Released | BLOCKED | BLOCKED |
| TUI Crash Recovery | NOT TESTED | NOT TESTED |
| Core Crash Recovery | NOT TESTED | NOT TESTED |

## Network state comparison

| Item | Before | Final |
|---|---|---|
| IPv4 default | WLAN → 192.168.66.1 | unchanged |
| IPv6 default | none | unchanged |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 | unchanged |
| WinINet proxy | disabled (string retained) | unchanged |
| WinHTTP | Direct | unchanged |
| xray_tun | absent | absent |
| ProxyTUI TUN state | none | none |
| ProxyTUI core processes | none | none |

No TUN activation, route, DNS, proxy or adapter changes were made in either attempt.

## Final judgment

1. Mihomo TUN really usable? **NOT VERIFIED** — activation-ready mechanics passed, but the
   only profile is a synthetic unreachable node, so live traffic was not tested.
2. sing-box TUN really usable? **NOT VERIFIED** — no profile.
3. TCP / UDP normal? **NOT TESTED**.
4. DNS leak? **NOT TESTED**.
5. IPv4 / IPv6 leak? **NOT TESTED**; host has no IPv6 default route.
6. Normal stop releases interfaces? **NOT TESTED** (not started).
7. TUI crash recovery? **NOT TESTED**.
8. Core crash recovery? **NOT TESTED**.
9. Original environment restored? **No changes were made**; environment remains clean.
10. Phase 3D entry? **NOT MET**.

## Required to continue

1. Import a real, working subscription/profile on this machine (do not paste tokens into
   the conversation):
   `proxy profile add --core mihomo --name <name> <subscription-url>`
   then `proxy profile use <id>`; repeat for `--core sing-box` with a sing-box-compatible
   source.
2. Confirm the node works without TUN first (start the core normally and test through the
   mixed port), so the live run does not blackhole the default route.
3. Then authorize the TUN takeover. With that, the planned sequence is:
   Mihomo preflight → enable → adapters/routes → IPv4/IPv6 → DNS capture → TCP/UDP/QUIC/LAN →
   disable → verify release; repeat independently for sing-box; then TUI/core crash tests;
   restore the original proxy client and settings.
