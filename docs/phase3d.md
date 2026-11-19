# Phase 3D — Supervisor & IPC

Status: **3D.1 implemented; 3D.2 client migration implemented, but not COMPLETE because
Windows IPC authentication (Named Pipe/SID ACL) and multi-TUI live validation are still
outstanding. 3D.3/3D.4 not started.**

## Architecture now

    TUI / CLI
       │  app.Frontend (same fields/methods as before)
       ▼
    Remote frontend (IPC client)
       │  app.call / core.* / supervisor.*
       ▼
    Supervisor (single instance)
       │
       ▼
    Local App (Cores, Network, TUN, Profiles, Logs)

- `internal/app/frontend.go` defines `CoreService`, `ProfileService`, `TUNService`,
  `LogService` and the `Frontend` façade used by both clients.
- `NewLocalFrontend` is only constructed by the supervisor; `NewRemote` is used by TUI/CLI.
- TUI no longer constructs `app.App`; it receives `*app.Frontend` from `runTUI`.
- CLI `openApp` always ensures and connects to the supervisor. The only local path is an
  in-package test flag, not an environment variable or CLI flag.
- A supervisor facade call is reflection-dispatched over `app.call` with the Facade as the
  only reachable target; mutation methods are serialized by the supervisor mutex.
- Shared settings (theme, traffic mode, TUN defaults, update policy) are read and written
  through `SettingsSnapshot`/`UpdateSettings`; TUI and CLI no longer write `config.json`
  directly. The supervisor writes atomically and updates its in-memory copy only after a
  successful write.
- Write requests carry an OperationID. The supervisor caches mutation results for five
  minutes and returns the cached result for a retried ID, so a lost response cannot start a
  core twice. The remote frontend retries transport failures with 1s/2s/5s/10s backoff and
  reuses the same OperationID.
- The TUI shows `Supervisor CONNECTED/DISCONNECTED` on Overview and blocks mutations while
  disconnected. The refresh loop calls `Health` and clears live state on failure instead of
  showing stale data as current.

## Real verification

- Eight concurrent `core start mihomo` clients → exactly one start, seven
  `CORE_ALREADY_RUNNING`, one mihomo process.
- Remote frontend live test: IPC → supervisor → activate profile → start core → status →
  stop core; PASS.
- Bare `proxy status` with no supervisor auto-started exactly one supervisor; five
  concurrent `proxy status` clients shared it.
- TUI process started through `RemoteAppClient`, was killed, and the supervisor survived.
- `supervisor stop` took 169 ms with no cores/TUN; state and socket removed.
- `supervisor stop` with a running core is refused with `NETWORK_BLOCKED`.
- Facade mutation calls are serialized (test), unknown methods are rejected.
- `go test -race` clean for ipc/supervisor.

## Security status

| Platform | Transport | Peer authentication | Real verification |
|---|---|---|---|
| Linux | Unix socket 0600, dir 0700 | `SO_PEERCRED` UID check | compiled and unit-tested; not run on a native Linux host this round |
| macOS | Unix socket 0600, dir 0700 | `LOCAL_PEERCRED` UID check | cross-compiled; not run |
| Windows | AF_UNIX under LocalAppData | **none**; no Named Pipe/SID ACL | run on Windows; relies on per-user profile ACL |

No TCP endpoint is exposed. There is no token; Unix relies on peer credentials, Windows on
directory ownership. Cross-user adversarial tests have not been run. Windows Named Pipe with
an explicit SID ACL remains required before claiming Windows IPC security.

## Remaining gaps (not COMPLETE)

- TUI/CLI are migrated in code. Multi-TUI live mutation tests (two interactive TUIs +
  concurrent node/Traffic Mode changes) are still not performed; only CLI concurrency and
  single-TUI startup/exit were exercised.
- Bounded transport reconnect is implemented (1s/2s/5s/10s, same OperationID). If the
  supervisor process itself has exited, the remote client does not auto-ensure a new
  supervisor yet; the TUI reports DISCONNECTED and blocks writes until a supervisor is
  started manually.
- Windows Named Pipe/SID ACL and cross-user denial are missing.
- Payload fuzzing, secret-in-log tests and route-replacement tests are not added.
- TUI-internal Disable (Phase 3C.6) still requires real manual acceptance.
- 3D.3 health monitor/auto-restart and 3D.4 autostart are not started.

## Stages

| Stage | State |
|---|---|
| 3D.1 Supervisor lifecycle, single instance, detached process, state, CLI | implemented |
| 3D.2 TUI/CLI client migration, auto-ensure, serialization, no local fallback | implemented; security/live gaps above |
| 3D.3 Health monitor and recovery | not started |
| 3D.4 Autostart | not started |
