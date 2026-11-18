package tun

import "testing"

func TestClassifyVirtualBoxHostOnly(t *testing.T) {
	raw := AdapterIdentity{
		InterfaceAlias:       "Ethernet 9",
		InterfaceDescription: "VirtualBox Host-Only Ethernet Adapter",
		PnPDeviceID:          `ROOT\NET\0000`,
		HardwareIDs:          []string{"sun_vboxnetadp"},
		DriverProvider:       "Oracle Corporation",
		DriverService:        "VBoxNetAdp",
		ComponentID:          "sun_vboxnetadp",
		InterfaceType:        "ethernet",
	}
	class, reason, virtual := ClassifyAdapter(raw)
	if class != AdapterHostOnly || !virtual {
		t.Fatalf("class = %s virtual=%v reason=%s", class, virtual, reason)
	}
}

func TestClassifyWintunTunnel(t *testing.T) {
	raw := AdapterIdentity{
		InterfaceAlias:       "Ethernet 10",
		InterfaceDescription: "Wintun Userspace Tunnel",
		PnPDeviceID:          `SWD\WINTUN\{7D7D9015-6C82-D838-2430-F41B93B28148}`,
		HardwareIDs:          []string{"Wintun"},
		DriverProvider:       "WireGuard LLC",
		DriverService:        "wintun",
	}
	class, reason, virtual := ClassifyAdapter(raw)
	if class != AdapterVPNTunnel || !virtual {
		t.Fatalf("class = %s virtual=%v reason=%s", class, virtual, reason)
	}
}

func TestClassifyWireGuard(t *testing.T) {
	raw := AdapterIdentity{
		InterfaceAlias:       "WireGuard",
		InterfaceDescription: "WireGuard Tunnel",
		PnPDeviceID:          `ROOT\NET\0001`,
		HardwareIDs:          []string{"WireGuard"},
		DriverService:        "WireGuard",
	}
	class, _, virtual := ClassifyAdapter(raw)
	if class != AdapterVPNWireGuard || !virtual {
		t.Fatalf("class = %s virtual=%v", class, virtual)
	}
}

func TestClassifyPhysical(t *testing.T) {
	raw := AdapterIdentity{
		InterfaceAlias:       "WLAN",
		InterfaceDescription: "Intel(R) Wi-Fi 7 BE200 320MHz",
		PnPDeviceID:          `PCI\VEN_8086&DEV_272B&SUBSYS_00F48086&REV_00\6&1A2B3C4D&0&0000`,
		HardwareIDs:          []string{`PCI\VEN_8086&DEV_272B`},
		DriverProvider:       "Intel",
		DriverService:        "Netwaw18",
		ComponentID:          `PCI\VEN_8086&DEV_272B`,
		InterfaceType:        "wifi",
	}
	class, _, virtual := ClassifyAdapter(raw)
	if class != AdapterPhysical || virtual {
		t.Fatalf("class = %s virtual=%v", class, virtual)
	}
}

func TestClassifyUnknownVirtual(t *testing.T) {
	raw := AdapterIdentity{
		InterfaceAlias:       "Ethernet 11",
		InterfaceDescription: "Mystery Virtual Adapter",
		PnPDeviceID:          `ROOT\NET\0002`,
		DriverService:        "mystery",
	}
	class, _, virtual := ClassifyAdapter(raw)
	if class != AdapterVirtualUnknown || !virtual {
		t.Fatalf("class = %s virtual=%v", class, virtual)
	}
}

func TestClassifyUnknownIdentity(t *testing.T) {
	class, _, virtual := ClassifyAdapter(AdapterIdentity{})
	if class != AdapterUnknown || virtual {
		t.Fatalf("class = %s virtual=%v", class, virtual)
	}
}

func TestAdapterInfrastructureExcluded(t *testing.T) {
	deferred := []AdapterIdentity{
		{InterfaceAlias: "vEthernet (WSL)", PnPDeviceID: `ROOT\VMS_MP\0000`, DriverService: "VMSMP"},
		{InterfaceAlias: "vSwitch (Default Switch)", PnPDeviceID: `ROOT\VMS_MP\0001`, DriverService: "VMSMP"},
	}
	for _, raw := range deferred {
		if !adapterInfrastructure(raw) {
			t.Errorf("%s should be infrastructure", raw.InterfaceAlias)
		}
	}
}
