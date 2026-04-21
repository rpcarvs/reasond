package processing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rpcarvs/reasond/internal/judge"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/storage"
)

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, _ string, _ string, auditMarkdown string) (judge.Response, error) {
	switch {
	case strings.Contains(auditMarkdown, "issue"):
		return judge.Response{
			Findings: []judge.Finding{
				{
					Title: "Issue found",
					Issue: "The reasoning failed.",
					Why:   "The reasoning ignored the prompt.",
					How:   "The prompt required validation.",
					Score: 0.7,
				},
			},
		}, nil
	case strings.Contains(auditMarkdown, "zero"):
		return judge.Response{Findings: nil}, nil
	case strings.Contains(auditMarkdown, "fail"):
		return judge.Response{}, fmt.Errorf("runner failure")
	default:
		return judge.Response{}, nil
	}
}

func TestProcessUnprocessedContinuesAfterPerFileFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	files := map[string]string{
		"issue.md": "# issue\n",
		"zero.md":  "# zero\n",
		"fail.md":  "# fail\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := storage.Open(root)
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

	var updates []ProgressUpdate
	processor := &Processor{
		Store:       store,
		CodexRunner: fakeRunner{},
	}
	result, err := processor.ProcessUnprocessed(context.Background(), ProviderCodex, "gpt-5.4-mini", func(update ProgressUpdate) {
		updates = append(updates, update)
	})
	if err != nil {
		t.Fatalf("process unprocessed sources: %v", err)
	}

	if result.Total != 3 || result.Succeeded != 2 || len(result.Failed) != 1 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	if len(updates) != 3 {
		t.Fatalf("expected one progress update per source, got %d", len(updates))
	}
	if result.Failed[0].Source.FilePath != "fail.md" {
		t.Fatalf("expected fail.md failure, got %+v", result.Failed[0])
	}

	pending, err := store.ListUnprocessedSources()
	if err != nil {
		t.Fatalf("list unprocessed sources: %v", err)
	}
	if len(pending) != 1 || pending[0].FilePath != "fail.md" {
		t.Fatalf("expected fail.md to remain pending, got %+v", pending)
	}

	board, err := store.ListBoardFindingsForFilter(storage.BoardFilter{
		Provider:   storage.JudgeProviderCodex,
		IncludeAll: true,
	})
	if err != nil {
		t.Fatalf("list board findings: %v", err)
	}
	if len(board) != 1 || board[0].Title != "Issue found" {
		t.Fatalf("unexpected board findings: %+v", board)
	}
}

type measuringRunner struct {
	mu      sync.Mutex
	active  int
	maxSeen int
	delay   time.Duration
}

func (r *measuringRunner) Run(_ context.Context, _ string, _ string, _ string) (judge.Response, error) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxSeen {
		r.maxSeen = r.active
	}
	r.mu.Unlock()

	time.Sleep(r.delay)

	r.mu.Lock()
	r.active--
	r.mu.Unlock()

	return judge.Response{Findings: nil}, nil
}

