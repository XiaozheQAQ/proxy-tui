package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/network"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tun"
)

func newNetworkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Inspect and change the desktop network takeover state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNetworkStatus(cmd)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show desktop proxy, shell proxy and backup state",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runNetworkStatus(cmd)
			},
		},
		newNetworkModeCommand(),
		newSystemProxyCommand(),
		newNetworkTunCommand(),
		&cobra.Command{
			Use:   "recover",
			Short: "Restore network state left behind by a crash or a stopped core",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				application, err := openApp()
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				result, err := application.RecoverNetwork(ctx)
				if err != nil {
					return err
				}
				if result.Action == "" {
					printLine(cmd.OutOrStdout(), "nothing to recover")
					return nil
				}
				printLine(cmd.OutOrStdout(), "%s", result.Action)
				if result.Detail != "" {
					printLine(cmd.OutOrStdout(), "  %s", result.Detail)
				}
				return nil
			},
		},
	)
	return cmd
}

func newNetworkModeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mode <off|system-proxy|manual|tun>",
		Short: "Set the traffic takeover mode",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := network.ParseMode(args[0])
			if err != nil {
				return err
			}
			return applyTrafficMode(cmd, mode)
		},
	}
}

func newSystemProxyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "system-proxy <enable|disable>",
		Short: "Enable or disable the desktop system proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(args[0]) {
			case "enable", "on":
				return applyTrafficMode(cmd, network.ModeSystemProxy)
			case "disable", "off":
				return applyTrafficMode(cmd, network.ModeOff)
			default:
				return fmt.Errorf("usage: proxy network system-proxy <enable|disable>")
			}
		},
	}
}

func applyTrafficMode(cmd *cobra.Command, mode network.Mode) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	settings := appTunSettings(application)
	result, err := application.ApplyTrafficMode(ctx, mode, "", settings)
	if err != nil {
		printTrafficWarnings(cmd, result.Warnings)
		if mode == network.ModeTUN {
			printPreflightReport(cmd, result.TUN.Report)
		}
		return err
	}
	if err := saveTrafficMode(ctx, application, mode); err != nil {
		printLine(cmd.OutOrStdout(), "warning: could not persist the traffic mode: %v", err)
	}

	out := cmd.OutOrStdout()
	printLine(out, "traffic mode: %s", mode.Display())
	switch mode {
	case network.ModeTUN:
		printTUNStatus(out, result.TUN.Status)
	default:
		networkResult := result.Network
		switch {
		case networkResult.Applied:
			printLine(out, "desktop proxy: %s (%s)", networkResult.State.Address(), networkResult.Adapter)
		case networkResult.Restored:
			printLine(out, "desktop proxy restored (%s)", networkResult.Adapter)
		}
		if networkResult.Original.Enabled && networkResult.Original.Address() != "" {
			printLine(out, "note: the original desktop proxy was %s; it is restored on disable", networkResult.Original.Address())
		}
	}
	printTrafficWarnings(cmd, result.Warnings)
	return nil
}

func printTrafficWarnings(cmd *cobra.Command, warnings []string) {
	for _, warning := range warnings {
		printLine(cmd.OutOrStdout(), "warning: %s", warning)
	}
}

func printPreflightReport(cmd *cobra.Command, report tun.Report) {
	if report.Level == "" {
		return
	}
	for _, line := range report.Lines() {
		printLine(cmd.OutOrStdout(), "  %s", line)
	}
}

func printTUNStatus(out io.Writer, status tun.Status) {
	iface := status.Interface
	if iface == "" {
		iface = "—"
	}
	printLine(out, "tun:      %s (%s)", enabledLabel(status), status.Level)
	coreName := status.Core.DisplayName()
	if coreName == "" {
		coreName = "—"
	}
	printLine(out, "core:     %s (running: %v)", coreName, status.CoreRunning)
	printLine(out, "interface: %s", iface)
	printLine(out, "outbound: %s", emptyOr(status.Outbound, "unknown"))
	printLine(out, "dns:      %s", emptyOr(status.DNSMode, "unknown"))
	printLine(out, "routing:  %s", emptyOr(status.Routing, "unknown"))
	if status.Detail != "" {
		printLine(out, "detail:   %s", status.Detail)
	}
	for _, leftover := range status.Leftover {
		printLine(out, "leftover: %s", leftover)
	}
}

func enabledLabel(status tun.Status) string {
	if status.Enabled {
		return "enabled"
	}
	return "disabled"
}

func emptyOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func appTunSettings(application *app.Frontend) tun.Settings {
	return app.TunSettingsFrom(application.SettingsSnapshot(context.Background()))
}

