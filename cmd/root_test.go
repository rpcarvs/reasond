package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/settings"
)

func TestRootHelpFocusesOnTUIWorkflow(t *testing.T) {
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
		"Human setup:",
		"Press i in the TUI to install Codex or Claude assets, or run reasond init.",
		"Return to reasond to process and review archived audits.",
		"reasond onboard",
		"reasond judge",
		"reasond latest",
		"reasond show ID",
	} {
		if !strings.Contains(helpText, expected) {
			t.Fatalf("expected help text %q in output:\n%s", expected, helpText)
		}
	}
}

func TestAgentCLIJudgeLatestAndShowWithFakeCodex(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	t.Chdir(root)
	installFakeCodex(t)

	archiveDir := filepath.Join(root, ".reasond", "reasond_audits")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	sourcePath := filepath.Join(archiveDir, "audit.md")
	if err := os.WriteFile(sourcePath, []byte("# User Prompt\n\nFix the bug.\n\n# Reasoning by Codex\n\nI skipped tests.\n"), 0o644); err != nil {
		t.Fatalf("write audit source: %v", err)
	}
	displaySourcePath, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		t.Fatalf("resolve display source path: %v", err)
	}

	var judgeOut bytes.Buffer
	judgeCmd := newRootCmd()
	judgeCmd.SetOut(&judgeOut)
	judgeCmd.SetErr(&judgeOut)
	judgeCmd.SetArgs([]string{"judge"})
	if err := judgeCmd.Execute(); err != nil {
		t.Fatalf("run judge command: %v\n%s", err, judgeOut.String())
	}
	for _, expected := range []string{
		"Provider: codex",
		"Model: gpt-5.4-mini",
		"Mode: pending",
		"[1/1] audit.md ok",
		"Done: 1 succeeded, 0 failed, 1 total.",
	} {
		if !strings.Contains(judgeOut.String(), expected) {
			t.Fatalf("expected judge output %q in:\n%s", expected, judgeOut.String())
		}
	}

	var latestOut bytes.Buffer
	latestCmd := newRootCmd()
	latestCmd.SetOut(&latestOut)
	latestCmd.SetErr(&latestOut)
	latestCmd.SetArgs([]string{"latest"})
	if err := latestCmd.Execute(); err != nil {
		t.Fatalf("run latest command: %v\n%s", err, latestOut.String())
	}
	for _, expected := range []string{
		"ID",
		"codex:1",
		"0.75",
		"Fake finding",
		"audit.md",
	} {
		if !strings.Contains(latestOut.String(), expected) {
			t.Fatalf("expected latest output %q in:\n%s", expected, latestOut.String())
		}
	}

	var showOut bytes.Buffer
	showCmd := newRootCmd()
	showCmd.SetOut(&showOut)
	showCmd.SetErr(&showOut)
	showCmd.SetArgs([]string{"show", "codex:1"})
	if err := showCmd.Execute(); err != nil {
		t.Fatalf("run show command: %v\n%s", err, showOut.String())
	}
	for _, expected := range []string{
		"ID: codex:1",
		"Title: Fake finding",
		"Source: " + displaySourcePath,
		"Issue:",
		"The audit skipped verification.",
		"Why:",
		"How:",
	} {
		if !strings.Contains(showOut.String(), expected) {
			t.Fatalf("expected show output %q in:\n%s", expected, showOut.String())
		}
	}
}

func TestOnboardPrintsAgentWorkflow(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"onboard"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run onboard command: %v\n%s", err, out.String())
	}

	for _, expected := range []string{
		"## Reasoning Debug",
		"reasond judge",
		"reasond latest",
		"reasond show <provider:id>",
		"Human setup happens before agent work",
		"or the TUI install flow",
		".reasond/settings.json",
		"Treat judge findings as evidence",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("expected onboard output %q in:\n%s", expected, out.String())
		}
	}
}

func TestRunInitRequestInstallsProviderAndSavesSettings(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	t.Chdir(root)

	var out bytes.Buffer
	if err := runInitRequest(&out, initRequest{
		Providers: []assetbundle.Provider{assetbundle.ProviderClaude},
		Settings: settings.Settings{
			DefaultJudgeProvider: "claude",
			DefaultJudgeModel:    settings.DefaultClaudeModel,
		},
	}); err != nil {
		t.Fatalf("run init request: %v\n%s", err, out.String())
	}

	for _, path := range []string{
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".claude", "skills", "reasoning-audit", "SKILL.md"),
		filepath.Join(root, ".claude", "skills", "reasoning-debug", "SKILL.md"),
		filepath.Join(root, ".reasond", "settings.json"),
		filepath.Join(root, ".reasond", "audits_reports.db"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat initialized path %q: %v", path, err)
		}
	}

	loaded, err := settings.Load(root)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if loaded.DefaultJudgeProvider != "claude" || loaded.DefaultJudgeModel != settings.DefaultClaudeModel {
		t.Fatalf("unexpected loaded settings: %+v", loaded)
	}

	for _, expected := range []string{
		"Installed Claude assets.",
		"Default judge: claude / claude-haiku-4-5",
		"Settings: .reasond/settings.json",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("expected init output %q in:\n%s", expected, out.String())
		}
	}
}

