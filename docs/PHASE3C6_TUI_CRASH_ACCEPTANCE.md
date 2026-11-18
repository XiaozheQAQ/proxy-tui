# Phase 3C.6 — Interactive TUI Crash Recovery Acceptance

- Date: 2026-11-18 23:30 (Asia/Shanghai)
- Version: 0.2.0-dev
- Branch: `test/phase3c5-windows-live`
- Environment: Windows 11 26200, Administrator, Mihomo 1.19.34, sing-box 1.14.4
- Method: real TUI windows driven by injected keystrokes (settings page), then only the TUI
  PID terminated with `Stop-Process -Id` (no tree kill).

## Result summary

| Test | Mihomo | sing-box |
|---|---|---|
| TUI-started TUN | PASS | PASS |
| TUI kill: core continues | PASS | PASS |
| TUN interface survives TUI kill | PASS (Meta) | PASS (tun0) |
| Default route survives | PASS (198.18.0.2) | PASS (172.19.0.2) |
| Traffic after TUI kill | PASS (128.1.186.57) | PASS (152.69.205.105) |
| DNS after TUI kill | PASS | PASS |
| New TUI adopts core | PASS | PASS |
| No second core started | PASS | PASS |
| Persistent state identity | PASS | PASS |
| TUI-driven Disable | NOT TESTED (automation focus/navigation) | NOT TESTED |
| CLI Disable releases resources | PASS | PASS |
| Original environment restored | PASS | PASS |

## Mihomo sequence

1. TUI PID 18724 started TUN via Settings (6 → TUN row → Enter).
2. Core PID 23128, binary
   `...\pt-tun-live\data\cores\mihomo\1.19.34\mihomo.exe`, SHA-256
   `5A0231...CA147`, creation 23:13:41.
3. State file identity: core_pid matched, binary SHA matched, config SHA-256 length 64.
4. Killed only TUI 18724. Core 23128 stayed alive; Meta stayed up; default route stayed
   via 198.18.0.2; traffic 128.1.186.57; DNS resolved; state file remained.
5. New TUI 4104 adopted the same core (core count 1, same PID, state intact).
6. A second TUI was started concurrently: both TUIs ran, core count stayed 1, no second
   core. There is **no single-instance lock** (see findings).
7. CLI `network tun disable` released Meta and restored WLAN routing.

## sing-box sequence

1. TUI switched to the sing-box page (key 3) and Settings, then TUN was enabled from the
   TUI: core PID 37516, binary
   `...\pt-tun-live\data\cores\sing-box\1.14.4\sing-box.exe`, SHA-256
   `8F3068...E7C8B9`, creation 23:24:11.
2. State identity matched (PID, binary SHA, config SHA length 64); tun0 up, route via
   172.19.0.2, traffic 152.69.205.105, DNS resolved.
3. Killed only TUI 8888. Core 37516 stayed alive; tun0 and routing stayed; traffic
   152.69.205.105 continued; state file remained.
4. New TUI 29816 adopted the same core (one core instance).
5. CLI `network tun disable` released tun0 and restored WLAN routing.

## Findings and risks

1. **No single-instance coordination**: two ProxyTUI processes can run simultaneously.
   They share the same `tun-state.json` and core, and no duplicate core was started in this
   test, but concurrent actions could race. No lock file or IPC exists yet (that belongs to
   Phase 3D). Treated as a documented limitation, not a crash-recovery failure.
2. **TUI-driven Disable not proven**: keystroke automation could focus the TUI, but
   navigation/selection was not reliable enough to guarantee the Disable action; the CLI
   disable path was used to restore. This item is NOT TESTED, not a PASS.
3. **Automation side effect (incident)**: during repeated key injection the TUI toggled
   Traffic mode and enabled the WinINet proxy (ProxyEnable=1, server 127.0.0.1:10808), which
   pointed at a stopped core. This likely interrupted the previous agent turn's upstream
   stream. ProxyTUI correctly refused to clear a foreign proxy without a backup. Because the
   pre-test state was recorded as `ProxyEnable=0`, the flag was restored to 0 while keeping
   ProxyServer/ProxyOverride unchanged; the harness and direct egress were verified
   afterwards.
4. Core identity checks behave correctly under crash: PID + binary SHA-256 + config SHA-256
   all matched; startup recovery did not stop a running core, and a killed core was recovered
   on the next invocation.
5. Killing the TUI did not delete TUN state or modify routes/DNS.

## Final state

- No ProxyTUI TUI, no mihomo/sing-box process, no Meta/tun0 adapter, no `tun-state.json`.
- IPv4 default route: WLAN → 192.168.66.1; DNS: 192.168.66.1.
- WinINet ProxyEnable=0 (ProxyServer string preserved), WinHTTP Direct.
- Direct harness endpoint reachable; no third-party client was started or stopped by this
  round (v2rayN/Xray remained stopped as before the test).

## Verdict

Core/TUN survival across TUI crash and cross-process adoption: **PASS** for both cores.
TUI-driven Disable remains **NOT TESTED**; therefore the Phase 3C.6 acceptance is
**PARTIAL**, and Phase 3D should not start until an interactive Disable test is completed.
