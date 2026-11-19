//go:build !windows && !linux && !darwin

package ipc

import "net"

func verifyPeer(net.Conn) error { return nil }
