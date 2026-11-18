# Platform support

ProxyTUI builds for Windows, Linux and macOS. Everything below is either implemented and
exercised, or explicitly marked as not implemented.

## Feature matrix

| Feature | Windows | Linux | macOS |
|---|---|---|---|
| TUI, subscription import | yes | yes | yes (built; TUI tests run on all OS in CI) |
| Core install / start / stop | yes | yes | yes (built) |
| Proxy groups, node switch, latency | yes | yes | yes (built) |
| Connections and throughput | yes | yes | yes (built) |
| Core adoption after TUI exit | yes | yes | yes (built) |
| Desktop system proxy | yes (WinINet) | yes (GNOME GSettings) | yes (networksetup) |
| System proxy backup / restore / recovery | yes | yes | yes (built) |
| Foreign proxy conflict detection | yes | yes | yes (built) |
| Core-native TUN (render + preflight + lifecycle) | preflight/config validation PASS; activation BLOCKED (needs Administrator) | needs root (WSL-tested) | built, unverified on real hardware |
| TUN real traffic / DNS hijack / leak tests | not verified (non-elevated session) | not verified (WSL only) | not verified |
| Autostart / service install | — | — | — |
| Proxy Supervisor + IPC | — | — | — |

Legend: yes = implemented and verified as described under "Verification"; — = planned.

## System proxy implementation

| Platform | Mechanism | Details |
|---|---|---|
| Windows | WinINet registry, current user | `ProxyEnable`, `ProxyServer`, `ProxyOverride`; `InternetSetOption` refresh; no elevation |
| macOS | `networksetup` | all enabled network services; web, secure web, socks and bypass domains |
| Linux (GNOME family) | `gsettings org.gnome.system.proxy` | mode, http/https/socks host and port, ignore-hosts |
| Linux (KDE and others) | — | reported as unsupported with a reason; KDE is a follow-up |

