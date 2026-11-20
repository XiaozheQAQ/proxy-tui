// Package tui implements the full screen terminal interface.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/logging"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/dialogs"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
	"github.com/proxytui/proxy-tui/internal/tui/pages"
	"github.com/proxytui/proxy-tui/internal/tun"
)

// tickInterval drives the periodic refresh.
const tickInterval = time.Second

// Model is the root Bubble Tea model.
type Model struct {
	app *app.Frontend

	width, height int
	ready         bool

	page     pages.ID
	navIndex int
	core     model.CoreKind
	pages    map[pages.ID]pages.Page

	profiles      []model.Profile
	statuses      map[model.CoreKind]core.Status
	kinds         []model.CoreKind
	active        map[model.CoreKind]model.Profile
	corePaths     map[model.CoreKind]string
	logs          []logging.Entry
	live          liveState
	network       network.Report
	tun           tun.Status
	connectivity  app.ProxyConnectivity
	supervisorErr string

	settings       storage.Settings
	settingsSaving bool

	dialog  dialogs.Dialog
	confirm *pages.Action

	toast     toast
	importing bool
	updating  bool
	busy      string

	lastAutoCheck time.Time
}

type toast struct {
	text  string
	level string
	at    time.Time
}

// liveState holds the last snapshot fetched from a running core.
type liveState struct {
	kind     model.CoreKind
	proxies  []core.Proxy
	proxyErr string
	traffic  core.Traffic
	conns    []core.Connection
	connErr  string
	at       time.Time
}

