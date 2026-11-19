# IPC Security

| Platform | Transport | Authentication | Real verification |
|---|---|---|---|
| Windows | Named Pipe `\\.\pipe\proxy-tui-<LogonSID>` | explicit DACL granting only the current Logon SID | Windows live: 5 concurrent CLI, TUI, restart PASS; cross-SID NOT TESTED |
| Linux | Unix socket 0600, dir 0700 | `SO_PEERCRED` UID check | cross-compiled only |
| macOS | Unix socket 0600, dir 0700 | `LOCAL_PEERCRED` UID check | cross-compiled only |

## Windows

- Transport: `github.com/Microsoft/go-winio v0.6.2` (pinned), `winio.ListenPipe` /
  `winio.DialPipeContext`.
- Endpoint suffix is the process **Logon SID**; endpoints are isolated per login session.
  Logon SID retrieval failure fails closed (no endpoint, no server).
- DACL: `D:P(A;;GA;;;<LogonSID>)` — no Everyone, Anonymous, Authenticated Users, SYSTEM or
  Administrators entries. The DACL is protected.
- Access control is enforced by the Windows kernel at connect time. go-winio's public API
  does not expose a reliable client PID to application code, so there is no additional
  app-layer SID check; this is documented rather than faked.
- go-winio v0.6.2 creates pipes with `FILE_PIPE_REJECT_REMOTE_CLIENTS` (verified in its
  source), so remote SMB clients are rejected.
- Pipe creation fails if the name already exists (no takeover of an unknown object); the
  server never falls back to AF_UNIX.
- Live DACL observation: the pipe ACL contains only the Logon SID with FullControl.
- Cross-SID denial: **NOT TESTED** — this machine has no second enabled local account and no
  account was created. It is not claimed as passed.

## Unix

- Socket mode 0600, parent directory 0700, owner checked; symlink/non-socket endpoints are
  rejected. Stale sockets are removed only after a failed dial and an ownership check.
- Linux verifies `SO_PEERCRED` UID; macOS verifies `LOCAL_PEERCRED` UID before serving.
- The client verifies the server peer UID and the socket/owner/permissions before use.
- Tests for round trip, 32 clients, permissions, symlink and stale sockets were added for
  native Linux/macOS. Native execution and CI are **NOT RUN** in this environment.
