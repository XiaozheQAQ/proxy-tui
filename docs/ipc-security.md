# IPC Security

| Platform | Transport | Authentication | Real verification |
|---|---|---|---|
| Windows | AF_UNIX under LocalAppData | **none** (no peer credentials; Named Pipe not shipped) | Windows run, but cross-user denial NOT TESTED |
| Linux | Unix socket 0600, dir 0700 | `SO_PEERCRED` UID check | cross-compiled only |
| macOS | Unix socket 0600, dir 0700 | `LOCAL_PEERCRED` UID check | cross-compiled only |

- No TCP endpoint is exposed.
- Unix peer UID mismatch is rejected before serving a request.
- Windows relies on the per-user LocalAppData directory ACL. A Named Pipe with an explicit
  user-SID DACL and server-side client SID check is required before claiming Windows IPC
  security. An attempt in this round was reverted after handle-lifecycle crashes.
- No token authentication exists.
- Request payloads are decoded into fixed facade method signatures; arbitrary methods and
  free-form paths are not reachable. Message size is capped at 1 MiB.
- Cross-user adversarial tests and secret-log regression tests are NOT TESTED.
