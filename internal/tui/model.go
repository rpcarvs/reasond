package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	progressbar "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	assetbundle "github.com/rpcarvs/rdit/cmd/assets"
	"github.com/rpcarvs/rdit/internal/app"
	"github.com/rpcarvs/rdit/internal/integrity"
	"github.com/rpcarvs/rdit/internal/processing"
	"github.com/rpcarvs/rdit/internal/storage"
)

var providerModels = map[string][]string{
	processing.ProviderClaude: {
		"claude-haiku-4-5",
		"claude-sonnet-4-6",
		"claude-opus-4-6",
	},
	processing.ProviderCodex: {
		"gpt-5.4-mini",
		"gpt-5.1-codex-mini",
		"gpt-5.3-codex",
		"gpt-5.4",
	},
}

type phase string

const (
	phaseBoard          phase = "board"
	phaseInitSelect     phase = "init_select"
	phaseInitRunning    phase = "init_running"
	phaseInitResult     phase = "init_result"
	phaseInitFollowup   phase = "init_followup"
	phaseAuditPrompt    phase = "audit_prompt"
	phaseProviderSelect phase = "provider_select"
	phaseModelSelect    phase = "model_select"
	phaseProcessing     phase = "processing"
	phaseDetail         phase = "detail"
	phaseSource         phase = "source"
	phaseFileFilter     phase = "file_filter"
)

type processMode string

const (
	processModePending processMode = "pending"
	processModeAll     processMode = "all"
)

type model struct {
	bootstrap app.Bootstrap
	report    integrity.Report
	err       error
	store     *storage.Store
	processor *processing.Processor

	width  int
	height int
	ready  bool

	pendingCount         int
	phase                phase
	queuedPhase          phase
	events               <-chan tea.Msg
	cancel               context.CancelFunc
	auditPromptDismissed bool
	selectedProvider     string
	selectedModel        string
	processMode          processMode

	initProviderIndex int
	initFollowupIndex int
	providerIndex     int
	modelIndex        int

	progress         progressbar.Model
	completed        int
	total            int
	currentFile      string
	succeeded        int
	failedCount      int
	lastError        string
	statusLine       string
	initStatus       string
	initFailed       bool
	initProvider     assetbundle.Provider
	initOfferedOther bool

	boardFindings []storage.FindingSummary
	boardIndex    int
	boardProvider string
	showAllRuns   bool
	fileFilter    string
	filterFiles   []string
	filterIndex   int
	detail        *storage.FindingDetail
	detailScroll  int
	sourcePath    string
	sourceLines   []string
	sourceScroll  int
	showSummary   bool
}

type progressMsg struct {
	update processing.ProgressUpdate
}

type doneMsg struct {
	result processing.BatchResult
	err    error
}

type initDoneMsg struct {
	provider assetbundle.Provider
	result   app.InitResult
	err      error
}

type startupModalMsg struct {
	next phase
}

// Run launches the current rdit Bubble Tea interface.
func Run(rootDir string) error {
	bootstrap, err := app.NewBootstrap(rootDir)
	if err != nil {
		return err
	}

	progress := progressbar.New(
		progressbar.WithDefaultGradient(),
		progressbar.WithWidth(42),
	)

	m := model{
		bootstrap:        bootstrap,
		phase:            phaseBoard,
		progress:         progress,
		boardProvider:    processing.ProviderCodex,
		selectedProvider: processing.ProviderCodex,
		selectedModel:    providerModels[processing.ProviderCodex][0],
		processMode:      processModePending,
	}
	if err := m.reloadState(); err != nil {
		m.err = err
	}

	program := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := program.Run()
	if typed, ok := finalModel.(model); ok && typed.store != nil {
		_ = typed.store.Close()
	}
	return err
}