The shell environment (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`) is detected
and displayed but never modified. The TUI shows "Desktop proxy" and "Shell proxy"
separately, because they affect different things.

If the desktop proxy is already enabled and points at another client, ProxyTUI reports a
conflict, backs the settings up unchanged and restores them on disable. It never assumes the
proxy was off before.

## TUN implementation

TUN is delegated to the cores; ProxyTUI only writes their configuration and observes the
result.

| Item | Detail |
|---|---|
| Mihomo | `tun.enable/stack/mtu/auto-route/auto-detect-interface/strict-route/dns-hijack` |
| sing-box | `inbounds[type=tun]` (address/auto_route/strict_route/stack/mtu), `route.auto_detect_interface`, one `hijack-dns` route rule |
| Minimum versions | Mihomo 1.18.0; sing-box 1.11.0 (1.14.4 is the verified baseline) |
| Preserved | DNS, route rules, proxies, groups, providers and unknown TUN fields |
| Overrides | reported in the result, preflight and logs; never silent |
| Validation | rendered config is checked with the core binary before start |
| Preflight | OS, WSL, privileges, core/version, profile, egress, IPv4/IPv6, DNS, TUN/VPN devices, firewall, nftables, route conflicts, profile overrides |
| Lifecycle | enable/change/disable/status/verify/recover; baseline snapshot; release verification; automatic rollback on start failure |
| State | `state/runtime/tun-state.json` (owner-only) |
| Conflict policy | external TUN carrying the IPv4/IPv6 default route → BLOCKED; idle external TUN → WARNING; unverified ownership → BLOCKED (default route) or UNKNOWN |
| System proxy | WinINet/GSettings/networksetup plus WinHTTP and external proxy processes are reported as warnings; nothing is cleared automatically |
| Activation gate | preflight completion ≠ activation; any BLOCKED or safety UNKNOWN refuses activation |

## Verification

Real runs performed for this iteration:

* **Windows**
  * Mihomo 1.19.34 and sing-box 1.14.4: release install, config validation with the real
    binaries, start, Clash API traffic/connections, cross-process adoption, stop, no
    leftover process.
  * System proxy adapter: snapshot → apply → verify → restore against a dedicated
    `HKCU\Software\proxy-tui-test\...` key using the real registry code path and
    `InternetSetOption` refresh.
  * Live round trip on the production WinINet key while another client owned
    `127.0.0.1:10808`: enable installed `127.0.0.1:7890`, a second process reported the
    enabled state and the preserved backup, disable restored the original
    `ProxyEnable/ProxyServer/ProxyOverride` exactly. Enabling without a running core is
    refused, and the machine was left byte-identical.
  * `go test ./...` passes, including the Windows-only registry tests.
  * TUN: rendered configs for Mihomo 1.19.34 and sing-box 1.14.4 are accepted by the real
    binaries (`mihomo -t`, `sing-box check`). Preflight on the real host correctly reports
    BLOCKED privileges, warns about the existing `xray_tun` adapter and that the default route
    already goes through it. `network tun enable` stops at the privilege check: no TUN state
    is written, no core starts, the desktop proxy stays untouched.
  * **Real TUN activation is not verified**: it requires an elevated terminal and would
    change this machine's routing while another TUN client is active.
  * Windows acceptance run (Phase 3C.5, 2026-11-18): preflight PASS for both cores,
    real-binary TUN config validation PASS, blocked enable produced no state/core/proxy
    changes, forged-state recovery refused to touch `xray_tun` or any core process.
    Activation remains BLOCKED pending an elevated, explicitly authorized run.
  * Final safety hardening (2026-11-18): preflight now reports
    `completed=true` / `activation_ready=no` with structured blocking issues, warnings and
    unknown facts. On the live host `xray_tun` carries the IPv4 default route and is
    correctly BLOCKED regardless of elevation; the WinINet proxy and the running
    `v2rayN/xray` pair are warnings; `以太网 2` (VirtualBox host-only) is reported as an
    unrecognised virtual adapter (UNKNOWN), which intentionally keeps activation blocked
    until a human confirms it is not a tunnel.
* **Linux (Ubuntu 26.04 in WSL)**
  * `go test ./...` passes; cross-platform test defects found by this run were fixed.
  * Real `gsettings`/dconf round trip in an isolated `dbus-run-session`: enable set
    `mode=manual`, `host=127.0.0.1`, `port=7890`, wrote a backup; disable restored
    `mode=none`, empty host and the previous default port, and removed the backup.
  * Mihomo 1.19.34 (linux-amd64-compatible): install, import, start, adoption from a new
    process, SIGTERM stop, no leftover process.
  * Foreign proxy detection against a manually configured `127.0.0.1:10808` desktop proxy,
    including refusal to disable without a backup.
  * TUN preflight inside WSL: correctly reports WSL, non-root privileges, missing core and
    missing nftables. WSL cannot validate native nftables or policy routing, so no Linux TUN
    activation claim is made.
* **macOS**
  * Builds, and the adapter is covered by unit tests with a scripted `networksetup` runner.
  * **No real macOS host was exercised.** Do not treat macOS system proxy as verified until
    it has been run on real hardware.

Cross-compilation for `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/amd64` and
`darwin/arm64` succeeds; cross-compiling is not the same as testing on those platforms.

## Limitations

* System proxy restoration depends on the backup file under `state/runtime`. If that file is
  deleted manually, ProxyTUI refuses to guess and returns `ErrNoBackup` instead of disabling
  a configuration it does not own.
* macOS applies the proxy to all enabled network services; per-service selection is not
  exposed in the UI yet.
* Linux support targets the GNOME family. KDE needs its own adapter (KConfig/DBus).
* TUN uses the cores' own implementations. Real traffic, DNS hijack and IPv4/IPv6 leak
  tests still need an elevated Windows run and a native Linux host.
* macOS TUN is compiled and unit-tested but not verified on real hardware, so it stays
  unofficial.
* There is no autostart or supervisor yet, so after a reboot a core is not restarted
  automatically (Phase 3D).
