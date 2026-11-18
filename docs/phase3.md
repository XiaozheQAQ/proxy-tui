# Phase 3 — Network takeover

Phase 3 turns ProxyTUI from "a TUI that can run a core" into "a client that owns the
machine's traffic". It is split into four sub-phases; **3A, 3B and the first 3C slice are
implemented**, 3D is designed but not built.

| Sub-phase | Scope | State |
|---|---|---|
| 3A | Unified network state manager: snapshot, transaction, rollback, recovery | implemented |
| 3B | Desktop system proxy: Windows WinINet, macOS networksetup, Linux GNOME GSettings | implemented |
| 3C | Core-native TUN: config rendering, preflight, lifecycle, recovery | implemented, activation needs Administrator/root |
| 3D | Proxy Supervisor: autostart, IPC, crash recovery | not implemented |

## Traffic takeover modes

Exactly one mode is active at a time. Enabling one mode disables the other.

| Mode | Behaviour |
|---|---|
| Off | Nothing is taken over; desktop proxy and TUN are released |
| System Proxy | Desktop HTTP/SOCKS proxy points at the local core |
| TUN | The core creates a TUN device and takes over routes/DNS with its own implementation |
| Manual | The user configures their own client; ProxyTUI stays out of the way |

The desktop proxy, the shell environment proxy and TUN are three different mechanisms and
are shown separately. ProxyTUI never edits shell variables and never writes routes itself.

## 3A — the transaction model (desktop proxy)

Every desktop-proxy change follows the same sequence. The snapshot is written to disk
**before** the system is touched, so even a hard crash leaves enough information to restore:

```text
read current state → snapshot → write journal → apply → verify
                                                    ├── success → commit backup, remove journal
                                                    └── failure → restore snapshot, journal rolled-back
```

Files: `state/runtime/network-journal.json` (in-flight) and `network-backup.json`
(committed original + installed config). Recovery restores an interrupted transaction
immediately and only restores a committed backup when the proxy still points at the address
ProxyTUI installed and no core is running.

## 3B — desktop system proxy

| Platform | Mechanism | Notes |
|---|---|---|
| Windows | WinINet registry, current user | `ProxyEnable`, `ProxyServer`, `ProxyOverride`, `InternetSetOption` refresh |
| macOS | `networksetup` | all enabled network services, web/secure/socks, bypass domains |
| Linux (GNOME) | `gsettings org.gnome.system.proxy` | mode, http/https/socks, ignore-hosts |

Foreign proxy detection reports a desktop proxy owned by another client and still backs up
and restores it. Enabling the system proxy requires a running core.

## 3C — Core-native TUN (implemented slice)

TUN is delegated to the two cores. ProxyTUI produces their native configuration, runs a
cross-platform preflight, drives the lifecycle and verifies the result. It does **not**
create devices, write routes or run nftables.

### Configuration rendering

* `internal/tun` has one core-agnostic `Settings` model and two renderers.
* **Mihomo**: writes `tun.enable`, `stack`, `mtu`, `auto-route`, `auto-detect-interface`,
  `strict-route` and (optional) `dns-hijack`. Existing DNS, proxies, groups, rules and
  unknown TUN fields are preserved. Overridden fields are reported to the user.
* **sing-box 1.11+**: adds or patches a `type: tun` inbound (`address`, `auto_route`,
  `strict_route`, `stack`, `mtu`), sets `route.auto_detect_interface` and prepends one
  `{protocol: dns, action: hijack-dns}` rule when DNS hijack is on. Existing DNS, route
  rules, outbounds and unknown TUN fields are preserved.
* The source profile files are never modified; the rendered runtime config stays in
  `state/runtime`.
* Rendered configs are validated with the real binaries (`mihomo -t` / `sing-box check`)
  before anything is started; a rejected config never replaces a working one.
* Minimum versions: Mihomo 1.18.0, sing-box 1.11.0 (1.14.4 is the verified baseline).

### Preflight

`proxy network tun preflight` and the TUI report every check with a level, a reason and an
actionable hint:

```text
READY / WARNING / BLOCKED / UNKNOWN
```

Checks: operating system, WSL, privileges, core and version, active profile, default egress,
IPv4, IPv6 agreement, DNS servers, existing TUN devices, VPN interfaces, firewall state,
Linux nftables, route conflicts, and profile TUN overrides. Anything that cannot be observed
is reported as UNKNOWN instead of pretending success.

### Lifecycle

* **Enable / change**: render → core validation → preflight (BLOCKED refuses) → stop the
  core if needed → baseline snapshot → start with the TUN config → wait for the new
  interface → verify route/DNS observations → persist `tun-state.json`.
* **Failure**: if the core fails to start, the plain configuration is restarted and no TUN
  state is written.
* **Disable**: stop the core → wait until every interface it created is gone → restart the
  plain config if the core was running before TUN → remove the state.
* **Abnormal exit**: `proxy network tun recover` (and startup recovery) stops a stale core,
  verifies interface release and reports leftovers with manual steps. ProxyTUI never deletes
  routes itself.
* Status reports the interface, outbound interface, DNS mode, routing state and degraded
  conditions across processes (state file + live interface observations).

### 3C.5 hardening and Windows live acceptance

The first Windows acceptance run (2026-11-18) was executed in a **non-elevated Console
session** with an existing third-party TUN (`xray_tun`), a running `v2rayN/xray` pair and an
enabled desktop proxy (`127.0.0.1:10808`). Per the acceptance discipline, the real TUN
activation was **not** attempted.

Hardening added after the audit:

* `tun-state.json` now records core identity (`core_pid`, `core_started_at`, `core_binary`)
  and a rendered-config digest. `Status/Disable/Recover` require an identity match; a
  legacy state without a pid is treated as "not owned".
