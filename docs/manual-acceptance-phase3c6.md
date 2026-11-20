# Phase 3C.6 — TUI TUN Disable Manual Acceptance

Status: **DOCUMENT READY / MANUAL TEST READY / MANUAL TEST NOT RUN / NOT TESTED.**

This acceptance must be performed by a human on a real Windows desktop. Do not automate
TUN enable/disable, route, DNS or WinINet changes.

## 0. Build the binary under test (do not use an old dist copy)

    cd <proxy-tui-checkout>
    go build -trimpath -ldflags "-X github.com/proxytui/proxy-tui/internal/app.Version=0.2.0-dev" -o proxy.exe ./cmd/proxy
    .\proxy.exe version

Record the Git commit (git rev-parse HEAD) and the version output. The dist/ folder is
ignored and may predate the current HEAD; it is not an acceptance artifact.

## 1. Baseline (record before touching anything)

    $env:PROXY_TUI_HOME = "$env:LOCALAPPDATA\proxy-tui"
    .\proxy.exe supervisor status
    .\proxy.exe status
    Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' |
      Select-Object ProxyEnable, ProxyServer, ProxyOverride
    Get-NetRoute -DestinationPrefix 0.0.0.0/0 | Format-Table ifIndex, NextHop, RouteMetric
    Get-DnsClientServerAddress | Format-Table InterfaceAlias, ServerAddresses
    Get-NetAdapter | Format-Table Name, InterfaceDescription, Status
    Get-Process v2rayN, xray -ErrorAction SilentlyContinue | Select-Object Id, ProcessName

Stop if an external TUN or proxy already owns the default route. Protect the external
v2rayN baseline `127.0.0.1:10808`; never modify it.

## 2. Start and enable TUN through the TUI

1. Start .\proxy.exe and let it connect to the supervisor (Named Pipe, per-user endpoint).
2. Confirm the header shows SUPERVISOR CONNECTED.
3. Open Settings, choose Traffic Mode TUN, confirm the dialog.
4. Wait until the TUI reports TUN running **and** a non-empty interface name. A warning-only
   result is not a PASS; the code refuses to report TUN active without an observed interface.
5. Record: supervisor PID, core PID, core config path/digest, TUN interface name, default
   route, DNS servers.
6. Verify the TUN interface exists:

       Get-NetAdapter | Where-Object Status -eq 'Up'
       Get-NetRoute -DestinationPrefix 0.0.0.0/0

## 3. Kill only the TUI UI process

Find the TUI process (not the supervisor, not the core):

    Get-Process proxy | Select-Object Id, StartTime, Path

Close the TUI terminal or stop only that PID. Do **not** use `taskkill /T`.

Verify the supervisor and core survive:

    .\proxy.exe supervisor status
    Get-Process mihomo, sing-box -ErrorAction SilentlyContinue
    Get-NetAdapter | Where-Object Name -match 'tun|Meta'

## 4. Reconnect a new TUI

Start .\proxy.exe again. It must connect to the existing supervisor (same supervisor PID),
show the existing core as running/adopted, and **not** start a second core or TUN device.

## 5. Disable TUN from the TUI

1. Open Settings / Traffic Mode.
2. Select `Off` (or the TUI Disable action).
3. Confirm the TUI's explicit confirmation dialog.
4. Observe the TUI report that TUN is off and resources were released.

Record the exact TUI screen text and timestamp.

## 6. Verify release

    .\proxy.exe network status
    Get-NetAdapter | Where-Object Name -match 'tun|Meta'
    Get-NetRoute -DestinationPrefix 0.0.0.0/0 | Format-Table ifIndex, NextHop
    Get-DnsClientServerAddress | Format-Table InterfaceAlias, ServerAddresses
    Get-Process mihomo, sing-box -ErrorAction SilentlyContinue
    Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' |
      Select-Object ProxyEnable, ProxyServer

Expected: TUN adapter gone, default route/DNS restored to the baseline, no proxy-tui core
left running, external v2rayN proxy settings unchanged.

## 7. Result table (fill in by hand)

| Step | Expected | Observed | PASS/FAIL |
|---|---|---|---|
| Baseline recorded | values saved | | |
| TUI TUN enabled | core + TUN up | | |
| TUI killed | supervisor/core alive | | |
| New TUI | adopts same core | | |
| TUI Disable | request accepted | | |
| Core stopped | no core process | | |
| TUN adapter released | no tun/Meta | | |
| Route restored | baseline route | | |
| DNS restored | baseline DNS | | |
| External proxy unchanged | ProxyEnable/Server unchanged | | |
| No leftovers | no supervisor/core/TUN | | |

## 8. Final state

If every row is PASS with real screen/command evidence, record:

    Phase 3C.6 TUI Disable: PASS (date, operator, TUI version 0.2.0-dev)

Otherwise keep **NOT TESTED** or record the failure. CLI Disable is not a substitute for
this test.
