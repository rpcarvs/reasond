package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpFocusesOnTUIWorkflow(t *testing.T) {
	t.Parallel()

	rootCmd := newRootCmd()
	rootCmd.InitDefaultVersionFlag()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}

	helpText := out.String()
	for _, unwanted := range []string{
		"completion [command]",
		"help [command]",
	} {
		if strings.Contains(helpText, unwanted) {
			t.Fatalf("unexpected help entry %q in output:\n%s", unwanted, helpText)
		}
	}

	for _, expected := range []string{
		"Run reasond inside the repository you want to audit.",
		"Press i in the TUI to install Codex or Claude assets.",
		"Return to reasond to process and review archived audits.",
	} {
		if !strings.Contains(helpText, expected) {
			t.Fatalf("expected help text %q in output:\n%s", expected, helpText)
		}
	}
}
