# Phase 3D.2.2 / 3D.2.3 — Security & Coordination Hardening

Status: **Windows Named Pipe and supervisor crash/stop lifecycle are implemented and
natively tested. Phase 3D.2 is still PARTIAL because cross-SID denial, native Linux/macOS
authentication, FakeCore adoption and interactive two-TUI coordination remain NOT TESTED.**

## Windows Named Pipe (Round 1)

- Transport: go-winio v0.6.2, pinned in go.mod/go.sum.
- Endpoint: `\\.\pipe\proxy-tui-<LogonSID>`; Logon SID failure fails closed.
- DACL: `D:P(A;;GA;;;<LogonSID>)` only; kernel-enforced at connect.
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
- Cross-SID denial: **NOT TESTED** (no second enabled local account; none created).

## Other hardening

- Shared settings: TUI/CLI use `SettingsSnapshot`/`UpdateSettings`; supervisor atomically
  writes and updates memory only after success.
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

## Remaining

- Real cross-SID denial test (controlled second account or CI runner).
- Native Linux/macOS execution of the new peer/endpoint tests (CI added, NOT RUN).
- FakeCore crash/adopt test (core unchanged across supervisor crash).
- Interactive two-TUI node/settings/Operation consistency.
- Phase 3C.6 TUI Disable manual acceptance (still NOT TESTED).
- Payload fuzz, path replacement and secret-log regression tests.
