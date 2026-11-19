# Supervisor IPC protocol

Version `1`, JSON request/response over a user-local Unix-domain socket (Windows 10+
AF_UNIX). Each message has a 4-byte big-endian length prefix; maximum message size 1 MiB.

## Messages

    {"version":1,"request_id":"req-1","operation":"core.start","payload":{"core":"mihomo"}}
    {"version":1,"request_id":"req-1","success":true,"payload":{"core":"mihomo","state":"running"}}

- `request_id` is required and echoed; a mismatch fails the call.
- Version mismatch returns `PROTOCOL_VERSION_MISMATCH`.
- Calls have deadlines; the server applies a 30s handler timeout.
- Clients serialize calls per connection; the server serializes mutations with a mutex.

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

Errors: `SUPERVISOR_UNAVAILABLE`, `PROTOCOL_VERSION_MISMATCH`, `UNAUTHORIZED`,
`CORE_ALREADY_RUNNING`, `CORE_NOT_RUNNING`, `CORE_IDENTITY_MISMATCH`,
`PROFILE_NOT_FOUND`, `NETWORK_BLOCKED`, `OPERATION_IN_PROGRESS`,
`OPERATION_TIMEOUT`, `UNSUPPORTED_OPERATION`, `BAD_REQUEST`, `INTERNAL_ERROR`.

## Security limitations

The endpoint is a socket in the user runtime directory with mode 0600 (Unix) and inherits
the per-user LocalAppData profile on Windows. There is no token or peer-credential
handshake and no Windows Named Pipe SID ACL yet. Never expose it over TCP.
