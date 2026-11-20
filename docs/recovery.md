# Recovery

Existing recovery is unchanged:

- Core state and `tun-state.json` ownership use PID, process start time, executable path,
  binary SHA-256 and the actual rendered-config SHA-256 (path + digest). Legacy or
  mismatched state is never killed; it is reported for manual inspection.
- A verified process whose control API is temporarily unavailable is DEGRADED, not STOPPED;
  its state is kept and a second core is not started.
- Network transactions keep the Phase 3A journal/backup and restore only when the proxy is
  still owned by ProxyTUI.
- After a core crash the TUN adapter normally disappears; the next ProxyTUI invocation
  performs startup recovery and clears state once interfaces are gone. Leftovers keep the
  state and are reported for manual action.

Supervisor-specific:

- A supervisor start uses the same `app.New` startup recovery before serving IPC.
- `supervisor.stop` refuses to exit while a core, TUN or ProxyTUI-owned desktop proxy is
  active; it never force-removes routes or DNS.
- `supervisor-state.json` is removed on clean shutdown.
- No automatic core restart/backoff yet (3D.3); a crashed core is reported, not silently
  restarted.
