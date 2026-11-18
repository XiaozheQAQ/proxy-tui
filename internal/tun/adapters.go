package tun

import (
	"sort"
	"strings"
)

// AdapterClass is the identity-based classification of a network adapter.
type AdapterClass string

// Adapter classes.
const (
	AdapterPhysical       AdapterClass = "PHYSICAL"
	AdapterHostOnly       AdapterClass = "HOST_ONLY"
	AdapterVPNTunnel      AdapterClass = "VPN_TUNNEL"
	AdapterVPNWireGuard   AdapterClass = "VPN_WIREGUARD"
	AdapterVirtualUnknown AdapterClass = "VIRTUAL_UNKNOWN"
	AdapterUnknown        AdapterClass = "UNKNOWN"
)

// AdapterInfo is the observed identity of one adapter. InterfaceAlias and
// InterfaceDescription are informational: classification never relies on the
// friendly name alone.
type AdapterInfo struct {
	InterfaceIndex       int          `json:"interface_index"`
	InterfaceAlias       string       `json:"interface_alias"`
	InterfaceDescription string       `json:"interface_description,omitempty"`
	PnPDeviceID          string       `json:"pnp_device_id,omitempty"`
	HardwareIDs          []string     `json:"hardware_ids,omitempty"`
	DriverProvider       string       `json:"driver_provider,omitempty"`
	DriverService        string       `json:"driver_service,omitempty"`
	Manufacturer         string       `json:"manufacturer,omitempty"`
	ComponentID          string       `json:"component_id,omitempty"`
	InterfaceType        string       `json:"interface_type,omitempty"`
	Virtual              bool         `json:"virtual"`
	Up                   bool         `json:"up"`
	Class                AdapterClass `json:"class"`
	ClassReason          string       `json:"class_reason,omitempty"`
	Infrastructure       bool         `json:"infrastructure,omitempty"`
}

// AdapterIdentity is the raw evidence used by ClassifyAdapter.
type AdapterIdentity struct {
	InterfaceAlias       string
	InterfaceDescription string
	PnPDeviceID          string
	HardwareIDs          []string
	DriverProvider       string
	DriverService        string
	Manufacturer         string
	ComponentID          string
	InterfaceType        string
}

// ClassifyAdapter derives the adapter class strictly from driver and PnP
// identity. A friendly name alone can never produce HOST_ONLY or VPN classes.
func ClassifyAdapter(raw AdapterIdentity) (AdapterClass, string, bool) {
	desc := lowerASCII(raw.InterfaceDescription)
	alias := lowerASCII(raw.InterfaceAlias)
	provider := lowerASCII(raw.DriverProvider)
	service := lowerASCII(raw.DriverService)
	pnp := lowerASCII(raw.PnPDeviceID)
	inf := lowerASCII(raw.ComponentID)
	ids := lowerASCII(strings.Join(raw.HardwareIDs, " "))
	all := strings.Join([]string{desc, alias, provider, service, pnp, inf, ids}, " ")

	isVBoxHostOnly := strings.Contains(service, "vboxnetadp") ||
		strings.Contains(ids, "sun_vboxnetadp") ||
		strings.Contains(inf, "vboxnetadp") ||
		(strings.Contains(provider, "oracle") && strings.Contains(desc, "host-only") && strings.Contains(pnp, "root\\net"))
	if isVBoxHostOnly {
		return AdapterHostOnly, "driver/device confirms VirtualBox Host-Only (VBoxNetAdp)", true
	}

	isWireGuard := strings.Contains(service, "wireguard") || strings.Contains(ids, "wireguard") ||
		strings.Contains(desc, "wireguard")
	if isWireGuard && !strings.Contains(service, "wintun") && !strings.Contains(ids, "wintun") {
		return AdapterVPNWireGuard, "WireGuard driver identity", true
	}

	isWintun := strings.Contains(service, "wintun") || strings.Contains(ids, "wintun") ||
		strings.Contains(pnp, "swd\\wintun") || strings.Contains(desc, "wintun")
	isTAP := strings.Contains(service, "tap0901") || strings.Contains(ids, "tap0901") ||
		strings.Contains(desc, "tap-windows") || strings.Contains(desc, "tap-win")
	isTunnelDriver := isWintun || isTAP || strings.Contains(service, "tun") ||
		strings.Contains(desc, "tunnel") || strings.Contains(pnp, "wintun") || strings.Contains(pnp, "tap")
	if isTunnelDriver {
		return AdapterVPNTunnel, "tunnel driver identity (Wintun/TAP)", true
	}

	physicalBus := strings.HasPrefix(pnp, "pci\\") || strings.HasPrefix(pnp, "usb\\") ||
		strings.HasPrefix(pnp, "bth\\") || strings.HasPrefix(pnp, "sd\\") ||
		strings.HasPrefix(pnp, "acpi\\")
	virtualBus := strings.HasPrefix(pnp, "root\\") || strings.HasPrefix(pnp, "swd\\") ||
		strings.Contains(all, "virtual") || strings.Contains(all, "vbox") ||
		strings.Contains(all, "vmware") || strings.Contains(all, "hyper-v")
	if physicalBus && !virtualBus {
		return AdapterPhysical, "physical bus device", false
	}
	if virtualBus {
		return AdapterVirtualUnknown, "virtual/root device without a known tunnel driver", true
	}
	if raw.PnPDeviceID == "" && raw.DriverService == "" && raw.DriverProvider == "" {
		return AdapterUnknown, "no PnP or driver identity available", false
	}
	return AdapterUnknown, "identity did not match a known class", false
}

// adapterInfrastructure reports virtual network infrastructure that is not a
// tunnel and is intentionally excluded from conflict checks.
func adapterInfrastructure(raw AdapterIdentity) bool {
	all := lowerASCII(strings.Join([]string{
		raw.InterfaceAlias, raw.InterfaceDescription, raw.DriverProvider,
		raw.DriverService, strings.Join(raw.HardwareIDs, " "), raw.PnPDeviceID,
	}, " "))
	for _, token := range []string{
		"vethernet", "vswitch", "default switch", "hyper-v", "wsl",
		"loopback", "bluetooth", "kernel debug", "teredo", "6to4", "ip-https", "isatap",
	} {
		if strings.Contains(all, token) {
			return true
		}
	}
	return false
}

// SortAdapters orders adapters by interface index for stable output.
func SortAdapters(adapters []AdapterInfo) {
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].InterfaceIndex < adapters[j].InterfaceIndex })
}
