# Phase 3D — Supervisor & IPC

Status: **3D.1 implemented; 3D.2 implemented for the supervisor/CLI control path, TUI
migration pending. 3D.3 and 3D.4 not started.**

## Delivered

- `internal/ipc`: versioned JSON-RPC, 4-byte length framing, 1 MiB message cap, request IDs,
  per-call deadlines, standardized errors, client/server over a user-local Unix socket.
- `internal/supervisor`: detached per-user supervisor with a real `app.App` backend, single
  instance enforced by atomic socket bind, `supervisor-state.json`, supervisor log,
  status/stop, core start/stop/restart, proxies list/select, network status, log tail.
- `proxy supervisor start|status|stop|logs` and hidden `supervisor run`.
- `proxy core start|stop` use the supervisor when it is running; a running supervisor is
  authoritative and errors are not silently retried locally.
- Idempotency: start of a running core → `CORE_ALREADY_RUNNING`; stop of a stopped core
  succeeds with `already`; mutations are serialized by one supervisor mutex.
- Stop is refused while a core runs, TUN is enabled, or ProxyTUI still owns the desktop
  proxy.

## Real verification

- Eight parallel `core start mihomo` clients against one supervisor: exactly 1 started,
  7 `CORE_ALREADY_RUNNING`, exactly one mihomo process.
- `supervisor stop` with a running core was refused with `NETWORK_BLOCKED`.
- Supervisor survived all client exits; closing clients did not stop it.
- `supervisor status` reports pid, endpoint, protocol, version and per-core state.
- Unit tests: framing, oversize rejection, single instance, concurrent start, stop guards,
  protocol-version mismatch; `go test -race` clean for ipc/supervisor.

## Not delivered yet

- The TUI still creates its own `app.App` and manages cores/TUN locally. It does not connect
  to the supervisor, so "all TUI mutations go through one writer" is **not met**. Multiple
  TUIs can still issue conflicting mutations.
- Bare `proxy` does not auto-start the supervisor; `supervisor start` is explicit.
- Windows uses AF_UNIX under the user's LocalAppData (verified on Windows 10+). A Named Pipe
  with an explicit SID ACL is **not** implemented; current protection is the per-user
  directory/socket location.
- No token or peer-credential authentication; no adversarial cross-user test.
- No health monitor, auto-restart/backoff, autostart, log rotation or event subscription
  (3D.3/3D.4).
- No TUI supervisor indicator, reconnect logic or disable-on-disconnect.

## Stages

| Stage | State |
|---|---|
| 3D.1 Supervisor lifecycle, single instance, detached process, state, CLI | implemented except TUI auto-start |
| 3D.2 Protocol, client/server, serialization, idempotency, multi-client CLI | implemented; TUI migration missing |
| 3D.3 Health monitor and recovery | not started |
| 3D.4 Autostart | not started |
