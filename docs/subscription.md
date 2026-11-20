# Subscription pipeline

Importing existing subscriptions is the main feature of ProxyTUI. This document describes
the exact behaviour so it can be tested and trusted.

## Accepted inputs

| Input | Handling |
|---|---|
| One or more HTTP(S) URLs | fetched, then detected |
| Mihomo / Clash YAML | parsed, nodes extracted, routing preserved |
| sing-box JSON | parsed, outbounds extracted, routing preserved |
| Base64 encoded node list | decoded once, then detected again |
| Plain share-link list | parsed line by line |
| Local file (CLI only) | read and treated as inline content |

Share links supported today: `ss://`, `vmess://`, `vless://`, `trojan://`, `hysteria2://`
(and `hy2://`), `tuic://`, `socks://`, `socks5://`, `http://`, `https://` (http(s) links are
only treated as proxies when they carry credentials or a bare IP, so subscription URLs are
not mistaken for proxies).

## Detection order

1. **JSON structure** — `outbounds`, `inbounds`, `route`, `dns`, `log` indicate sing-box.
2. **YAML structure** — `proxies`, `proxy-providers`, `proxy-groups`, `rules`, `mixed-port`
   indicate Mihomo.
3. **Share links** — if every non-empty line looks like a share link, the payload is a node
   list. This format is core agnostic; the target core comes from the dialog.
4. **Base64** — only if the payload is valid base64 and decodes to printable text; detection
   then runs again on the decoded bytes and the encoding is recorded.

Anything else is rejected with an explanation instead of being stored.

## Conversion rules

| Source | Target | Result |
|---|---|---|
| Mihomo config with rules/groups/DNS | Mihomo | stored verbatim, activatable |
| Mihomo config without routing | Mihomo | expanded with the base template (groups + rules) |
| sing-box config with route/DNS | sing-box | stored verbatim, activatable |
| sing-box config without routing | sing-box | expanded with the base template |
| Mihomo config with routing | sing-box | stored, **not activatable**, warning shown |
| sing-box config with routing | Mihomo | stored, **not activatable**, warning shown |
| Node list (links/base64) | either | rendered from nodes into the target core format |

Cross-core imports never drop rules silently. The profile keeps the original bytes and the
UI explains why it cannot be activated.

## Validation

* When the target binary is installed, the rendered config is checked with
  `mihomo -t -f <file>` or `sing-box check -c <file>` before it is written as active.
* When the binary is missing, the import succeeds with a warning. The profile becomes
  activatable later; starting the core re-validates implicitly.
* A failed validation aborts the import; nothing is written.

## Updates and rollback

* Each profile stores the source URL, ETag, Last-Modified, interval and the last result.
* `Update` fetches with `If-None-Match`/`If-Modified-Since`. A `304` only refreshes
  timestamps.
* New content is detected, parsed, rendered and validated **before** it replaces anything.
* On success the new revision is written as a snapshot and the active file is swapped
  atomically. On failure the previous revision keeps serving traffic and the error is
  recorded in the profile (`last_update`).
* `Rollback` restores any snapshot after re-validating it, and `Snapshots` can be inspected
  from the CLI/TUI.
* The last 10 snapshots per profile are retained.

## Limits and protections

* 25 s request timeout, 5 redirects, 8 MiB response cap.
* Only `http://` and `https://` URLs are fetched; `file://` and other schemes are refused.
* Empty responses, invalid YAML/JSON and payloads without usable nodes are rejected.
* Unsupported entries are listed with protocol and reason; the import continues with the
  supported ones and reports the count.
* Tokens in URLs are redacted in logs, errors and the UI (including path segments). The
  original URL is persisted with owner-only permissions because updates must reuse it.

## Known limitations

* `proxy-providers` blocks are preserved verbatim; ProxyTUI does not resolve provider nodes
  itself, so the node count may be 0 for those profiles.
* Only the protocols above are parsed. WireGuard, SSH, Hysteria v1, AnyTLS and similar
  entries are reported as unsupported.
* Conversion between cores is intentionally limited to node lists.
