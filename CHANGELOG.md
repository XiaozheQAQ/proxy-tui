# Changelog

## [0.2.0] — Unreleased

Phase 3A/3B/3C: transactional network state, desktop system proxy and core-native TUN.

### Added

* `internal/network`: unified network state manager with capabilities, full-fidelity
  snapshots, a crash-safe transaction journal, committed backups, verification, rollback and
  recovery.
* Desktop system proxy adapters:
  * Windows: WinINet registry (current user) with `InternetSetOption` refresh.
  * macOS: `networksetup` for every enabled network service, including bypass domains.
  * Linux: GNOME `gsettings` (mode, http/https/socks, ignore-hosts); other desktops are
    reported as unsupported with a reason.
* Foreign proxy detection: an active desktop proxy owned by another client is reported as a
  conflict and its settings are backed up and restored instead of being discarded.
* Traffic takeover modes (off / system-proxy / tun / manual) with mutual exclusion; TUN is
  declared and reports "not implemented yet" until Phase 3C.
* CLI: `proxy network status|system-proxy|mode|recover`; TUI Settings gained a traffic mode
  row plus desktop/shell proxy state; Overview shows the desktop proxy; `/system-proxy` and
  `/network` slash commands work; `doctor` reports network capabilities.
* Recovery on startup: interrupted transactions and desktop proxies left behind by a stopped
  core are repaired automatically.
* Core-native TUN (`internal/tun`):
  * Mihomo and sing-box renderers that preserve DNS, rules, proxy groups and providers,
    report overridden TUN fields and reject configs the core version cannot run.
  * Rendered TUN configs are validated by the real core binaries before start.
  * Cross-platform preflight (READY/WARNING/BLOCKED/UNKNOWN) covering privileges, core
    version, active profile, default egress, IPv4/IPv6, DNS, existing TUN/VPN devices,
    firewall, nftables, route conflicts and profile overrides.
  * Lifecycle: enable/change/disable/status/verify/recover, baseline snapshot, interface and
    route observation, release verification, rollback to the plain config on start failure,
    and `tun-state.json` for cross-process status.
  * TUI Settings: traffic mode, TUN stack/MTU/DNS hijack/IPv6, TUN toggle, preflight report,
    recovery; CLI: `proxy network tun status|enable|disable|preflight|recover`.
  * TUN is mutually exclusive with the desktop system proxy; enabling one releases the other.
  * TUN hardening (Phase 3C.5): persisted core identity (pid/start time/binary) and config
    digest, ownership-checked status/disable/recover, early preflight before stopping a
    core, release-before-restart, lifecycle mutex, and refusal to clean up a state that
    points at a foreign interface. Preflight now also reports the desktop system proxy.
  * Verification boundary: rendered TUN configs are accepted by real Mihomo 1.19.34 and
    sing-box 1.14.4; Windows real activation remains BLOCKED (non-elevated session with an
    existing third-party TUN) and is not claimed as tested.
  * Final safety hardening: preflight now separates "completed" from "activation ready",
    reports blocking issues / warnings / unknown facts, and refuses activation on any
    BLOCKED or safety-related UNKNOWN. External TUN adapters carrying the IPv4/IPv6 default
    route are BLOCKED even with Administrator rights; recorded-but-unverifiable ownership
    is BLOCKED or UNKNOWN; unrecognised virtual adapters are UNKNOWN. WinHTTP proxy and
    external proxy processes are detected as separate warnings, stale system proxy
    configuration is labelled, and core identity now also includes the binary SHA-256.
  * `network tun preflight` exits 0 when the inspection completed; activation remains the
    job of `network tun enable`.
  * Phase 3D.2.2 hardening: operation result lifecycle (`proxy operation status`,
    RUNNING/SUCCEEDED/FAILED/EXPIRED with a five-minute bounded cache) and shared settings
    ownership. A Windows Named Pipe transport was prototyped but reverted due handle
    lifecycle crashes; Windows remains on AF_UNIX without SID-level authentication.
  * Phase 3D.2 hardening: shared settings writes now go through supervisor
    `UpdateSettings` (no direct config writes from TUI/CLI), mutation requests carry
    OperationIDs with a five-minute dedupe cache, and the remote frontend retries transport
    failures with bounded backoff (1s/2s/5s/10s). Windows Named Pipe/SID ACL and
    supervisor auto-ensure after a process exit remain open.
  * Phase 3D.2 client migration: `app.Frontend` abstraction, remote frontend over `app.call`,
    TUI/CLI ensure the supervisor automatically, production local fallback removed, TUI
    CONNECTED/DISCONNECTED state with mutation blocking, Unix peer-UID verification, and
    serialized facade mutations. Windows Named Pipe ACL/reconnect and multi-TUI live
    validation remain open.
  * Phase 3D.1/3D.2 (partial): detached per-user supervisor, atomic single-instance lock,
    persisted supervisor state/log, versioned length-framed JSON IPC, status/stop, serialized
    core start/stop/restart, idempotency, stop guards, and CLI lifecycle calls through the
    supervisor. Adds `proxy supervisor start|status|stop|logs`. TUI IPC migration, Windows
    Named Pipe ACL, authentication, health monitoring and autostart remain open.
  * Proxy browsing and selection: a real Proxies dialog backed by `/proxies` with
    recursive Selector navigation, automatic-group refusal, DIRECT/REJECT labels, member
    search, per-node delay testing, and post-selection `now` verification. Root groups are
    computed from API references; selecting a parent never rewrites a child group's value.
  * End-to-end proxy check (`v`): HTTP request through the local mixed port with exit IP and
    latency, explicitly not using the system proxy. Overview now shows Traffic mode
    (off/system proxy/TUN) and the last proxy check, so "core running" is not presented as
    "connected". Import focuses the core page and suggests activate/start/pick-node steps.
  * TUI traffic-mode safety: every mode change requires an explicit confirmation; while a
    confirmation or action is active, global shortcuts are ignored; the system proxy is only
    enabled after the core process and local proxy port are verified; `/tun` and
    `/system-proxy` use the same path. Added unit tests for row dispatch, confirmation,
    cancel, page-switch focus, small terminals and dialog isolation.
  * Live-test fix: the Windows DNS probe now ignores registry DNS entries of adapters that
    no longer exist (removed Wintun/TUN and stale third-party adapters), so preflight reports
    only DNS servers of currently present interfaces.
  * Windows adapter identity classifier: reads InterfaceIndex, alias, description, PnP
    device ID, hardware IDs, driver provider/service, manufacturer, component ID and
    `*IfType`, then classifies as PHYSICAL / HOST_ONLY / VPN_TUNNEL / VPN_WIREGUARD /
    VIRTUAL_UNKNOWN / UNKNOWN. VirtualBox Host-Only is accepted only when confirmed by
    `VBoxNetAdp`/`sun_vboxnetadp` (never by display name); Wintun/xray is VPN_TUNNEL and
    WireGuard is VPN_WIREGUARD. Confirmed host-only adapters no longer produce a safety
    UNKNOWN; carrying the default route is only a warning. Unknown adapters carrying the
    default route remain BLOCKED. Live host now reports `以太网 2` as HOST_ONLY (READY) and
    `xray_tun` as VPN_TUNNEL (BLOCKED).

