package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/rpcarvs/reasond/internal/tui"
)

var (
	commit = ""
)

var runTUI = tui.Run

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reasond",
		Short: "Review reasoning audits in the TUI",
		Long: strings.TrimSpace(`
Run reasond inside the repository you want to audit.

Human setup:
  1. Start reasond in the target repository.
  2. Press i in the TUI to install Codex or Claude assets, or run reasond init.
  3. Run coding-agent sessions so audits are archived.
  4. Return to reasond to process and review archived audits.

Agent flow:
  reasond onboard    Print agent-facing workflow instructions.
  reasond judge      Judge unprocessed archived audits with configured defaults.
  reasond latest     Print findings from the latest judge run.
  reasond show ID    Print one finding detail.
`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRootTUI()
		},
	}
	cmd.AddCommand(newInitCmd(), newOnboardCmd(), newJudgeCmd(), newLatestCmd(), newListCmd(), newShowCmd())
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	return cmd
}

func runRootTUI() error {
	rootDir, err := currentProjectDir()
	if err != nil {
		return err
	}
	if rootDir == "" {
		return fmt.Errorf("git repository root was empty")
	}
	return runTUI(rootDir)
}

// Execute runs the CLI command tree.
func Execute(version string) error {
	rootCmd := newRootCmd()
	rootCmd.InitDefaultVersionFlag()
	rootCmd.SetVersionTemplate("{{printf \"%s version %s\\n\" .Name .Version}}")

	return fang.Execute(
		context.Background(),
		rootCmd,
		fangOptions(version)...,
	)
}

func fangOptions(version string) []fang.Option {
	var options []fang.Option
	if version != "" {
		options = append(options, fang.WithVersion(version))
	}
	if commit != "" {
		options = append(options, fang.WithCommit(commit))
	}
	return options
}
