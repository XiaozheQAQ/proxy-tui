// Command proxy is the ProxyTUI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/proxytui/proxy-tui/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
