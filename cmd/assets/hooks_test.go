package assets

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptHookAppendsPendingPromptInStagingDir(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			stagingDir := filepath.Join(root, ".reasond_tmp")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}

			script := writeEmbeddedScript(t, provider.promptScript)
			cmd := exec.Command("bash", script)
			cmd.Dir = root
			cmd.Env = provider.env(root)
			cmd.Stdin = bytes.NewBufferString(`{"prompt":"first prompt"}`)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("run prompt hook: %v output=%s", err, string(output))
			}

			content := readFile(t, filepath.Join(stagingDir, ".pending_prompt"))
			if content != "first prompt\n\n" {
				t.Fatalf("unexpected pending prompt contents: %q", content)
			}
		})
	}
}

func TestSessionStartPreservesPendingPrompt(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := provider.setupRoot(t)
			reasondBinDir := installFakeReasond(t, root)
			stagingDir := filepath.Join(root, ".reasond_tmp")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}
			pendingPath := filepath.Join(stagingDir, ".pending_prompt")
			if err := os.WriteFile(pendingPath, []byte("cached prompt\n\n"), 0o644); err != nil {
				t.Fatalf("write pending prompt: %v", err)
			}

			env := append(provider.env(root), "PATH="+reasondBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			runHookCommand(t, provider.sessionStartCommand(t), root, env)

			if content := readFile(t, pendingPath); content != "cached prompt\n\n" {
				t.Fatalf("expected SessionStart to preserve pending prompt, got %q", content)
			}
			if invoked := readFile(t, filepath.Join(stagingDir, "onboard-invoked")); invoked != "onboard\n" {
				t.Fatalf("expected SessionStart to run reasond onboard, got %q", invoked)
			}
		})
	}
}

func TestStopHookPreservesPendingPromptWithoutAuditTarget(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			stagingDir := filepath.Join(root, ".reasond_tmp")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}
			pendingPath := filepath.Join(stagingDir, ".pending_prompt")
			if err := os.WriteFile(pendingPath, []byte("cached prompt\n\n"), 0o644); err != nil {
				t.Fatalf("write pending prompt: %v", err)
			}

			runStopHook(t, provider, root)

			if content := readFile(t, pendingPath); content != "cached prompt\n\n" {
				t.Fatalf("expected Stop to preserve pending prompt without target, got %q", content)
			}
		})
	}
}

func TestStopHookPrependsPromptAndArchivesAudit(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			stagingDir := filepath.Join(root, ".reasond_tmp")
			archiveDir := filepath.Join(root, ".reasond", "reasond_audits")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(stagingDir, "123.md"), []byte(provider.reasoningHeader+"\nbody\n"), 0o644); err != nil {
				t.Fatalf("write staged audit: %v", err)
			}
			if err := os.WriteFile(filepath.Join(stagingDir, ".pending_prompt"), []byte("prompt text\n\n"), 0o644); err != nil {
				t.Fatalf("write pending prompt: %v", err)
			}

			runStopHook(t, provider, root)

			expected := "# User Prompt\n\nprompt text\n\n" + provider.reasoningHeader + "\nbody\n"
			if stagedContent := readFile(t, filepath.Join(stagingDir, "123.md")); stagedContent != expected {
				t.Fatalf("unexpected staged audit contents: %q", stagedContent)
			}
			if archivedContent := readFile(t, filepath.Join(archiveDir, "123.md")); archivedContent != expected {
				t.Fatalf("unexpected archived audit contents: %q", archivedContent)
			}
			if control := readFile(t, filepath.Join(stagingDir, ".control")); control != "123.md\n" {
				t.Fatalf("unexpected control ledger contents: %q", control)
			}
			if _, err := os.Stat(filepath.Join(stagingDir, ".pending_prompt")); !os.IsNotExist(err) {
				t.Fatalf("expected pending prompt removed after archive success, stat err=%v", err)
			}
		})
	}
}

func TestStopHookPreservesPendingPromptWithMultipleAuditTargets(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			stagingDir := filepath.Join(root, ".reasond_tmp")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(stagingDir, "first.md"), []byte(provider.reasoningHeader+"\nfirst\n"), 0o644); err != nil {
				t.Fatalf("write first audit: %v", err)
			}
			if err := os.WriteFile(filepath.Join(stagingDir, "second.md"), []byte(provider.reasoningHeader+"\nsecond\n"), 0o644); err != nil {
				t.Fatalf("write second audit: %v", err)
			}
			pendingPath := filepath.Join(stagingDir, ".pending_prompt")
			if err := os.WriteFile(pendingPath, []byte("ambiguous prompt\n\n"), 0o644); err != nil {
				t.Fatalf("write pending prompt: %v", err)
			}

			err := runStopHookAllowError(t, provider, root)
			if err == nil {
				t.Fatal("expected Stop hook to fail closed with multiple audit targets")
			}
			if content := readFile(t, pendingPath); content != "ambiguous prompt\n\n" {
				t.Fatalf("expected ambiguous pending prompt to survive, got %q", content)
			}
			if strings.Contains(readFile(t, filepath.Join(stagingDir, "first.md")), "# User Prompt") {
				t.Fatal("expected first audit to remain unchanged")
			}
			if strings.Contains(readFile(t, filepath.Join(stagingDir, "second.md")), "# User Prompt") {
				t.Fatal("expected second audit to remain unchanged")
			}
		})
	}
}

