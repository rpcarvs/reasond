package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	appRuntime "rdit/internal/runtime"
)

const schemaVersion = 2

const (
	maxOpenAttempts = 8
	baseOpenBackoff = 20 * time.Millisecond

	maxWriteAttempts = 8
	baseWriteBackoff = 20 * time.Millisecond
)

// Store owns the runtime SQLite handle and all persistence operations for rdit.
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

// SyncResult reports which reasoning audit files were inserted, already known, or changed unexpectedly.
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
	Findings      []FindingInput
}

// SourceRow represents one immutable markdown file discovered under reasoning_audits.
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
	ID    int64
	Title string
	Score float64
}

// FindingDetail is the expanded detail payload shown when a board card is opened.
type FindingDetail struct {
	ID            int64
	SourceID      int64
	SourcePath    string
	Title         string
	Issue         string
	Why           string
	How           string
	Score         float64
	JudgeProvider string
	JudgeModel    string
	ProcessedAt   sql.NullString
}

// SyncReasoningAudits appends new audit markdown files into the source table.
func (s *Store) SyncReasoningAudits() (SyncResult, error) {
	entries, err := collectAuditFiles(filepath.Join(s.rootDir, "reasoning_audits"))
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

// PersistProcessedResult stores judge findings and marks the source row processed.
func (s *Store) PersistProcessedResult(input PersistResultInput) error {
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
		if processed == 1 {
			return fmt.Errorf("source row %d is already processed", input.SourceID)
		}

		for _, finding := range input.Findings {
			if finding.Score < 0.0 || finding.Score > 1.0 {
				return fmt.Errorf("finding score must be between 0.0 and 1.0")
			}
			_, err := tx.Exec(
				`INSERT INTO audit_findings (
					source_id,
					title,
					issue,
					why_text,
					how_text,
					score,
					judge_provider,
					judge_model,
					created_at,
					updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				input.SourceID,
				finding.Title,
				finding.Issue,
				finding.Why,
				finding.How,
				finding.Score,
				input.JudgeProvider,
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
				processed_at = ?,
				judge_provider = ?,
				judge_model = ?,
				updated_at = ?
			WHERE id = ?`,
			now,
			input.JudgeProvider,
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

// ListBoardFindings returns the card data used by the board view.
func (s *Store) ListBoardFindings() ([]FindingSummary, error) {
	rows, err := s.db.Query(
		`SELECT id, title, score
		FROM audit_findings
		ORDER BY score DESC, id ASC`,
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
		if err := rows.Scan(&finding.ID, &finding.Title, &finding.Score); err != nil {
			return nil, fmt.Errorf("scan board finding: %w", err)
		}
		findings = append(findings, finding)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board findings: %w", err)
	}

	return findings, nil
}

// GetFindingDetail loads the full detail payload for one board item.
func (s *Store) GetFindingDetail(findingID int64) (FindingDetail, error) {
	var detail FindingDetail
	err := s.db.QueryRow(
		`SELECT
			f.id,
			f.source_id,
			s.file_path,
			f.title,
			f.issue,
			f.why_text,
			f.how_text,
			f.score,
			f.judge_provider,
			f.judge_model,
			s.processed_at
		FROM audit_findings f
		INNER JOIN audit_sources s ON s.id = f.source_id
		WHERE f.id = ?`,
		findingID,
	).Scan(
		&detail.ID,
		&detail.SourceID,
		&detail.SourcePath,
		&detail.Title,
		&detail.Issue,
		&detail.Why,
		&detail.How,
		&detail.Score,
		&detail.JudgeProvider,
		&detail.JudgeModel,
		&detail.ProcessedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return FindingDetail{}, fmt.Errorf("finding %d not found", findingID)
		}
		return FindingDetail{}, fmt.Errorf("load finding %d: %w", findingID, err)
	}

	return detail, nil
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
		`CREATE TABLE IF NOT EXISTS audit_findings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			issue TEXT NOT NULL,
			why_text TEXT NOT NULL,
			how_text TEXT NOT NULL,
			score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
			judge_provider TEXT NOT NULL,
			judge_model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES audit_sources(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_findings_source_id ON audit_findings(source_id, id);`,
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

type auditFile struct {
	RelativePath string
	FileName     string
	SizeBytes    int64
	ContentHash  string
}

func collectAuditFiles(auditDir string) ([]auditFile, error) {
	info, err := os.Stat(auditDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat reasoning_audits: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q exists but is not a directory", auditDir)
	}

	var files []auditFile
	err = filepath.WalkDir(auditDir, func(path string, entry os.DirEntry, walkErr error) error {
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

		relativePath, err := filepath.Rel(auditDir, path)
		if err != nil {
			return fmt.Errorf("derive relative path for %q: %w", path, err)
		}

		files = append(files, auditFile{
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

	slices.SortFunc(files, func(a, b auditFile) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	return files, nil
}

func syncAuditEntry(tx *sql.Tx, entry auditFile, now string) (string, error) {
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
