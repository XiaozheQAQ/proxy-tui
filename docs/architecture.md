# Architecture

ProxyTUI is split into layers that can be tested without a terminal. The TUI never talks to
the network, the filesystem or a proxy core directly; everything goes through services.

```text
cmd/proxy/main.go
  └── internal/cli            cobra commands (TUI, core, profile, network, status, doctor)

internal/app                  wiring: paths, settings, logs, subscriptions, cores, network
internal/tui                  Bubble Tea model/update/view + widgets
  ├── layout                  geometry, palette, header/sidebar/footer, ANSI-safe fitting
  ├── components              cards, tables, badges, key-value blocks
  ├── pages                   Overview, Mihomo, sing-box, Connections, Logs, Settings
  ├── dialogs                 Add Subscription, proxy picker, confirm, help, commands
  └── commands                slash command registry

internal/subscription         fetch, detect, parse, render, snapshot, update, rollback
  ├── model                   Profile, Index, Snapshot, CoreKind, Format
  └── parser                  node model plus mihomo / singbox / shareuri parsers

internal/core                 core lifecycle: install, process, runtime config, Clash API
  ├── clashapi                Clash compatible REST client (Mihomo and sing-box)
  ├── mihomo                  Mihomo adapter
  └── singbox                 sing-box adapter

internal/network              transactional network state (desktop proxy)
  ├── manager.go              apply/verify/commit, journal, backup, recovery
  ├── journal.go              network-journal.json / network-backup.json
  ├── network.go              modes, snapshots, capabilities, environment detection
  └── systemproxy             WinINet, networksetup, GSettings adapters

internal/tun                  core-native TUN
  ├── settings.go             core-agnostic TUN settings and version gates
  ├── renderer.go             dispatch plus Mihomo and sing-box renderers
  ├── preflight.go            READY/WARNING/BLOCKED/UNKNOWN checks and hints
  ├── probe.go                host facts (interfaces, egress, DNS, privileges, nft)
  └── lifecycle.go            enable/change/disable/status/verify/recover

internal/storage              paths, atomic file IO, settings, keymap
internal/logging              bounded log ring shared by the TUI and the file sink
internal/security             URL and text redaction
configs/templates             embedded base configs for node-only subscriptions
```

## Data flow: importing a subscription

1. The Add Subscription dialog collects a core, an optional name and the pasted payload.
2. `subscription.Import` classifies the input as HTTP(S) URLs or inline content.
3. URLs are fetched with the limits in `subscription/fetcher.go` (25s timeout, 5 redirects,
   8 MiB body, ETag/Last-Modified support). A UTF-8 BOM is stripped before parsing.
4. `Detect` identifies the payload (JSON/YAML structure, then share links, then base64).
5. The matching parser normalises the payload. Unsupported entries are reported, never
   silently dropped.
6. `RenderConfig` keeps a config verbatim when it already targets the core and carries its
   own routing, or expands node lists with the embedded base template. A config with rules
   is never converted to the other core; it is stored with a warning instead.
7. The rendered config is validated by the core binary when it is installed.
8. The profile is written (source, snapshot, active config, metadata) and indexed.

## Data flow: starting a core

1. The TUI asks the app to start the active profile of a core.
2. The app loads `active.yaml` / `active.json` and calls `core.Manager.Start`.
3. The manager stops the other core (both would claim the same local ports) and delegates.
4. The adapter injects runtime settings (`mixed-port`, a random controller secret, the
   Clash API for sing-box), writes the runtime config under `state/runtime`, launches the
   child process and waits for the control API to answer.
5. The adapter persists `<core>-state.json` (pid, API address, secret) so a later process
   can adopt the running core: status, proxies, traffic and stop all keep working.
6. The TUI polls traffic and connections once per second through the Clash API.

## Supervisor and IPC (Phase 3D, partial)

    proxy CLI ──► IPC client ──► supervisor (single instance)
                                   │
                                   ├── App (profiles, network, cores)
                                   └── Mihomo / sing-box

The supervisor is the same binary with the hidden `supervisor run` command. It is started
detached and binds either a Windows Named Pipe (go-winio, Logon-SID DACL, remote clients
rejected) or a Unix socket (0600, UID peer check), writes `supervisor-state.json` and serves
versioned length-framed JSON. Mutations are serialized by a supervisor mutex; reads may
run concurrently. CLI core lifecycle calls use IPC when the supervisor is running.

TUI and CLI construct an `app.Frontend` backed by `NewRemote`; the local application is
built only inside the supervisor. CLI commands call `ensureSupervisor` before opening the
frontend. Overview shows `Supervisor CONNECTED/DISCONNECTED` and mutations are blocked while
disconnected. The supervisor mutex serializes both dedicated core operations and facade
mutation calls (`app.call`).

## Data flow: proxy browsing, selection and connectivity

1. The refresh loop calls `Cores.Proxies` for the running core; the Clash API response is
   mapped to `core.Proxy` entries (type, `now`, members, delay history, health).
2. `p` opens `dialogs.ProxyPicker` with the full list. Root groups are those not referenced
   as a member of another non-GLOBAL group; selectors sort before automatic groups.
3. Enter on a group member pushes it on the navigation stack (Esc pops), Enter on a leaf in
   a `Selector` emits `ProxyResult`; automatic groups refuse manual selection. `s` selects a
   group member itself. `t` emits a test intent, which calls the delay endpoint.
