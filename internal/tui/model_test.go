package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	progressbar "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/app"
	"github.com/rpcarvs/reasond/internal/integrity"
	"github.com/rpcarvs/reasond/internal/install"
	"github.com/rpcarvs/reasond/internal/processing"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/storage"
	"github.com/rpcarvs/reasond/internal/testutil"
)

func TestReloadStateOnUninitializedRepoStaysInOverview(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/clean")

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.report.Healthy() {
		t.Fatalf("expected uninitialized repo to be unhealthy")
	}
	if m.phase != phaseBoard {
		t.Fatalf("expected board phase, got %s", m.phase)
	}
	if m.queuedPhase != phaseInitSelect {
		t.Fatalf("expected queued init modal, got %s", m.queuedPhase)
	}
}

func sourceIDForPath(t *testing.T, store *storage.Store, path string) int64 {
	t.Helper()

	sources, err := store.ListAllSources()
	if err != nil {
		t.Fatalf("list all sources: %v", err)
	}
	for _, source := range sources {
		if source.FilePath == path {
			return source.ID
		}
	}
	t.Fatalf("source %q not found", path)
	return 0
}

func TestReloadStateOnHealthyRepoWithPendingAuditQueuesPrompt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := appRuntime.EnsureLayout(root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# Reasoning\n\nfixture\n"), 0o644); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if !m.report.Healthy() {
		t.Fatalf("expected initialized repo to be healthy")
	}
	if m.pendingCount != 1 {
		t.Fatalf("expected one pending audit, got %d", m.pendingCount)
	}
	if m.phase != phaseBoard {
		t.Fatalf("expected board phase, got %s", m.phase)
	}
	if m.queuedPhase != phaseAuditPrompt {
		t.Fatalf("expected queued audit prompt, got %s", m.queuedPhase)
	}
}

