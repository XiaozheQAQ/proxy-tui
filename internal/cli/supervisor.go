package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/supervisor"
)

func newSupervisorCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "supervisor", Short: "Manage the per-user proxy supervisor"}
	cmd.AddCommand(
		&cobra.Command{Use: "run", Hidden: true, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				paths, err := storage.DefaultPaths()
				if err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return supervisor.Run(ctx, paths, app.Version)
			}},
		&cobra.Command{Use: "start", Short: "Start the supervisor in the background", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				endpoint, started, err := ensureSupervisor(cmd.Context())
				if err != nil {
					return err
				}
				if started {
					printLine(cmd.OutOrStdout(), "supervisor started (%s)", endpoint)
				} else {
					printLine(cmd.OutOrStdout(), "supervisor already running (%s)", endpoint)
				}
				return nil
			}},
		newSupervisorStatusCommand(),
		&cobra.Command{Use: "stop", Short: "Stop the supervisor after releasing cores and network state", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSupervisorStop(cmd)
			}},
		&cobra.Command{Use: "logs", Short: "Show recent supervisor log lines", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSupervisorLogs(cmd)
			}},
	)
	return cmd
}

func newSupervisorStatusCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "status", Short: "Show supervisor status", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine readable output")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := dialSupervisor(cmd.Context())
		if err != nil {
			return err
		}
		defer client.Close()
		var status supervisor.StatusPayload
		if err := client.Call(cmd.Context(), ipc.OpSupervisorStatus, nil, &status); err != nil {
			return err
		}
		if asJSON {
			data, err := json.MarshalIndent(status, "", "  ")
			if err != nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "%s", data)
			return nil
		}
		out := cmd.OutOrStdout()
		printLine(out, "supervisor: RUNNING")
		printLine(out, "pid:        %d", status.PID)
		printLine(out, "endpoint:   %s", status.Endpoint)
		printLine(out, "version:    %s (protocol %d)", status.Version, status.Protocol)
		printLine(out, "started:    %s", status.StartedAt.Format(time.RFC3339))
		for kind, coreStatus := range status.Cores {
			state := coreStatus.Label()
			pid := ""
			if coreStatus.Running {
				pid = fmt.Sprintf(" pid=%d", coreStatus.PID)
			}
			printLine(out, "%-10s %s%s", kind.DisplayName(), state, pid)
		}
		return nil
	}
	return cmd
}

func runSupervisorStop(cmd *cobra.Command) error {
	client, err := dialSupervisor(cmd.Context())
	if err != nil {
		return err
	}
	defer client.Close()
	var result map[string]bool
	if err := client.Call(cmd.Context(), ipc.OpSupervisorStop, nil, &result); err != nil {
		return err
	}
	printLine(cmd.OutOrStdout(), "supervisor stopping")
	paths, err := storage.DefaultPaths()
	if err != nil {
		return err
	}
	endpoint := ipc.EndpointPath(paths.RuntimeDir())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := ipc.Dial(cmd.Context(), endpoint, 500*time.Millisecond)
		if dialErr != nil {
			printLine(cmd.OutOrStdout(), "supervisor stopped")
			return nil
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("supervisor did not stop within 5s; see %s", filepath.Join(paths.LogsDir(), "supervisor.log"))
}

func runSupervisorLogs(cmd *cobra.Command) error {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(paths.LogsDir(), "supervisor.log"))
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	for _, line := range lines {
		printLine(cmd.OutOrStdout(), "%s", line)
	}
	return nil
}

func dialSupervisor(ctx context.Context) (*ipc.Client, error) {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return nil, err
	}
	client, err := ipc.Dial(ctx, ipc.EndpointPath(paths.RuntimeDir()), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("supervisor unavailable: %w (run: proxy supervisor start)", err)
	}
	return client, nil
}

// supervisorCoreAction forwards lifecycle changes to the supervisor when it is
// running. A present supervisor is authoritative: failures are returned, never
// silently retried locally.
func supervisorCoreAction(ctx context.Context, operation string, kind model.CoreKind) (supervisor.CoreAction, bool, error) {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return supervisor.CoreAction{}, false, nil
	}
	client, err := ipc.Dial(ctx, ipc.EndpointPath(paths.RuntimeDir()), 2*time.Minute)
	if err != nil {
		return supervisor.CoreAction{}, false, nil
	}
	defer client.Close()
	var action supervisor.CoreAction
	err = client.Call(ctx, operation, map[string]string{"core": string(kind)}, &action)
	return action, true, err
}

// ensureSupervisor starts the detached supervisor when it is not running.
func ensureSupervisor(ctx context.Context) (string, bool, error) {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return "", false, err
	}
	endpoint := ipc.EndpointPath(paths.RuntimeDir())
	if client, dialErr := ipc.Dial(ctx, endpoint, time.Second); dialErr == nil {
		var status map[string]string
		_ = client.Call(ctx, ipc.OpPing, nil, &status)
		_ = client.Close()
		return endpoint, false, nil
	}
	if err := paths.Ensure(); err != nil {
		return "", false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false, err
	}
	logPath := filepath.Join(paths.LogsDir(), "supervisor.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", false, err
	}
	command := exec.Command(exe, "supervisor", "run")
	command.Env = os.Environ()
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = detachedSysProcAttr()
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return "", false, fmt.Errorf("start supervisor: %w", err)
	}
	_ = logFile.Close()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		client, dialErr := ipc.Dial(ctx, endpoint, 500*time.Millisecond)
		if dialErr == nil {
			var status map[string]string
			pingErr := client.Call(ctx, ipc.OpPing, nil, &status)
			_ = client.Close()
			if pingErr == nil {
				return endpoint, true, nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", false, fmt.Errorf("supervisor did not become ready; see %s", logPath)
}