func (m model) Init() tea.Cmd {
	if m.queuedPhase == "" || m.queuedPhase == phaseBoard {
		return nil
	}
	next := m.queuedPhase
	return func() tea.Msg {
		return startupModalMsg{next: next}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.ready = m.width > 0 && m.height > 0
		return m, nil
	case startupModalMsg:
		if m.phase == phaseBoard {
			m.phase = msg.next
		}
		return m, nil
	case progressMsg:
		m.phase = phaseProcessing
		m.completed = msg.update.Completed
		m.total = msg.update.Total
		m.currentFile = msg.update.Source.FilePath
		if msg.update.Err != nil {
			m.failedCount++
			m.lastError = msg.update.Err.Error()
			m.statusLine = fmt.Sprintf("Processed %s with an error.", msg.update.Source.FilePath)
		} else {
			m.succeeded++
			m.statusLine = fmt.Sprintf("Processed %s successfully.", msg.update.Source.FilePath)
		}

		percent := 0.0
		if m.total > 0 {
			percent = float64(m.completed) / float64(m.total)
		}
		cmd := m.progress.SetPercent(percent)
		return m, tea.Batch(cmd, waitForEvent(m.events))
	case doneMsg:
		m.events = nil
		m.cancel = nil
		if msg.err != nil {
			m.lastError = msg.err.Error()
		}
		if m.store != nil {
			pendingCount, err := m.store.CountUnprocessedSources()
			if err != nil {
				m.lastError = err.Error()
			} else {
				m.pendingCount = pendingCount
			}
		}
		m.auditPromptDismissed = true
		if err := m.loadBoardFindings(); err != nil {
			m.lastError = err.Error()
		}
		m.phase = phaseBoard
		return m, nil
	case initDoneMsg:
		m.events = nil
		m.initProvider = msg.provider
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.initStatus = fmt.Sprintf("%s init failed.", msg.provider.Label())
			m.initFailed = true
			m.phase = phaseInitResult
			return m, nil
		}

		m.initStatus = fmt.Sprintf("%s init completed.", msg.provider.Label())
		m.initFailed = false
		if err := m.reloadState(); err != nil {
			m.err = err
			m.lastError = err.Error()
		}
		m.phase = phaseInitResult
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	var cmd tea.Cmd
	progressModel, cmd := m.progress.Update(msg)
	m.progress = progressModel.(progressbar.Model)
	return m, cmd
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}

	if m.phase == phaseDetail {
		switch key {
		case "up", "k":
			m.scrollDetail(-1)
			return m, nil
		case "down", "j":
			m.scrollDetail(1)
			return m, nil
		}
	}
	if m.phase == phaseSource {
		switch key {
		case "up", "k":
			m.scrollSource(-1)
			return m, nil
		case "down", "j":
			m.scrollSource(1)
			return m, nil
		}
	}

	switch key {
	case "up", "k":
		return m.moveSelection(-1)
	case "down", "j":
		return m.moveSelection(1)
	}

	switch key {
	case "q":
		if m.showSummary {
			m.showSummary = false
			return m, nil
		}
		switch m.phase {
		case phaseSource:
			m.phase = phaseDetail
			return m, nil
		case phaseFileFilter:
			m.phase = phaseBoard
			return m, nil
		case phaseDetail:
			m.phase = phaseBoard
			m.detail = nil
			m.detailScroll = 0
			return m, nil
		case phaseBoard:
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		default:
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
				m.events = nil
			}
			m.phase = phaseBoard
			return m, nil
		}
	case "esc":
		if m.showSummary {
			m.showSummary = false
			return m, nil
		}
		if m.phase == phaseDetail {
			m.phase = phaseBoard
			m.detail = nil
			m.detailScroll = 0
			return m, nil
		}
		if m.phase == phaseSource {
			m.phase = phaseDetail
			return m, nil
		}
		if m.phase == phaseFileFilter {
			m.phase = phaseBoard
			return m, nil
		}
		if m.phase != phaseBoard {
			m.phase = phaseBoard
			return m, nil
		}
		return m, nil
	case "s":
		if m.phase == phaseBoard || m.phase == phaseDetail {
			m.showSummary = !m.showSummary
			return m, nil
		}
	case "a":
		if m.phase == phaseBoard {
			m.showAllRuns = !m.showAllRuns
			if err := m.loadBoardFindings(); err != nil {
				m.lastError = err.Error()
			}
			return m, nil
		}
	case "f":
		if m.phase == phaseBoard {
			if err := m.loadFilterFiles(); err != nil {
				m.lastError = err.Error()
				return m, nil
			}
			m.filterIndex = 0
			if m.fileFilter != "" {
				for idx, filePath := range m.filterFiles {
					if filePath == m.fileFilter {
						m.filterIndex = idx + 1
						break
					}
				}
			}
			m.phase = phaseFileFilter
			return m, nil
		}
	case "i":
		if m.phase == phaseBoard {
			m.beginInitSession(providerSelectionIndex(m.boardProvider))
			return m, nil
		}
	case "r":
		if m.phase == phaseBoard {
			m.processMode = processModeAll
			m.phase = phaseProviderSelect
			return m, nil
		}
	case "tab":
		if m.phase == phaseBoard {
			if m.boardProvider == processing.ProviderCodex {
				m.boardProvider = processing.ProviderClaude
			} else {
				m.boardProvider = processing.ProviderCodex
			}
			if err := m.loadFilterFiles(); err != nil {
				m.lastError = err.Error()
				return m, nil
			}
			if m.fileFilter != "" && !slices.Contains(m.filterFiles, m.fileFilter) {
				m.fileFilter = ""
			}
			if err := m.loadBoardFindings(); err != nil {
				m.lastError = err.Error()
			}
			return m, nil
		}
	case "y":
		if m.phase == phaseAuditPrompt {
			m.processMode = processModePending
			m.phase = phaseProviderSelect
			return m, nil
		}
		if m.phase == phaseInitFollowup {
			m.initFollowupIndex = 0
			m.initOfferedOther = true
			return m.startInit(otherProvider(m.initProvider))
		}
	case "n":
		if m.phase == phaseAuditPrompt {
			m.auditPromptDismissed = true
			m.phase = phaseBoard
			return m, nil
		}
		if m.phase == phaseInitFollowup {
			m.initFollowupIndex = 1
			m.phase = phaseBoard
			return m, m.Init()
		}
	case "enter":
		switch m.phase {
		case phaseBoard:
			return m.openDetail()
		case phaseDetail:
			m.phase = phaseBoard
			m.detail = nil
			m.detailScroll = 0
			return m, nil
		case phaseSource:
			m.phase = phaseDetail
			return m, nil
		case phaseInitSelect:
			if m.initProviderIndex == 1 {
				return m.startInit(assetbundle.ProviderClaude)
			}
			return m.startInit(assetbundle.ProviderCodex)
		case phaseInitResult:
			if m.initFailed {
				m.phase = phaseBoard
				return m, nil
			}
			if !m.shouldOfferInitFollowup() {
				m.phase = phaseBoard
				return m, nil
			}
			m.initFollowupIndex = 1
			m.phase = phaseInitFollowup
			return m, nil
		case phaseInitFollowup:
			if m.initFollowupIndex == 0 {
				return m.startInit(otherProvider(m.initProvider))
			}
			m.phase = phaseBoard
			return m, m.Init()
		case phaseAuditPrompt:
			m.processMode = processModePending
			m.phase = phaseProviderSelect
			return m, nil
		case phaseProviderSelect:
			providers := []string{processing.ProviderCodex, processing.ProviderClaude}
			m.selectedProvider = providers[m.providerIndex]
			m.modelIndex = 0
			m.selectedModel = providerModels[m.selectedProvider][0]
			m.phase = phaseModelSelect
			return m, nil
		case phaseModelSelect:
			models := providerModels[m.selectedProvider]
			if len(models) == 0 {
				return m, nil
			}
			if m.modelIndex < 0 {
				m.modelIndex = 0
			}
			if m.modelIndex >= len(models) {
				m.modelIndex = len(models) - 1
			}
			m.selectedModel = models[m.modelIndex]
			return m.startProcessing()
		case phaseFileFilter:
			if m.filterIndex == 0 {
				m.fileFilter = ""
			} else if m.filterIndex > 0 && m.filterIndex-1 < len(m.filterFiles) {
				m.fileFilter = m.filterFiles[m.filterIndex-1]
			}
			m.phase = phaseBoard
			if err := m.loadBoardFindings(); err != nil {
				m.lastError = err.Error()
			}
			return m, nil
		}
	case "c":
		if m.phase == phaseInitSelect {
			m.initProviderIndex = 0
			return m.startInit(assetbundle.ProviderCodex)
		}
	case "l":
		switch m.phase {
		case phaseInitSelect:
			m.initProviderIndex = 1
			return m.startInit(assetbundle.ProviderClaude)
		case phaseProviderSelect:
			m.providerIndex = 1
			m.selectedProvider = processing.ProviderClaude
			m.modelIndex = 0
			m.selectedModel = providerModels[m.selectedProvider][0]
			m.phase = phaseModelSelect
			return m, nil
		}
	case "o":
		if m.phase == phaseDetail {
			return m.openSourceViewer()
		}
	}

	return m, nil
}

