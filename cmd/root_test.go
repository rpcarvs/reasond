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

func TestFangOptionsOmitVersionOverrideByDefault(t *testing.T) {
	t.Parallel()

	originalCommit := commit
	commit = ""
	t.Cleanup(func() {
		commit = originalCommit
	})

	options := fangOptions("")
	if len(options) != 0 {
		t.Fatalf("expected no Fang options when commit is empty, got %d", len(options))
	}
}

func TestFangOptionsIncludeVersionOverrideWhenProvided(t *testing.T) {
	t.Parallel()

	originalCommit := commit
	commit = ""
	t.Cleanup(func() {
		commit = originalCommit
	})

	options := fangOptions("v1.2.3")
	if len(options) != 1 {
		t.Fatalf("expected one Fang option when version is provided, got %d", len(options))
	}
}

func TestFangOptionsIncludeVersionAndCommit(t *testing.T) {
	t.Parallel()

	originalCommit := commit
	commit = "abcdef123456"
	t.Cleanup(func() {
		commit = originalCommit
	})

	options := fangOptions("v1.2.3")
	if len(options) != 2 {
		t.Fatalf("expected version and commit Fang options, got %d", len(options))
	}
}