func TestReloadStateRefreshesReportAfterDatabaseBootstrap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := appRuntime.EnsureLayout(root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	databasePath := filepath.Join(root, appRuntime.DirectoryName, appRuntime.DatabaseFileName)
	if _, err := os.Stat(databasePath); !os.IsNotExist(err) {
		t.Fatalf("expected database to be absent before reloadState, stat err=%v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.report.Runtime.Database.Status != integrity.StatusPresent {
		t.Fatalf("expected refreshed report to show database present, got %s", m.report.Runtime.Database.Status)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("expected database to exist after reloadState, stat err=%v", err)
	}
}

func TestAuditPromptDeclineMovesToBoard(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := appRuntime.EnsureLayout(root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# Reasoning\n\nfixture\n"), 0o644); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}
	m.phase = phaseAuditPrompt

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	next := nextModel.(model)

	if next.phase != phaseBoard {
		t.Fatalf("expected board phase after declining audit, got %s", next.phase)
	}
	if !next.auditPromptDismissed {
		t.Fatalf("expected audit prompt to be dismissed")
	}
}

func TestBoardInstallKeyOpensInitSelection(t *testing.T) {
	t.Parallel()

	m := model{
		phase:         phaseBoard,
		boardProvider: processing.ProviderClaude,
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	next := nextModel.(model)

	if next.phase != phaseInitSelect {
		t.Fatalf("expected init selection phase, got %s", next.phase)
	}
	if next.initProviderIndex != 1 {
		t.Fatalf("expected Claude to be preselected, got index %d", next.initProviderIndex)
	}
}

func TestSuccessfulInitShowsCompletionThenFollowupPrompt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	result, err := bootstrap.InitProvider(assetbundle.ProviderClaude)
	if err != nil {
		t.Fatalf("init provider: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
		phase:     phaseInitRunning,
	}

	nextModel, _ := m.Update(initDoneMsg{
		provider: assetbundle.ProviderClaude,
		result:   result,
		err:      nil,
	})
	next := nextModel.(model)

	if next.phase != phaseInitResult {
		t.Fatalf("expected init result phase, got %s", next.phase)
	}
	if next.initStatus != "Claude init completed." {
		t.Fatalf("unexpected init status: %q", next.initStatus)
	}

	followupModel, _ := next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	followup := followupModel.(model)
	if followup.phase != phaseInitFollowup {
		t.Fatalf("expected init followup phase, got %s", followup.phase)
	}
	if followup.initFollowupIndex != 1 {
		t.Fatalf("expected followup default to No, got %d", followup.initFollowupIndex)
	}

	finalModel, _ := followup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := finalModel.(model)
	if final.phase != phaseBoard {
		t.Fatalf("expected board phase after declining followup, got %s", final.phase)
	}
}

func TestSecondInstallInSameSessionDoesNotPromptFollowupAgain(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	firstResult, err := bootstrap.InitProvider(assetbundle.ProviderClaude)
	if err != nil {
		t.Fatalf("init claude provider: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
		phase:     phaseInitRunning,
	}

	nextModel, _ := m.Update(initDoneMsg{
		provider: assetbundle.ProviderClaude,
		result:   firstResult,
		err:      nil,
	})
	next := nextModel.(model)
	if next.phase != phaseInitResult {
		t.Fatalf("expected init result phase, got %s", next.phase)
	}

	followupModel, _ := next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	followup := followupModel.(model)
	if followup.phase != phaseInitFollowup {
		t.Fatalf("expected init followup phase, got %s", followup.phase)
	}

	followup.initOfferedOther = true
	secondResult, err := bootstrap.InitProvider(assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("init codex provider: %v", err)
	}

	doneModel, _ := followup.Update(initDoneMsg{
		provider: assetbundle.ProviderCodex,
		result:   secondResult,
		err:      nil,
	})
	done := doneModel.(model)
	if done.phase != phaseInitResult {
		t.Fatalf("expected second init result phase, got %s", done.phase)
	}

	finalModel, _ := done.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := finalModel.(model)
	if final.phase != phaseBoard {
		t.Fatalf("expected board phase after second init completion, got %s", final.phase)
	}
	if final.initOfferedOther != true {
		t.Fatalf("expected install session to remember the followup was already offered")
	}
}

func TestQuitFromDetailReturnsToBoard(t *testing.T) {
	t.Parallel()

	m := model{
		phase:  phaseDetail,
		detail: &storage.FindingDetail{ID: 1, Title: "x"},
	}

	nextModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	next := nextModel.(model)
	if cmd != nil {
		t.Fatalf("expected no quit command while detail is open")
	}
	if next.phase != phaseBoard {
		t.Fatalf("expected board phase after closing detail, got %s", next.phase)
	}
	if next.detail != nil {
		t.Fatalf("expected detail to be cleared")
	}
}

func TestDetailModalUpDownScrollsContentWithoutChangingSelection(t *testing.T) {
	t.Parallel()

	m := model{
		phase:      phaseDetail,
		boardIndex: 2,
		height:     14,
		width:      80,
		detail: &storage.FindingDetail{
			ID:            1,
			Title:         "Title",
			Score:         0.73,
			Issue:         strings.Repeat("issue ", 40),
			Why:           strings.Repeat("why ", 40),
			How:           strings.Repeat("how ", 40),
			SourcePath:    ".reasond/reasond_audits/a.md",
			JudgeProvider: "codex",
			JudgeModel:    "gpt-5.4-mini",
		},
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := nextModel.(model)

	if next.boardIndex != 2 {
		t.Fatalf("expected board index to stay unchanged, got %d", next.boardIndex)
	}
	if next.detailScroll == 0 {
		t.Fatalf("expected detail scroll to advance")
	}
}

func TestDetailModalOpenSourceViewerAndReturn(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := persistFixtureFinding(store, "a.md", storage.JudgeProviderCodex, "gpt-5.4-mini", "x", 0.5); err != nil {
		t.Fatalf("persist fixture finding: %v", err)
	}

	board, err := store.ListBoardFindingsForFilter(storage.BoardFilter{
		Provider:   storage.JudgeProviderCodex,
		IncludeAll: true,
	})
	if err != nil {
		t.Fatalf("list board findings: %v", err)
	}
	if len(board) != 1 {
		t.Fatalf("expected one board finding, got %d", len(board))
	}

	detail, err := store.GetFindingDetailForProvider(storage.JudgeProviderCodex, board[0].ID)
	if err != nil {
		t.Fatalf("load detail: %v", err)
	}

	m := model{
		phase: phaseDetail,
		bootstrap: app.Bootstrap{
			RootDir: root,
		},
		detail: &detail,
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	next := nextModel.(model)
	if next.phase != phaseSource {
		t.Fatalf("expected source phase, got %s", next.phase)
	}
	if len(next.sourceLines) == 0 {
		t.Fatalf("expected source lines to be loaded")
	}
	if next.sourcePath != filepath.Join(root, ".reasond", "reasond_audits", "a.md") {
		t.Fatalf("expected source path inside reasond_audits, got %q", next.sourcePath)
	}
	if next.sourceLines[0] != "# a.md" {
		t.Fatalf("expected persisted source contents, got %q", next.sourceLines[0])
	}

	backModel, cmd := next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	back := backModel.(model)
	if cmd != nil {
		t.Fatalf("expected no quit cmd when closing source view")
	}
	if back.phase != phaseDetail {
		t.Fatalf("expected phaseDetail after q in source view, got %s", back.phase)
	}
}

func TestSourceViewUpDownScrollsWithoutChangingSelection(t *testing.T) {
	t.Parallel()

	m := model{
		phase:      phaseSource,
		boardIndex: 3,
		height:     10,
		sourceLines: make([]string, 30),
	}
	for i := range m.sourceLines {
		m.sourceLines[i] = "line"
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := nextModel.(model)

	if next.boardIndex != 3 {
		t.Fatalf("expected board index unchanged, got %d", next.boardIndex)
	}
	if next.sourceScroll != 10 {
		t.Fatalf("expected source scroll to advance by 10, got %d", next.sourceScroll)
	}
}

func TestSourceViewUpClampsAtTopAfterBlockScroll(t *testing.T) {
	t.Parallel()

	m := model{
		phase:       phaseSource,
		height:      10,
		sourceScroll: 10,
		sourceLines: make([]string, 30),
	}
	for i := range m.sourceLines {
		m.sourceLines[i] = "line"
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	next := nextModel.(model)
	if next.sourceScroll != 0 {
		t.Fatalf("expected source scroll to clamp at 0, got %d", next.sourceScroll)
	}
}

func TestSourceViewDownClampsAtRenderedBottom(t *testing.T) {
	t.Parallel()

	m := model{
		phase:  phaseSource,
		height: 12,
		width:  80,
		sourceLines: make([]string, 15),
	}
	for i := range m.sourceLines {
		m.sourceLines[i] = "line"
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := nextModel.(model)
	if next.sourceScroll != 9 {
		t.Fatalf("expected source scroll to clamp at rendered bottom offset 9, got %d", next.sourceScroll)
	}
}

func TestSourceViewScrollsWrappedContentByRenderedRows(t *testing.T) {
	t.Parallel()

	longLine := strings.Repeat("wrapped content ", 20)
	m := model{
		phase: phaseSource,
		width: 24,
		height: 12,
		sourceLines: []string{
			longLine,
			longLine,
			longLine,
		},
	}

	initial := m.renderSourceView()
	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := nextModel.(model)

	if next.sourceScroll == 0 {
		t.Fatalf("expected wrapped source scroll to advance")
	}

	after := next.renderSourceView()
	if after == initial {
		t.Fatalf("expected rendered source view to change after scrolling wrapped content")
	}
}

func TestSourceViewHighlightsMarkdownSyntax(t *testing.T) {
	t.Parallel()

	renderer := newSourceMarkdownRenderer(80)
	for _, line := range []string{
		"# Heading",
		"> quoted `code`",
		"- list item with **bold** and *italic*",
	} {
		rendered := renderer.renderLine(line)
		if !strings.Contains(rendered, line) {
			t.Fatalf("expected rendered line to retain %q", line)
		}
	}

	rendered := renderer.renderLine("```go")
	if !strings.Contains(rendered, "```go") {
		t.Fatalf("expected rendered fence opener to retain content")
	}
	if !renderer.inFence {
		t.Fatalf("expected renderer to enter fenced code block after opening fence")
	}
	rendered = renderer.renderLine("fmt.Println(\"hi\")")
	if !strings.Contains(rendered, "fmt.Println(\"hi\")") {
		t.Fatalf("expected rendered fenced content to retain content")
	}
	rendered = renderer.renderLine("```")
	if !strings.Contains(rendered, "```") {
		t.Fatalf("expected rendered fence closer to retain content")
	}
	if renderer.inFence {
		t.Fatalf("expected renderer to exit fenced code block after closing fence")
	}

	rendered = renderer.renderLine("plain with `inline` and _emphasis_")
	if !strings.Contains(rendered, "plain with `inline` and _emphasis_") {
		t.Fatalf("expected rendered inline markdown to retain content")
	}
}

func TestMarkdownLineDetectors(t *testing.T) {
	t.Parallel()

	if !isHeadingLine("# Heading") {
		t.Fatalf("expected heading detector to match heading")
	}
	if !isBlockquoteLine("> quote") {
		t.Fatalf("expected blockquote detector to match blockquote")
	}
	if !isListLine("- item") || !isListLine("1. item") {
		t.Fatalf("expected list detector to match unordered and ordered lists")
	}
	if !isFenceLine("```go") || !isFenceLine("~~~") {
		t.Fatalf("expected fence detector to match fenced code blocks")
	}
}

func TestBoardTabSwitchesProviderDataset(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if err := persistFixtureFinding(store, "a.md", "codex", "gpt-5.4-mini", "Codex finding", 0.8); err != nil {
		t.Fatalf("persist codex finding: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := persistFixtureFinding(store, "b.md", "claude", "claude-haiku-4-5", "Claude finding", 0.6); err != nil {
		t.Fatalf("persist claude finding: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	m := model{bootstrap: bootstrap, progress: progressbar.New()}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.boardProvider != processing.ProviderClaude {
		t.Fatalf("expected most recent provider to be claude, got %s", m.boardProvider)
	}
	if len(m.boardFindings) != 1 || m.boardFindings[0].Title != "Claude finding" {
		t.Fatalf("expected claude board finding, got %+v", m.boardFindings)
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	next := nextModel.(model)
	if next.boardProvider != processing.ProviderCodex {
		t.Fatalf("expected provider to switch to codex, got %s", next.boardProvider)
	}
	if len(next.boardFindings) != 1 || next.boardFindings[0].Title != "Codex finding" {
		t.Fatalf("expected codex board finding, got %+v", next.boardFindings)
	}
}

func TestReloadStateDefaultsProviderToMostRecentRun(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if err := persistFixtureFinding(store, "a.md", "codex", "gpt-5.4-mini", "Codex older", 0.4); err != nil {
		t.Fatalf("persist codex finding: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := persistFixtureFinding(store, "b.md", "claude", "claude-haiku-4-5", "Claude latest", 0.8); err != nil {
		t.Fatalf("persist claude finding: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	m := model{bootstrap: bootstrap, progress: progressbar.New()}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.boardProvider != processing.ProviderClaude {
		t.Fatalf("expected provider claude, got %s", m.boardProvider)
	}
	if len(m.boardFindings) != 1 || m.boardFindings[0].Title != "Claude latest" {
		t.Fatalf("expected claude latest finding, got %+v", m.boardFindings)
	}
}

func TestReloadStatePrefersProviderWithVisibleBoardFindings(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if err := persistFixtureFinding(store, "a.md", "claude", "claude-haiku-4-5", "Claude visible", 0.8); err != nil {
		t.Fatalf("persist claude finding: %v", err)
	}
	if err := store.PersistProcessedResult(storage.PersistResultInput{
		SourceID:      sourceIDForPath(t, store, "b.md"),
		JudgeProvider: storage.JudgeProviderCodex,
		JudgeModel:    "gpt-5.4-mini",
	}); err != nil {
		t.Fatalf("persist codex zero-finding run: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	m := model{bootstrap: bootstrap, progress: progressbar.New()}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.boardProvider != processing.ProviderClaude {
		t.Fatalf("expected provider claude, got %s", m.boardProvider)
	}
	if len(m.boardFindings) != 1 || m.boardFindings[0].Title != "Claude visible" {
		t.Fatalf("expected claude visible finding, got %+v", m.boardFindings)
	}
}

func TestBoardATogglesLatestAndAllRuns(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if err := persistFixtureFinding(store, "a.md", "codex", "gpt-5.4-mini", "Old finding", 0.3); err != nil {
		t.Fatalf("persist old finding: %v", err)
	}
	if err := persistFixtureFinding(store, "a.md", "codex", "gpt-5.4-mini", "Latest finding", 0.9); err != nil {
		t.Fatalf("persist latest finding: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	m := model{bootstrap: bootstrap, progress: progressbar.New()}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if len(m.boardFindings) != 1 || m.boardFindings[0].Title != "Latest finding" {
		t.Fatalf("expected latest-only board finding, got %+v", m.boardFindings)
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	next := nextModel.(model)
	if !next.showAllRuns {
		t.Fatalf("expected showAllRuns to be true after toggle")
	}
	if len(next.boardFindings) != 2 {
		t.Fatalf("expected all-runs board to show 2 findings, got %+v", next.boardFindings)
	}
}

func TestFileFilterModalAppliesSelectedFileFilter(t *testing.T) {
	t.Parallel()

	root, store := setupBoardTestStore(t)
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	if err := persistFixtureFinding(store, "a.md", "codex", "gpt-5.4-mini", "A finding", 0.4); err != nil {
		t.Fatalf("persist a finding: %v", err)
	}
	if err := persistFixtureFinding(store, "b.md", "codex", "gpt-5.4-mini", "B finding", 0.7); err != nil {
		t.Fatalf("persist b finding: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}
	m := model{bootstrap: bootstrap, progress: progressbar.New()}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if len(m.boardFindings) != 2 {
		t.Fatalf("expected two findings before filter, got %+v", m.boardFindings)
	}

	filterModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	filter := filterModel.(model)
	if filter.phase != phaseFileFilter {
		t.Fatalf("expected file filter phase, got %s", filter.phase)
	}

	downModel, _ := filter.Update(tea.KeyMsg{Type: tea.KeyDown})
	down := downModel.(model)
	enterModel, _ := down.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := enterModel.(model)
	if next.phase != phaseBoard {
		t.Fatalf("expected phase board after applying filter, got %s", next.phase)
	}
	if next.fileFilter == "" {
		t.Fatalf("expected a selected file filter")
	}
	for _, finding := range next.boardFindings {
		if finding.SourcePath != next.fileFilter {
			t.Fatalf("expected filtered findings for %s, got %+v", next.fileFilter, next.boardFindings)
		}
	}
}

func TestRenderKeyBindingsWrapsToTwoRowsOnNarrowWidth(t *testing.T) {
	t.Parallel()

	m := model{width: 62}
	rendered := m.renderKeyBindings()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two keybinding rows, got %d (%q)", len(lines), rendered)
	}
	if !strings.Contains(rendered, "q close") {
		t.Fatalf("expected q close keybinding to remain visible, got %q", rendered)
	}
}

func setupBoardTestStore(t *testing.T) (string, *storage.Store) {
	t.Helper()

	root := t.TempDir()
	auditDir := appRuntime.ArchivePath(root)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncArchivedAudits(); err != nil {
		t.Fatalf("sync archived audits: %v", err)
	}

	return root, store
}

func persistFixtureFinding(store *storage.Store, path, provider, modelName, title string, score float64) error {
	var sourceID int64
	if err := store.DB().QueryRow(`SELECT id FROM audit_sources WHERE file_path = ?`, path).Scan(&sourceID); err != nil {
		return err
	}
	return store.PersistProcessedResult(storage.PersistResultInput{
		SourceID:      sourceID,
		JudgeProvider: provider,
		JudgeModel:    modelName,
		Findings: []storage.FindingInput{
			{
				Title: title,
				Issue: "issue",
				Why:   "why",
				How:   "how",
				Score: score,
			},
		},
	})
}