func TestProcessUnprocessedRunsJudgeWorkConcurrently(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	for _, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte("# test\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := storage.Open(root)
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

	runner := &measuringRunner{delay: 40 * time.Millisecond}
	processor := &Processor{
		Store:       store,
		CodexRunner: runner,
		Concurrency: 3,
	}

	result, err := processor.ProcessUnprocessed(context.Background(), ProviderCodex, "gpt-5.4-mini", nil)
	if err != nil {
		t.Fatalf("process unprocessed sources: %v", err)
	}
	if result.Total != 4 || result.Succeeded != 4 || len(result.Failed) != 0 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	if runner.maxSeen < 2 {
		t.Fatalf("expected concurrent judge execution, max overlap was %d", runner.maxSeen)
	}
}

func TestProcessAllIndexedRerunsAlreadyProcessedSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# issue\n"), 0o644); err != nil {
		t.Fatalf("write one.md: %v", err)
	}

	store, err := storage.Open(root)
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

	processor := &Processor{
		Store:       store,
		CodexRunner: fakeRunner{},
	}
	if _, err := processor.ProcessUnprocessed(context.Background(), ProviderCodex, "gpt-5.4-mini", nil); err != nil {
		t.Fatalf("initial process unprocessed: %v", err)
	}

	firstPass, err := store.ListBoardFindingsForFilter(storage.BoardFilter{
		Provider:   storage.JudgeProviderCodex,
		IncludeAll: true,
	})
	if err != nil {
		t.Fatalf("list first pass findings: %v", err)
	}
	if len(firstPass) != 1 {
		t.Fatalf("expected one finding after first pass, got %+v", firstPass)
	}

	if _, err := processor.ProcessAllIndexed(context.Background(), ProviderCodex, "gpt-5.4", nil); err != nil {
		t.Fatalf("process all indexed: %v", err)
	}

	allRuns, err := store.ListBoardFindingsForFilter(storage.BoardFilter{
		Provider:   storage.JudgeProviderCodex,
		IncludeAll: true,
	})
	if err != nil {
		t.Fatalf("list all runs findings: %v", err)
	}
	if len(allRuns) != 2 {
		t.Fatalf("expected two findings across reruns, got %+v", allRuns)
	}
}

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingRunner) Run(_ context.Context, _ string, _ string, _ string) (judge.Response, error) {
	r.once.Do(func() {
		close(r.started)
	})
	<-r.release
	return judge.Response{Findings: nil}, nil
}

func TestProcessUnprocessedRejectsOverlappingRuns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# one\n"), 0o644); err != nil {
		t.Fatalf("write one.md: %v", err)
	}

	store, err := storage.Open(root)
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

	runner := &blockingRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	processor := &Processor{
		Store:       store,
		CodexRunner: runner,
		Concurrency: 1,
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := processor.ProcessUnprocessed(context.Background(), ProviderCodex, "gpt-5.4-mini", nil)
		errCh <- err
	}()

	<-runner.started

	_, err = processor.ProcessUnprocessed(context.Background(), ProviderCodex, "gpt-5.4-mini", nil)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected overlapping run rejection, got %v", err)
	}

	close(runner.release)
	if err := <-errCh; err != nil {
		t.Fatalf("expected first run to finish cleanly, got %v", err)
	}
}

type cancelAwareRunner struct {
	mu      sync.Mutex
	started int
	ready   chan struct{}
	once    sync.Once
}

func (r *cancelAwareRunner) Run(ctx context.Context, _ string, _ string, _ string) (judge.Response, error) {
	r.mu.Lock()
	r.started++
	r.mu.Unlock()

	r.once.Do(func() {
		close(r.ready)
	})

	<-ctx.Done()
	return judge.Response{}, ctx.Err()
}

func TestProcessUnprocessedStopsSchedulingNewWorkAfterCancel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}

	for _, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte("# test\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := storage.Open(root)
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

	runner := &cancelAwareRunner{ready: make(chan struct{})}
	processor := &Processor{
		Store:       store,
		CodexRunner: runner,
		Concurrency: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updatesCh := make(chan []ProgressUpdate, 1)
	go func() {
		var updates []ProgressUpdate
		_, _ = processor.ProcessUnprocessed(ctx, ProviderCodex, "gpt-5.4-mini", func(update ProgressUpdate) {
			updates = append(updates, update)
		})
		updatesCh <- updates
	}()

	<-runner.ready
	cancel()

	select {
	case updates := <-updatesCh:
		if len(updates) != 1 {
			t.Fatalf("expected one progress update from the in-flight source, got %d", len(updates))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected processing to stop promptly after cancel")
	}

	runner.mu.Lock()
	started := runner.started
	runner.mu.Unlock()
	if started != 1 {
		t.Fatalf("expected only one started judge call after cancel, got %d", started)
	}
}
