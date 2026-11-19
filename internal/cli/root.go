// Package cli implements the cobra command tree.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/app"
	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/tui"
)

// Execute runs the CLI.
func Execute() error {
	return newRootCommand().Execute()
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "proxy",
		Short: "ProxyTUI — a terminal proxy client",
		Long: strings.TrimSpace(`ProxyTUI is a terminal native proxy client.

It imports subscription URLs, generates runnable Mihomo or sing-box
configurations and starts the core for you.

Run "proxy" without arguments to open the TUI.`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.AddCommand(
		newCoreCommand(),
		newProfileCommand(),
		newNetworkCommand(),
		newStatusCommand(),
		newDoctorCommand(),
		newSupervisorCommand(),
		newOperationCommand(),
		newVersionCommand(),
	)
	return root
}

// localFrontendForTests is set only by in-package unit tests. It is not
// reachable from the environment or from command-line flags.
var localFrontendForTests bool

// openApp returns a supervisor-backed frontend. Production commands never fall
// back to a local core manager.
func openApp() (*app.Frontend, error) {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if localFrontendForTests {
		local, err := app.New(paths)
		if err != nil {
			return nil, err
		}
		return app.NewLocalFrontend(local), nil
	}
	if _, _, err := ensureSupervisor(context.Background()); err != nil {
		return nil, err
	}
	client, err := dialSupervisor(context.Background())
	if err != nil {
		return nil, err
	}
	frontend, err := app.NewRemote(context.Background(), client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return frontend, nil
}

func runTUI() error {
	frontend, err := openApp()
	if err != nil {
		return err
	}
	model := tui.New(frontend)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = program.Run()
	return err
}

func printLine(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format+"\n", args...)
}