func (m model) moveSelection(step int) (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseBoard:
		if len(m.boardFindings) == 0 {
			return m, nil
		}
		next := m.boardIndex + step
		if next < 0 {
			next = 0
		}
		if next >= len(m.boardFindings) {
			next = len(m.boardFindings) - 1
		}
		m.boardIndex = next
		return m, nil
	case phaseInitSelect:
		next := m.initProviderIndex + step
		if next < 0 {
			next = 0
		}
		if next > 1 {
			next = 1
		}
		m.initProviderIndex = next
		return m, nil
	case phaseInitFollowup:
		next := m.initFollowupIndex + step
		if next < 0 {
			next = 0
		}
		if next > 1 {
			next = 1
		}
		m.initFollowupIndex = next
		return m, nil
	case phaseProviderSelect:
		next := m.providerIndex + step
		if next < 0 {
			next = 0
		}
		if next > 1 {
			next = 1
		}
		m.providerIndex = next
		return m, nil
	case phaseModelSelect:
		models := providerModels[m.selectedProvider]
		if len(models) == 0 {
			return m, nil
		}
		next := m.modelIndex + step
		if next < 0 {
			next = 0
		}
		if next >= len(models) {
			next = len(models) - 1
		}
		m.modelIndex = next
		return m, nil
	case phaseFileFilter:
		limit := len(m.filterFiles)
		if limit > 0 {
			limit++
		} else {
			limit = 1
		}
		next := m.filterIndex + step
		if next < 0 {
			next = 0
		}
		if next >= limit {
			next = limit - 1
		}
		m.filterIndex = next
		return m, nil
	default:
		return m, nil
	}
}

