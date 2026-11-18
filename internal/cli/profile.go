package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/format"
	"github.com/proxytui/proxy-tui/internal/security"
	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func newProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage subscriptions and profiles",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runProfileList(cmd) },
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List stored profiles", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return runProfileList(cmd) }},
		newProfileAddCommand(),
		newProfileUpdateCommand(),
		newProfileUseCommand(),
		newProfileShowCommand(),
		newProfileDeleteCommand(),
	)
	return cmd
}

func runProfileList(cmd *cobra.Command) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	profiles := application.Subs.List()
	out := cmd.OutOrStdout()
	if len(profiles) == 0 {
		printLine(out, "no profiles yet — run \"proxy profile add --core mihomo <url>\"")
		return nil
	}
	printLine(out, "%-28s %-18s %-9s %-12s %-6s %-6s %s", "NAME", "ID", "CORE", "FORMAT", "NODES", "REV", "ACTIVE")
	for _, profile := range profiles {
		active := ""
		if current, ok := application.Subs.ActiveFor(profile.Core); ok && current.ID == profile.ID {
			active = "yes"
		}
		printLine(out, "%-28s %-18s %-9s %-12s %-6d %-6d %s",
			format.Truncate(profile.Label(), 28),
			profile.ID,
			profile.Core.DisplayName(),
			profile.Format.DisplayName(),
			profile.NodeCount,
			profile.Revision,
			active,
		)
	}
	return nil
}

func newProfileAddCommand() *cobra.Command {
	var coreFlag, nameFlag string
	var duplicate bool
	cmd := &cobra.Command{
		Use:   "add [url|file|-]",
		Short: "Import a subscription from a URL, a file or stdin",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileAdd(cmd, args, coreFlag, nameFlag, duplicate)
		},
	}
	cmd.Flags().StringVar(&coreFlag, "core", "mihomo", "target core (mihomo|sing-box)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "profile name")
	cmd.Flags().BoolVar(&duplicate, "allow-duplicate", false, "import even when the URL is already stored")
	return cmd
}

func runProfileAdd(cmd *cobra.Command, args []string, coreFlag, nameFlag string, allowDuplicate bool) error {
	kind, err := model.ParseCoreKind(coreFlag)
	if err != nil {
		return err
	}
	application, err := openApp()
	if err != nil {
		return err
	}

	input, err := readSubscriptionInput(cmd, args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("nothing to import: pass a URL, a file path or - to read stdin")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	outcomes, err := application.Import(ctx, subscription.ImportRequest{
		Core:           kind,
		Name:           nameFlag,
		Input:          input,
		AllowDuplicate: allowDuplicate,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	failures := 0
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			failures++
			printLine(out, "✗ %s: %s", outcome.Source, outcome.Err.Error())
			continue
		}
		profile := outcome.Profile
		printLine(out, "✓ %s: %s, %d nodes, revision %d (%s)",
			security.RedactURL(profile.Label()), profile.Format.DisplayName(), profile.NodeCount, profile.Revision,
			profile.ID)
		for _, warning := range profile.Warnings {
			printLine(out, "  warning: %s", warning)
		}
		if len(profile.Unsupported) > 0 {
			printLine(out, "  unsupported: %s", strings.Join(profile.Unsupported, "; "))
		}
	}
	if failures > 0 && failures == len(outcomes) {
		return fmt.Errorf("all %d import(s) failed", failures)
	}
	return nil
}

func readSubscriptionInput(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no subscription source given")
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case arg == "-":
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			parts = append(parts, string(data))
		case looksLikeURL(arg):
			parts = append(parts, arg)
		default:
			data, err := os.ReadFile(arg)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", arg, err)
			}
			parts = append(parts, string(data))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func looksLikeURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.Contains(value, "://")
}

func newProfileUpdateCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [id...]",
		Short: "Refresh one or all subscriptions",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileUpdate(cmd, args, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update every URL backed profile")
	return cmd
}

func runProfileUpdate(cmd *cobra.Command, args []string, all bool) error {
	application, err := openApp()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if all || len(args) == 0 {
		outcomes := application.Subs.UpdateAll(ctx)
		if len(outcomes) == 0 {
			printLine(out, "no URL backed profiles to update")
			return nil
		}
		for _, outcome := range outcomes {
			if outcome.Err != nil {
				printLine(out, "✗ %s: %s", security.RedactURL(outcome.Profile.Label()), outcome.Err.Error())
				continue
			}
			printLine(out, "✓ %s: revision %d, %d nodes", outcome.Profile.Label(), outcome.Profile.Revision, outcome.Profile.NodeCount)
		}
		return nil
	}

	for _, id := range args {
		profile, err := application.Subs.Update(ctx, id)
		if err != nil {
			printLine(out, "✗ %s: %s", id, err.Error())
			continue
		}
		printLine(out, "✓ %s: revision %d, %d nodes", profile.Label(), profile.Revision, profile.NodeCount)
	}
	return nil
}

func newProfileUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <id>",
		Short: "Mark a profile as active",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := openApp()
			if err != nil {
				return err
			}
			profile, err := application.Subs.SetActive(args[0])
			if err != nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "active profile for %s: %s", profile.Core.DisplayName(), profile.Label())
			return nil
		},
	}
}

func newProfileShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a profile in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := openApp()
			if err != nil {
				return err
			}
			profile, ok := application.Subs.Get(args[0])
			if !ok {
				return fmt.Errorf("profile %q not found", args[0])
			}
			out := cmd.OutOrStdout()
			printLine(out, "name:        %s", profile.Label())
			printLine(out, "id:          %s", profile.ID)
			printLine(out, "core:        %s", profile.Core.DisplayName())
			printLine(out, "format:      %s", profile.Format.DisplayName())
			printLine(out, "source:      %s", security.RedactURL(profile.Source.URL))
			printLine(out, "nodes:       %d", profile.NodeCount)
			printLine(out, "revision:    %d", profile.Revision)
			printLine(out, "activatable: %v", profile.Activatable)
			printLine(out, "updated:     %s", profile.UpdatedAt.Format(time.RFC3339))
			snapshots, err := application.Subs.Snapshots(profile.ID)
			if err == nil {
				printLine(out, "snapshots:   %d", len(snapshots))
			}
			for _, warning := range profile.Warnings {
				printLine(out, "warning:     %s", warning)
			}
			return nil
		},
	}
}

func newProfileDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a stored profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := openApp()
			if err != nil {
				return err
			}
			if err := application.Subs.Delete(args[0]); err != nil {
				return err
			}
			printLine(cmd.OutOrStdout(), "deleted %s", args[0])
			return nil
		},
	}
}