func patchValue(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func saveTrafficMode(ctx context.Context, application *app.Frontend, mode network.Mode) error {
	return application.PatchSettings(ctx, map[string]json.RawMessage{
		"traffic_mode": patchValue(string(mode)),
	})
}

func newNetworkTunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tun",
		Short: "Core-native TUN integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUNStatus(cmd)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show the TUN state, interface, routing and DNS mode",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTUNStatus(cmd)
			},
		},
		newTunEnableCommand(),
		&cobra.Command{
			Use:   "disable",
			Short: "Stop the core TUN and verify that it released its resources",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTUNDisable(cmd)
			},
		},
		newTunPreflightCommand(),
		&cobra.Command{
			Use:   "recover",
			Short: "Repair a TUN state left behind by a crash or abnormal exit",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTUNRecover(cmd)
			},
		},
	)
	return cmd
}

func newTunEnableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Render TUN configs, preflight the host and restart the core with TUN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUNEnable(cmd)
		},
	}
	cmd.Flags().String("core", "", "core to use (mihomo|sing-box); defaults to the running core")
	cmd.Flags().String("stack", "", "TUN stack (mixed|system|gvisor)")
	cmd.Flags().Int("mtu", 0, "TUN MTU (1280-65535)")
	cmd.Flags().Bool("dns-hijack", true, "hijack DNS through the core")
	cmd.Flags().Bool("ipv6", false, "enable the IPv6 TUN address")
	return cmd
}

func newTunPreflightCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check privileges, core, routing, DNS and conflicts for TUN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUNPreflight(cmd)
		},
	}
	cmd.Flags().String("core", "", "core to inspect (mihomo|sing-box)")
	cmd.Flags().String("stack", "", "TUN stack (mixed|system|gvisor)")
	cmd.Flags().Int("mtu", 0, "TUN MTU (1280-65535)")
	cmd.Flags().Bool("dns-hijack", true, "assume DNS hijack is enabled")
	cmd.Flags().Bool("ipv6", false, "assume IPv6 TUN is enabled")
	return cmd
}