func (m model) View() string {
	if m.err != nil {
		return "rdit\n\nFailed to inspect repository integrity.\n\n" + m.err.Error() + "\n\nPress q to quit.\n"
	}
	if m.phase == phaseSource {
		return m.renderSourceView()
	}

	base := m.renderBoard()
	if m.showSummary {
		return m.overlay(base, m.renderSummaryModal())
	}

	switch m.phase {
	case phaseInitSelect:
		title := "Install provider"
		if !hasRuntimeAndAuditDir(m.report.RootDir, m.report.Runtime.RuntimeDir.Status) {
			title = "rdit needs initialization"
		}
		return m.overlay(base, m.renderSelectionModal(title, []string{
			"Codex",
			"Claude Code",
		}, m.initProviderIndex, "Enter to continue, up/down to choose, q to close"))
	case phaseInitRunning:
		return m.overlay(base, m.renderMessageModal(
			"Running init",
			[]string{m.initStatus, "Please wait while rdit installs local assets."},
			"q closes this popup",
		))
	case phaseInitResult:
		title := "Init completed"
		footer := "Enter continue • q close"
		if m.initFailed {
			title = "Init failed"
			footer = "Enter or q close"
		}
		return m.overlay(base, m.renderMessageModal(title, []string{m.initStatus}, footer))
	case phaseInitFollowup:
		provider := otherProvider(m.initProvider)
		action := "install"
		status, ok := m.report.Providers[provider]
		if ok && status.Healthy() {
			action = "reinstall"
		}
		return m.overlay(base, m.renderSelectionModal(
			fmt.Sprintf("Also %s %s?", action, provider.Label()),
			[]string{"Yes", "No"},
			m.initFollowupIndex,
			"Enter confirm • up/down choose • q close",
		))
	case phaseAuditPrompt:
		return m.overlay(base, m.renderMessageModal(
			"New logs detected",
			[]string{"There are new unprocessed logs. Do you want to audit them now?"},
			"Enter/y yes, n no, q closes popup",
		))
	case phaseProviderSelect:
		return m.overlay(base, m.renderSelectionModal("Select judge provider", []string{
			"Codex",
			"Claude Code",
		}, m.providerIndex, "Enter to continue, up/down to choose, q to close"))
	case phaseModelSelect:
		return m.overlay(base, m.renderSelectionModal(
			fmt.Sprintf("Select model (%s)", strings.ToUpper(m.selectedProvider)),
			providerModels[m.selectedProvider],
			m.modelIndex,
			"Enter to process, up/down to choose, q closes popup",
		))
	case phaseProcessing:
		progressBody := []string{
			fmt.Sprintf("Judge: %s / %s", m.selectedProvider, m.selectedModel),
			fmt.Sprintf("Current file: %s", fallbackText(m.currentFile, "-")),
			m.progress.View(),
			fmt.Sprintf("Completed: %d/%d", m.completed, m.total),
			fmt.Sprintf("Succeeded: %d  Failed: %d", m.succeeded, m.failedCount),
		}
		if m.statusLine != "" {
			progressBody = append(progressBody, m.statusLine)
		}
		if m.lastError != "" {
			progressBody = append(progressBody, "Last error: "+m.lastError)
		}
		return m.overlay(base, m.renderMessageModal("Processing audits", progressBody, "q closes popup"))
	case phaseDetail:
		if m.detail == nil {
			return base
		}
		return m.overlay(base, m.renderDetailModal(*m.detail))
	case phaseFileFilter:
		options := []string{"All files"}
		options = append(options, m.filterFiles...)
		return m.overlay(base, m.renderSelectionModal("Filter by file", options, m.filterIndex, "Enter apply • up/down choose • q close"))
	default:
		return base
	}
}

