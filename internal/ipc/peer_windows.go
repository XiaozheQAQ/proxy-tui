//go:build windows

package ipc

import "net"

// verifyPeer is a no-op on Windows AF_UNIX: Go does not expose peer
// credentials for AF_UNIX, and Named Pipes with an explicit SID ACL are not
// implemented yet. Access relies on the per-user LocalAppData directory.
func verifyPeer(net.Conn) error { return nil }
