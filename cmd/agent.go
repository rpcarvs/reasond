package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rpcarvs/reasond/internal/app"
	"github.com/rpcarvs/reasond/internal/processing"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/settings"
	"github.com/rpcarvs/reasond/internal/storage"
)

func newOnboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "onboard",
		Short: "Print agent-facing reasond workflow instructions",
		RunE: func(cmd *cobra.Command, args []string) error {
			printOnboard(cmd.OutOrStdout())
			return nil
		},
	}
}

func newJudgeCmd() *cobra.Command {
	var runAll bool

	cmd := &cobra.Command{
		Use:   "judge",
		Short: "Judge archived audits for agent workflows",
		Long: strings.TrimSpace(`
Judge archived reasoning audits without opening the TUI.

By default this processes only archived markdown files that have not been judged yet,
using the local default judge provider/model from .reasond/settings.json. Use
--all only when you intentionally want to re-judge every indexed audit source.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJudgeCommand(cmd, runAll)
		},
	}
	cmd.Flags().BoolVar(&runAll, "all", false, "re-judge every indexed audit source instead of only pending files")
	return cmd
}

func printOnboard(out io.Writer) {
	_, _ = fmt.Fprint(out, strings.TrimSpace(
		"## Reasoning Debug\n\n"+
			"This repository can use **reasond** to inspect judged reasoning audits during code reviews, debugging, and bug fixing.\n\n"+
			"**Agent quick start:**\n"+
			"- `reasond judge` - Judge archived audits that have not been judged yet.\n"+
			"- `reasond latest` - Print findings from the latest judge run.\n"+
			"- `reasond show <provider:id>` - Print one finding detail with the source markdown path.\n"+
			"- `reasond list` - Print all stored findings when a broader review needs history.\n\n"+
			"**Rules of use:**\n"+
			"- Human setup happens before agent work: use `reasond init` or the TUI install flow to install assets and choose the default judge.\n"+
			"- Judge provider and model come from `.reasond/settings.json`.\n"+
			"- Use `reasond judge --all` only when explicitly asked to re-judge all indexed audits.\n"+
			"- Treat judge findings as evidence to verify against the code, tests, and user request.\n"+
			"- If `reasond latest` returns no findings, continue normal debugging instead of inventing issues.",
	)+"\n")
}

func newLatestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "latest",
		Short: "Print findings from the latest judge run",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, closeStore, err := openAgentStore()
			if err != nil {
				return err
			}
			defer closeStore()

			findings, err := store.ListLatestBatchFindings()
			if err != nil {
				return err
			}
			printFindingTable(cmd.OutOrStdout(), findings)
			return nil
		},
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print all stored judge findings",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, closeStore, err := openAgentStore()
			if err != nil {
				return err
			}
			defer closeStore()

			findings, err := store.ListAllAgentFindings()
			if err != nil {
				return err
			}
			printFindingTable(cmd.OutOrStdout(), findings)
			return nil
		},
	}
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <provider:id>",
		Short: "Print one judge finding detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			publicID, err := storage.ParseFindingPublicID(args[0])
			if err != nil {
				return err
			}

			store, closeStore, err := openAgentStore()
			if err != nil {
				return err
			}
			defer closeStore()

			detail, err := store.GetAgentFindingDetail(publicID)
			if err != nil {
				return err
			}
			printFindingDetail(cmd.OutOrStdout(), detail)
			return nil
		},
	}
}

func runJudgeCommand(cmd *cobra.Command, runAll bool) error {
	store, closeStore, err := openAgentStore()
	if err != nil {
		return err
	}
	defer closeStore()

	bootstrap, err := app.NewBootstrap(store.RootDir())
	if err != nil {
		return err
	}
	processor := bootstrap.NewProcessor(store)
	config, err := settings.Load(store.RootDir())
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	mode := processing.ProcessModePending
	if runAll {
		mode = processing.ProcessModeAll
	}

	_, _ = fmt.Fprintf(out, "Provider: %s\n", config.DefaultJudgeProvider)
	_, _ = fmt.Fprintf(out, "Model: %s\n", config.DefaultJudgeModel)
	_, _ = fmt.Fprintf(out, "Mode: %s\n", mode)

	progress := func(update processing.ProgressUpdate) {
		status := "ok"
		if update.Err != nil {
			status = "error: " + update.Err.Error()
		}
		_, _ = fmt.Fprintf(out, "[%d/%d] %s %s\n", update.Completed, update.Total, update.Source.FilePath, status)
	}

	var result processing.BatchResult
	if runAll {
		result, err = processor.ProcessAllIndexed(context.Background(), config.DefaultJudgeProvider, config.DefaultJudgeModel, progress)
	} else {
		result, err = processor.ProcessUnprocessed(context.Background(), config.DefaultJudgeProvider, config.DefaultJudgeModel, progress)
	}
	if err != nil {
		return err
	}

	printJudgeSummary(out, result)
	return nil
}

func printJudgeSummary(out io.Writer, result processing.BatchResult) {
	if result.Total == 0 {
		_, _ = fmt.Fprintln(out, "No archived audits to judge.")
		return
	}

	_, _ = fmt.Fprintf(out, "Done: %d succeeded, %d failed, %d total.\n", result.Succeeded, len(result.Failed), result.Total)
	for _, failure := range result.Failed {
		_, _ = fmt.Fprintf(out, "Failure: %s: %v\n", failure.Source.FilePath, failure.Err)
	}
}

func openAgentStore() (*storage.Store, func(), error) {
	rootDir, err := currentProjectDir()
	if err != nil {
		return nil, nil, err
	}

	if _, err := appRuntime.EnsureLayout(rootDir); err != nil {
		return nil, nil, err
	}

	bootstrap, err := app.NewBootstrap(rootDir)
	if err != nil {
		return nil, nil, err
	}

	store, err := bootstrap.OpenStore()
	if err != nil {
		return nil, nil, err
	}
	closeStore := func() {
		_ = store.Close()
	}

	if _, err := store.SyncArchivedAudits(); err != nil {
		closeStore()
		return nil, nil, err
	}

	return store, closeStore, nil
}

func printFindingTable(out io.Writer, findings []storage.AgentFindingSummary) {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(out, "No findings.")
		return
	}

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tSCORE\tPROVIDER\tMODEL\tTITLE\tSOURCE")
	for _, finding := range findings {
		_, _ = fmt.Fprintf(
			table,
			"%s\t%.2f\t%s\t%s\t%s\t%s\n",
			finding.PublicID,
			finding.Score,
			finding.Provider,
			finding.JudgeModel,
			finding.Title,
			finding.SourcePath,
		)
	}
	_ = table.Flush()
}

func printFindingDetail(out io.Writer, detail storage.FindingDetail) {
	_, _ = fmt.Fprintf(out, "ID: %s\n", storage.FormatFindingPublicID(detail.JudgeProvider, detail.ID))
	_, _ = fmt.Fprintf(out, "Title: %s\n", detail.Title)
	_, _ = fmt.Fprintf(out, "Score: %.2f\n", detail.Score)
	_, _ = fmt.Fprintf(out, "Provider: %s\n", detail.JudgeProvider)
	_, _ = fmt.Fprintf(out, "Model: %s\n", detail.JudgeModel)
	_, _ = fmt.Fprintf(out, "Source: %s\n", detail.SourceFullPath)
	_, _ = fmt.Fprintf(out, "\nIssue:\n%s\n", strings.TrimSpace(detail.Issue))
	_, _ = fmt.Fprintf(out, "\nWhy:\n%s\n", strings.TrimSpace(detail.Why))
	_, _ = fmt.Fprintf(out, "\nHow:\n%s\n", strings.TrimSpace(detail.How))
}