// New builds the root model.
func New(application *app.Frontend) *Model {
	m := &Model{
		app:       application,
		page:      pages.IDOverview,
		core:      model.CoreMihomo,
		pages:     map[pages.ID]pages.Page{},
		statuses:  map[model.CoreKind]core.Status{},
		kinds:     application.Cores.Kinds(),
		active:    map[model.CoreKind]model.Profile{},
		corePaths: map[model.CoreKind]string{},
		settings:  application.Settings,
	}
	m.pages[pages.IDOverview] = pages.NewOverview()
	m.pages[pages.IDMihomo] = pages.NewCorePage(model.CoreMihomo)
	m.pages[pages.IDSingBox] = pages.NewCorePage(model.CoreSingBox)
	m.pages[pages.IDConnections] = pages.NewConnections()
	m.pages[pages.IDLogs] = pages.NewLogs()
	m.pages[pages.IDSettings] = pages.NewSettings()
	m.rebuildData()
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// theme resolves the active palette.
func (m *Model) theme() layout.Theme {
	return layout.ThemeByName(m.settings.Theme)
}

// currentPage returns the focused page.
func (m *Model) currentPage() pages.Page { return m.pages[m.page] }

func (m *Model) pageHints() []layout.Hint { return m.currentPage().Hints() }

func (m *Model) selector() (model.Profile, bool) {
	if selector, ok := m.currentPage().(interface{ SelectedProfile() (model.Profile, bool) }); ok {
		return selector.SelectedProfile()
	}
	return model.Profile{}, false
}

// rebuildData pushes a fresh DataMsg into every page.
func (m *Model) rebuildData() {
	profiles := m.profiles
	findActive := func(kind model.CoreKind) (model.Profile, bool) {
		active, ok := m.active[kind]
		return active, ok
	}

	status := m.statusOf(m.core)
	other := model.CoreMihomo
	if m.core == model.CoreMihomo {
		other = model.CoreSingBox
	}

	active, hasActive := findActive(m.core)
	systemProxy := pages.FeatureState{Detail: "unavailable"}
	if m.network.Capabilities.Supported {
		systemProxy.Known = true
		systemProxy.Enabled = m.network.Desktop.Enabled
		systemProxy.Detail = m.network.Desktop.Address()
	}
	overview := pages.OverviewData{
		Core:        m.core,
		Status:      status,
		OtherStatus: m.statusOf(other),
		Active:      active,
		HasActive:   hasActive,
		Traffic:     m.liveTraffic(),
		Connections: len(m.liveConns()),
		SystemProxy: systemProxy,
		TUN:         tunFeature(m.tun),
		TrafficMode: m.trafficModeLabel(),
		ProxyCheck: pages.ProxyCheck{
			Attempted: m.connectivity.Attempted,
			OK:        m.connectivity.OK,
			Address:   m.connectivity.Address,
			ExitIP:    m.connectivity.ExitIP,
			LatencyMS: m.connectivity.LatencyMS,
			Err:       m.connectivity.Err,
		},
		Proxies:    m.liveProxies(),
		Supervisor: m.supervisorLabel(),
	}
	if !status.Running {
		overview.Proxies = nil
	}
	overview.ProfileCount = countForCore(profiles, m.core)
	if m.live.proxyErr != "" && status.Running {
		overview.ProxiesErr = m.live.proxyErr
	}
	if m.live.connErr != "" && status.Running {
		overview.ConnectionsErr = m.live.connErr
	}

	cores := make(map[model.CoreKind]pages.CoreData, len(m.kinds))
	for _, kind := range m.kinds {
		data := pages.CoreData{
			Core:     kind,
			Status:   m.statusOf(kind),
			Profiles: profilesForCore(profiles, kind),
		}
		if active, ok := findActive(kind); ok {
			data.ActiveID = active.ID
		}
		if kind == m.live.kind {
			data.Proxies = m.live.proxies
			data.ProxiesErr = m.live.proxyErr
			data.Traffic = m.live.traffic
		}
		cores[kind] = data
	}

	connections := pages.ConnectionsData{
		Core:        m.core,
		Running:     status.Running,
		Supported:   status.Running,
		Connections: m.liveConns(),
		Traffic:     m.liveTraffic(),
	}
	if status.Running && m.live.connErr != "" {
		connections.Err = m.live.connErr
	}

	logs := pages.LogsData{Entries: m.logs}

	coreDirs := make([]string, 0, len(m.kinds))
	for _, kind := range m.kinds {
		if binary := m.corePaths[kind]; binary != "" {
			coreDirs = append(coreDirs, strings.TrimSuffix(binary, ".exe"))
		}
	}
	settingsData := pages.SettingsData{
		Settings: m.settings,
		Paths:    m.app.Paths,
		Version:  app.Version,
		CoreDirs: coreDirs,
		Network:  m.network,
		TUN:      m.tun,
	}

	data := pages.DataMsg{
		Overview:    overview,
		Cores:       cores,
		Connections: connections,
		Logs:        logs,
		Settings:    settingsData,
	}
	for _, id := range pages.IDs() {
		page, _, _ := m.pages[id].Update(data)
		m.pages[id] = page
	}
}

// tunFeature renders the TUN state for the overview page.
func tunFeature(status tun.Status) pages.FeatureState {
	feature := pages.FeatureState{Known: true, Enabled: status.Enabled, Detail: status.Detail}
	if status.Enabled && status.Interface != "" {
		feature.Detail = status.Interface
	}
	if !status.Enabled && status.Level == "" {
		feature.Detail = "not enabled"
	}
	return feature
}

func (m *Model) statusOf(kind model.CoreKind) core.Status {
	if status, ok := m.statuses[kind]; ok {
		return status
	}
	return core.Status{Core: kind}
}

func (m *Model) supervisorLabel() string {
	if m.supervisorErr != "" {
		return "DISCONNECTED — " + m.supervisorErr
	}
	return "CONNECTED"
}

func (m *Model) trafficModeLabel() string {
	if m.tun.Enabled {
		detail := "TUN"
		if m.tun.Interface != "" {
			detail += " · " + m.tun.Interface
		}
		return detail
	}
	if m.network.Desktop.Enabled {
		return "System Proxy · " + m.network.Desktop.Address()
	}
	mode := network.Mode(m.settings.TrafficMode)
	if mode == "" {
		mode = network.ModeOff
	}
	return mode.Display()
}

func (m *Model) liveTraffic() core.Traffic {
	if m.live.kind == "" {
		return core.Traffic{}
	}
	return m.live.traffic
}

func (m *Model) liveConns() []core.Connection {
	if m.live.kind == "" {
		return nil
	}
	return m.live.conns
}

func (m *Model) liveProxies() []core.Proxy {
	if m.live.kind == "" {
		return nil
	}
	return m.live.proxies
}

func profilesForCore(profiles []model.Profile, kind model.CoreKind) []model.Profile {
	out := make([]model.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Core == kind {
			out = append(out, profile)
		}
	}
	return out
}

func countForCore(profiles []model.Profile, kind model.CoreKind) int {
	count := 0
	for _, profile := range profiles {
		if profile.Core == kind {
			count++
		}
	}
	return count
}

// runningCore reports which core is currently running, if any.
func (m *Model) runningCore() (model.CoreKind, bool) {
	for _, kind := range m.kinds {
		if m.statusOf(kind).Running {
			return kind, true
		}
	}
	return "", false
}

// refreshCmd collects statuses, profiles, logs and live core data.
func (m *Model) refreshCmd() tea.Cmd {
	application := m.app
	kind, running := m.runningCore()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		msg := refreshMsg{
			statuses:  map[model.CoreKind]core.Status{},
			profiles:  application.Subs.List(),
			logs:      application.Logs.Tail(800),
			active:    map[model.CoreKind]model.Profile{},
			corePaths: map[model.CoreKind]string{},
		}
		if err := application.Health(ctx); err != nil {
			msg.supervisorErr = err.Error()
			msg.profiles = nil
			msg.logs = nil
			return msg
		}
		msg.settings = application.SettingsSnapshot(ctx)
		msg.settingsLoaded = true
		if report, err := application.NetworkReport(ctx); err != nil {
			msg.networkErr = err.Error()
		} else {
			msg.network = report
		}
		if tunStatus, err := application.TUNStatus(ctx); err == nil {
			msg.tun = tunStatus
		}
		for _, coreKind := range m.kinds {
			msg.statuses[coreKind] = application.Cores.Status(ctx, coreKind)
			if active, ok := application.Subs.ActiveFor(coreKind); ok {
				msg.active[coreKind] = active
			}
			msg.corePaths[coreKind] = application.CoreBinaryPath(coreKind)
		}
		if running {
			msg.live.kind = kind
			if proxies, err := application.Cores.Proxies(ctx, kind); err == nil {
				msg.live.proxies = proxies
			} else {
				msg.live.proxyErr = err.Error()
			}
			if connections, err := application.Cores.Connections(ctx, kind); err == nil {
				msg.live.conns = connections
			} else {
				msg.live.connErr = err.Error()
			}
			if traffic, err := application.Cores.Traffic(ctx, kind); err == nil {
				msg.live.traffic = traffic
			} else if msg.live.connErr == "" {
				msg.live.connErr = err.Error()
			}
			msg.live.at = time.Now()
		}
		return msg
	}
}

// refreshMsg is the result of refreshCmd.
type refreshMsg struct {
	statuses       map[model.CoreKind]core.Status
	profiles       []model.Profile
	kinds          []model.CoreKind
	active         map[model.CoreKind]model.Profile
	corePaths      map[model.CoreKind]string
	logs           []logging.Entry
	live           liveState
	network        network.Report
	supervisorErr  string
	networkErr     string
	tun            tun.Status
	settings       storage.Settings
	settingsLoaded bool
}

// notice sets the transient footer message.
func (m *Model) notice(level, format string, args ...any) {
	m.toast = toast{text: fmt.Sprintf(format, args...), level: level, at: time.Now()}
}

func (m *Model) setPage(id pages.ID) {
	m.page = id
	for index, candidate := range pages.IDs() {
		if candidate == id {
			m.navIndex = index
		}
	}
	if id == pages.IDMihomo {
		m.core = model.CoreMihomo
	}
	if id == pages.IDSingBox {
		m.core = model.CoreSingBox
	}
	m.rebuildData()
}

// setDialog opens a modal, clearing any pending confirmation.
func (m *Model) setDialog(dialog dialogs.Dialog) {
	m.dialog = dialog
	m.confirm = nil
}
