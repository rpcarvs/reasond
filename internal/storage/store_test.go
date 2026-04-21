package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/testutil"

	_ "modernc.org/sqlite"
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

	var legacyFindingsCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_findings';`).Scan(&legacyFindingsCount); err != nil {
		t.Fatalf("query audit_findings table count: %v", err)
	}
	if legacyFindingsCount != 0 {
		t.Fatalf("expected no legacy audit_findings table in fresh schema, got %d", legacyFindingsCount)
	}
	for _, table := range []string{
		"audit_runs_codex",
		"audit_runs_claude",
		"audit_findings_codex",
		"audit_findings_claude",
	} {
		var name string
		if err := store.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&name); err != nil {
			t.Fatalf("query %s table: %v", table, err)
		}
		if name != table {
			t.Fatalf("expected %s table, got %q", table, name)
		}
	}

	expectedPath := filepath.Join(root, ".reasond", "audits_reports.db")
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

func TestSyncArchivedAuditsInsertsAndDetectsImmutableConflicts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(logDir, "first.md"), []byte("# one\n"), 0o644); err != nil {
		t.Fatalf("write first log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "second.md"), []byte("# two\n"), 0o644); err != nil {
		t.Fatalf("write second log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, ".control"), []byte("ignore\n"), 0o644); err != nil {
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

	result, err := store.SyncArchivedAudits()
	if err != nil {
		t.Fatalf("sync archived audits: %v", err)
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

	result, err = store.SyncArchivedAudits()
	if err != nil {
		t.Fatalf("sync known archived audits: %v", err)
	}
	if !slices.Equal(result.Known, expectedInserted) {
		t.Fatalf("expected known %v, got %v", expectedInserted, result.Known)
	}

	if err := os.WriteFile(filepath.Join(logDir, "second.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatalf("mutate audit file: %v", err)
	}

	result, err = store.SyncArchivedAudits()
	if err != nil {
		t.Fatalf("sync mutated archived audits: %v", err)
	}
	if !slices.Equal(result.ImmutableConflicts, []string{"second.md"}) {
		t.Fatalf("expected immutable conflict for second.md, got %v", result.ImmutableConflicts)
	}
}

func TestSyncArchivedAuditsLoadsFixtureFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixtureLogs := testutil.CopyFixtureTree(t, "audits")
	if err := os.MkdirAll(filepath.Join(root, appRuntime.DirectoryName), 0o755); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}
	if err := os.Rename(fixtureLogs, appRuntime.ArchivePath(root)); err != nil {
		t.Fatalf("move fixture logs into repo: %v", err)
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

	result, err := store.SyncArchivedAudits()
	if err != nil {
		t.Fatalf("sync fixture archived audits: %v", err)
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
	logDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(logDir, "zero.md"), []byte("# zero\n"), 0o644); err != nil {
		t.Fatalf("write zero audit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "multi.md"), []byte("# multi\n"), 0o644); err != nil {
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

	if _, err := store.SyncArchivedAudits(); err != nil {
		t.Fatalf("sync archived audits: %v", err)
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
		`SELECT COUNT(*) FROM audit_findings_codex WHERE source_id = ?`,
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
		`SELECT COUNT(*) FROM audit_findings_claude WHERE source_id = ?`,
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
	logDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	for _, name := range []string{"pending.md", "zero.md", "issue.md"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
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

	if _, err := store.SyncArchivedAudits(); err != nil {
		t.Fatalf("sync archived audits: %v", err)
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

	board, err := store.ListBoardFindingsForFilter(BoardFilter{
		Provider:   JudgeProviderClaude,
		IncludeAll: true,
	})
	if err != nil {
		t.Fatalf("list board findings: %v", err)
	}
	if len(board) != 1 || board[0].Title != "Critical mismatch" {
		t.Fatalf("unexpected board findings: %+v", board)
	}

	detail, err := store.GetFindingDetailForProvider(JudgeProviderClaude, board[0].ID)
	if err != nil {
		t.Fatalf("get finding detail: %v", err)
	}
	if detail.SourcePath != "issue.md" || detail.JudgeProvider != "claude" || detail.JudgeModel != "claude-haiku-4-5" {
		t.Fatalf("unexpected finding detail: %+v", detail)
	}
}

func TestMostRecentProviderReturnsLatestRunPartition(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	for _, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
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

	if _, err := store.SyncArchivedAudits(); err != nil {
		t.Fatalf("sync archived audits: %v", err)
	}

	provider, ok, err := store.MostRecentProvider()
	if err != nil {
		t.Fatalf("most recent provider on empty runs: %v", err)
	}
	if ok || provider != "" {
		t.Fatalf("expected no provider before runs, got ok=%t provider=%q", ok, provider)
	}

	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "one.md"),
		JudgeProvider: JudgeProviderCodex,
		JudgeModel:    "gpt-5.4-mini",
	}); err != nil {
		t.Fatalf("persist codex run: %v", err)
	}
	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "two.md"),
		JudgeProvider: JudgeProviderClaude,
		JudgeModel:    "claude-haiku-4-5",
	}); err != nil {
		t.Fatalf("persist claude run: %v", err)
	}

	// RFC3339Nano text is not lexicographically stable across optional fractions.
	// This update makes the earlier run sort later as plain text, so the test
	// verifies that provider recency uses parsed timestamps instead.
	if _, err := store.DB().Exec(
		`UPDATE audit_runs_codex SET created_at = '2026-04-21T12:00:00Z'`,
	); err != nil {
		t.Fatalf("update codex created_at: %v", err)
	}
	if _, err := store.DB().Exec(
		`UPDATE audit_runs_claude SET created_at = '2026-04-21T12:00:00.000000001Z'`,
	); err != nil {
		t.Fatalf("update claude created_at: %v", err)
	}

	provider, ok, err = store.MostRecentProvider()
	if err != nil {
		t.Fatalf("most recent provider: %v", err)
	}
	if !ok {
		t.Fatalf("expected a provider result")
	}
	if provider != JudgeProviderClaude {
		t.Fatalf("expected claude as latest provider, got %q", provider)
	}
}

func TestPreferredBoardProviderSkipsNewestProviderWithoutVisibleFindings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	for _, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
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

	if _, err := store.SyncArchivedAudits(); err != nil {
		t.Fatalf("sync archived audits: %v", err)
	}

	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "one.md"),
		JudgeProvider: JudgeProviderClaude,
		JudgeModel:    "claude-haiku-4-5",
		Findings: []FindingInput{{
			Title: "Claude issue",
			Issue: "Issue",
			Why:   "Why",
			How:   "How",
			Score: 0.6,
		}},
	}); err != nil {
		t.Fatalf("persist claude finding: %v", err)
	}
	if err := store.PersistProcessedResult(PersistResultInput{
		SourceID:      sourceIDByPath(t, store, "two.md"),
		JudgeProvider: JudgeProviderCodex,
		JudgeModel:    "gpt-5.4-mini",
	}); err != nil {
		t.Fatalf("persist codex zero-finding run: %v", err)
	}

	provider, ok, err := store.PreferredBoardProvider()
	if err != nil {
		t.Fatalf("preferred board provider: %v", err)
	}
	if !ok {
		t.Fatalf("expected a preferred board provider")
	}
	if provider != JudgeProviderClaude {
		t.Fatalf("expected claude as preferred board provider, got %q", provider)
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

func TestOpenMigratesLegacyFindingsIntoProviderRunTables(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runtimeDir := filepath.Join(root, ".reasond")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}

	dbPath := filepath.Join(runtimeDir, "audits_reports.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := legacyDB.Exec(`PRAGMA user_version = 2;`); err != nil {
		t.Fatalf("set legacy schema version: %v", err)
	}
	legacySchema := []string{
		`CREATE TABLE audit_sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL UNIQUE,
			file_name TEXT NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			processed INTEGER NOT NULL DEFAULT 0 CHECK (processed IN (0, 1)),
			detected_at TEXT NOT NULL,
			processed_at TEXT,
			judge_provider TEXT,
			judge_model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE audit_findings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			issue TEXT NOT NULL,
			why_text TEXT NOT NULL,
			how_text TEXT NOT NULL,
			score REAL NOT NULL,
			judge_provider TEXT NOT NULL,
			judge_model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, statement := range legacySchema {
		if _, err := legacyDB.Exec(statement); err != nil {
			t.Fatalf("apply legacy schema: %v", err)
		}
	}

	if _, err := legacyDB.Exec(
		`INSERT INTO audit_sources (id, file_path, file_name, content_hash, size_bytes, processed, detected_at, processed_at, judge_provider, judge_model, created_at, updated_at)
		VALUES (1, 'legacy.md', 'legacy.md', 'hash', 42, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'claude', 'claude-haiku-4-5', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert legacy source: %v", err)
	}

	for _, title := range []string{"Legacy one", "Legacy two"} {
		if _, err := legacyDB.Exec(
			`INSERT INTO audit_findings (source_id, title, issue, why_text, how_text, score, judge_provider, judge_model, created_at, updated_at)
			VALUES (1, ?, 'issue', 'why', 'how', 0.7, 'claude', 'claude-haiku-4-5', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			title,
		); err != nil {
			t.Fatalf("insert legacy finding: %v", err)
		}
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	var version int
	if err := store.DB().QueryRow(`PRAGMA user_version;`).Scan(&version); err != nil {
		t.Fatalf("query version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("expected schema version %d, got %d", schemaVersion, version)
	}

	var runCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM audit_runs_claude;`).Scan(&runCount); err != nil {
		t.Fatalf("count migrated runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected one migrated run, got %d", runCount)
	}

	board, err := store.ListBoardFindingsForFilter(BoardFilter{
		Provider:   JudgeProviderClaude,
		IncludeAll: false,
	})
	if err != nil {
		t.Fatalf("list migrated board findings: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("expected two migrated findings in latest view, got %+v", board)
	}

	var legacyFindingsCountAfter int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_findings';`).Scan(&legacyFindingsCountAfter); err != nil {
		t.Fatalf("query cleaned legacy findings table count: %v", err)
	}
	if legacyFindingsCountAfter != 0 {
		t.Fatalf("expected migrated database to remove legacy audit_findings table, got %d", legacyFindingsCountAfter)
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