func (m model) renderBoard() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	lines := []string{
		titleStyle.Render("rdit - Reasoning Audits"),
		lineStyle.Render(fmt.Sprintf("Repository: %s", m.bootstrap.RootDir)),
	}
	if m.pendingCount > 0 {
		lines = append(lines, lineStyle.Render(fmt.Sprintf("Pending audits: %d", m.pendingCount)))
	}
	if m.initStatus != "" {
		lines = append(lines, lineStyle.Render(m.initStatus))
	}
	if m.lastError != "" {
		lines = append(lines, lineStyle.Render("Last error: "+m.lastError))
	}
	mode := "latest per file"
	if m.showAllRuns {
		mode = "all runs"
	}
	filter := m.fileFilter
	if strings.TrimSpace(filter) == "" {
		filter = "all files"
	}
	lines = append(lines, lineStyle.Render(fmt.Sprintf("View: %s • Filter: %s", mode, filter)))
	lines = append(lines, "")
	lines = append(lines, m.renderProviderHeader())
	lines = append(lines, "")
	lines = append(lines, m.renderBoardTable())
	lines = append(lines, "")
	lines = append(lines, m.renderKeyBindings())

	return strings.Join(lines, "\n")
}

func (m model) renderProviderHeader() string {
	text := "Audits Judged by Codex"
	bg := lipgloss.Color("24")
	if m.boardProvider == processing.ProviderClaude {
		text = "Audits Judged by Claude"
		bg = lipgloss.Color("166")
	}

	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(bg).
		Align(lipgloss.Center).
		Width(m.boardTableWidth())
	return style.Render(text)
}

func (m model) renderBoardTable() string {
	if len(m.boardFindings) == 0 {
		empty := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(1, 2).
			Foreground(lipgloss.Color("244")).
			Render("No processed audit issues.")
		return empty
	}

	totalWidth := m.boardTableInnerWidth()
	scoreWidth := 10
	titleWidth := totalWidth - scoreWidth - 5
	if titleWidth < 20 {
		titleWidth = 20
	}

	headerTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Width(titleWidth).
		Padding(0, 1).
		Render("Title")
	headerScore := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Width(scoreWidth).
		Align(lipgloss.Right).
		Padding(0, 1).
		Render("Score")

	rows := []string{headerTitle + " " + headerScore}
	for index, finding := range m.boardFindings {
		baseRowStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(titleWidth).
			Padding(0, 1)
		scoreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Width(scoreWidth).
			Align(lipgloss.Right).
			Padding(0, 1)

		if index == m.boardIndex {
			baseRowStyle = baseRowStyle.
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("240")).
				Bold(true)
			scoreStyle = scoreStyle.
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("240")).
				Bold(true)
		}

		title := truncateLine(finding.Title, titleWidth-2)
		score := fmt.Sprintf("%.2f", finding.Score)
		rows = append(rows, baseRowStyle.Render(title)+" "+scoreStyle.Render(score))
	}

	table := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Render(table)
}

func (m model) renderSummaryModal() string {
	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Padding(0, 1)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	stateLine := func(label, value string) string {
		return labelStyle.Render(label+": ") + valueStyle.Render(value)
	}

	lines := []string{
		sectionStyle.Render("Repository"),
		stateLine("Path", m.report.RootDir),
		stateLine("Healthy", fmt.Sprintf("%t", m.report.Healthy())),
		stateLine("Runtime dir", string(m.report.Runtime.RuntimeDir.Status)),
		stateLine("Database", string(m.report.Runtime.Database.Status)),
		stateLine(".gitignore", string(m.report.Runtime.GitIgnore.Status)),
	}

	if len(m.report.Runtime.MissingGitIgnores) > 0 {
		lines = append(lines, stateLine("Missing ignore entries", strings.Join(m.report.Runtime.MissingGitIgnores, ", ")))
	}

	lines = append(lines, "", sectionStyle.Render("Providers"))
	for _, provider := range []assetbundle.Provider{assetbundle.ProviderCodex, assetbundle.ProviderClaude} {
		status, ok := m.report.Providers[provider]
		if !ok {
			continue
		}
		lines = append(lines, stateLine(provider.Label()+" installed", fmt.Sprintf("%t", status.Healthy())))
		if missing := status.MissingPaths(); len(missing) > 0 {
			lines = append(lines, stateLine("Missing", strings.Join(missing, ", ")))
		}
		if modified := status.ModifiedPaths(); len(modified) > 0 {
			lines = append(lines, stateLine("Modified", strings.Join(modified, ", ")))
		}
		lines = append(lines, "")
	}

	return m.renderMessageModal("State", lines, "s or q close")
}

func (m model) renderMessageModal(title string, lines []string, footer string) string {
	body := append([]string{}, lines...)
	if footer != "" {
		body = append(body, "", footer)
	}
	return m.renderModalBox(title, strings.Join(body, "\n"))
}

