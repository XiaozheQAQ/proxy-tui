# Phase 3D.2.2 / 3D.2.3 — Security & Coordination Hardening

Status: **Windows Named Pipe transport implemented and natively tested (Round 1). Phase 3D.2
is still PARTIAL because cross-SID denial, native Linux/macOS authentication, supervisor
auto-ensure after a crash, and two-TUI interactive acceptance remain open.**

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

## Remaining for Rounds 2/3

- Real cross-SID denial test (controlled second account or CI runner).
- Native Linux/macOS peer-credential integration tests.
- Supervisor crash vs intentional stop detection and safe auto-ensure.
- Two real TUI clients: shared node/Traffic Mode consistency.
- Phase 3C.6 TUI Disable manual acceptance (still NOT TESTED).
- Payload fuzz, path replacement and secret-log regression tests.
