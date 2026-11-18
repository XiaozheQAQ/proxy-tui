//go:build windows

package tun

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsNetworkConnectionsKey = `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`
	windowsAdapterClassRoot      = `SYSTEM\CurrentControlSet\Control\Class\{4D36E972-E325-11CE-BFC1-08002BE10318}`
	windowsEnumRoot              = `SYSTEM\CurrentControlSet\Enum`
)

type windowsConnection struct {
	guid string
	pnp  string
}

type windowsClassMeta struct {
	driverDesc string
	provider   string
	infPath    string
	ifType     int
}

type windowsEnumMeta struct {
	deviceDesc   string
	manufacturer string
	service      string
	hardwareIDs  []string
	componentID  string
}

func inspectWindowsAdapters(facts *Facts) {
	connections := windowsConnections()
	classes := windowsClassInfo()
	facts.Adapters = make([]AdapterInfo, 0, len(facts.Interfaces))
	for _, iface := range facts.Interfaces {
		conn := connections[lowerASCII(iface.Name)]
		class := classes[strings.ToLower(conn.guid)]
		enum := windowsEnumInfo(conn.pnp)

		description := class.driverDesc
		if description == "" {
			description = enum.deviceDesc
		}
		hardware := enum.hardwareIDs
		component := enum.componentID
		if component == "" && len(hardware) > 0 {
			component = hardware[0]
		}
		manufacturer := enum.manufacturer
		if manufacturer == "" {
			manufacturer = class.provider
		}
		raw := AdapterIdentity{
			InterfaceAlias:       iface.Name,
			InterfaceDescription: description,
			PnPDeviceID:          conn.pnp,
			HardwareIDs:          hardware,
			DriverProvider:       class.provider,
			DriverService:        enum.service,
			Manufacturer:         manufacturer,
			ComponentID:          component,
			InterfaceType:        ifTypeName(class.ifType),
		}
		class_, reason, virtual := ClassifyAdapter(raw)
		info := AdapterInfo{
			InterfaceIndex:       iface.Index,
			InterfaceAlias:       iface.Name,
			InterfaceDescription: description,
			PnPDeviceID:          conn.pnp,
			HardwareIDs:          hardware,
			DriverProvider:       class.provider,
			DriverService:        enum.service,
			Manufacturer:         manufacturer,
			ComponentID:          component,
			InterfaceType:        ifTypeName(class.ifType),
			Virtual:              virtual,
			Up:                   interfaceUp(iface),
			Class:                class_,
			ClassReason:          reason,
			Infrastructure:       adapterInfrastructure(raw),
		}
		if conn.pnp == "" && description == "" {
			info.ClassReason = "no PnP or driver metadata"
		}
		facts.Adapters = append(facts.Adapters, info)

		switch class_ {
		case AdapterVPNTunnel:
			facts.TUNDevices = append(facts.TUNDevices, iface.Name)
		case AdapterVPNWireGuard:
			facts.VPNDevices = append(facts.VPNDevices, iface.Name)
		case AdapterHostOnly:
			facts.HostOnlyDevices = append(facts.HostOnlyDevices, iface.Name)
		case AdapterVirtualUnknown:
			if !info.Infrastructure {
				facts.UnclassifiedVirtual = append(facts.UnclassifiedVirtual, iface.Name)
			}
		case AdapterUnknown:
			if info.Up && !info.Infrastructure {
				facts.UnknownAdapters = append(facts.UnknownAdapters, iface.Name)
			}
		}
	}
	SortAdapters(facts.Adapters)
}

func windowsConnections() map[string]windowsConnection {
	out := map[string]windowsConnection{}
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsNetworkConnectionsKey,
		registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return out
	}
	defer func() { _ = root.Close() }()
	guids, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return out
	}
	for _, guid := range guids {
		conn, err := registry.OpenKey(root, guid+`\Connection`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, nameErr := conn.GetStringValue("Name")
		pnp, _, _ := conn.GetStringValue("PnpInstanceID")
		_ = conn.Close()
		if nameErr != nil || name == "" {
			continue
		}
		out[lowerASCII(name)] = windowsConnection{guid: guid, pnp: pnp}
	}
	return out
}

func windowsClassInfo() map[string]windowsClassMeta {
	out := map[string]windowsClassMeta{}
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsAdapterClassRoot,
		registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return out
	}
	defer func() { _ = root.Close() }()
	subkeys, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return out
	}
	for _, sub := range subkeys {
		entry, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		instance, _, instanceErr := entry.GetStringValue("NetCfgInstanceId")
		desc, _, _ := entry.GetStringValue("DriverDesc")
		provider, _, _ := entry.GetStringValue("ProviderName")
		inf, _, _ := entry.GetStringValue("InfPath")
		ifType, _, _ := entry.GetIntegerValue("*IfType")
		_ = entry.Close()
		if instanceErr != nil || instance == "" {
			continue
		}
		out[strings.ToLower(instance)] = windowsClassMeta{
			driverDesc: desc,
			provider:   provider,
			infPath:    inf,
			ifType:     int(ifType),
		}
	}
	return out
}

func windowsEnumInfo(pnp string) windowsEnumMeta {
	if strings.TrimSpace(pnp) == "" {
		return windowsEnumMeta{}
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsEnumRoot+`\`+pnp, registry.QUERY_VALUE)
	if err != nil {
		return windowsEnumMeta{}
	}
	defer func() { _ = key.Close() }()
	meta := windowsEnumMeta{}
	meta.deviceDesc, _, _ = key.GetStringValue("DeviceDesc")
	meta.manufacturer, _, _ = key.GetStringValue("Manufacturer")
	meta.service, _, _ = key.GetStringValue("Service")
	meta.componentID, _, _ = key.GetStringValue("ComponentId")
	if values, _, err := key.GetStringsValue("HardwareID"); err == nil {
		meta.hardwareIDs = values
	} else if value, _, err := key.GetStringValue("HardwareID"); err == nil && value != "" {
		meta.hardwareIDs = []string{value}
	}
	return meta
}

func ifTypeName(value int) string {
	switch value {
	case 6:
		return "ethernet"
	case 24:
		return "loopback"
	case 71:
		return "wifi"
	case 131:
		return "tunnel"
	case 53:
		return "proprietary"
	case 0:
		return ""
	default:
		return "iftype-" + strconv.Itoa(value)
	}
}

func interfaceUp(iface InterfaceInfo) bool {
	return strings.Contains(lowerASCII(iface.Flags), "up")
}