4. The TUI sends `PUT /proxies/{group}` via `Cores.SelectProxy`, re-reads `/proxies` and
   only reports success when the group's `now` matches the requested member.
5. `v` runs `VerifyProxyConnectivity`: a TCP check of the local mixed port followed by an
   HTTP request with that port as the proxy. The result (exit IP, latency or error) is shown
   in a dialog and on the Overview. The system proxy is never used for this check.

## Data flow: TUI traffic-mode change

1. A page action (Settings row, header toggle, or `/tun` / `/system-proxy` command) is routed
   through `requestTrafficMode`.
2. The TUI stages a confirmation dialog and performs **no** network call yet. Cancel clears
   the pending action.
3. On confirm, `ApplyTrafficMode` enforces the TUN/system-proxy exclusivity, verifies the
   system-proxy target port is listening, and only then changes the network.
4. The result notice shows the previous mode → new mode and the interface/address; the TUI
   ignores further shortcuts while the action is in flight.

## Data flow: core-native TUN

1. The user enables TUN (TUI Settings, `proxy network tun enable` or `network mode tun`).
2. `tun.Manager` loads the active profile config, renders the core's TUN block
   (`Render`) and records any overridden user TUN fields.
3. The rendered config is validated by the core binary (`mihomo -t` / `sing-box check`).
4. Preflight evaluates host facts: privileges, core/version, egress, IPv4/IPv6, DNS,
   existing TUN/VPN devices, firewall/nftables, route conflicts. BLOCKED refuses to continue.
5. The core is stopped if needed, a baseline network snapshot is taken, and the core starts
   with the rendered config.
6. ProxyTUI observes the new interfaces, default egress and DNS changes, then persists
   `state/runtime/tun-state.json`. It never writes routes or firewall rules itself.
7. Disable stops the core, waits for every interface it created to disappear, restarts the
   plain configuration if the core was running before TUN, and removes the state. Leftover
   interfaces keep the state for `proxy network tun recover`.

## Data flow: changing the desktop proxy

1. The user selects a traffic mode in Settings (or runs `proxy network ...`).
2. `network.Manager` picks the first adapter whose capabilities say it is supported.
3. The current desktop settings are snapshotted (full fidelity, adapter-specific JSON).
4. A journal entry is written **before** the change, so a crash is recoverable.
5. The adapter applies the change and verifies the observed state.
6. On success the original settings are committed to `network-backup.json` and the journal is
   removed; on failure the snapshot is restored and the journal records the rollback.
7. Disable restores the exact original configuration. `network recover` repairs state after a
   crash, a forced exit or a stopped core.

## Invariants

* **Validate before activate.** A config that fails core validation never replaces the
  active file. The old revision stays usable.
* **Snapshot before mutating.** No operating system network setting is changed without a
  full-fidelity snapshot and a journal on disk.
* **No silent conversion.** Cross-core imports with routing are stored but not activatable.
* **Secrets stay local.** Control APIs listen on loopback with a per-start secret; URLs are
  redacted in logs and errors; the persisted core state is owner-only.
* **Honest capabilities.** Adapters return `core.ErrUnsupported` / `network.ErrUnsupported`
  (or a capability flag of `false`) for features the core or the platform cannot provide.
* **One takeover mode.** System proxy and TUN are mutually exclusive by construction:
  enabling one releases the other through the same manager.
* **No direct OS network writes for TUN.** ProxyTUI renders configuration and observes the
  result; the core owns the TUN device, routes and DNS. Overridden profile TUN fields are
  always reported.
* **Never fight another client.** Recovery only restores the desktop proxy if it still points
  at the address ProxyTUI installed; otherwise the stale backup is reported and left alone.
  The system proxy can only be enabled while a core is running.

## Concurrency

* Each TUI action runs as a `tea.Cmd` so the event loop never blocks on the network.
* Status and profile data are collected inside the command, not while rendering.
* `core.Manager` serialises install/start/stop per core and stops the other core before
  starting one, because the local proxy port is shared.
* `subscription.Manager` guards the profile index with a mutex and performs network work
  outside the lock; `network.Manager` serialises desktop proxy transactions with a mutex and
  its journal.

## Core Process Lifecycle Management vs Proxy Supervisor

Two different things, deliberately named differently:

* **Core Process Lifecycle Management** (implemented) — start, stop, restart, validate,
  monitor and adopt core processes. Cores are children of the `proxy` process. A started core
  survives the TUI exit and a later process can adopt it via `<core>-state.json`. This is
  cross-session process adoption, **not IPC**: there is no socket, no daemon and no
  autostart.
* **Proxy Supervisor** (planned, Phase 3D) — a long-lived background service started by the
  same binary, exposing a Unix socket / named pipe to the CLI and TUI, with autostart and
  crash recovery.

## Deliberate deviations from the original plan

* **Bubble Tea v1.** The plan mentions Bubble Tea v2. v1 (v1.3.10) is the stable line and
  the widget set (bubbles v1.0.0) matches it; migrating later only touches `internal/tui`.
* **Templates are embedded from configs/templates.** Package `templates` lives in that
  directory so the files stay discoverable while still being compiled into the binary.
* **TUN is delegated to the cores.** ProxyTUI will generate TUN configuration and manage the
  lifecycle, not reimplement a network stack, TProxy or nftables management.
