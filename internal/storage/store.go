package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
)

const schemaVersion = 4

const (
	JudgeProviderCodex  = "codex"
	JudgeProviderClaude = "claude"
)

const (
	maxOpenAttempts = 8
	baseOpenBackoff = 20 * time.Millisecond

	maxWriteAttempts = 8
	baseWriteBackoff = 20 * time.Millisecond
)

// Store owns the runtime SQLite handle and all persistence operations for reasond.
type Store struct {
	db      *sql.DB
	path    string
	rootDir string
}

// Open creates or opens the runtime SQLite database and ensures the schema exists.
func Open(rootDir string) (*Store, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}

	dbPath := appRuntime.DatabasePath(rootDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}

	var lastRetryErr error
	for attempt := 0; attempt < maxOpenAttempts; attempt++ {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return nil, fmt.Errorf("open sqlite database: %w", err)
		}

		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)

		if err := db.Ping(); err != nil {
			_ = db.Close()
			wrapped := fmt.Errorf("ping sqlite database: %w", err)
			if isRetryableSQLError(wrapped) {
				lastRetryErr = wrapped
				time.Sleep(openBackoff(attempt))
				continue
			}
			return nil, wrapped
		}

		store := &Store{
			db:      db,
			path:    dbPath,
			rootDir: rootDir,
		}

		if err := store.bootstrap(); err != nil {
			_ = db.Close()
			if isRetryableSQLError(err) {
				lastRetryErr = err
				time.Sleep(openBackoff(attempt))
				continue
			}
			return nil, err
		}

		return store, nil
	}

	if lastRetryErr != nil {
		return nil, fmt.Errorf("open sqlite database failed after %d attempts: %w", maxOpenAttempts, lastRetryErr)
	}

	return nil, fmt.Errorf("open sqlite database failed after %d attempts", maxOpenAttempts)
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the SQLite database location on disk.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// RootDir returns the repository root associated with the store.
func (s *Store) RootDir() string {
	if s == nil {
		return ""
	}
	return s.rootDir
}

// DB exposes the underlying sql.DB for repository operations.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// SyncResult reports which archived audit files were inserted, already known, or changed unexpectedly.
type SyncResult struct {
	Inserted           []string
	Known              []string
	ImmutableConflicts []string
}

// FindingInput is the normalized finding payload persisted for one processed source.
type FindingInput struct {
	Title string
	Issue string
	Why   string
	How   string
	Score float64
}

// PersistResultInput describes one processed source row and its zero-or-many findings.
type PersistResultInput struct {
	SourceID      int64
	JudgeProvider string
	JudgeModel    string
	BatchID       int64
	Findings      []FindingInput
}

// SourceRow represents one immutable markdown file discovered under the archive directory.
type SourceRow struct {
	ID            int64
	FilePath      string
	FileName      string
	ContentHash   string
	SizeBytes     int64
	Processed     bool
	JudgeProvider string
	JudgeModel    string
	ProcessedAt   sql.NullString
}

// FindingSummary is the compact card payload shown in the board view.
type FindingSummary struct {
	ID         int64
	Provider   string
	Title      string
	Score      float64
	SourcePath string
	JudgeModel string
	RunID      int64
}

// FindingDetail is the expanded detail payload shown when a board card is opened.
type FindingDetail struct {
	ID             int64
	RunID          int64
	SourceID       int64
	SourcePath     string
	SourceFullPath string
	Title          string
	Issue          string
	Why            string
	How            string
	Score          float64
	JudgeProvider  string
	JudgeModel     string
	ProcessedAt    sql.NullString
}

// BoardFilter defines provider and optional visibility constraints for board queries.
type BoardFilter struct {
	Provider   string
	FilePath   string
	IncludeAll bool
}

// JudgeBatchInput describes one non-interactive or TUI judge invocation.
type JudgeBatchInput struct {
	JudgeProvider string
	JudgeModel    string
	Mode          string
	Total         int
}

// FindingPublicID is the provider-qualified identifier exposed by agent CLI output.
type FindingPublicID struct {
	Provider string
	ID       int64
}

// AgentFindingSummary is the compact finding row exposed to non-interactive CLI consumers.
type AgentFindingSummary struct {
	PublicID    string
	ID          int64
	Provider    string
	Title       string
	Score       float64
	SourcePath  string
	JudgeModel  string
	RunID       int64
	BatchID     int64
	ProcessedAt sql.NullString
}

