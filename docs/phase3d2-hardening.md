# Phase 3D.2.2 — Security & Coordination Hardening

Status: **PARTIAL.** Shared settings, OperationID dedupe and bounded reconnect are
implemented and tested. Windows Named Pipe/SID ACL is **not shipped** (an implementation was
attempted but its handle lifecycle caused crashes in testing and was reverted). Cross-SID,
native Linux/macOS and multi-TUI interactive acceptance remain NOT TESTED.

## Delivered

- Shared settings: TUI/CLI use `SettingsSnapshot`/`UpdateSettings`; no direct `config.json`
  writes. Supervisor atomically writes and only then updates its in-memory copy.
- Operation lifecycle: mutation requests carry `operation_id`; the supervisor records
  RUNNING/SUCCEEDED/FAILED and caches results for five minutes. Duplicate IDs return the
  cached result; concurrent duplicates return `OPERATION_IN_PROGRESS`.
- `proxy operation status <id> [--json]` queries a result without executing anything.
- Remote frontend retries transport failures with 1s/2s/5s/10s and reuses the operation ID.
  It reconnects to the same endpoint but does not auto-start a supervisor after a process
  exit yet.
- Unix: Linux `SO_PEERCRED` and macOS `LOCAL_PEERCRED` verification code exists; only
  cross-compiled, not run natively.

## Not delivered

- **Windows Named Pipe + SID DACL / client SID verification is not in the shipped binary.**
  Windows still uses AF_UNIX under LocalAppData and has no peer-credential check.
- No real cross-SID denial test (only one enabled local account).
- No native Linux/macOS integration run.
- No interactive two-TUI live acceptance; concurrency was exercised via CLI/IPC tests.
- Supervisor auto-ensure after a crashed supervisor is not implemented.
- Operation result is only available for IDs the client generated; the CLI does not expose
  the ID of an operation it started.
- Payload fuzzing, path-replacement and secret-regression tests are incomplete.
- Phase 3C.6 TUI Disable remains NOT TESTED.

## Tests

- Shared settings visible across two remote clients (live).
- Duplicate OperationID executes once; distinct ID executes twice; operation status
  SUCCEEDED (unit).
- Facade mutations serialized; unknown facade method rejected.
- 8 concurrent core starts -> one start.
- Transport failure reconnect code exists (not live-tested with a supervisor restart).
- `gofmt`/`go vet`/`go test ./...`/`-race` and five-platform builds pass.

## Completion

Phase 3D.2 remains **PARTIAL**; 3D.3/3D.4 are not started.
