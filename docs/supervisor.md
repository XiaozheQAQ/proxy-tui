# Supervisor

Per-user, detached ProxyTUI process that owns the application layer. It is the intended
single writer for runtime state; the CLI already uses it for core lifecycle when running.

## Commands

    proxy supervisor start
    proxy supervisor status [--json]
    proxy supervisor stop
    proxy supervisor logs
    proxy supervisor run        # hidden, used by start

State: `state/runtime/supervisor-state.json`. Endpoint: `state/runtime/supervisor.sock`.
Log: `state/logs/supervisor.log`.

## Lifecycle

- `start` checks the endpoint and reports "already running" if a supervisor answers.
- Otherwise it spawns the same binary with `supervisor run` detached (Windows:
  DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP; Unix: setsid), redirects output to the log
  and waits up to 8s for a ping.
- Single instance is enforced by binding the socket: a live endpoint wins; a stale socket is
  removed only after a failed dial.
- `stop` is refused while a core runs, TUN is enabled, or ProxyTUI still owns the desktop
  proxy.

## Integration boundary

TUI and CLI both connect through the supervisor. `openApp` ensures the supervisor, dials the
local socket and constructs a remote `app.Frontend`; the local application is built only
inside the supervisor. A test-only in-package flag exists for unit tests and is not reachable
from the environment or CLI.

Shared settings are written through `UpdateSettings`. The remote frontend reconnects on
transport failures with bounded backoff and reuses OperationIDs; the supervisor deduplicates
retried mutations. If the supervisor process is gone, it is not auto-restarted yet.

The TUI Overview shows `Supervisor CONNECTED/DISCONNECTED` and blocks mutations while
disconnected. There is no automatic reconnect/backoff yet; a later refresh reports the
disconnect. See `docs/phase3d.md` for the remaining security and validation gaps.