type providerRecency struct {
	Provider string
	Time     time.Time
}

// SyncArchivedAudits appends new archived audit markdown files into the source table.
func (s *Store) SyncArchivedAudits() (SyncResult, error) {
	entries, err := collectLogFiles(appRuntime.ArchivePath(s.rootDir))
	if err != nil {
		return SyncResult{}, err
	}

	result := SyncResult{}
	now := utcNow()

	err = s.withWriteTx(func(tx *sql.Tx) error {
		for _, entry := range entries {
			status, err := syncAuditEntry(tx, entry, now)
			if err != nil {
				return err
			}

			switch status {
			case "inserted":
				result.Inserted = append(result.Inserted, entry.RelativePath)
			case "known":
				result.Known = append(result.Known, entry.RelativePath)
			case "conflict":
				result.ImmutableConflicts = append(result.ImmutableConflicts, entry.RelativePath)
			default:
				return fmt.Errorf("unsupported sync status %q", status)
			}
		}

		return nil
	})
	if err != nil {
		return SyncResult{}, err
	}

	slices.Sort(result.Inserted)
	slices.Sort(result.Known)
	slices.Sort(result.ImmutableConflicts)
	return result, nil
}

// StartJudgeBatch records one judge invocation and returns its batch id.
func (s *Store) StartJudgeBatch(input JudgeBatchInput) (int64, error) {
	_, _, provider, err := providerTables(input.JudgeProvider)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(input.JudgeModel) == "" {
		return 0, fmt.Errorf("judge model is required")
	}
	if strings.TrimSpace(input.Mode) == "" {
		return 0, fmt.Errorf("judge batch mode is required")
	}
	if input.Total < 0 {
		return 0, fmt.Errorf("judge batch total cannot be negative")
	}

	now := utcNow()
	var batchID int64
	err = s.withWriteTx(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`INSERT INTO audit_judge_batches (
				judge_provider,
				judge_model,
				mode,
				total,
				succeeded,
				failed,
				started_at,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, 0, 0, ?, ?, ?)`,
			provider,
			input.JudgeModel,
			input.Mode,
			input.Total,
			now,
			now,
			now,
		)
		if err != nil {
			return fmt.Errorf("insert judge batch: %w", err)
		}
		batchID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("load judge batch id: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return batchID, nil
}

// FinishJudgeBatch records the terminal counts for one judge invocation.
func (s *Store) FinishJudgeBatch(batchID int64, succeeded int, failed int) error {
	if batchID <= 0 {
		return fmt.Errorf("judge batch id is required")
	}
	if succeeded < 0 || failed < 0 {
		return fmt.Errorf("judge batch counts cannot be negative")
	}

	now := utcNow()
	return s.withWriteTx(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`UPDATE audit_judge_batches
			SET succeeded = ?,
				failed = ?,
				completed_at = ?,
				updated_at = ?
			WHERE id = ?`,
			succeeded,
			failed,
			now,
			now,
			batchID,
		)
		if err != nil {
			return fmt.Errorf("finish judge batch %d: %w", batchID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("load judge batch update count: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("judge batch %d not found", batchID)
		}
		return nil
	})
}

// PersistProcessedResult stores judge findings and marks the source row processed.
func (s *Store) PersistProcessedResult(input PersistResultInput) error {
	runTable, findingsTable, provider, err := providerTables(input.JudgeProvider)
	if err != nil {
		return err
	}

	now := utcNow()
	return s.withWriteTx(func(tx *sql.Tx) error {
		var processed int
		err := tx.QueryRow(
			`SELECT processed FROM audit_sources WHERE id = ?`,
			input.SourceID,
		).Scan(&processed)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("source row %d not found", input.SourceID)
			}
			return fmt.Errorf("load source row %d: %w", input.SourceID, err)
		}

		runResult, err := tx.Exec(
			fmt.Sprintf(`INSERT INTO %s (
				source_id,
				judge_model,
				batch_id,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, ?)`, runTable),
			input.SourceID,
			input.JudgeModel,
			nullableBatchID(input.BatchID),
			now,
			now,
		)
		if err != nil {
			return fmt.Errorf("insert run for source row %d: %w", input.SourceID, err)
		}
		runID, err := runResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("load inserted run id for source row %d: %w", input.SourceID, err)
		}

		for _, finding := range input.Findings {
			if finding.Score < 0.0 || finding.Score > 1.0 {
				return fmt.Errorf("finding score must be between 0.0 and 1.0")
			}
			_, err := tx.Exec(
				fmt.Sprintf(`INSERT INTO %s (
					run_id,
					source_id,
					title,
					issue,
					why_text,
					how_text,
					score,
					judge_model,
					created_at,
					updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, findingsTable),
				runID,
				input.SourceID,
				finding.Title,
				finding.Issue,
				finding.Why,
				finding.How,
				finding.Score,
				input.JudgeModel,
				now,
				now,
			)
			if err != nil {
				return fmt.Errorf("insert finding for source row %d: %w", input.SourceID, err)
			}
		}

		_, err = tx.Exec(
			`UPDATE audit_sources
			SET processed = 1,
				processed_at = CASE WHEN processed_at IS NULL THEN ? ELSE processed_at END,
				judge_provider = ?,
				judge_model = ?,
				updated_at = ?
			WHERE id = ?`,
			now,
			provider,
			input.JudgeModel,
			now,
			input.SourceID,
		)
		if err != nil {
			return fmt.Errorf("mark source row %d processed: %w", input.SourceID, err)
		}

		return nil
	})
}

// CountUnprocessedSources returns how many audit source rows are still pending processing.
func (s *Store) CountUnprocessedSources() (int, error) {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM audit_sources WHERE processed = 0`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unprocessed sources: %w", err)
	}
	return count, nil
}

// ListUnprocessedSources returns source rows waiting to be judged.
func (s *Store) ListUnprocessedSources() ([]SourceRow, error) {
	rows, err := s.db.Query(
		`SELECT id, file_path, file_name, content_hash, size_bytes, processed, COALESCE(judge_provider, ''), COALESCE(judge_model, ''), processed_at
		FROM audit_sources
		WHERE processed = 0
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list unprocessed sources: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var sources []SourceRow
	for rows.Next() {
		var source SourceRow
		var processed int
		if err := rows.Scan(
			&source.ID,
			&source.FilePath,
			&source.FileName,
			&source.ContentHash,
			&source.SizeBytes,
			&processed,
			&source.JudgeProvider,
			&source.JudgeModel,
			&source.ProcessedAt,
		); err != nil {
			return nil, fmt.Errorf("scan unprocessed source row: %w", err)
		}
		source.Processed = processed == 1
		sources = append(sources, source)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unprocessed source rows: %w", err)
	}

	return sources, nil
}

// ListAllSources returns every known audit source row in insertion order.
func (s *Store) ListAllSources() ([]SourceRow, error) {
	rows, err := s.db.Query(
		`SELECT id, file_path, file_name, content_hash, size_bytes, processed, COALESCE(judge_provider, ''), COALESCE(judge_model, ''), processed_at
		FROM audit_sources
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all sources: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var sources []SourceRow
	for rows.Next() {
		var source SourceRow
		var processed int
		if err := rows.Scan(
			&source.ID,
			&source.FilePath,
			&source.FileName,
			&source.ContentHash,
			&source.SizeBytes,
			&processed,
			&source.JudgeProvider,
			&source.JudgeModel,
			&source.ProcessedAt,
		); err != nil {
			return nil, fmt.Errorf("scan source row: %w", err)
		}
		source.Processed = processed == 1
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source rows: %w", err)
	}
	return sources, nil
}

// ListBoardFindingsForFilter returns board rows constrained by provider and optional file/latest filters.
func (s *Store) ListBoardFindingsForFilter(filter BoardFilter) ([]FindingSummary, error) {
	runTable, findingsTable, normalizedProvider, err := providerTables(filter.Provider)
	if err != nil {
		return nil, err
	}

	where := ""
	args := []any{}
	if strings.TrimSpace(filter.FilePath) != "" {
		where = " WHERE s.file_path = ?"
		args = append(args, strings.TrimSpace(filter.FilePath))
	}

	query := ""
	if filter.IncludeAll {
		query = fmt.Sprintf(`SELECT f.id, f.title, f.score, s.file_path, f.judge_model, f.run_id
		FROM %s f
		INNER JOIN audit_sources s ON s.id = f.source_id%s
		ORDER BY f.score DESC, f.id ASC`, findingsTable, where)
	} else {
		query = fmt.Sprintf(`WITH latest_runs AS (
			SELECT source_id, MAX(id) AS run_id
			FROM %s
			GROUP BY source_id
		)
		SELECT f.id, f.title, f.score, s.file_path, f.judge_model, f.run_id
		FROM latest_runs lr
		INNER JOIN %s f ON f.run_id = lr.run_id
		INNER JOIN audit_sources s ON s.id = f.source_id%s
		ORDER BY f.score DESC, f.id ASC`, runTable, findingsTable, where)
	}

	rows, err := s.db.Query(
		query,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list board findings: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var findings []FindingSummary
	for rows.Next() {
		var finding FindingSummary
		if err := rows.Scan(
			&finding.ID,
			&finding.Title,
			&finding.Score,
			&finding.SourcePath,
			&finding.JudgeModel,
			&finding.RunID,
		); err != nil {
			return nil, fmt.Errorf("scan board finding: %w", err)
		}
		finding.Provider = normalizedProvider
		findings = append(findings, finding)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board findings: %w", err)
	}

	return findings, nil
}

// GetFindingDetailForProvider loads one board item from the selected provider partition.
func (s *Store) GetFindingDetailForProvider(provider string, findingID int64) (FindingDetail, error) {
	_, findingsTable, normalizedProvider, err := providerTables(provider)
	if err != nil {
		return FindingDetail{}, err
	}

	var detail FindingDetail
	err = s.db.QueryRow(
		fmt.Sprintf(`SELECT
			f.id,
			f.run_id,
			f.source_id,
			s.file_path,
			f.title,
			f.issue,
			f.why_text,
			f.how_text,
			f.score,
			f.judge_model,
			s.processed_at
		FROM %s f
		INNER JOIN audit_sources s ON s.id = f.source_id
		WHERE f.id = ?`, findingsTable),
		findingID,
	).Scan(
		&detail.ID,
		&detail.RunID,
		&detail.SourceID,
		&detail.SourcePath,
		&detail.Title,
		&detail.Issue,
		&detail.Why,
		&detail.How,
		&detail.Score,
		&detail.JudgeModel,
		&detail.ProcessedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return FindingDetail{}, fmt.Errorf("finding %d not found", findingID)
		}
		return FindingDetail{}, fmt.Errorf("load finding %d: %w", findingID, err)
	}
	detail.JudgeProvider = normalizedProvider
	detail.SourceFullPath = filepath.Join(appRuntime.ArchivePath(s.rootDir), filepath.FromSlash(detail.SourcePath))

	return detail, nil
}

// ListLatestBatchFindings returns compact rows from the most recent completed judge batch.
func (s *Store) ListLatestBatchFindings() ([]AgentFindingSummary, error) {
	var batchID int64
	var provider string
	err := s.db.QueryRow(
		`SELECT id, judge_provider
		FROM audit_judge_batches
		WHERE completed_at IS NOT NULL
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&batchID, &provider)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load latest judge batch: %w", err)
	}

	return s.listAgentFindingsForProvider(provider, "WHERE r.batch_id = ?", []any{batchID})
}

// ListAllAgentFindings returns compact rows for all persisted judge findings.
func (s *Store) ListAllAgentFindings() ([]AgentFindingSummary, error) {
	var all []AgentFindingSummary
	for _, provider := range []string{JudgeProviderCodex, JudgeProviderClaude} {
		findings, err := s.listAgentFindingsForProvider(provider, "", nil)
		if err != nil {
			return nil, err
		}
		all = append(all, findings...)
	}

	slices.SortFunc(all, func(a, b AgentFindingSummary) int {
		if a.RunID != b.RunID {
			if a.RunID > b.RunID {
				return -1
			}
			return 1
		}
		return strings.Compare(a.PublicID, b.PublicID)
	})
	return all, nil
}

// GetAgentFindingDetail loads one finding detail from a provider-qualified public id.
func (s *Store) GetAgentFindingDetail(publicID FindingPublicID) (FindingDetail, error) {
	if publicID.ID <= 0 {
		return FindingDetail{}, fmt.Errorf("finding id is required")
	}
	return s.GetFindingDetailForProvider(publicID.Provider, publicID.ID)
}

// ListResultFiles returns distinct source file paths that were judged for the selected provider.
func (s *Store) ListResultFiles(provider string) ([]string, error) {
	runTable, _, _, err := providerTables(provider)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT DISTINCT s.file_path
		FROM %s r
		INNER JOIN audit_sources s ON s.id = r.source_id
		ORDER BY s.file_path ASC`, runTable),
	)
	if err != nil {
		return nil, fmt.Errorf("list result files: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var files []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan result file path: %w", err)
		}
		files = append(files, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result file paths: %w", err)
	}

	return files, nil
}

func (s *Store) listAgentFindingsForProvider(provider string, where string, args []any) ([]AgentFindingSummary, error) {
	runTable, findingsTable, normalizedProvider, err := providerTables(provider)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`SELECT
			f.id,
			f.title,
			f.score,
			s.file_path,
			f.judge_model,
			f.run_id,
			COALESCE(r.batch_id, 0),
			s.processed_at
		FROM %s f
		INNER JOIN %s r ON r.id = f.run_id
		INNER JOIN audit_sources s ON s.id = f.source_id
		%s
		ORDER BY f.score DESC, f.id ASC`, findingsTable, runTable, where)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent findings for %q: %w", normalizedProvider, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var findings []AgentFindingSummary
	for rows.Next() {
		var finding AgentFindingSummary
		if err := rows.Scan(
			&finding.ID,
			&finding.Title,
			&finding.Score,
			&finding.SourcePath,
			&finding.JudgeModel,
			&finding.RunID,
			&finding.BatchID,
			&finding.ProcessedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent finding: %w", err)
		}
		finding.Provider = normalizedProvider
		finding.PublicID = FormatFindingPublicID(normalizedProvider, finding.ID)
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent findings: %w", err)
	}

	return findings, nil
}

// MostRecentProvider returns the provider with the most recent persisted run.
// Recency is determined by the latest run in each provider partition and then
// compared in Go using parsed timestamps.
func (s *Store) MostRecentProvider() (string, bool, error) {
	return s.mostRecentProvider(false)
}

// PreferredBoardProvider returns the provider that should be shown first on the
// latest-only board view. It prefers the most recent provider that currently
// has visible findings on the board, and falls back to the most recent run when
// neither provider has findings.
func (s *Store) PreferredBoardProvider() (string, bool, error) {
	provider, ok, err := s.mostRecentProvider(true)
	if err != nil {
		return "", false, err
	}
	if ok {
		return provider, true, nil
	}
	return s.MostRecentProvider()
}

func (s *Store) bootstrap() error {
	if err := applyConnectionPragmas(s.db); err != nil {
		return err
	}

	version, err := currentVersion(s.db)
	if err != nil {
		return err
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS audit_sources (
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
		`CREATE INDEX IF NOT EXISTS idx_audit_sources_processed ON audit_sources(processed, id);`,
		`CREATE TABLE IF NOT EXISTS audit_judge_batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			judge_provider TEXT NOT NULL,
			judge_model TEXT NOT NULL,
			mode TEXT NOT NULL,
			total INTEGER NOT NULL DEFAULT 0,
			succeeded INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			completed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_judge_batches_completed ON audit_judge_batches(completed_at, id);`,
		`CREATE TABLE IF NOT EXISTS audit_runs_codex (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			judge_model TEXT NOT NULL,
			batch_id INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES audit_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(batch_id) REFERENCES audit_judge_batches(id) ON DELETE SET NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_runs_codex_batch_id ON audit_runs_codex(batch_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_runs_codex_source_id ON audit_runs_codex(source_id, id);`,
		`CREATE TABLE IF NOT EXISTS audit_runs_claude (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			judge_model TEXT NOT NULL,
			batch_id INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES audit_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(batch_id) REFERENCES audit_judge_batches(id) ON DELETE SET NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_runs_claude_batch_id ON audit_runs_claude(batch_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_runs_claude_source_id ON audit_runs_claude(source_id, id);`,
		`CREATE TABLE IF NOT EXISTS audit_findings_codex (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL,
			source_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			issue TEXT NOT NULL,
			why_text TEXT NOT NULL,
			how_text TEXT NOT NULL,
			score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
			judge_model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES audit_runs_codex(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES audit_sources(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_findings_codex_run_id ON audit_findings_codex(run_id, id);`,
		`CREATE TABLE IF NOT EXISTS audit_findings_claude (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL,
			source_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			issue TEXT NOT NULL,
			why_text TEXT NOT NULL,
			how_text TEXT NOT NULL,
			score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
			judge_model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES audit_runs_claude(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES audit_sources(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_findings_claude_run_id ON audit_findings_claude(run_id, id);`,
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin bootstrap transaction: %w", err)
	}

	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}

	if version == 1 {
		migrations := []string{
			`ALTER TABLE audit_sources ADD COLUMN judge_provider TEXT;`,
			`ALTER TABLE audit_sources ADD COLUMN judge_model TEXT;`,
		}

		for _, statement := range migrations {
			if _, err := tx.Exec(statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply schema migration: %w", err)
			}
		}
	}

	if version < 3 {
		if err := migrateLegacyFindings(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate legacy findings: %w", err)
		}
	}
	if version > 0 && version < 4 {
		if err := migrateBatchTracking(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate batch tracking: %w", err)
		}
	}
	if err := dropLegacyFindingsArtifacts(tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("drop legacy findings artifacts: %w", err)
	}

	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("set schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap transaction: %w", err)
	}

	version, err = currentVersion(s.db)
	if err != nil {
		return err
	}
	if version != schemaVersion {
		return fmt.Errorf("unexpected schema version %d", version)
	}

	return nil
}

