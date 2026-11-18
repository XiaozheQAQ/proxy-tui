//go:build !windows

package systemproxy

import (
	"os"
	"runtime"
	"strings"

	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/network/execx"
)

func platformAdapters() []network.Adapter {
	switch runtime.GOOS {
	case "darwin":
		return []network.Adapter{NewDarwin(execx.OSRunner{})}
	case "linux":
		desktop := strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP") + " " + os.Getenv("DESKTOP_SESSION"))
		return []network.Adapter{NewLinux(execx.OSRunner{}, desktop)}
	default:
		return nil
	}
}