func (m model) renderSelectionModal(title string, options []string, selected int, footer string) string {
	lines := make([]string, 0, len(options)+2)
	for idx, option := range options {
		prefix := "  "
		if idx == selected {
			prefix = "> "
		}
		lines = append(lines, prefix+option)
	}
	if footer != "" {
		lines = append(lines, "", footer)
	}
	return m.renderModalBox(title, strings.Join(lines, "\n"))
}

func (m model) renderDetailModal(detail storage.FindingDetail) string {
	contentWidth := m.modalWidth() - 6
	if contentWidth < 24 {
		contentWidth = 24
	}

	lines := m.detailContentLines(detail, contentWidth)
	viewportHeight := m.detailViewportHeight()
	maxOffset := len(lines) - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.detailScroll
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}

	end := offset + viewportHeight
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[offset:end]

	var footer string
	if maxOffset > 0 {
		footer = fmt.Sprintf("up/down scroll (%d/%d) • o open source • enter/esc/q close", offset+1, maxOffset+1)
	} else {
		footer = "up/down scroll • o open source • enter/esc/q close"
	}
	visible = append(visible, "", lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(footer))
	return m.renderModalBox("Audit detail", strings.Join(visible, "\n"))
}

func (m model) detailContentLines(detail storage.FindingDetail, contentWidth int) []string {
	type section struct {
		title   string
		content string
	}
	sections := []section{
		{title: "Title", content: detail.Title},
		{title: "Score", content: fmt.Sprintf("%.2f", detail.Score)},
		{title: "Issue", content: detail.Issue},
		{title: "Why", content: detail.Why},
		{title: "How", content: detail.How},
		{title: "Source", content: detail.SourcePath},
		{title: "Judge", content: fmt.Sprintf("%s / %s", detail.JudgeProvider, detail.JudgeModel)},
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Padding(0, 1)
	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Width(contentWidth)

	lines := make([]string, 0, len(sections)*3)
	for i, section := range sections {
		lines = append(lines, headerStyle.Render(section.title))
		rendered := contentStyle.Render(strings.TrimSpace(section.content))
		lines = append(lines, strings.Split(rendered, "\n")...)
		if i != len(sections)-1 {
			lines = append(lines, "")
		}
	}

	return lines
}

func (m model) detailViewportHeight() int {
	if m.height <= 0 {
		return 18
	}
	viewport := m.height - 12
	if viewport < 8 {
		return 8
	}
	return viewport
}

func (m *model) scrollDetail(step int) {
	if m.detail == nil {
		return
	}

	contentWidth := m.modalWidth() - 6
	if contentWidth < 24 {
		contentWidth = 24
	}
	lines := m.detailContentLines(*m.detail, contentWidth)
	maxOffset := len(lines) - m.detailViewportHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}

	next := m.detailScroll + step
	if next < 0 {
		next = 0
	}
	if next > maxOffset {
		next = maxOffset
	}
	m.detailScroll = next
}

func (m model) renderSourceView() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	height := m.height
	if height <= 0 {
		height = 30
	}
	width := m.width
	if width <= 0 {
		width = 100
	}

	headerLines := []string{
		titleStyle.Render("Source file"),
		lineStyle.Render(fmt.Sprintf("Repository: %s", m.bootstrap.RootDir)),
		lineStyle.Render(fmt.Sprintf("Path: %s", fallbackText(m.sourcePath, "-"))),
		"",
	}

	contentHeight := height - len(headerLines) - 2
	if contentHeight < 6 {
		contentHeight = 6
	}
	maxOffset := len(m.sourceLines) - contentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.sourceScroll
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	end := offset + contentHeight
	if end > len(m.sourceLines) {
		end = len(m.sourceLines)
	}

	lines := make([]string, 0, contentHeight+1)
	lines = append(lines, m.sourceLines[offset:end]...)
	if len(lines) < contentHeight {
		for len(lines) < contentHeight {
			lines = append(lines, "")
		}
	}
	scrollLine := "up/down scroll • q close"
	if maxOffset > 0 {
		scrollLine = fmt.Sprintf("up/down scroll (%d/%d) • q close", offset+1, maxOffset+1)
	}
	lines = append(lines, "", lineStyle.Render(scrollLine))

	rendered := append(headerLines, contentStyle.Render(strings.Join(lines, "\n")))
	return lipgloss.NewStyle().Width(width).Render(strings.Join(rendered, "\n"))
}

func (m *model) scrollSource(step int) {
	maxOffset := len(m.sourceLines) - m.sourceViewportHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	next := m.sourceScroll + step
	if next < 0 {
		next = 0
	}
	if next > maxOffset {
		next = maxOffset
	}
	m.sourceScroll = next
}

func (m model) sourceViewportHeight() int {
	if m.height <= 0 {
		return 20
	}
	viewport := m.height - 7
	if viewport < 6 {
		return 6
	}
	return viewport
}

