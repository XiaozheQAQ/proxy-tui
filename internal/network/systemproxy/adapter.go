// Package systemproxy implements desktop system proxy control per platform.
//
// Windows uses the WinINet registry settings of the current user, macOS uses
// networksetup and Linux uses GSettings (GNOME family). The shell environment
// variables HTTP_PROXY/HTTPS_PROXY are detected but never modified.
package systemproxy

import (
	"github.com/proxytui/proxy-tui/internal/network"
)

// ForPlatform returns the adapters available on the current operating system,
// most preferred first.
func ForPlatform() []network.Adapter {
	return platformAdapters()
}
