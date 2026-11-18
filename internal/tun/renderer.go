package tun

import (
	"fmt"

	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// RenderResult is a configuration with the TUN settings applied.
type RenderResult struct {
	Config    []byte
	Format    model.Format
	Overrides []string
	Warnings  []string
}

// Render applies settings to a raw core configuration without mutating it.
// Existing DNS, rules, proxy groups and providers are preserved; only the TUN
// inbound/block and the routing hooks it needs are touched.
func Render(kind model.CoreKind, raw []byte, settings Settings, coreVersion string) (RenderResult, error) {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return RenderResult{}, err
	}
	switch kind {
	case model.CoreMihomo:
		return renderMihomo(raw, settings)
	case model.CoreSingBox:
		return renderSingBox(raw, settings, coreVersion)
	default:
		return RenderResult{}, fmt.Errorf("TUN is not supported for core %q", kind)
	}
}
