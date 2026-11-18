# Phase 3C.5 Windows Live TUN Acceptance Report

- Date: 2026-11-18 21:48 (Asia/Shanghai)
- Version: 0.2.0-dev
- HEAD: f1166c9 (branch `test/phase3c5-windows-live`)
- Evidence: `%TEMP%\proxy-tui-live-20261118-214756`
- Result: **BLOCKED — live TUN activation was not executed.** The session is not elevated
  and no real user profile is available.

## Environment

| Item | Value |
|---|---|
| Windows | Microsoft Windows 11 专业工作站版 10.0.26200 build 26200 |
| Computer | LENOVO / 83F5 |
| User / session | 喝彩城堡\35646 / Console |
| Administrator | **false** |
| Go | go1.27.4 windows/amd64 (F:\tools\golang) |
| Mihomo | 1.19.34 (test home `%TEMP%\pt-tun-live`) |
| sing-box | 1.14.4 (test home) |
| Original egress | WLAN (ifIndex 20 → 192.168.66.1) |
| IPv4 default | WLAN only; no secondary TUN default route |
| IPv6 default | none |
| DNS | 1.1.1.1, 192.168.66.1, 8.8.8.8 (registry) |
| WinINet proxy | ProxyEnable=0; ProxyServer string retained `127.0.0.1:10808` |
| WinHTTP | Direct |
| Historical third-party | v2rayN/Xray not running; `xray_tun` absent (checked this run) |
| Host-only | `以太网 2` (VirtualBox, ifIndex 8) classified HOST_ONLY |

## Phase results

- Git: worktree clean; HEAD f1166c9; created branch `test/phase3c5-windows-live`.
- `gofmt -l .` empty; `go vet ./...` clean; `go test ./...` all packages pass.
- Baseline seven files re-read; SHA-256 unchanged after the run.
- No route, DNS, proxy, adapter or TUN state was modified.

## Core test matrix

| Test item | Mihomo 1.19.34 | sing-box 1.14.4 |
|---|---|---|
| Preflight | PASS (completed) | PASS (completed) |
| Activation Ready | **NO** (privileges) | **NO** (privileges + no profile) |
| Config Validation | PASS (real `mihomo -t`) | PASS (real `sing-box check`) |
| TUN Adapter | BLOCKED (not activated) | BLOCKED (not activated) |
| Default Route takeover | BLOCKED | BLOCKED |
| IPv4 traffic | BLOCKED | BLOCKED |
| IPv6 traffic | NOT TESTED (host has no IPv6 default route) | NOT TESTED |
| DNS hijack / leak check | BLOCKED | BLOCKED |
| TCP | BLOCKED | BLOCKED |
| UDP / QUIC | BLOCKED | BLOCKED |
| LAN access | BLOCKED | BLOCKED |
| Normal Stop | BLOCKED (not started) | BLOCKED (not started) |
| Interface Released | BLOCKED | BLOCKED |
| TUI Crash Recovery | NOT TESTED | NOT TESTED |
| Core Crash Recovery | NOT TESTED | NOT TESTED |

Preflight counts: Mihomo 19 READY / 0 WARNING / 1 BLOCKED / 0 UNKNOWN; sing-box 18 READY /
0 WARNING / 2 BLOCKED / 0 UNKNOWN.

Blocking issues:

1. **Privileges** — the agent process is not elevated; creating a Wintun adapter requires
   Administrator. This cannot be bypassed from a normal process.
2. **Active profile (sing-box)** — no sing-box profile exists.
3. **Real traffic profile (both)** — the only Mihomo profile in the test home
   (`TUN Live`) is a synthetic verification profile with an unreachable node
   (server 1.2.3.4). The default user home has no profiles. Starting TUN with it would take
   over routes/DNS without providing real proxy traffic, so live traffic/DNS testing would
   still be meaningless. A valid user subscription/profile is required.

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

## Final judgment

1. Mihomo TUN really usable? **NOT VERIFIED** (BLOCKED: non-admin, no real profile).
2. sing-box TUN really usable? **NOT VERIFIED** (BLOCKED: non-admin, no profile).
3. TCP / UDP normal? **NOT TESTED**.
4. DNS leak? **NOT TESTED**.
5. IPv4 / IPv6 leak? **NOT TESTED**; host has no IPv6 default route.
6. Normal stop releases interfaces? **NOT TESTED** (logic only unit-tested previously).
7. TUI crash recovery? **NOT TESTED**.
8. Core crash recovery? **NOT TESTED**.
9. Original environment restored? **No changes were made**, environment already clean.
10. Phase 3D entry? **NOT MET**.

## Required to continue

1. Restart the agent/terminal **elevated** (Run as administrator) with this repository and
   `PROXY_TUI_HOME` set as intended.
2. Import a **valid user subscription** (or provide a real profile) without exposing tokens;
   do not use the synthetic `TUN Live` node for traffic acceptance.
3. Explicitly authorize the temporary network takeover (TUN + default route + DNS), and
   confirm that no remote-management connection depends on this machine.
4. Then run, in order: Mihomo preflight → enable → adapter/route/IPv4/IPv6/DNS/TCP/UDP/QUIC/
   LAN checks → disable → verify release → restore; repeat for sing-box; then crash tests.

No real TUN was started in this round.