func runTUNStatus(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := application.TUNStatus(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	printLine(out, "platform: %s", application.TunPlatform())
	printTUNStatus(out, status)
	settings := app.TunSettingsFrom(application.SettingsSnapshot(ctx))
	printLine(out, "settings: stack=%s mtu=%d dns-hijack=%v ipv6=%v", settings.Stack.Display(), settings.MTU, settings.DNSHijack, settings.IPv6)
	for _, warning := range status.Leftover {
		printLine(out, "warning: interface %s was not released; run proxy network tun recover", warning)
	}
	return nil
}

func runTUNEnable(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	kind, settings, err := tunSettingsFromFlags(cmd, application)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	result, err := application.ApplyTrafficMode(ctx, network.ModeTUN, kind, settings)
	printTrafficWarnings(cmd, result.Warnings)
	if err != nil {
		printPreflightReport(cmd, result.TUN.Report)
		return err
	}
	if err := saveTunConfig(ctx, application, kind, settings); err != nil {
		printLine(cmd.OutOrStdout(), "warning: could not persist TUN settings: %v", err)
	}
	if err := saveTrafficMode(ctx, application, network.ModeTUN); err != nil {
		printLine(cmd.OutOrStdout(), "warning: could not persist the traffic mode: %v", err)
	}
	out := cmd.OutOrStdout()
	printLine(out, "traffic mode: TUN")
	printTUNStatus(out, result.TUN.Status)
	if len(result.TUN.Verify.Notes) > 0 {
		for _, note := range result.TUN.Verify.Notes {
			printLine(out, "note: %s", note)
		}
	}
	if result.TUN.Report.Level == tun.LevelWarning || result.TUN.Report.Level == tun.LevelUnknown {
		printLine(out, "preflight: %s", result.TUN.Report.Summary())
	}
	return nil
}

func runTUNDisable(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := application.TUN.Disable(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, warning := range result.Warnings {
		printLine(out, "warning: %s", warning)
	}
	if result.Released {
		printLine(out, "TUN disabled; core resources released")
		if err := saveTrafficMode(ctx, application, network.ModeOff); err != nil {
			printLine(out, "warning: could not persist the traffic mode: %v", err)
		}
	} else {
		printLine(out, "TUN disabled but resources remain: %s", strings.Join(result.Leftover, ", "))
		printLine(out, "run proxy network tun recover after stopping the conflicting process")
	}
	return nil
}

func runTUNPreflight(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	kind, settings, err := tunSettingsFromFlags(cmd, application)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := application.TUNPreflight(ctx, kind, settings)
	if err != nil {
		return err
	}
	for _, line := range report.Lines() {
		printLine(cmd.OutOrStdout(), "%s", line)
	}
	printLine(cmd.OutOrStdout(), "result: %s", report.Summary())
	// Preflight completion is not activation approval: print the structured
	// result and exit 0; "network tun enable" performs the actual gate.
	return nil
}

func runTUNRecover(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := application.RecoverTUN(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if result.Action == "" {
		printLine(out, "nothing to recover")
		return nil
	}
	printLine(out, "%s", result.Action)
	for _, warning := range result.Warnings {
		printLine(out, "warning: %s", warning)
	}
	return nil
}

func tunSettingsFromFlags(cmd *cobra.Command, application *app.Frontend) (model.CoreKind, tun.Settings, error) {
	settings := app.TunSettingsFrom(application.SettingsSnapshot(cmd.Context()))
	kind := application.DefaultTunCore(context.Background())
	if raw, err := cmd.Flags().GetString("core"); err == nil && strings.TrimSpace(raw) != "" {
		parsed, parseErr := model.ParseCoreKind(raw)
		if parseErr != nil {
			return "", settings, parseErr
		}
		kind = parsed
	}
	if raw, err := cmd.Flags().GetString("stack"); err == nil && strings.TrimSpace(raw) != "" {
		stack, parseErr := tun.ParseStack(raw)
		if parseErr != nil {
			return "", settings, parseErr
		}
		settings.Stack = stack
	}
	if raw, err := cmd.Flags().GetInt("mtu"); err == nil && raw != 0 {
		settings.MTU = raw
	}
	if cmd.Flags().Changed("dns-hijack") {
		value, _ := cmd.Flags().GetBool("dns-hijack")
		settings.DNSHijack = value
	}
	if cmd.Flags().Changed("ipv6") {
		value, _ := cmd.Flags().GetBool("ipv6")
		settings.IPv6 = value
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return "", settings, err
	}
	return kind, settings, nil
}

func saveTunConfig(ctx context.Context, application *app.Frontend, kind model.CoreKind, settings tun.Settings) error {
	return application.PatchSettings(ctx, map[string]json.RawMessage{
		"tun_core":       patchValue(string(kind)),
		"tun_stack":      patchValue(string(settings.Stack)),
		"tun_mtu":        patchValue(settings.MTU),
		"tun_dns_hijack": patchValue(settings.DNSHijack),
		"tun_ipv6":       patchValue(settings.IPv6),
	})
}

func runNetworkStatus(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	report, err := application.NetworkReport(ctx)
	if err != nil {
		return err
	}
	settings := application.SettingsSnapshot(ctx)

	out := cmd.OutOrStdout()
	printLine(out, "platform: %s", report.Capabilities.Platform)
	if report.Capabilities.Supported {
		printLine(out, "support:  yes (%s)", report.Capabilities.Method)
	} else {
		printLine(out, "support:  no (%s)", report.Capabilities.Reason)
	}
	printLine(out, "mode:     %s", settings.TrafficMode)

	desktop := "disabled"
	if report.Desktop.Enabled {
		desktop = "enabled " + report.Desktop.Address()
	}
	printLine(out, "desktop:  %s", desktop)
	if len(report.Desktop.Services) > 0 {
		for _, service := range report.Desktop.Services {
			state := "off"
			if service.Enabled {
				state = fmt.Sprintf("on %s:%d", service.Host, service.Port)
			}
			printLine(out, "          %s: %s", service.Name, state)
		}
	}

	if report.Environment.Detected() {
		if report.Environment.HTTP != "" {
			printLine(out, "shell:    HTTP_PROXY=%s (not managed by proxy-tui)", report.Environment.HTTP)
		}
		if report.Environment.HTTPS != "" {
			printLine(out, "          HTTPS_PROXY=%s (not managed by proxy-tui)", report.Environment.HTTPS)
		}
		if report.Environment.All != "" {
			printLine(out, "          ALL_PROXY=%s (not managed by proxy-tui)", report.Environment.All)
		}
	} else {
		printLine(out, "shell:    no proxy environment variables in this shell")
	}

	if report.BackupActive {
		original := "disabled"
		if report.BackupOriginal.Enabled {
			original = report.BackupOriginal.Address()
		}
		printLine(out, "backup:   yes, original desktop proxy: %s (taken %s)", original, report.BackupAt.Format(time.RFC3339))
	} else {
		printLine(out, "backup:   no")
	}
	for _, note := range report.Notes {
		printLine(out, "note:     %s", note)
	}
	return nil
}
