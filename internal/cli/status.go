package cli

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/format"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show core, profile and traffic status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := openApp()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out := cmd.OutOrStdout()
			for _, kind := range application.Cores.Kinds() {
				status := application.Cores.Status(ctx, kind)
				printLine(out, "%s: %s  version %s", kind.DisplayName(), status.Label(), dash(status.Version))
				if status.Running {
					printLine(out, "  pid %d since %s, API %s", status.PID, status.StartedAt.Format("15:04:05"), status.APIAddr)
					traffic, err := application.Cores.Traffic(ctx, kind)
					if err == nil {
						printLine(out, "  up %s/s  down %s/s  total %s", format.Bytes(traffic.UpRate), format.Bytes(traffic.DownRate), format.Bytes(traffic.UpTotal+traffic.DownTotal))
					} else {
						printLine(out, "  traffic unavailable: %s", err.Error())
					}
					connections, err := application.Cores.Connections(ctx, kind)
					if err == nil {
						printLine(out, "  %d active connection(s)", len(connections))
					}
				}
				if active, ok := application.Subs.ActiveFor(kind); ok {
					printLine(out, "  active profile: %s (%d nodes, revision %d)", active.Label(), active.NodeCount, active.Revision)
				} else {
					printLine(out, "  active profile: none")
				}
			}
			return nil
		},
	}
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run local diagnostics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := openApp()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			for _, line := range application.Doctor(ctx) {
				printLine(cmd.OutOrStdout(), "%s", line)
			}
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			printLine(out, "proxy-tui %s", app.Version)
			printLine(out, "go %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}

var _ = fmt.Sprintf
