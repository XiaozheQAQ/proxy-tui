//go:build windows

package ipc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// UserSID returns the current Windows user SID. The supervisor scope is the
// user, not the logon session: multiple sessions of the same user share one
// runtime directory, core ports and state files, so they must share one
// supervisor. Different users remain isolated by the kernel DACL.
func UserSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read token user: %w", err)
	}
	if user == nil || user.User.Sid == nil {
		return "", fmt.Errorf("user SID is unavailable")
	}
	return user.User.Sid.String(), nil
}

// EndpointPath returns the Named Pipe name. The default installation is one
// supervisor per Windows user: the endpoint is keyed only by the user SID.
// An explicit PROXY_TUI_HOME is a portable namespace; a runtime-directory
// suffix keeps separate homes and test processes from sharing one pipe. The
// DACL still scopes access to the user, and the pipe name is not the
// authentication mechanism.
func EndpointPath(runtimeDir string) (string, error) {
	sid, err := UserSID()
	if err != nil {
		return "", err
	}
	suffix := strings.ReplaceAll(sid, "\\", "-")
	if os.Getenv("PROXY_TUI_HOME") != "" {
		sum := sha256.Sum256([]byte(filepath.Clean(runtimeDir)))
		suffix += "-" + hex.EncodeToString(sum[:4])
	}
	return `\\.\pipe\proxy-tui-` + suffix, nil
}

// securityDescriptor grants full access only to the current user SID.
func securityDescriptor() (string, error) {
	sid, err := UserSID()
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