func (m model) renderModalBox(title, body string) string {
	head := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("24")).
		Padding(0, 1).
		Render(title)

	box := lipgloss.NewStyle().
		Width(m.modalWidth()).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("69")).
		Padding(1, 2).
		Background(lipgloss.Color("235")).
		Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, head, box)
}

func (m model) modalWidth() int {
	if m.width <= 0 {
		return 88
	}
	width := m.width - 10
	if width < 56 {
		return 56
	}
	if width > 98 {
		return 98
	}
	return width
}

func (m model) boardTableInnerWidth() int {
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 90
	}
	if totalWidth > 120 {
		totalWidth = 120
	}
	if totalWidth < 56 {
		totalWidth = 56
	}
	return totalWidth
}

func (m model) boardTableWidth() int {
	return m.boardTableInnerWidth() + 2
}

func (m model) renderKeyBindings() string {
	keys := []string{
		"up/down navigate",
		"enter open/confirm",
		"tab provider",
		"i install",
		"r re-audit",
		"f file filter",
		"a latest/all",
		"s state",
		"q close",
	}
	lines := wrapKeyBindings(keys, m.boardTableWidth(), 2)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Render(strings.Join(lines, "\n"))
}

func wrapKeyBindings(keys []string, width int, maxLines int) []string {
	if len(keys) == 0 {
		return []string{""}
	}
	if width <= 0 {
		width = 90
	}
	if maxLines < 1 {
		maxLines = 1
	}

	separator := "  •  "
	lines := []string{keys[0]}
	for _, key := range keys[1:] {
		candidate := lines[len(lines)-1] + separator + key
		if lipgloss.Width(candidate) <= width {
			lines[len(lines)-1] = candidate
			continue
		}

		if len(lines) >= maxLines {
			lines[len(lines)-1] += separator + key
			continue
		}
		lines = append(lines, key)
	}
	return lines
}

func (m model) overlay(base, modal string) string {
	if !m.ready {
		return modal
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m model) startProcessing() (model, tea.Cmd) {
	if m.processor == nil {
		m.lastError = "runtime database is not available yet"
		m.phase = phaseBoard
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 1)

	m.phase = phaseProcessing
	m.completed = 0
	m.total = m.pendingCount
	m.currentFile = ""
	m.succeeded = 0
	m.failedCount = 0
	m.lastError = ""
	m.statusLine = "Starting audit processing..."
	m.events = events
	m.cancel = cancel
	m.detail = nil
	m.boardProvider = m.selectedProvider

	go func() {
		var (
			result processing.BatchResult
			err    error
		)
		switch m.processMode {
		case processModeAll:
			result, err = m.processor.ProcessAllIndexed(ctx, m.selectedProvider, m.selectedModel, func(update processing.ProgressUpdate) {
				events <- progressMsg{update: update}
			})
		default:
			result, err = m.processor.ProcessUnprocessed(ctx, m.selectedProvider, m.selectedModel, func(update processing.ProgressUpdate) {
				events <- progressMsg{update: update}
			})
		}
		events <- doneMsg{result: result, err: err}
		close(events)
	}()

	if m.store != nil {
		switch m.processMode {
		case processModeAll:
			sources, err := m.store.ListAllSources()
			if err != nil {
				m.lastError = err.Error()
			} else {
				m.total = len(sources)
			}
		default:
			m.total = m.pendingCount
		}
	}

	return m, tea.Batch(m.progress.SetPercent(0), waitForEvent(events))
}

func (m model) startInit(provider assetbundle.Provider) (model, tea.Cmd) {
	events := make(chan tea.Msg, 1)

	m.phase = phaseInitRunning
	m.initStatus = fmt.Sprintf("Running %s init...", provider.Label())
	m.lastError = ""
	m.events = events

	go func() {
		result, err := m.bootstrap.InitProvider(provider)
		events <- initDoneMsg{
			provider: provider,
			result:   result,
			err:      err,
		}
		close(events)
	}()

	return m, waitForEvent(events)
}

func (m model) openDetail() (model, tea.Cmd) {
	if m.store == nil || len(m.boardFindings) == 0 {
		return m, nil
	}

	detail, err := m.store.GetFindingDetailForProvider(m.boardProvider, m.boardFindings[m.boardIndex].ID)
	if err != nil {
		m.lastError = err.Error()
		return m, nil
	}

	m.detail = &detail
	m.detailScroll = 0
	m.phase = phaseDetail
	return m, nil
}

func (m model) openSourceViewer() (model, tea.Cmd) {
	if m.detail == nil {
		return m, nil
	}
	path := strings.TrimSpace(m.detail.SourcePath)
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(m.bootstrap.RootDir, path)
	}
	m.sourcePath = path
	m.sourceScroll = 0

	if path == "" {
		m.sourceLines = []string{"No source path is available for this audit."}
		m.phase = phaseSource
		return m, nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		m.sourceLines = []string{
			"Failed to open source file.",
			err.Error(),
		}
		m.phase = phaseSource
		return m, nil
	}

	normalized := strings.ReplaceAll(string(contents), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.sourceLines = lines
	m.phase = phaseSource
	return m, nil
}

func waitForEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *model) reloadState() error {
	if m.store != nil {
		_ = m.store.Close()
		m.store = nil
	}

	report, err := m.bootstrap.Inspect()
	if err != nil {
		return err
	}
	m.report = report
	m.err = nil
	m.pendingCount = 0
	m.processor = nil
	m.boardFindings = nil
	m.boardIndex = 0
	m.detail = nil
	m.detailScroll = 0
	m.sourcePath = ""
	m.sourceLines = nil
	m.sourceScroll = 0
	if m.boardProvider == "" {
		m.boardProvider = processing.ProviderCodex
	}
	if m.selectedProvider == "" {
		m.selectedProvider = processing.ProviderCodex
	}
	if m.selectedModel == "" {
		m.selectedModel = providerModels[m.selectedProvider][0]
	}

	if report.Runtime.RuntimeDir.Status == integrity.StatusPresent {
		store, err := m.bootstrap.OpenStore()
		if err != nil {
			return err
		}
		if _, err := store.SyncReasoningAudits(); err != nil {
			_ = store.Close()
			return err
		}

		pendingCount, err := store.CountUnprocessedSources()
		if err != nil {
			_ = store.Close()
			return err
		}

		m.store = store
		m.processor = m.bootstrap.NewProcessor(store)
		m.pendingCount = pendingCount
		if provider, ok, err := store.MostRecentProvider(); err != nil {
			_ = store.Close()
			m.store = nil
			return err
		} else if ok {
			m.boardProvider = provider
			m.selectedProvider = provider
			models := providerModels[m.selectedProvider]
			if len(models) > 0 {
				m.selectedModel = models[0]
			}
		}
		if err := m.loadBoardFindings(); err != nil {
			_ = store.Close()
			m.store = nil
			return err
		}
	}

	m.phase = phaseBoard
	m.queuedPhase = phaseBoard
	if !hasRuntimeAndAuditDir(report.RootDir, report.Runtime.RuntimeDir.Status) {
		m.initOfferedOther = false
		m.queuedPhase = phaseInitSelect
	} else if m.pendingCount > 0 && !m.auditPromptDismissed {
		m.queuedPhase = phaseAuditPrompt
	}
	return nil
}

