# ProxyTUI

A terminal-native proxy client for Windows, Linux and macOS.

ProxyTUI wraps the mature proxy cores — [Mihomo](https://github.com/MetaCubeX/mihomo) and
[sing-box](https://github.com/SagerNet/sing-box) — in a full screen TUI. You paste a
subscription URL, ProxyTUI detects the format, builds a runnable configuration, starts the
core and can take over the desktop proxy. No hand-written YAML, no GUI.

> Reuse the core. Simplify the experience.

## Status

Version **0.2.0-dev**: subscription-first MVP, local core management, transactional desktop
system proxy and core-native TUN integration (requires an elevated terminal).

| Area | State |
|---|---|
| Add subscription by pasting a URL | working |
| Detect Mihomo YAML / sing-box JSON / base64 node lists / share links | working |
| Parse ss, vmess, vless, trojan, hysteria2, tuic, socks5, http links | working |
| Generate runnable configs from node lists (base templates) | working |
| Profile storage, snapshots, rollback, update with ETag | working |
| Background profile auto-update (interval per profile) | working |
| Install / start / stop Mihomo and sing-box | working (both verified against real releases) |
| Proxy groups, node switching, latency test | working (via the Clash compatible API) |
| Connections and live throughput | working (via the Clash compatible API) |
| Cross-session core adoption after the TUI exits | working (pid + API state file) |
| Desktop system proxy, transaction + restore | working (Windows WinINet, macOS networksetup, Linux GNOME) |
| Traffic takeover modes (off / system-proxy / tun / manual) | working, mutually exclusive |
| Core-native TUN (Mihomo / sing-box) | implemented: rendering, validation, preflight, lifecycle, recovery |
| TUN real activation | **requires Administrator/root**; preflight acceptance passed, activation BLOCKED pending an elevated authorized run |
| DNS takeover and routing integration | delegated to the core's own TUN implementation |
| Background Proxy Supervisor / IPC / autostart | **not implemented (Phase 3D)** |

Everything that is not implemented says so in the UI instead of pretending. See
[docs/platform-support.md](docs/platform-support.md) and the [Phase 3 plan](docs/phase3.md).

## Install

ProxyTUI is a single static binary.

```bash
git clone https://github.com/proxytui/proxy-tui
cd proxy-tui
go build -o proxy ./cmd/proxy
./proxy
```

Go 1.24 or newer is required. Prebuilt releases are produced by the CI workflow.

ProxyTUI does not bundle the proxy cores. On first use, install the core you need:

```bash
proxy core install mihomo
proxy core install sing-box
```

or press `s` in the TUI (the error message tells you when the binary is missing).

## Quick start

1. Start the TUI: ```proxy```
2. Open the **Mihomo** or **sing-box** page with `Tab` or a number key.
3. Press `A`, paste one or more subscription URLs (or raw share links), press `Ctrl+S`.
4. Press `Enter` on the imported profile to make it active.
5. Press `s` (or `Enter` on the Overview page) to start the core.
6. Press `p` to pick a node, `t` to measure latency.
7. To route the desktop through it, open **Settings**, select **Traffic mode**, press
   `Enter` until it shows **System Proxy**.

The same flow from the CLI:

```bash
proxy profile add --core mihomo "https://example.com/api/v1/client/subscribe?token=..."
proxy profile list
proxy profile use <profile-id>
proxy core start mihomo
proxy network system-proxy enable
proxy status
```

## Choosing a proxy node

The TUI has a real proxy browser backed by the core's Clash-compatible API:

* Press `p` on Overview or a core page to open **PROXIES**.
* Left pane lists root proxy groups; right pane lists their members. Enter on a group
  member opens that nested group; Esc goes back one level.
* Enter on a leaf selects it in a `Selector` group; `s` selects a group member as the value
  of the parent. `URLTest`/`Fallback`/`LoadBalance` groups stay automatic and refuse manual
  selection. `DIRECT` and `REJECT` are labelled and can be selected when the core allows it.
* `t` tests the highlighted node, `/` searches members, `R` refreshes the status.
* Selection talks to the running core; it does **not** change the system proxy or TUN.

**Core running is not the same as proxy connected.** Press `v` on Overview or a core page
for an explicit end-to-end check: ProxyTUI sends an HTTP request through its local proxy
port (never through the system proxy) and reports the exit IP, latency, or the exact error.
The Overview also shows the current `Traffic mode` (off / system proxy / TUN) and the last
proxy check, so "running but not redirecting traffic" is visible.

After importing a subscription the TUI focuses the core page and suggests the next steps:
Enter activates the profile, `s` starts the core, `p` opens the node browser. Import never
starts a core or enables global takeover by itself.

## Supervisor (Phase 3D.1/3D.2, partial)

ProxyTUI now ships a per-user detached supervisor and a versioned local IPC endpoint:

    proxy supervisor start
    proxy supervisor status --json
    proxy supervisor stop
    proxy supervisor logs

- One instance per user is enforced by an atomic local-socket bind; a live endpoint wins.
- State is persisted in `state/runtime/supervisor-state.json`; the log is
  `state/logs/supervisor.log`.
- `proxy core start|stop` use the supervisor when it is running; a present supervisor is
  authoritative. A stop is refused while a core, TUN or ProxyTUI-owned desktop proxy is
  still active.
- IPC is a local Unix-domain socket (Windows 10+ AF_UNIX) with length-framed JSON,
  request IDs, deadlines, version checks and serialized mutations. No TCP is exposed.

**Current boundary:** TUI and CLI now run through the supervisor via a remote
`app.Frontend`; bare `proxy` ensures the supervisor, and no production command falls back
to a local core manager. Windows still uses AF_UNIX without peer-credential/Named Pipe SID
ACL enforcement, there is no reconnect/backoff yet, and health monitoring/auto-restart/
autostart are not implemented. See `docs/phase3d.md` and `docs/supervisor.md`.

## Traffic takeover

Exactly one mode is active at a time; the TUI and CLI share the same state machine.

| Mode | Behaviour |
|---|---|
| Off | ProxyTUI does not touch traffic; any desktop proxy it changed is restored |
| System Proxy | The desktop HTTP/SOCKS proxy points at the local core |
| TUN | The core creates the TUN device and manages routes/DNS; ProxyTUI renders its config, preflights the host and verifies release |
| Manual | You keep your own client; ProxyTUI restores the desktop proxy and stays out of the way |

Safety rules:

* The system proxy can only be enabled while a core is running; this prevents a dead proxy
  after the core stops.
* The original desktop proxy is snapshotted **before** anything changes (full settings, not
  just on/off) and restored on disable.
* If another program changed the proxy after ProxyTUI did, recovery leaves it alone instead of
  overwriting the other client's settings.
* Every change writes a journal first; an interrupted change is rolled back on the next
  start. A crash never leaves an unrepairable configuration behind.
* If another proxy client already owns the desktop proxy, ProxyTUI reports the conflict and
  still backs up the original values instead of assuming "it was off".
* The shell environment proxy (`HTTP_PROXY`/`HTTPS_PROXY`) is detected for display only;
  ProxyTUI never edits your shell.
* TUN never writes routes or firewall rules itself: the core does that. ProxyTUI refuses to
  enable TUN when preflight is BLOCKED (for example without Administrator/root), reports
  overridden TUN fields in a profile, and verifies that the core released its interfaces on
  disable. `proxy network tun recover` repairs a state left by a crash.

## Keys

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | next / previous page |
| `1` … `6` | jump to a page |
| `↑` `↓` | move the selection |
| `Enter` | primary action (activate profile, start/stop core, toggle a setting) |
| `A` | add subscription (modal) |
| `u` | update the selected subscription |
| `d` | delete the selected subscription |
| `s` / `x` / `r` | start / stop / restart the core |
| `p` | pick a proxy node |
| `t` | test node latency |
| `/` or `Ctrl+P` | command palette |
| `?` | keyboard reference |
| `q` / `Ctrl+C` | quit |

In the Add Subscription dialog: `Tab` moves between fields, `←/→` changes the target core,
`Enter` in the URL field inserts a newline, `Ctrl+S` imports, `Esc` cancels. Bracketed paste
works, and multi-line pastes are preserved.

## Slash commands

```text
/help
/core mihomo|sing-box
/profile add|list|update|use
/proxy select|test
/system-proxy enable|disable
/network                  show the desktop/shell/TUN state
/tun enable|disable       enable or disable core-native TUN (needs elevation)
/refresh
/logs
/doctor
/quit
```

## CLI

```text
proxy                                  open the TUI
proxy core list                        show core state and version
proxy core install [mihomo|sing-box]   download the latest release
proxy core start|stop <core>           start/stop with the active profile
proxy profile add [--core X] [--name Y] <url|file|->
proxy profile list | show <id> | use <id> | update [--all] | delete <id>
proxy network status                   desktop proxy, shell proxy, restore point
proxy network system-proxy enable|disable
proxy network mode <off|system-proxy|manual|tun>
proxy network tun status               TUN state, interface, outbound, DNS, routing
proxy network tun preflight            privileges, core, routes, DNS, conflict checks
proxy network tun enable [--core X] [--stack mixed|system|gvisor] [--mtu N] [--dns-hijack=false] [--ipv6]
proxy network tun disable              stop TUN and verify the core released its resources
proxy network tun recover              repair TUN state left by a crash or abnormal exit
proxy network recover                  repair desktop proxy state left by a crash
proxy status                           cores, traffic, active profiles
proxy doctor                           local diagnostics
proxy version
```

## Data layout

```text
<config>/proxy-tui/config.json         settings (theme, traffic mode, intervals)
<config>/proxy-tui/keymap.json         keyboard overrides
<data>/proxy-tui/profiles.json         profile index
<data>/proxy-tui/profiles/<id>/        source.<ext>, active.<ext>, snapshots/, meta.json
<data>/proxy-tui/cores/<core>/         installed binaries, current.txt, meta.json
<state>/proxy-tui/logs/proxy-tui.log   rolling log file
<state>/proxy-tui/runtime/             runtime configs, core state, network journal/backup, tun-state.json
```

On Windows these map to `%APPDATA%` and `%LOCALAPPDATA%`; on Linux/macOS the XDG
locations and `~/Library/Application Support` are used. Set `PROXY_TUI_HOME` to force a
portable layout (used by the tests).

## Preflight and the activation gate

`proxy network tun preflight` always reports two separate facts:

* **Preflight completed** — the host was inspected; this is not permission to activate.
* **Activation ready** — no BLOCKED check and no safety-related UNKNOWN remains.

The result is grouped into **blocking issues**, **warnings** and **unknown facts**. The CLI
exits 0 once preflight completes; `network tun enable` refuses unless activation is ready.

Safety rules enforced by preflight:

* A **non-ProxyTUI TUN adapter that carries the IPv4 or IPv6 default route is BLOCKED** —
  Administrator rights do not bypass it. ProxyTUI never removes the adapter or edits routes.
* A recorded TUN state that cannot be matched to its core process is treated as unverified:
  its adapter carrying the default route is BLOCKED, an idle one is UNKNOWN.
* Unrecognised virtual adapters are UNKNOWN; if one carries the default route it is BLOCKED.
* WinINet system proxy, WinHTTP proxy and external proxy processes are reported separately
  from TUN conflicts. They are warnings, are never cleared automatically, and stale proxy
  configuration (no listener) is called out.
* "Activation ready" covers live core state, profiles, versions, privileges, egress and the
  TUN/system-proxy conflict checks above.
* Every traffic-mode change in the TUI asks for confirmation. While the confirmation is
  open or an action is running, other shortcuts are ignored, so a repeated key cannot start
  a second network transition.
* Enabling the desktop system proxy first verifies that the core is running **and** that the
  configured local proxy port accepts TCP; otherwise it refuses before touching WinINet.

## Security notes

* Subscription URLs are redacted before they reach logs, the UI or error messages.
* Core control APIs bind to `127.0.0.1` only and use a random per-start secret.
* The persisted core state (pid, API address, secret) lives in `state/runtime` with owner-only
  permissions and is removed on stop.
* Config files are written with user-only permissions (`0600`) through atomic renames.
* Release downloads verify SHA-256 when the upstream publishes a checksum file and say so
  when it does not.
* Imported configs are validated with the core binary before they can be activated.
* ProxyTUI never overwrites a working config or a working desktop proxy without a snapshot.

## Documentation

* [Architecture](docs/architecture.md)
* [Subscription pipeline](docs/subscription.md)
* [Platform support](docs/platform-support.md)
* [Phase 3 plan and acceptance criteria](docs/phase3.md)

## Roadmap

* Phase 0 — project skeleton (done)
* Phase 1 — subscription-first MVP (done)
* Phase 2 — core management and node operations (done; both cores verified on real releases)
* Phase 3A — network state manager with backup, journal and recovery (done)
* Phase 3B — desktop system proxy with conflict detection (done)
* Phase 3C — TUN through the cores, DNS handling (planned)
* Phase 3D — Proxy Supervisor, IPC, autostart (planned)
* Phase 5 — release automation and docs (partially done)

## License

MIT — see [LICENSE](LICENSE).
