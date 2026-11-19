package cli

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/proxytui/proxy-tui/internal/ipc"
	"github.com/proxytui/proxy-tui/internal/supervisor"
)

func newOperationCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "operation", Short: "Inspect supervisor mutation operations"}
	status := &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Query a mutation result without executing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialSupervisor(cmd.Context())
			if err != nil {
				return err
			}
			defer client.Close()
			var result supervisor.OperationStatusPayload
			ctx, cancel := contextWithTimeout(cmd, 10*time.Second)
			defer cancel()
			if err := client.Call(ctx, ipc.OpOperationGet, map[string]string{"operation_id": args[0]}, &result); err != nil {
				return err
			}
			if asJSON {
				data, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				printLine(cmd.OutOrStdout(), "%s", data)
				return nil
			}
			printLine(cmd.OutOrStdout(), "operation: %s", result.OperationID)
			printLine(cmd.OutOrStdout(), "status:    %s", result.Status)
			if result.StoredAt.IsZero() == false {
				printLine(cmd.OutOrStdout(), "stored:    %s", result.StoredAt.Format(time.RFC3339))
			}
			if result.Error != nil {
				printLine(cmd.OutOrStdout(), "error:     %s: %s", result.Error.Code, result.Error.Message)
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "machine readable output")
	cmd.AddCommand(status)
	return cmd
}
