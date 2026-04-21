package assets

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCodexPromptHookAppendsPendingPromptInStagingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stagingDir := filepath.Join(root, ".reasond_tmp")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("create staging dir: %v", err)
	}

	script := writeEmbeddedScript(t, "codex_assets/codex/hooks/reasoning-audit-prompt.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Stdin = bytes.NewBufferString(`{"prompt":"first prompt"}`)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run prompt hook: %v output=%s", err, string(output))
	}

	content, err := os.ReadFile(filepath.Join(stagingDir, ".pending_prompt"))
	if err != nil {
		t.Fatalf("read pending prompt: %v", err)
	}
	if string(content) != "first prompt\n\n" {
		t.Fatalf("unexpected pending prompt contents: %q", string(content))
	}
}

func TestCodexStopHookPrependsPromptAndArchivesAudit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stagingDir := filepath.Join(root, ".reasond_tmp")
	archiveDir := filepath.Join(root, ".reasond", "reasond_audits")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("create staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "123.md"), []byte("# Reasoning\nbody\n"), 0o644); err != nil {
		t.Fatalf("write staged audit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, ".pending_prompt"), []byte("prompt text\n\n"), 0o644); err != nil {
		t.Fatalf("write pending prompt: %v", err)
	}

	script := writeEmbeddedScript(t, "codex_assets/codex/hooks/reasoning-audit-stop.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run stop hook: %v output=%s", err, string(output))
	}

	stagedContent, err := os.ReadFile(filepath.Join(stagingDir, "123.md"))
	if err != nil {
		t.Fatalf("read staged audit: %v", err)
	}
	expected := "# User Prompt\n\nprompt text\n\n# Reasoning\nbody\n"
	if string(stagedContent) != expected {
		t.Fatalf("unexpected staged audit contents: %q", string(stagedContent))
	}

	archivedContent, err := os.ReadFile(filepath.Join(archiveDir, "123.md"))
	if err != nil {
		t.Fatalf("read archived audit: %v", err)
	}
	if string(archivedContent) != expected {
		t.Fatalf("unexpected archived audit contents: %q", string(archivedContent))
	}

	control, err := os.ReadFile(filepath.Join(stagingDir, ".control"))
	if err != nil {
		t.Fatalf("read control ledger: %v", err)
	}
	if string(control) != "123.md\n" {
		t.Fatalf("unexpected control ledger contents: %q", string(control))
	}

	if _, err := os.Stat(filepath.Join(stagingDir, ".pending_prompt")); !os.IsNotExist(err) {
		t.Fatalf("expected pending prompt removed after archive success, stat err=%v", err)
	}
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
