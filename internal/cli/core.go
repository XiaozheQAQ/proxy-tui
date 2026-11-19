package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func newCoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "core",
		Short: "Manage proxy cores",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCoreList(cmd)
		},
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List cores and their state", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return runCoreList(cmd) }},
		newCoreInstallCommand(),
		&cobra.Command{Use: "start [mihomo|sing-box]", Short: "Start the active profile of a core", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return runCoreStart(cmd, args[0]) }},
		&cobra.Command{Use: "stop [mihomo|sing-box]", Short: "Stop a core", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return runCoreStop(cmd, args[0]) }},
	)
	return cmd
}

func runCoreList(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out := cmd.OutOrStdout()
	printLine(out, "%-10s %-12s %-14s %s", "CORE", "VERSION", "STATE", "BINARY")
	for _, kind := range application.Cores.Kinds() {
		status := application.Cores.Status(ctx, kind)
		binary := status.BinaryPath
		if binary == "" {
			binary = "—"
		}
		printLine(out, "%-10s %-12s %-14s %s", kind.DisplayName(), dash(status.Version), status.Label(), binary)
	}
	return nil
}

func newCoreInstallCommand() *cobra.Command {
	var targets []string
	cmd := &cobra.Command{
		Use:   "install [mihomo|sing-box]",
		Short: "Download and install a core release",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCoreInstall(cmd, args)
		},
	}
	cmd.Flags().StringSliceVar(&targets, "target", nil, "core to install (repeatable)")
	return cmd
}

func runCoreInstall(cmd *cobra.Command, args []string) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	names := args
	if len(names) == 0 {
		names = []string{string(model.CoreMihomo), string(model.CoreSingBox)}
	}
	for _, name := range names {
		kind, err := model.ParseCoreKind(name)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		result, err := application.Cores.Install(ctx, kind, func(status string) {
			printLine(out, "  %s: %s", kind.DisplayName(), status)
		})
		cancel()
		if err != nil {
			return fmt.Errorf("install %s: %w", kind.DisplayName(), err)
		}
		verified := "not verified"
		if result.Verified {
			verified = "sha256 verified"
		}
		printLine(out, "installed %s %s (%s) -> %s", kind.DisplayName(), result.Version, verified, result.BinaryPath)
		for _, note := range result.Notes {
			printLine(out, "  note: %s", note)
		}
	}
	return nil
}

func runCoreStart(cmd *cobra.Command, raw string) error {
	kind, err := model.ParseCoreKind(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if action, handled, err := supervisorCoreAction(ctx, ipc.OpCoreStart, kind); handled {
		if err != nil {
			return err
		}
		printLine(cmd.OutOrStdout(), "%s started via supervisor (%s)", kind.DisplayName(), action.State)
		return nil
	}
	application, err := openApp()
	if err != nil {
		return err
	}
	if err := application.StartCore(ctx, kind); err != nil {
		return err
	}
	printLine(cmd.OutOrStdout(), "%s started", kind.DisplayName())
	return nil
}

func runCoreStop(cmd *cobra.Command, raw string) error {
	kind, err := model.ParseCoreKind(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if action, handled, err := supervisorCoreAction(ctx, ipc.OpCoreStop, kind); handled {
		if err != nil {
			return err
		}
		state := action.State
		if action.Already {
			state = "already stopped"
		}
		printLine(cmd.OutOrStdout(), "%s %s via supervisor", kind.DisplayName(), state)
		return nil
	}
	application, err := openApp()
	if err != nil {
		return err
	}
	if err := application.Cores.Stop(ctx, kind); err != nil {
		return err
	}
	printLine(cmd.OutOrStdout(), "%s stopped", kind.DisplayName())
	return nil
}

func dash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