func TestStopHookPreservesPendingPromptWhenAuditAlreadyStamped(t *testing.T) {
	for _, provider := range hookProviders() {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			stagingDir := filepath.Join(root, ".reasond_tmp")
			if err := os.MkdirAll(stagingDir, 0o755); err != nil {
				t.Fatalf("create staging dir: %v", err)
			}
			stamped := "# User Prompt\n\nold prompt\n\n" + provider.reasoningHeader + "\nbody\n"
			if err := os.WriteFile(filepath.Join(stagingDir, "stamped.md"), []byte(stamped), 0o644); err != nil {
				t.Fatalf("write stamped audit: %v", err)
			}
			pendingPath := filepath.Join(stagingDir, ".pending_prompt")
			if err := os.WriteFile(pendingPath, []byte("new prompt\n\n"), 0o644); err != nil {
				t.Fatalf("write pending prompt: %v", err)
			}

			err := runStopHookAllowError(t, provider, root)
			if err == nil {
				t.Fatal("expected Stop hook to fail closed when target is already stamped and prompt is pending")
			}
			if content := readFile(t, pendingPath); content != "new prompt\n\n" {
				t.Fatalf("expected pending prompt to survive stamped target, got %q", content)
			}
			if content := readFile(t, filepath.Join(stagingDir, "stamped.md")); content != stamped {
				t.Fatalf("expected stamped audit to remain unchanged, got %q", content)
			}
		})
	}
}

type hookProvider struct {
	name            string
	configPath      string
	promptScript    string
	stopScript      string
	reasoningHeader string
	env             func(root string) []string
	setupRoot       func(t *testing.T) string
}

func hookProviders() []hookProvider {
	return []hookProvider{
		{
			name:            "codex",
			configPath:      "codex_assets/codex/hooks.json",
			promptScript:    "codex_assets/codex/hooks/reasoning-audit-prompt.sh",
			stopScript:      "codex_assets/codex/hooks/reasoning-audit-stop.sh",
			reasoningHeader: "# Reasoning by Codex",
			env: func(root string) []string {
				return os.Environ()
			},
			setupRoot: setupGitRoot,
		},
		{
			name:            "claude",
			configPath:      "claude_assets/claude/settings.json",
			promptScript:    "claude_assets/claude/hooks/reasoning-audit-prompt.sh",
			stopScript:      "claude_assets/claude/hooks/reasoning-audit-stop.sh",
			reasoningHeader: "# Reasoning by Claude",
			env: func(root string) []string {
				return append(os.Environ(), "CLAUDE_PROJECT_DIR="+root)
			},
			setupRoot: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
		},
	}
}

func setupGitRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize git repo: %v output=%s", err, string(output))
	}
	return root
}

func runStopHook(t *testing.T, provider hookProvider, root string) {
	t.Helper()

	if err := runStopHookAllowError(t, provider, root); err != nil {
		t.Fatalf("run stop hook: %v", err)
	}
}

func runStopHookAllowError(t *testing.T, provider hookProvider, root string) error {
	t.Helper()

	script := writeEmbeddedScript(t, provider.stopScript)
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Env = provider.env(root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return hookError{err: err, output: string(output)}
	}
	return nil
}

func runHookCommand(t *testing.T, command string, root string, env []string) {
	t.Helper()

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = root
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run hook command: %v output=%s", err, string(output))
	}
}

type hookError struct {
	err    error
	output string
}

func (e hookError) Error() string {
	return e.err.Error() + " output=" + e.output
}

func (p hookProvider) sessionStartCommand(t *testing.T) string {
	t.Helper()

	var config struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	content, err := FS.ReadFile(p.configPath)
	if err != nil {
		t.Fatalf("read embedded config %q: %v", p.configPath, err)
	}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("parse embedded config %q: %v", p.configPath, err)
	}
	if len(config.Hooks.SessionStart) != 1 || len(config.Hooks.SessionStart[0].Hooks) != 1 {
		t.Fatalf("expected one SessionStart command in %q", p.configPath)
	}
	return config.Hooks.SessionStart[0].Hooks[0].Command
}

func installFakeReasond(t *testing.T, root string) string {
	t.Helper()

	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "reasond")
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$#\" -ne 1 ] || [ \"$1\" != \"onboard\" ]; then\n" +
		"  echo \"unexpected args: $*\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'onboard\\n' > \"" + filepath.Join(root, ".reasond_tmp", "onboard-invoked") + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake reasond: %v", err)
	}
	return binDir
}

func writeEmbeddedScript(t *testing.T, embeddedPath string) string {
	t.Helper()

	content, err := FS.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("read embedded script %q: %v", embeddedPath, err)
	}

	path := filepath.Join(t.TempDir(), filepath.Base(embeddedPath))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write embedded script %q: %v", embeddedPath, err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %q: %v", path, err)
	}
	return string(content)
}
