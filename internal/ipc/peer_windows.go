//go:build windows

package ipc

import "net"

// verifyPeer is a no-op for Windows Named Pipes: there is no AF_UNIX peer
// credential, and access is enforced by the per-user SID DACL at connect time.
func verifyPeer(net.Conn) error { return nil }
