package processing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rpcarvs/reasond/internal/judge"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/storage"
)

const (
	ProviderCodex  = "codex"
	ProviderClaude = "claude"
)

// Processor coordinates parallel judge execution with serialized persistence.
type Processor struct {
	Store        *storage.Store
	CodexRunner  judge.Runner
	ClaudeRunner judge.Runner
	Concurrency  int

	mu      sync.Mutex
	running bool
}

// ProgressUpdate reports the latest completed source during a batch run.
type ProgressUpdate struct {
	Completed int
	Total     int
	Source    storage.SourceRow
	Err       error
}

// FileFailure captures one source file that could not be judged or persisted.
type FileFailure struct {
	Source storage.SourceRow
	Err    error
}

// BatchResult summarizes a processing run across all pending audit sources.
type BatchResult struct {
	Total     int
	Succeeded int
	Failed    []FileFailure
}

// ProcessUnprocessed evaluates every pending audit source using the selected judge provider.
func (p *Processor) ProcessUnprocessed(
	ctx context.Context,
	provider string,
	model string,
	progress func(ProgressUpdate),
) (BatchResult, error) {
	sources, err := p.Store.ListUnprocessedSources()
	if err != nil {
		return BatchResult{}, err
	}
	return p.processSources(ctx, provider, model, sources, progress)
}

// ProcessAllIndexed evaluates every indexed audit source regardless of processed status.
func (p *Processor) ProcessAllIndexed(
	ctx context.Context,
	provider string,
	model string,
	progress func(ProgressUpdate),
) (BatchResult, error) {
	sources, err := p.Store.ListAllSources()
	if err != nil {
		return BatchResult{}, err
	}
	return p.processSources(ctx, provider, model, sources, progress)
}

func (p *Processor) processSources(
	ctx context.Context,
	provider string,
	model string,
	sources []storage.SourceRow,
	progress func(ProgressUpdate),
) (BatchResult, error) {
	if p == nil {
		return BatchResult{}, fmt.Errorf("processor is required")
	}
	if p.Store == nil {
		return BatchResult{}, fmt.Errorf("store is required")
	}
	if !p.tryStart() {
		return BatchResult{}, fmt.Errorf("audit processing is already running")
	}
	defer p.finish()

	runner, err := p.runnerFor(provider)
	if err != nil {
		return BatchResult{}, err
	}

	result := BatchResult{Total: len(sources)}
	if len(sources) == 0 {
		return result, nil
	}

	workerCount := p.workerCount(len(sources))
	jobs := make(chan storage.SourceRow)
	outcomes := make(chan evaluationOutcome, len(sources))

	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case source, ok := <-jobs:
					if !ok {
						return
					}
					outcomes <- evaluateSource(ctx, runner, p.Store.RootDir(), model, source)
				}
			}
		}()
	}

	go func() {
		for _, source := range sources {
			select {
			case <-ctx.Done():
				close(jobs)
				workers.Wait()
				close(outcomes)
				return
			case jobs <- source:
			}
		}
		close(jobs)
		workers.Wait()
		close(outcomes)
	}()

	completed := 0
	for outcome := range outcomes {
		completed++
		update := ProgressUpdate{
			Completed: completed,
			Total:     len(sources),
			Source:    outcome.Source,
		}

		if outcome.Err != nil {
			update.Err = outcome.Err
			result.Failed = append(result.Failed, FileFailure{Source: outcome.Source, Err: update.Err})
			emitProgress(progress, update)
			continue
		}

		if err := p.Store.PersistProcessedResult(storage.PersistResultInput{
			SourceID:      outcome.Source.ID,
			JudgeProvider: provider,
			JudgeModel:    model,
			Findings:      outcome.Findings,
		}); err != nil {
			update.Err = fmt.Errorf("persist result for %q: %w", outcome.Source.FilePath, err)
			result.Failed = append(result.Failed, FileFailure{Source: outcome.Source, Err: update.Err})
			emitProgress(progress, update)
			continue
		}

		result.Succeeded++
		emitProgress(progress, update)
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	return result, nil
}

func (p *Processor) runnerFor(provider string) (judge.Runner, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderCodex:
		if p.CodexRunner == nil {
			return nil, fmt.Errorf("codex runner is not configured")
		}
		return p.CodexRunner, nil
	case ProviderClaude:
		if p.ClaudeRunner == nil {
			return nil, fmt.Errorf("claude runner is not configured")
		}
		return p.ClaudeRunner, nil
	default:
		return nil, fmt.Errorf("unsupported judge provider %q", provider)
	}
}

func (p *Processor) workerCount(total int) int {
	workers := p.Concurrency
	if workers <= 0 {
		workers = 4
	}
	if total < workers {
		return total
	}
	return workers
}

func (p *Processor) tryStart() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return false
	}
	p.running = true
	return true
}

func (p *Processor) finish() {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

type evaluationOutcome struct {
	Source   storage.SourceRow
	Findings []storage.FindingInput
	Err      error
}

func evaluateSource(
	ctx context.Context,
	runner judge.Runner,
	rootDir string,
	model string,
	source storage.SourceRow,
) evaluationOutcome {
	if err := ctx.Err(); err != nil {
		return evaluationOutcome{
			Source: source,
			Err:    err,
		}
	}

	auditMarkdown, err := os.ReadFile(filepath.Join(appRuntime.ArchivePath(rootDir), filepath.FromSlash(source.FilePath)))
	if err != nil {
		return evaluationOutcome{
			Source: source,
			Err:    fmt.Errorf("read audit file %q: %w", source.FilePath, err),
		}
	}

	response, err := runner.Run(ctx, rootDir, model, string(auditMarkdown))
	if err != nil {
		return evaluationOutcome{
			Source: source,
			Err:    fmt.Errorf("judge source %q: %w", source.FilePath, err),
		}
	}

	findings := make([]storage.FindingInput, 0, len(response.Findings))
	for _, finding := range response.Findings {
		findings = append(findings, storage.FindingInput{
			Title: finding.Title,
			Issue: finding.Issue,
			Why:   finding.Why,
			How:   finding.How,
			Score: finding.Score,
		})
	}

	return evaluationOutcome{
		Source:   source,
		Findings: findings,
	}
}

func emitProgress(progress func(ProgressUpdate), update ProgressUpdate) {
	if progress != nil {
		progress(update)
	}
}
