package cmd

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/rpcarvs/rdit/internal/tui"
)

var (
	version = "dev"
	commit  = ""
)

var rootCmd = &cobra.Command{
	Use:           "rdit",
	Short:         "Open the rdit reasoning audit TUI",
	Long:          "Open the rdit Bubble Tea interface to inspect reasoning audits and manage provider installation from inside the TUI.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run(".")
	},
}

// Execute runs the CLI command tree.
func Execute() error {
	rootCmd.InitDefaultVersionFlag()
	rootCmd.SetVersionTemplate("{{printf \"%s version %s\\n\" .Name .Version}}")

	return fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	)
}
