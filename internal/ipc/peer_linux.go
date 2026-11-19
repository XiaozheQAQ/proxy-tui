//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifyPeer(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	var verifyErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			verifyErr = err
			return
		}
		if int(cred.Uid) != os.Geteuid() {
			verifyErr = fmt.Errorf("peer uid %d is not the current user", cred.Uid)
		}
	}); err != nil {
		return err
	}
	return verifyErr
}
