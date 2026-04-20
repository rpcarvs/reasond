package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"rdit/internal/testutil"
)

func TestOpenBootstrapsRuntimeDatabase(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("stat database path: %v", err)
	}

	var sourceTable string
	if err := store.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'audit_sources';`).Scan(&sourceTable); err != nil {
		t.Fatalf("query audit_sources table: %v", err)
	}
	if sourceTable != "audit_sources" {
		t.Fatalf("expected audit_sources table, got %q", sourceTable)
	}

	var findingsTable string
	if err := store.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'audit_findings';`).Scan(&findingsTable); err != nil {
		t.Fatalf("query audit_findings table: %v", err)
	}
	if findingsTable != "audit_findings" {
		t.Fatalf("expected audit_findings table, got %q", findingsTable)
	}

	expectedPath := filepath.Join(root, ".rdit", "audits_reports.db")
	if store.Path() != expectedPath {
		t.Fatalf("expected database path %q, got %q", expectedPath, store.Path())
	}

	var journalMode string
	if err := store.DB().QueryRow(`PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := store.DB().QueryRow(`PRAGMA busy_timeout;`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout 5000, got %d", busyTimeout)
	}

	stats := store.DB().Stats()
	if stats.MaxOpenConnections != 1 {
		t.Fatalf("expected max open connections 1, got %d", stats.MaxOpenConnections)
	}
}

func TestSyncReasoningAuditsInsertsAndDetectsImmutableConflicts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := filepath.Join(root, "reasoning_audits")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create audit dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(auditDir, "first.md"), []byte("# one\n"), 0o644); err != nil {
		t.Fatalf("write first audit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "second.md"), []byte("# two\n"), 0o644); err != nil {
		t.Fatalf("write second audit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, ".control"), []byte("ignore\n"), 0o644); err != nil {
		t.Fatalf("write control file: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	result, err := store.SyncReasoningAudits()
	if err != nil {
		t.Fatalf("sync audit files: %v", err)
	}

	expectedInserted := []string{"first.md", "second.md"}
	if !slices.Equal(result.Inserted, expectedInserted) {
		t.Fatalf("expected inserted %v, got %v", expectedInserted, result.Inserted)
	}
	if len(result.Known) != 0 {
		t.Fatalf("expected no known files on first sync, got %v", result.Known)
	}
	if len(result.ImmutableConflicts) != 0 {
		t.Fatalf("expected no immutable conflicts on first sync, got %v", result.ImmutableConflicts)
	}

	result, err = store.SyncReasoningAudits()
	if err != nil {
		t.Fatalf("sync known audit files: %v", err)
	}
	if !slices.Equal(result.Known, expectedInserted) {
		t.Fatalf("expected known %v, got %v", expectedInserted, result.Known)
	}

	if err := os.WriteFile(filepath.Join(auditDir, "second.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatalf("mutate audit file: %v", err)
	}

	result, err = store.SyncReasoningAudits()
	if err != nil {
		t.Fatalf("sync mutated audit files: %v", err)
	}
	if !slices.Equal(result.ImmutableConflicts, []string{"second.md"}) {
		t.Fatalf("expected immutable conflict for second.md, got %v", result.ImmutableConflicts)
	}
}

func TestSyncReasoningAuditsLoadsFixtureFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixtureAudits := testutil.CopyFixtureTree(t, "audits")
	if err := os.Rename(fixtureAudits, filepath.Join(root, "reasoning_audits")); err != nil {
		t.Fatalf("move fixture audits into repo: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	result, err := store.SyncReasoningAudits()
	if err != nil {
		t.Fatalf("sync fixture audit files: %v", err)
	}

	expected := []string{
		"multiple-findings.md",
		"no-issues.md",
		"pending-simple.md",
	}
	if !slices.Equal(result.Inserted, expected) {
		t.Fatalf("expected inserted %v, got %v", expected, result.Inserted)
	}
}

func TestPersistProcessedResultHandlesZeroAndMultipleFindings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := filepath.Join(root, "reasoning_audits")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create audit dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(auditDir, "zero.md"), []byte("# zero\n"), 0o644); err != nil {
		t.Fatalf("write zero audit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "multi.md"), []byte("# multi\n"), 0o644); err != nil {
		t.Fatalf("write multi audit: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if _, err := store.SyncReasoningAudits(); err != nil {
		t.Fatalf("sync audit files: %v", err)
	}

	zeroID := sourceIDByPath(t, store, "zero.md")
	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      zeroID,
		JudgeProvider: "codex",
		JudgeModel:    "gpt-5.4-mini",
	}); err != nil {
		t.Fatalf("persist zero findings: %v", err)
	}

	var zeroProcessed int
	var zeroProvider, zeroModel string
	if err := store.DB().QueryRow(
		`SELECT processed, judge_provider, judge_model FROM audit_sources WHERE id = ?`,
		zeroID,
	).Scan(&zeroProcessed, &zeroProvider, &zeroModel); err != nil {
		t.Fatalf("query zero source row: %v", err)
	}
	if zeroProcessed != 1 || zeroProvider != "codex" || zeroModel != "gpt-5.4-mini" {
		t.Fatalf("unexpected zero source row state: processed=%d provider=%q model=%q", zeroProcessed, zeroProvider, zeroModel)
	}

	var zeroFindingCount int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_findings WHERE source_id = ?`,
		zeroID,
	).Scan(&zeroFindingCount); err != nil {
		t.Fatalf("count zero findings: %v", err)
	}
	if zeroFindingCount != 0 {
		t.Fatalf("expected zero finding rows, got %d", zeroFindingCount)
	}

	multiID := sourceIDByPath(t, store, "multi.md")
	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      multiID,
		JudgeProvider: "claude",
		JudgeModel:    "claude-haiku-4-5",
		Findings: []FindingInput{
			{
				Title: "Skipped user constraint",
				Issue: "The agent ignored a direct constraint.",
				Why:   "Ignoring explicit constraints breaks prompt compliance.",
				How:   "The user required a bounded output but the agent exceeded it.",
				Score: 0.8,
			},
			{
				Title: "Weak validation",
				Issue: "The agent did not verify a risky assumption.",
				Why:   "That can lead to a false conclusion.",
				How:   "The prompt depended on the real repo state but no check was done.",
				Score: 0.5,
			},
		},
	}); err != nil {
		t.Fatalf("persist multiple findings: %v", err)
	}

	var multiFindingCount int
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_findings WHERE source_id = ?`,
		multiID,
	).Scan(&multiFindingCount); err != nil {
		t.Fatalf("count multi findings: %v", err)
	}
	if multiFindingCount != 2 {
		t.Fatalf("expected two finding rows, got %d", multiFindingCount)
	}
}

func TestQueryMethodsExposePendingSourcesAndBoardData(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := filepath.Join(root, "reasoning_audits")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create audit dir: %v", err)
	}

	for _, name := range []string{"pending.md", "zero.md", "issue.md"} {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if _, err := store.SyncReasoningAudits(); err != nil {
		t.Fatalf("sync audit files: %v", err)
	}

	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "zero.md"),
		JudgeProvider: "codex",
		JudgeModel:    "gpt-5.4-mini",
	}); err != nil {
		t.Fatalf("persist zero findings: %v", err)
	}

	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "issue.md"),
		JudgeProvider: "claude",
		JudgeModel:    "claude-haiku-4-5",
		Findings: []FindingInput{
			{
				Title: "Critical mismatch",
				Issue: "The reasoning contradicts the prompt.",
				Why:   "That makes the result unreliable.",
				How:   "The prompt required repository validation that never happened.",
				Score: 0.9,
			},
		},
	}); err != nil {
		t.Fatalf("persist issue findings: %v", err)
	}

	pendingCount, err := store.CountUnprocessedSources()
	if err != nil {
		t.Fatalf("count unprocessed sources: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected one pending source, got %d", pendingCount)
	}

	pending, err := store.ListUnprocessedSources()
	if err != nil {
		t.Fatalf("list unprocessed sources: %v", err)
	}
	if len(pending) != 1 || pending[0].FilePath != "pending.md" {
		t.Fatalf("expected pending.md to remain unprocessed, got %+v", pending)
	}

	board, err := store.ListBoardFindings()
	if err != nil {
		t.Fatalf("list board findings: %v", err)
	}
	if len(board) != 1 || board[0].Title != "Critical mismatch" {
		t.Fatalf("unexpected board findings: %+v", board)
	}

	detail, err := store.GetFindingDetail(board[0].ID)
	if err != nil {
		t.Fatalf("get finding detail: %v", err)
	}
	if detail.SourcePath != "issue.md" || detail.JudgeProvider != "claude" || detail.JudgeModel != "claude-haiku-4-5" {
		t.Fatalf("unexpected finding detail: %+v", detail)
	}
}

func TestIsRetryableSQLError(t *testing.T) {
	t.Parallel()

	if !isRetryableSQLError(fmt.Errorf("outer: %w", fmt.Errorf("database is locked"))) {
		t.Fatalf("expected database is locked to be retryable")
	}
	if !isRetryableSQLError(fmt.Errorf("sqlite_busy: temporary contention")) {
		t.Fatalf("expected sqlite_busy to be retryable")
	}
	if isRetryableSQLError(fmt.Errorf("different error")) {
		t.Fatalf("expected unrelated error to be non-retryable")
	}
}

func sourceIDByPath(t *testing.T, store *Store, path string) int64 {
	t.Helper()

	var id int64
	if err := store.DB().QueryRow(
		`SELECT id FROM audit_sources WHERE file_path = ?`,
		path,
	).Scan(&id); err != nil {
		t.Fatalf("query source id for %q: %v", path, err)
	}
	return id
}