### Changed

* Documentation now separates **Core Process Lifecycle Management** (implemented) from the
  **Proxy Supervisor** (planned), and states explicitly that cross-session core adoption is
  not IPC.
* sing-box is now verified against a real release (1.14.4), not only mocks.

### Fixed

* Installer preferred the sing-box `legacy-windows-7` archive and Mihomo `-v1-go12x`
  variants; regular/compatible builds are now preferred.
* Mihomo archives contain platform-suffixed executables
  (`mihomo-windows-amd64-compatible.exe`); extraction now matches those names.
* Subscriptions fetched or read from disk with a UTF-8 BOM failed JSON parsing; the BOM is
  stripped before detection and parsing.
* Installer tests were platform dependent and failed on Linux; they now use a platform
  specific asset name.
* Importing inline or file based YAML trimmed every line, destroying nested indentation
  (`dns.enable` became a top-level key). Inline payloads are now preserved byte for byte.
* The Windows DNS probe used a registry path that had lost its backslashes; the TUN preflight
  now reads `HKLM\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces` correctly.

## [0.1.0] — 2026-11-17

First usable iteration: subscription-first MVP plus local core management.

### Added

* Full screen TUI (sidebar navigation, header status bar, contextual key hints):
  Overview, Mihomo, sing-box, Connections, Logs and Settings pages.
* Add Subscription dialog with bracketed paste, multi-line input, target core selection and
  inline error reporting.
* Subscription pipeline: HTTP fetcher (timeouts, redirect and size limits, ETag /
  Last-Modified), format detection, Mihomo YAML and sing-box JSON parsers, base64 node lists
  and share links (ss, vmess, vless, trojan, hysteria2, tuic, socks5, http).
* Config generation from node lists using embedded Mihomo and sing-box base templates.
* Profile storage with atomic writes, source/active separation, snapshots and rollback,
  validation before activation and update failure safety.
* Core management: install from GitHub releases (SHA-256 verification when published),
  runtime config injection, **core process lifecycle management** (start/stop/restart, log
  capture, cross-session adoption), not a background supervisor.
* Clash compatible API client: proxy groups, node switching, latency testing, live
  connections and throughput.
* Slash commands and a command palette; `proxy` CLI with `core`, `profile`, `status`,
  `doctor` and `version` commands.
* Secret redaction, user-only file permissions, loopback-only control APIs with random
  secrets.

### Known gaps

* System proxy, TUN, DNS and routing integration were not implemented in this release
  (Phase 3).
* No background supervisor or IPC; cores run as children of the TUI process.
* Cross-core conversion is limited to node lists; configs with routing are stored but not
  activatable for the other core.
