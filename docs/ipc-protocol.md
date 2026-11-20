# Supervisor IPC protocol

Version `1`, JSON request/response over a Windows Named Pipe (go-winio, user-SID DACL,
remote clients rejected) or a user-local Unix-domain socket. Each message has a 4-byte
big-endian length prefix; maximum message size 1 MiB.

## Messages

    {"version":1,"request_id":"req-1","operation":"core.start","payload":{"core":"mihomo"}}
    {"version":1,"request_id":"req-1","success":true,"payload":{"core":"mihomo","state":"running"}}

- `request_id` is required and echoed; a mismatch fails the call.
- Version mismatch returns `PROTOCOL_VERSION_MISMATCH`.
- Read calls have a short (30s) handler deadline. Mutations have a separate, longer
  business deadline (15 minutes) and the client uses the caller's deadline rather than a
  fixed transport cap. A client that loses the response polls `operation.get` and never
  blindly re-executes a mutation.
- Clients serialize calls per connection; the server serializes mutations with a mutex.
- The server rejects new mutations once a stop request has persisted its intent.
- Mutation requests may carry `operation_id`. The supervisor atomically claims the ID,
  fingerprints the request (operation + payload digest), and caches the terminal result for
  five minutes from completion (bounded to 256 terminal entries; RUNNING entries are never
  expired or evicted). A duplicate ID with the same fingerprint returns the cached result or
  `OPERATION_IN_PROGRESS`; a different fingerprint is rejected. The cache lives only in the
  supervisor process: a supervisor restart loses it.

## Operations

| Operation | Payload | Notes |
|---|---|---|
| `ping` | — | readiness |
| `supervisor.status` | — | pid, endpoint, version, protocol, cores |
| `supervisor.stop` | — | refused while cores/TUN/desktop proxy are active |
| `core.status` | — | same payload as supervisor.status |
| `core.start` | `{"core":"mihomo"}` | running → `CORE_ALREADY_RUNNING` |
| `core.stop` | `{"core":"mihomo"}` | already stopped → success with `already` |
| `core.restart` | `{"core":"mihomo"}` | serialized |
| `proxies.list` | `{"core":"mihomo"}` | read-only |
| `proxy.select` | `{"core","group","name"}` | serialized; core must be running |
| `network.status` | — | read-only |
| `logs.tail` | `{"limit":200}` | application logs |
| `app.call` | `{"method":"CoreProxies","args":[...]}` | dispatches only Facade methods |

Errors actually emitted: `PROTOCOL_VERSION_MISMATCH`, `CORE_ALREADY_RUNNING`,
`CORE_NOT_RUNNING`, `NETWORK_BLOCKED` (including "supervisor is stopping" and an
operation-id conflict), `OPERATION_IN_PROGRESS`, `UNSUPPORTED_OPERATION`, `BAD_REQUEST`,
`INTERNAL_ERROR`. Codes such as `UNAUTHORIZED`, `CORE_IDENTITY_MISMATCH`,
`PROFILE_NOT_FOUND`, `OPERATION_TIMEOUT` and `SUPERVISOR_UNAVAILABLE` are reserved for
future use; identity failures are currently returned as `INTERNAL_ERROR` messages with a
`CORE_IDENTITY_MISMATCH` prefix.

## Security limitations

Unix endpoints are sockets in the user runtime directory with mode 0600; Linux verifies
`SO_PEERCRED` UID and macOS verifies `LOCAL_PEERCRED` UID before serving a connection.
Windows uses a per-user Named Pipe with an explicit protected DACL granting only
that user SID; the kernel enforces access at connect time and go-winio rejects remote
clients. There is no token. Never expose the endpoint over TCP.
