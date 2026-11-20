# Phase 3D.2 — Security, Coordination and Final Engineering Closure

Status: **Engineering defects from the final audit are fixed and covered by local tests.
Phase 3D.2 remains PARTIAL because cross-user denial, native Linux/macOS authentication,
full cross-process FakeCore adoption, interactive two-TUI coordination, real CI and the
manual TUI Disable acceptance remain external evidence items.**

## Windows Named Pipe (Round 1)

- Transport: go-winio v0.6.2, pinned in go.mod/go.sum.
- Endpoint: per-user SID plus runtime-directory hash; user SID failure fails closed.
  The scope is deliberately per user (not per logon session) because runtime state, ports
  and config are shared by all sessions of one user.
- DACL: `D:P(A;;GA;;;<UserSID>)` only; kernel-enforced at connect.
- Remote clients rejected by go-winio's `FILE_PIPE_REJECT_REMOTE_CLIENTS`.
- No AF_UNIX fallback on Windows; pipe-name conflicts fail closed.
- Shared RPC layer unchanged (framing, RequestID, OperationID, 1 MiB limit, errors).

### Tests (native Windows)

- Round trip, 32 concurrent clients, 100 start/close cycles, Close unblocking Accept,
  partial frame followed by a healthy client, server close errors clients, name conflict,
  DACL assertions: all PASS.
- Live: Supervisor start, DACL observation, 5 concurrent CLI clients, operation query, TUI
  connect, TUI exit without stopping the supervisor, normal stop, restart, no process or
  pipe leftovers: PASS.
- Cross-user denial: **NOT TESTED** (no second enabled local account; none created).

## Other hardening

- Shared settings: TUI/CLI use `SettingsSnapshot` and field-level `PatchSettings`; the
  supervisor writes atomically, updates memory only after success, and the TUI refreshes
  snapshots through its Bubble Tea command rather than in `View`.
- Operation lifecycle: RUNNING/SUCCEEDED/FAILED cache (5 min, 256 entries); duplicate IDs
  return cached results; `proxy operation status <id>` queries without executing.
- Remote retries transport failures with 1s/2s/5s/10s and reuses the operation ID.
- Unix peer credential checks are implemented (Linux/macOS), but native execution is
  **NOT TESTED**.

## Round 2 additions

- Lifecycle state: `RUNNING` vs `INTENTIONALLY_STOPPED`. A user stop is authoritative;
  ordinary CLI/TUI commands do not resurrect the supervisor. An explicit
  `proxy supervisor start` clears it.
- Crash recovery: forced kill of the supervisor PID leaves `RUNNING`; the next CLI command
  ensures a new supervisor. Five concurrent clients after a kill produced exactly one
  supervisor process (old PID 3832 -> new PID 44704).
- Ensure lock: `state/runtime/ensure.lock` serializes concurrent Ensure calls.
- Unix: server verifies peer UID; client now verifies server UID, socket mode 0600, owner,
  parent 0700, and rejects symlinks/non-socket endpoints. Tests are added for native CI.
- Two TUI processes were started simultaneously; killing one left the other and the
  supervisor running. Interactive shared-state/node coordination is NOT TESTED.

## Round 3 additions

- Stop intent is persisted before acknowledgement/cancel. A failed persistence refuses the
  stop (`TestStopRefusedWhenIntentCannotPersist`); the lifecycle record is already
  `INTENTIONALLY_STOPPED` when the stop response returns
  (`TestStopIntentPersistedBeforeShutdown`). Instance ID/version/source were added.
- Fuzz targets for framing and request JSON were added and run locally for 8s each
  (7.3M/7.1M executions, no crash).
- Endpoint replacement tests exist for Unix (symlink, permissions, stale socket) and
  Windows (pipe-name conflict).
- `docs/manual-acceptance-phase3c6.md` prepared. TUI Disable remains **NOT TESTED**.

## Final closure (this round)

- IPC long operations: read timeout 30s, mutation timeout 15m; the client honours the
  caller deadline and polls operation.get instead of re-executing. Rollback uses a bounded,
  non-cancelled cleanup context.
- Core identity: persisted process start time, executable, binary SHA-256 and config
  SHA-256; Stop refuses legacy/mismatched identity and never kills an unverified PID. A
  verified process with a transient API outage is DEGRADED, not STOPPED.
- OperationID: atomic claim, request fingerprint, terminal-only TTL, RUNNING not evictable.
- Settings: locked snapshot, field-level PatchSettings, TUI refresh and render-only View.
- Network recovery: restore is verified before backup removal; committing phase prevents a
  committed enable from being rolled back; stale backups no longer block supervisor stop.
- Supervisor: stopping gate rejects new mutations; owner-guarded lifecycle/state writes;
  CLI stop waits for state + lifecycle cleanup; ensure lock checks holder liveness/start
  time instead of deleting by mtime alone.
- TUN: config path/digest verified; no-interface or state-write failure rolls the core back
  with a bounded cleanup context; enabling TUN refuses if system-proxy restore failed.
- Secret: UI and text redaction includes path tokens; canary tests cover URL/UI/error paths.
- Tests: Windows registry test keys fixed and isolated; TUN host probes are live-gated.

## Remaining external evidence

- Real cross-user denial test (controlled second account or CI runner).
- Native Linux/macOS execution of the new peer/endpoint tests (CI added, NOT RUN).
- Full supervisor A/B FakeCore crash/adopt run with real OS processes (adapter-level
  identity/adopt tests exist; the full process-level run remains manual).
- Interactive two-TUI node/settings/Operation consistency on real PTYs.
- Phase 3C.6 TUI Disable manual acceptance (still NOT TESTED).
- Payload fuzz and path-replacement race execution; endpoint replacement itself has
  symlink/permission/stale tests.
