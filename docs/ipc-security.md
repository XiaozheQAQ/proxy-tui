# IPC Security

| Platform | Transport | Authentication | Real verification |
|---|---|---|---|
| Windows | Named Pipe with UserSID (optional runtime hash for PROXY_TUI_HOME) | explicit protected DACL granting only the current user SID | Windows live: named-pipe lifecycle, 32 clients, 100 restarts, conflict, same-user round trip PASS; cross-user NOT TESTED |
| Linux | Unix socket 0600, dir 0700 | SO_PEERCRED UID check (server and client) | unit tests PASS on the Windows-built tree; native Linux execution NOT RUN here |
| macOS | Unix socket 0600, dir 0700 | LOCAL_PEERCRED UID check (server and client) | cross-compiled/vet PASS; native macOS NOT RUN |

## Windows

- Transport: github.com/Microsoft/go-winio v0.6.2 (pinned), winio.ListenPipe / winio.DialPipeContext.
- Scope is the Windows user, not the logon session. The runtime directory is per user and core ports are per user, so two sessions of the same user must share one supervisor; a per-session supervisor would race on the same state files and ports. The default endpoint is keyed only by the user SID (one supervisor per user). An explicit PROXY_TUI_HOME is a portable namespace and appends a short runtime-directory hash so separate homes/tests do not share a pipe; core ports are still global, so only one runtime home should manage cores at a time.
- User SID retrieval failure fails closed (no endpoint, no server).
- DACL: D:P(A;;GA;;;<UserSID>) - no Everyone, Anonymous, Authenticated Users, SYSTEM or Administrators entries. The DACL is protected.
- Access control is enforced by the Windows kernel at connect time. go-winio's public API does not expose a reliable client PID to application code, so there is no additional app-layer SID check; this is documented rather than faked.
- go-winio v0.6.2 creates pipes with FILE_PIPE_REJECT_REMOTE_CLIENTS (verified in its source), so remote SMB clients are rejected.
- Pipe creation fails if the name already exists (no takeover of an unknown object); the server never falls back to AF_UNIX.
- Cross-user denial: NOT TESTED - this machine has no second enabled local account and no account was created. It is not claimed as passed.

## Unix

- Socket mode 0600, parent directory 0700, owner checked; symlink/non-socket endpoints are rejected. Stale sockets are removed only after a failed dial and an ownership check.
- Linux verifies SO_PEERCRED UID; macOS verifies LOCAL_PEERCRED UID before serving.
- Client verifies the server peer UID and the socket/owner/permissions before use.
- Tests for round trip, 32 clients, permissions, symlink, stale sockets and a non-Unix connection fail-closed path exist. A real different-UID denial test requires a second account/root and is NOT TESTED. Native Linux/macOS execution is NOT RUN here.

## Trust boundary and non-goals

- Trust boundary: the local OS user (Unix UID / Windows user SID). Same-user processes are treated as trusted; a malicious same-user process can impersonate the supervisor endpoint or otherwise act as the user. Defending against same-user malware is not a current phase blocker and is not claimed.
- The client does not authenticate the server with a token on Windows; it relies on the kernel DACL of the connected pipe and the per-user endpoint. On Unix it additionally checks the server UID.
- There is no TCP listener and no unauthenticated fallback. Do not expose the endpoint over a network share.
- Upgrading from a build that used a Logon-SID-only pipe: stop the old supervisor before
  starting the new one. The new client does not discover the old pipe; leaving it running
  would leave a second supervisor competing for the same core ports.