func migrateLegacyFindings(tx *sql.Tx) error {
	exists, err := tableExists(tx, "audit_findings")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	rows, err := tx.Query(
		`SELECT
			source_id,
			title,
			issue,
			why_text,
			how_text,
			score,
			COALESCE(judge_provider, ''),
			COALESCE(judge_model, ''),
			COALESCE(created_at, ''),
			COALESCE(updated_at, '')
		FROM audit_findings
		ORDER BY id ASC`,
	)
	if err != nil {
		return fmt.Errorf("query legacy findings: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	type runKey struct {
		provider string
		sourceID int64
		model    string
		created  string
	}
	runByKey := map[runKey]int64{}

	for rows.Next() {
		var (
			sourceID      int64
			title         string
			issue         string
			whyText       string
			howText       string
			score         float64
			provider      string
			judgeModel    string
			createdAtText string
			updatedAtText string
		)
		if err := rows.Scan(
			&sourceID,
			&title,
			&issue,
			&whyText,
			&howText,
			&score,
			&provider,
			&judgeModel,
			&createdAtText,
			&updatedAtText,
		); err != nil {
			return fmt.Errorf("scan legacy finding: %w", err)
		}

		runTable, findingsTable, normalizedProvider, err := providerTables(provider)
		if err != nil {
			return err
		}
		if strings.TrimSpace(createdAtText) == "" {
			createdAtText = utcNow()
		}
		if strings.TrimSpace(updatedAtText) == "" {
			updatedAtText = createdAtText
		}
		if strings.TrimSpace(judgeModel) == "" {
			judgeModel = "unknown_migrated"
		}

		key := runKey{
			provider: normalizedProvider,
			sourceID: sourceID,
			model:    judgeModel,
			created:  createdAtText,
		}
		runID, ok := runByKey[key]
		if !ok {
			result, err := tx.Exec(
				fmt.Sprintf(`INSERT INTO %s (source_id, judge_model, created_at, updated_at)
				VALUES (?, ?, ?, ?)`, runTable),
				sourceID,
				judgeModel,
				createdAtText,
				updatedAtText,
			)
			if err != nil {
				return fmt.Errorf("insert migrated run: %w", err)
			}
			runID, err = result.LastInsertId()
			if err != nil {
				return fmt.Errorf("load migrated run id: %w", err)
			}
			runByKey[key] = runID
		}

		_, err = tx.Exec(
			fmt.Sprintf(`INSERT INTO %s (
				run_id,
				source_id,
				title,
				issue,
				why_text,
				how_text,
				score,
				judge_model,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, findingsTable),
			runID,
			sourceID,
			title,
			issue,
			whyText,
			howText,
			score,
			judgeModel,
			createdAtText,
			updatedAtText,
		)
		if err != nil {
			return fmt.Errorf("insert migrated finding: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy findings: %w", err)
	}

	return nil
}

func migrateBatchTracking(tx *sql.Tx) error {
	tables := []string{
		"audit_runs_codex",
		"audit_runs_claude",
	}

	for _, table := range tables {
		exists, err := columnExists(tx, table, "batch_id")
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN batch_id INTEGER;`, table)); err != nil {
			return fmt.Errorf("apply batch tracking migration: %w", err)
		}
	}
	return nil
}

func columnExists(tx *sql.Tx, tableName string, columnName string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf(`PRAGMA table_info(%s);`, tableName))
	if err != nil {
		return false, fmt.Errorf("load columns for %q: %w", tableName, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, fmt.Errorf("scan column for %q: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate columns for %q: %w", tableName, err)
	}
	return false, nil
}

func dropLegacyFindingsArtifacts(tx *sql.Tx) error {
	statements := []string{
		`DROP INDEX IF EXISTS idx_audit_findings_source_id;`,
		`DROP TABLE IF EXISTS audit_findings;`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply cleanup statement %q: %w", statement, err)
		}
	}
	return nil
}