* Preflight runs **before** stopping a working core; BLOCKED returns without touching it.
* A previous TUN must be provably released before a new TUN is started.
* The TUN lifecycle is serialised with a mutex; `Disable` restarts the plain config only
  after the interfaces are released.
* A stale state pointing at a foreign interface is kept and reported, never "cleaned" by
  deleting the interface or stopping an unrelated core.
* TUN warns when another desktop proxy is active (it never edits it), and preflight gained a
  **System proxy** check.

Verified in this run:

| Evidence | Result |
|---|---|
| Rendered Mihomo TUN config accepted by real `mihomo 1.19.34 -t` | PASS |
| Rendered sing-box TUN config accepted by real `sing-box 1.14.4 check` | PASS |
| Preflight for both cores (privileges BLOCKED, xray_tun/route/system-proxy warnings) | PASS |
| Blocked `network tun enable` changes nothing (no state, no core, proxy untouched) | PASS |
| Forged TUN state targeting `xray_tun`: recovery keeps state, stops nothing, deletes nothing | PASS |
| Original baseline hashes after the run | unchanged |

### 3C.5 final safety hardening

Preflight now distinguishes **completion** from **activation approval**:

* `preflight: completed=…` and `activation ready: yes|no` are separate outputs.
* `blocking issues`, `warnings` and `unknown facts` are listed before the checks.
* Any BLOCKED check or safety-related UNKNOWN refuses activation; warnings do not.
* `network tun enable` uses the same gate as preflight, so it cannot be bypassed by
  Administrator rights.

Conflict policy:

* External TUN carrying the IPv4 or IPv6 default route → **BLOCKED** (ownership is decided
  from the recorded TUN state plus core identity, never guessed from a name).
* Recorded state that no longer matches a running core → its adapter is BLOCKED if it
  carries the default route, UNKNOWN otherwise.
* Idle external TUN → WARNING. Unrecognised virtual adapter → UNKNOWN; carrying the
  default route → BLOCKED.
* WinINet proxy, WinHTTP proxy and external proxy processes are separate checks and stay
  warnings; ProxyTUI never clears them. A configured proxy with no listener is labelled as
  stale.

Evidence from the live host: `xray_tun` (IPv4 default route) → BLOCKED, WinINet
`127.0.0.1:10808` → WARNING, `v2rayN/xray` → WARNING, VirtualBox host-only adapter →
UNKNOWN, `preflight completed=true`, `activation ready=no`.

## Validation matrix (honest levels)

| Area | Unit tests | Simulated | WSL | Windows real | Linux native | macOS real |
|---|---|---|---|---|---|---|
| Mihomo TUN rendering | yes | — | yes (build/tests) | yes (config accepted by mihomo 1.19.34) | not run | not run |
| sing-box TUN rendering | yes | — | yes (build/tests) | yes (config accepted by sing-box 1.14.4) | not run | not run |
| Preflight checks | yes | fake facts | yes (WSL/non-root/nft missing detected) | yes (non-elevated, xray_tun and route conflict detected) | not run | not run |
| Lifecycle (start/stop/recover) | yes | fake cores/facts | yes (tests) | **not run** — needs Administrator | not run | not run |
| Real traffic through TUN | — | — | — | **not run** (no elevation) | not run | not run |
| DNS hijack on a real TUN | — | — | — | **not run** | not run | not run |
| IPv4/IPv6 leak tests | — | — | — | **not run** | not run | not run |

This iteration deliberately did **not** claim real TUN activation: the development session is
not elevated and the host already runs another TUN client (`xray_tun`). Enabling a TUN would
have changed the machine's routing, which is out of scope for an automated agent without
explicit approval. `proxy network tun enable` correctly stops at the privilege check.

## 3D — Proxy Supervisor (planned)

Today cores are children of the `proxy` process. A started core keeps running when the TUI
exits and a later process can adopt it through `<core>-state.json`. That is **cross-session
process adoption, not IPC**: there is no socket, no daemon and no autostart.

The Supervisor will be the same binary in an internal mode, exposing a Unix socket / named
pipe with start/stop/status and autostart at login, plus core and network state recovery
after reboot.

## Acceptance criteria for Phase 3

| Test | Must satisfy | Status |
|---|---|---|
| Enable system proxy | real desktop setting changes | verified on Windows (live) and Linux GNOME (real dconf) |
| Disable system proxy | exact previous settings restored | verified on Windows (live) and Linux GNOME |
| Enable TUN | real traffic passes through the core | **BLOCKED**: non-elevated session + existing third-party TUN; needs an authorized admin run |
| DNS takeover | DNS follows the configured policy | only config rendering verified |
| IPv4 / IPv6 | no unintended leaks | pending real TUN run |
| LAN access | follows the bypass policy | desktop proxy bypass verified; TUN pending |
| Core crash | no unusable network config left | lifecycle recovery covered by unit tests |
| Forced TUI exit | a later process detects and repairs | cross-process status/adoption covered by tests |
| System reboot | no stale proxy state remains | recovery logic covered by tests, not reboot-tested |
| Failed config update | previous working config keeps serving | covered by subscription and TUN tests |
| TUN in Docker/VMs, Wi-Fi/ethernet switching | behaves correctly | pending |

## Platform testing strategy

* **Windows**: rendered TUN configs validated by the real core binaries; preflight run on the
  real host (correctly BLOCKED for privileges, warning for `xray_tun` and its default route).
  Real activation requires an elevated terminal and is left to a deliberate manual run.
* **Linux**: full test suite and preflight in WSL; WSL cannot validate native nftables,
  policy routing or TUN behaviour, so those remain unverified.
* **macOS**: builds and unit tests only; TUN remains unofficial until run on real hardware.
