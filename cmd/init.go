package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	assetbundle "rdit/cmd/assets"
	"rdit/internal/app"
	"rdit/internal/codexconfig"
	"rdit/internal/install"
)

func init() {
	rootCmd.AddCommand(newProviderCommand(assetbundle.ProviderCodex))
	rootCmd.AddCommand(newProviderCommand(assetbundle.ProviderClaude))
}

func newProviderCommand(provider assetbundle.Provider) *cobra.Command {
	providerCmd := &cobra.Command{
		Use:   string(provider),
		Short: fmt.Sprintf("%s-specific commands", provider.Label()),
	}

	providerCmd.AddCommand(newInitCommand(provider))
	return providerCmd
}

func newInitCommand(provider assetbundle.Provider) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: fmt.Sprintf("Install %s hooks and skill files into the current repository", provider),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrap(".")
			if err != nil {
				return err
			}

			result, err := bootstrap.InitProvider(provider)
			if result.Codex != nil {
				if printErr := printCodexConfigResult(cmd, *result.Codex); printErr != nil {
					return printErr
				}
			}
			if err != nil {
				return err
			}
			if err := printInstallResult(cmd, provider, result.Install); err != nil {
				return err
			}
			if err := printLayoutResult(cmd, result.Layout); err != nil {
				return err
			}
			if err := printDatabaseResult(cmd, result.Database); err != nil {
				return err
			}
			return nil
		},
	}
}

func printInstallResult(cmd *cobra.Command, provider assetbundle.Provider, result install.Result) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "%s init result\n", provider.Label()); err != nil {
		return err
	}

	if len(result.Created) > 0 {
		if _, err := fmt.Fprintln(out, "Created:"); err != nil {
			return err
		}
		for _, path := range result.Created {
			if _, err := fmt.Fprintf(out, "  %s\n", path); err != nil {
				return err
			}
		}
	}

	if len(result.Reused) > 0 {
		if _, err := fmt.Fprintln(out, "Reused:"); err != nil {
			return err
		}
		for _, path := range result.Reused {
			if _, err := fmt.Fprintf(out, "  %s\n", path); err != nil {
				return err
			}
		}
	}

	if len(result.Conflicts) > 0 {
		if _, err := fmt.Fprintln(out, "Conflicts:"); err != nil {
			return err
		}
		for _, path := range result.Conflicts {
			if _, err := fmt.Fprintf(out, "  %s\n", path); err != nil {
				return err
			}
		}
	}

	return nil
}

func printLayoutResult(cmd *cobra.Command, result app.LayoutResult) error {
	out := cmd.OutOrStdout()

	if result.RuntimeDirCreated {
		if _, err := fmt.Fprintf(out, "Created runtime directory: %s\n", ".rdit"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "Runtime directory already present: %s\n", ".rdit"); err != nil {
			return err
		}
	}

	if result.GitIgnoreCreated {
		if _, err := fmt.Fprintln(out, "Created: .gitignore"); err != nil {
			return err
		}
	}

	if len(result.GitIgnoreAdded) > 0 {
		if _, err := fmt.Fprintln(out, "Added to .gitignore:"); err != nil {
			return err
		}
		for _, entry := range result.GitIgnoreAdded {
			if _, err := fmt.Fprintf(out, "  %s\n", entry); err != nil {
				return err
			}
		}
	}

	if len(result.GitIgnorePresent) > 0 {
		if _, err := fmt.Fprintln(out, "Already ignored:"); err != nil {
			return err
		}
		for _, entry := range result.GitIgnorePresent {
			if _, err := fmt.Fprintf(out, "  %s\n", entry); err != nil {
				return err
			}
		}
	}

	return nil
}

func printDatabaseResult(cmd *cobra.Command, result app.DatabaseResult) error {
	out := cmd.OutOrStdout()

	if result.Created {
		_, err := fmt.Fprintf(out, "Created runtime database: %s\n", result.Path)
		return err
	}

	_, err := fmt.Fprintf(out, "Runtime database already present: %s\n", result.Path)
	return err
}

func printCodexConfigResult(cmd *cobra.Command, result app.CodexConfigResult) error {
	out := cmd.OutOrStdout()

	switch result.Status {
	case codexconfig.StatusEnabled:
		_, err := fmt.Fprintf(out, "Codex hooks already enabled: %s\n", result.Path)
		return err
	case codexconfig.StatusAdded:
		_, err := fmt.Fprintf(out, "Enabled Codex hooks in: %s\n", result.Path)
		return err
	case codexconfig.StatusBlocked:
		_, err := fmt.Fprintf(out, "Codex hooks are set to false in %s and must be enabled manually.\n", result.Path)
		return err
	}

	return nil
}
