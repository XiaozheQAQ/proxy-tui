package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

func contextWithTimeout(cmd *cobra.Command, timeout time.Duration) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if cmd.Context() != nil {
		parent = cmd.Context()
	}
	return context.WithTimeout(parent, timeout)
}
