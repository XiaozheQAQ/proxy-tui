// Package cli implements the cobra command tree.
package cli

import (
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
		newVersionCommand(),
	)
	return root
}

func openApp() (*app.App, error) {
	paths, err := storage.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return app.New(paths)
}

func runTUI() error {
	application, err := openApp()
	if err != nil {
		return err
	}
	model := tui.New(application)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = program.Run()
	return err
}

func printLine(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format+"\n", args...)
}
