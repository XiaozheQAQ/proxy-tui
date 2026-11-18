//go:build windows

package systemproxy

import "github.com/proxytui/proxy-tui/internal/network"

func platformAdapters() []network.Adapter {
	return []network.Adapter{NewWindows()}
}