func tableExists(tx *sql.Tx, tableName string) (bool, error) {
	var count int
	err := tx.QueryRow(
		`SELECT COUNT(1)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?`,
		tableName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check table existence for %q: %w", tableName, err)
	}
	return count > 0, nil
}

func providerTables(provider string) (runTable string, findingsTable string, normalized string, err error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", JudgeProviderCodex:
		return "audit_runs_codex", "audit_findings_codex", JudgeProviderCodex, nil
	case JudgeProviderClaude:
		return "audit_runs_claude", "audit_findings_claude", JudgeProviderClaude, nil
	default:
		return "", "", "", fmt.Errorf("unsupported judge provider %q", provider)
	}
}

// FormatFindingPublicID returns the provider-qualified id used by agent-facing CLI commands.
func FormatFindingPublicID(provider string, findingID int64) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(provider)), findingID)
}

// ParseFindingPublicID parses a provider-qualified finding id such as "codex:12".
func ParseFindingPublicID(value string) (FindingPublicID, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return FindingPublicID{}, fmt.Errorf("finding id must use provider:id format")
	}

	_, _, provider, err := providerTables(parts[0])
	if err != nil {
		return FindingPublicID{}, err
	}

	id, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || id <= 0 {
		return FindingPublicID{}, fmt.Errorf("finding id must be a positive integer")
	}

	return FindingPublicID{
		Provider: provider,
		ID:       id,
	}, nil
}

