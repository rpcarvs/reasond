package cmd

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"rdit/internal/tui"
)

var (
	version = "dev"
	commit  = ""
)

var rootCmd = &cobra.Command{
	Use:           "rdit",
	Short:         "Audit coding-agent reasoning logs",
	Long:          "Run the rdit TUI to inspect reasoning audits, or initialize agent hooks for the current repository.",
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
