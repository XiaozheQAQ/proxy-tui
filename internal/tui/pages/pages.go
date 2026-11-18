// Package pages implements the workspace views of the TUI. Pages are pure
// presentation: they render data pushed by the root model and emit Actions.
package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// ID identifies a page.
type ID string

// Page identifiers in navigation order.
const (
	IDOverview    ID = "overview"
	IDMihomo      ID = "mihomo"
	IDSingBox     ID = "sing-box"
	IDConnections ID = "connections"
	IDLogs        ID = "logs"
	IDSettings    ID = "settings"
)

// IDs returns the navigation order.
func IDs() []ID {
	return []ID{IDOverview, IDMihomo, IDSingBox, IDConnections, IDLogs, IDSettings}
}

// TitleOf renders the navigation label.
func TitleOf(id ID) string {
	switch id {
	case IDOverview:
		return "Overview"
	case IDMihomo:
		return "Mihomo"
	case IDSingBox:
		return "sing-box"
	case IDConnections:
		return "Connections"
	case IDLogs:
		return "Logs"
	case IDSettings:
		return "Settings"
	default:
		return string(id)
	}
}

// ActionKind enumerates the side effects a page can request.
type ActionKind string

// Actions understood by the root model.
const (
	ActionNone              ActionKind = ""
	ActionRefresh           ActionKind = "refresh"
	ActionUseProfile        ActionKind = "use-profile"
	ActionUpdateProfile     ActionKind = "update-profile"
	ActionDeleteProfile     ActionKind = "delete-profile"
	ActionRollbackProfile   ActionKind = "rollback-profile"
	ActionStartCore         ActionKind = "start-core"
	ActionStopCore          ActionKind = "stop-core"
	ActionRestartCore       ActionKind = "restart-core"
	ActionSwitchCore        ActionKind = "switch-core"
	ActionProxyPicker       ActionKind = "proxy-picker"
	ActionTestProxies       ActionKind = "test-proxies"
	ActionSelectProxy       ActionKind = "select-proxy"
	ActionToggleSystemProxy ActionKind = "toggle-system-proxy"
	ActionToggleTUN         ActionKind = "toggle-tun"
	ActionToggleTheme       ActionKind = "toggle-theme"
	ActionToggleAutoUpdate  ActionKind = "toggle-auto-update"
	ActionToggleConfirmQuit ActionKind = "toggle-confirm-quit"
	ActionCycleInterval     ActionKind = "cycle-interval"
	ActionClearLogs         ActionKind = "clear-logs"
	ActionSetTrafficMode    ActionKind = "set-traffic-mode"
	ActionCycleTunStack     ActionKind = "cycle-tun-stack"
	ActionCycleTunMTU       ActionKind = "cycle-tun-mtu"
	ActionToggleTunDNS      ActionKind = "toggle-tun-dns"
	ActionToggleTunIPv6     ActionKind = "toggle-tun-ipv6"
	ActionRunTUNPreflight   ActionKind = "run-tun-preflight"
	ActionRecoverTUN        ActionKind = "recover-tun"
	ActionVerifyProxy       ActionKind = "verify-proxy"
	ActionAddProfile        ActionKind = "add-profile"
	ActionHelp              ActionKind = "help"
	ActionCommandPalette    ActionKind = "command-palette"
	ActionQuit              ActionKind = "quit"
)

// Action is a request from a page to the root model.
type Action struct {
	Kind      ActionKind
	Core      model.CoreKind
	ProfileID string
	Group     string
	Proxy     string
	Revision  int
	Mode      network.Mode
}

// FeatureState describes a platform capability such as system proxy or TUN.
type FeatureState struct {
	Known   bool
	Enabled bool
	Detail  string
}

// ProxyCheck is the last explicit end-to-end proxy verification.
type ProxyCheck struct {
	Attempted bool
	OK        bool
	Address   string
	ExitIP    string
	LatencyMS int64
	Err       string
}

// OverviewData is everything the overview page shows.
type OverviewData struct {
	Core           model.CoreKind
	Status         core.Status
	OtherStatus    core.Status
	Active         model.Profile
	HasActive      bool
	ProfileCount   int
	Traffic        core.Traffic
	Connections    int
	ConnectionsErr string
	Proxies        []core.Proxy
	ProxiesErr     string
	SystemProxy    FeatureState
	TUN            FeatureState
	TrafficMode    string
	ProxyCheck     ProxyCheck
	LastError      string
}

// CoreData is the payload for a core management page.
type CoreData struct {
	Core       model.CoreKind
	Status     core.Status
	Profiles   []model.Profile
	ActiveID   string
	Proxies    []core.Proxy
	ProxiesErr string
	Traffic    core.Traffic
	LastError  string
}

// ConnectionsData is the payload for the connections page.
type ConnectionsData struct {
	Core        model.CoreKind
	Running     bool
	Supported   bool
	Err         string
	Connections []core.Connection
	Traffic     core.Traffic
}

// LogsData is the payload for the logs page.
type LogsData struct {
	Entries []logging.Entry
}

// SettingsData is the payload for the settings page.
type SettingsData struct {
	Settings storage.Settings
	Paths    storage.Paths
	Version  string
	CoreDirs []string
	Network  network.Report
	TUN      tun.Status
}

// DataMsg carries a full snapshot of every page's data.
type DataMsg struct {
	Overview    OverviewData
	Cores       map[model.CoreKind]CoreData
	Connections ConnectionsData
	Logs        LogsData
	Settings    SettingsData
}

// Page is a workspace view.
type Page interface {
	ID() ID
	Title() string
	Update(msg tea.Msg) (Page, *Action, tea.Cmd)
	View(theme layout.Theme, width, height int) string
	Hints() []layout.Hint
}
