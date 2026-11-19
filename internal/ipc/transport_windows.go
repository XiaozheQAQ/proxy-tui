//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// LogonSID returns the current login session SID. It fails closed when the
// token does not expose a logon SID.
func LogonSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	groups, err := token.GetTokenGroups()
	if err != nil {
		return "", fmt.Errorf("read token groups: %w", err)
	}
	for _, group := range groups.AllGroups() {
		if group.Attributes&windows.SE_GROUP_LOGON_ID == windows.SE_GROUP_LOGON_ID {
			return group.Sid.String(), nil
		}
	}
	return "", fmt.Errorf("logon SID is unavailable")
}

// EndpointPath returns the per-login-session Named Pipe name. The pipe name is
// not a secret and is not the authentication mechanism; the DACL is.
func EndpointPath(string) (string, error) {
	sid, err := LogonSID()
	if err != nil {
		return "", err
	}
	suffix := strings.ReplaceAll(sid, "\\", "-")
	return `\\.\pipe\proxy-tui-` + suffix, nil
}

// securityDescriptor grants full access only to the current logon session.
func securityDescriptor() (string, error) {
	sid, err := LogonSID()
	if err != nil {
		return "", err
	}
	return "D:P(A;;GA;;;" + sid + ")", nil
}

// Listen creates a go-winio Named Pipe restricted to the current logon SID.
// go-winio sets FILE_PIPE_REJECT_REMOTE_CLIENTS, and creation fails if the pipe
// name already exists, so another process cannot be silently taken over.
func Listen(endpoint string) (net.Listener, error) {
	sddl, err := securityDescriptor()
	if err != nil {
		return nil, err
	}
	listener, err := winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("IPC_ENDPOINT_CONFLICT: create Named Pipe %s: %w", endpoint, err)
	}
	return listener, nil
}

// DialConn connects to the current login session's supervisor pipe. Access is
// enforced by the Named Pipe DACL at connect time.
func DialConn(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, endpoint)
}