func (s *Store) mostRecentProvider(visibleOnly bool) (string, bool, error) {
	candidates := make([]providerRecency, 0, 2)
	for _, provider := range []string{JudgeProviderCodex, JudgeProviderClaude} {
		candidate, ok, err := s.providerRecency(provider, visibleOnly)
		if err != nil {
			return "", false, err
		}
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return "", false, nil
	}

	latest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Time.After(latest.Time) {
			latest = candidate
		}
	}
	return latest.Provider, true, nil
}

func (s *Store) providerRecency(provider string, visibleOnly bool) (providerRecency, bool, error) {
	runTable, findingsTable, normalizedProvider, err := providerTables(provider)
	if err != nil {
		return providerRecency{}, false, err
	}

	query := fmt.Sprintf(`SELECT created_at FROM %s ORDER BY id DESC LIMIT 1`, runTable)
	if visibleOnly {
		query = fmt.Sprintf(`WITH latest_runs AS (
			SELECT source_id, MAX(id) AS run_id
			FROM %s
			GROUP BY source_id
		)
		SELECT r.created_at
		FROM latest_runs lr
		INNER JOIN %s r ON r.id = lr.run_id
		INNER JOIN %s f ON f.run_id = lr.run_id
		GROUP BY r.id, r.created_at
		ORDER BY r.id DESC
		LIMIT 1`, runTable, runTable, findingsTable)
	}

	var createdAt string
	err = s.db.QueryRow(query).Scan(&createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return providerRecency{}, false, nil
		}
		return providerRecency{}, false, fmt.Errorf("load provider recency for %q: %w", normalizedProvider, err)
	}

	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return providerRecency{}, false, fmt.Errorf("parse provider recency for %q: %w", normalizedProvider, err)
	}

	return providerRecency{
		Provider: normalizedProvider,
		Time:     parsed,
	}, true, nil
}