func (m *model) loadBoardFindings() error {
	if m.store == nil {
		m.boardFindings = nil
		m.boardIndex = 0
		return nil
	}

	findings, err := m.store.ListBoardFindingsForFilter(storage.BoardFilter{
		Provider:   m.boardProvider,
		FilePath:   m.fileFilter,
		IncludeAll: m.showAllRuns,
	})
	if err != nil {
		return err
	}

	m.boardFindings = findings
	if len(findings) == 0 {
		m.boardIndex = 0
		return nil
	}
	if m.boardIndex >= len(findings) {
		m.boardIndex = len(findings) - 1
	}
	return nil
}

func (m *model) loadFilterFiles() error {
	if m.store == nil {
		m.filterFiles = nil
		return nil
	}
	files, err := m.store.ListResultFiles(m.boardProvider)
	if err != nil {
		return err
	}
	m.filterFiles = files
	return nil
}

func hasRuntimeAndAuditDir(rootDir string, runtimeStatus integrity.Status) bool {
	if runtimeStatus != integrity.StatusPresent {
		return false
	}
	auditDir := filepath.Join(rootDir, "reasoning_audits")
	info, err := os.Stat(auditDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func (m *model) beginInitSession(providerIndex int) {
	m.initOfferedOther = false
	m.initProviderIndex = providerIndex
	m.phase = phaseInitSelect
}

func (m model) shouldOfferInitFollowup() bool {
	if m.initFailed || m.initOfferedOther {
		return false
	}

	status, ok := m.report.Providers[otherProvider(m.initProvider)]
	if !ok {
		return true
	}
	return !status.Healthy()
}

func otherProvider(provider assetbundle.Provider) assetbundle.Provider {
	if provider == assetbundle.ProviderClaude {
		return assetbundle.ProviderCodex
	}
	return assetbundle.ProviderClaude
}

func providerSelectionIndex(provider string) int {
	if provider == processing.ProviderClaude {
		return 1
	}
	return 0
}

func truncateLine(text string, width int) string {
	if width <= 1 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width-1]) + "…"
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
