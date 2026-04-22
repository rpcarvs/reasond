package cmd

import (
	"context"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/rpcarvs/reasond/internal/tui"
)

var (
	version = "dev"
	commit  = ""
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reasond",
		Short: "Review reasoning audits in the TUI",
		Long: strings.TrimSpace(`
Run reasond inside the repository you want to audit.

Typical flow:
  1. Start reasond in the target repository.
  2. Press i in the TUI to install Codex or Claude assets.
  3. Run coding-agent sessions so audits are archived.
  4. Return to reasond to process and review archived audits.
`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(".")
		},
	}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	return cmd
}

// Execute runs the CLI command tree.
func Execute() error {
	rootCmd := newRootCmd()
	rootCmd.InitDefaultVersionFlag()
	rootCmd.SetVersionTemplate("{{printf \"%s version %s\\n\" .Name .Version}}")

	return fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	)
}