func applyConnectionPragmas(db *sql.DB) error {
	statements := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA foreign_keys = ON;`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("apply connection pragma: %w", err)
		}
	}

	return nil
}

func currentVersion(db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version;`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func (s *Store) withWriteTx(fn func(*sql.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < maxWriteAttempts; attempt++ {
		tx, err := s.db.Begin()
		if err != nil {
			if isRetryableSQLError(err) {
				lastErr = err
				time.Sleep(writeBackoff(attempt))
				continue
			}
			return fmt.Errorf("begin write transaction: %w", err)
		}

		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			if isRetryableSQLError(err) {
				lastErr = err
				time.Sleep(writeBackoff(attempt))
				continue
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			if isRetryableSQLError(err) {
				lastErr = err
				time.Sleep(writeBackoff(attempt))
				continue
			}
			return fmt.Errorf("commit write transaction: %w", err)
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("sqlite write failed after %d attempts: %w", maxWriteAttempts, lastErr)
	}
	return fmt.Errorf("sqlite write failed after %d attempts", maxWriteAttempts)
}

type logFile struct {
	RelativePath string
	FileName     string
	SizeBytes    int64
	ContentHash  string
}

func collectLogFiles(logDir string) ([]logFile, error) {
	info, err := os.Stat(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat archived audits: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q exists but is not a directory", logDir)
	}

	var files []logFile
	err = filepath.WalkDir(logDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %q: %w", path, err)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}

		relativePath, err := filepath.Rel(logDir, path)
		if err != nil {
			return fmt.Errorf("derive relative path for %q: %w", path, err)
		}

		files = append(files, logFile{
			RelativePath: filepath.ToSlash(relativePath),
			FileName:     filepath.Base(path),
			SizeBytes:    info.Size(),
			ContentHash:  hashContent(content),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(files, func(a, b logFile) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	return files, nil
}

func syncAuditEntry(tx *sql.Tx, entry logFile, now string) (string, error) {
	var existingHash string
	err := tx.QueryRow(
		`SELECT content_hash FROM audit_sources WHERE file_path = ?`,
		entry.RelativePath,
	).Scan(&existingHash)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup source row for %q: %w", entry.RelativePath, err)
	}

	if err == sql.ErrNoRows {
		_, err := tx.Exec(
			`INSERT INTO audit_sources (
				file_path,
				file_name,
				content_hash,
				size_bytes,
				processed,
				detected_at,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
			entry.RelativePath,
			entry.FileName,
			entry.ContentHash,
			entry.SizeBytes,
			now,
			now,
			now,
		)
		if err != nil {
			return "", fmt.Errorf("insert source row for %q: %w", entry.RelativePath, err)
		}
		return "inserted", nil
	}

	if existingHash == entry.ContentHash {
		return "known", nil
	}

	return "conflict", nil
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func utcNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func nullableBatchID(batchID int64) any {
	if batchID <= 0 {
		return nil
	}
	return batchID
}

func isRetryableSQLError(err error) bool {
	if err == nil {
		return false
	}
	for current := err; current != nil; current = unwrap(current) {
		text := strings.ToLower(current.Error())
		if strings.Contains(text, "sqlite_busy") || strings.Contains(text, "database is locked") {
			return true
		}
	}
	return false
}

func openBackoff(attempt int) time.Duration {
	if attempt < 0 {
		return baseOpenBackoff
	}
	multiplier := 1 << attempt
	if multiplier > 16 {
		multiplier = 16
	}
	return time.Duration(multiplier) * baseOpenBackoff
}

func writeBackoff(attempt int) time.Duration {
	if attempt < 0 {
		return baseWriteBackoff
	}
	multiplier := 1 << attempt
	if multiplier > 16 {
		multiplier = 16
	}
	return time.Duration(multiplier) * baseWriteBackoff
}

func unwrap(err error) error {
	type unwrapper interface {
		Unwrap() error
	}
	w, ok := err.(unwrapper)
	if !ok {
		return nil
	}
	return w.Unwrap()
}