func TestInitPromptLabelsAreExplicit(t *testing.T) {
	if !strings.Contains(providerPromptDescription, "x or space") {
		t.Fatalf("provider prompt must explain selection keys")
	}
	if !strings.Contains(providerPromptDescription, "enter") {
		t.Fatalf("provider prompt must explain confirmation key")
	}
	if got := providerOptionLabel(assetbundle.ProviderCodex); got != "Codex" {
		t.Fatalf("unexpected codex provider label %q", got)
	}
	if got := providerOptionLabel(assetbundle.ProviderClaude); got != "Claude Code" {
		t.Fatalf("unexpected claude provider label %q", got)
	}
	choices, err := judgeChoices()
	if err != nil {
		t.Fatalf("build judge choices: %v", err)
	}
	if !hasJudgeChoice(choices, "codex", settings.DefaultCodexModel) {
		t.Fatalf("expected codex judge choice in %+v", choices)
	}
	if !hasJudgeChoice(choices, "claude", settings.DefaultClaudeModel) {
		t.Fatalf("expected claude judge choice in %+v", choices)
	}
	if got := judgeChoiceLabel(judgeChoice{Provider: "codex", Model: "gpt-5.4-mini"}); got != "Codex judge: gpt-5.4-mini" {
		t.Fatalf("unexpected codex judge label %q", got)
	}
	if got := judgeChoiceLabel(judgeChoice{Provider: "claude", Model: "claude-haiku-4-5"}); got != "Claude Code judge: claude-haiku-4-5" {
		t.Fatalf("unexpected claude judge label %q", got)
	}
}

func hasJudgeChoice(choices []judgeChoice, provider, model string) bool {
	for _, choice := range choices {
		if choice.Provider == provider && choice.Model == model {
			return true
		}
	}
	return false
}

func TestAgentCLIJudgeUsesConfiguredClaudeDefault(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	t.Chdir(root)
	installFakeClaude(t)

	if _, err := settings.Save(root, settings.Settings{
		DefaultJudgeProvider: "claude",
		DefaultJudgeModel:    settings.DefaultClaudeModel,
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	archiveDir := filepath.Join(root, ".reasond", "reasond_audits")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "claude-audit.md"), []byte("# Reasoning by Claude\n\nI skipped tests.\n"), 0o644); err != nil {
		t.Fatalf("write audit source: %v", err)
	}

	var judgeOut bytes.Buffer
	judgeCmd := newRootCmd()
	judgeCmd.SetOut(&judgeOut)
	judgeCmd.SetErr(&judgeOut)
	judgeCmd.SetArgs([]string{"judge"})
	if err := judgeCmd.Execute(); err != nil {
		t.Fatalf("run judge command: %v\n%s", err, judgeOut.String())
	}
	for _, expected := range []string{
		"Provider: claude",
		"Model: claude-haiku-4-5",
		"[1/1] claude-audit.md ok",
	} {
		if !strings.Contains(judgeOut.String(), expected) {
			t.Fatalf("expected judge output %q in:\n%s", expected, judgeOut.String())
		}
	}

	var latestOut bytes.Buffer
	latestCmd := newRootCmd()
	latestCmd.SetOut(&latestOut)
	latestCmd.SetErr(&latestOut)
	latestCmd.SetArgs([]string{"latest"})
	if err := latestCmd.Execute(); err != nil {
		t.Fatalf("run latest command: %v\n%s", err, latestOut.String())
	}
	for _, expected := range []string{
		"claude:1",
		"Fake Claude finding",
		"claude-audit.md",
	} {
		if !strings.Contains(latestOut.String(), expected) {
			t.Fatalf("expected latest output %q in:\n%s", expected, latestOut.String())
		}
	}
}

func TestFangOptionsOmitVersionOverrideByDefault(t *testing.T) {
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

func initGitRepo(t *testing.T, root string) {
	t.Helper()

	cmd := exec.Command("git", "init")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func installFakeCodex(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "codex")
	script := `#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift || true
done
if [ -z "$output" ]; then
  echo "missing output path" >&2
  exit 1
fi
cat >"$output" <<'JSON'
{"findings":[{"title":"Fake finding","issue":"The audit skipped verification.","why":"Verification was required for confidence.","how":"The user asked to fix a bug, but the agent did not validate the fix.","score":0.75}]}
JSON
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installFakeClaude(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "claude")
	script := `#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
{"structured_output":{"findings":[{"title":"Fake Claude finding","issue":"The audit skipped verification.","why":"Verification was required for confidence.","how":"The user asked to fix a bug, but the agent did not validate the fix.","score":0.65}]}}
JSON
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
