# Phase 3C.6 — Manual TUI Disable Acceptance

This procedure must be performed by a human at the real keyboard. Automated keystroke
injection or coordinate guessing is **not** an acceptable substitute: the earlier automated
run toggled the wrong row and enabled the system proxy, interrupting the upstream stream.
The TUI now asks for confirmation before every traffic-mode change, and the system proxy is
only enabled after the core port is verified.

## Preconditions

- Windows, elevated terminal (Run as administrator).
- `PROXY_TUI_HOME` points at the test home with the real profiles
  (`%TEMP%\pt-tun-live` in this acceptance) or the user's own valid profiles.
- No TUN active, no ProxyTUI core running.
- Record before-state:
  `proxy network tun status`, `proxy network status`,
  `Get-NetRoute -AddressFamily IPv4`, `Get-DnsClientServerAddress`,
  `Get-NetAdapter -IncludeHidden`.

## Mihomo

1. Start the TUI: `dist\proxy.exe` (elevated).
2. Press `2` for the Mihomo page; confirm the active profile is the real one.
3. Press `6` for Settings. Move to the TUN row (`TUN`, the second-to-last row) using
   `↑/↓`; the selected row must be visibly highlighted.
4. Press `Enter`. A **CONFIRM** dialog must appear with
   "Change traffic mode: off → TUN". Press `y` (or ←→ then Enter).
5. Wait for "traffic mode: off → TUN (Meta)". Verify:
   `proxy network tun status` shows enabled/Meta, default route via Meta, and a
   `curl --noproxy "*" https://api.ipify.org` succeeds.
6. Force-stop **only the TUI PID** from another elevated terminal:
   `Stop-Process -Id <TUI_PID> -Force` (never `taskkill /T`).
7. Verify the mihomo process, Meta adapter, route and `curl --noproxy` still work.
8. Start the TUI again; it must adopt the same core PID (no second mihomo process) and show
   the running TUN state.
9. Press `6` for Settings. Move to the TUN row and visually confirm it is selected.
10. Press `Enter`. The CONFIRM dialog must say "Change traffic mode: TUN → off". Press `y`.
11. Verify "traffic mode: TUN → off (restored)", `network tun status` disabled, Meta gone,
    default route back to WLAN, DNS restored and `ProxyEnable` unchanged from the
    before-state.
12. Repeat steps 3–11 for sing-box: press `3` for the sing-box page, `6` for Settings,
    then the same TUN row and confirmations.

## Pass criteria

- Confirmation appears for enable and disable; cancel changes nothing.
- TUI crash leaves core, TUN, route, DNS and traffic intact; new TUI adopts the core.
- TUI Disable releases the adapter and restores routing/DNS; no leftover core or state.
- No system-proxy change occurs unless the "System proxy" mode was explicitly confirmed,
  and it is refused when the core port is not listening.

## Record

Save: TUI/core PIDs and paths, TUI screenshots of the confirmation and final state,
`network tun status` before/after, routes/DNS/adapters before/after, and the egress results.
