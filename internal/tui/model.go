package tui

import (
	"context"
	"fmt"
	"strings"

	progressbar "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	assetbundle "rdit/cmd/assets"
	"rdit/internal/app"
	"rdit/internal/integrity"
	"rdit/internal/processing"
	"rdit/internal/storage"
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
	phaseOverview       phase = "overview"
	phaseInitSelect     phase = "init_select"
	phaseInitRunning    phase = "init_running"
	phaseAuditPrompt    phase = "audit_prompt"
	phaseProviderSelect phase = "provider_select"
	phaseModelSelect    phase = "model_select"
	phaseProcessing     phase = "processing"
	phaseBoard          phase = "board"
	phaseDetail         phase = "detail"
)

type model struct {
	bootstrap app.Bootstrap
	report    integrity.Report
	err       error
	store     *storage.Store
	processor *processing.Processor

	pendingCount         int
	phase                phase
	events               <-chan tea.Msg
	cancel               context.CancelFunc
	auditPromptDismissed bool
	selectedProvider     string
	selectedModel        string

	progress    progressbar.Model
	completed   int
	total       int
	currentFile string
	succeeded   int
	failedCount int
	lastError   string
	statusLine  string
	initStatus  string

	boardFindings []storage.FindingSummary
	boardIndex    int
	detail        *storage.FindingDetail
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

// Run launches the current rdit Bubble Tea interface.
func Run(rootDir string) error {
	bootstrap, err := app.NewBootstrap(rootDir)
	if err != nil {
		return err
	}

	progress := progressbar.New(
		progressbar.WithDefaultGradient(),
		progressbar.WithWidth(40),
	)

	m := model{
		bootstrap: bootstrap,
		phase:     phaseOverview,
		progress:  progress,
	}
	if err := m.reloadState(); err != nil {
		m.err = err
	}

	program := tea.NewProgram(m)
	finalModel, err := program.Run()
	if typed, ok := finalModel.(model); ok && typed.store != nil {
		_ = typed.store.Close()
	}
	return err
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "i":
			if !m.report.Healthy() && m.phase == phaseOverview {
				m.phase = phaseInitSelect
				return m, nil
			}
		case "esc":
			switch m.phase {
			case phaseInitSelect:
				m.phase = phaseOverview
				return m, nil
			case phaseProviderSelect:
				m.phase = phaseAuditPrompt
				return m, nil
			case phaseModelSelect:
				m.phase = phaseProviderSelect
				return m, nil
			case phaseDetail:
				m.phase = phaseBoard
				m.detail = nil
				return m, nil
			}
		case "y":
			if m.phase == phaseAuditPrompt {
				m.phase = phaseProviderSelect
				return m, nil
			}
		case "n":
			if m.phase == phaseAuditPrompt {
				m.auditPromptDismissed = true
				m.phase = phaseBoard
				return m, nil
			}
		case "a":
			if m.report.Healthy() && m.pendingCount > 0 && m.phase == phaseBoard {
				m.auditPromptDismissed = false
				m.phase = phaseAuditPrompt
				return m, nil
			}
		case "c":
			switch m.phase {
			case phaseInitSelect:
				return m.startInit(assetbundle.ProviderCodex)
			case phaseProviderSelect:
				m.selectedProvider = processing.ProviderCodex
				m.selectedModel = providerModels[processing.ProviderCodex][0]
				m.phase = phaseModelSelect
				return m, nil
			}
		case "l":
			switch m.phase {
			case phaseInitSelect:
				return m.startInit(assetbundle.ProviderClaude)
			case phaseProviderSelect:
				m.selectedProvider = processing.ProviderClaude
				m.selectedModel = providerModels[processing.ProviderClaude][0]
				m.phase = phaseModelSelect
				return m, nil
			}
		case "1", "2", "3", "4":
			if m.phase == phaseModelSelect {
				models := providerModels[m.selectedProvider]
				index := int(msg.String()[0] - '1')
				if index >= 0 && index < len(models) {
					m.selectedModel = models[index]
					return m.startProcessing()
				}
			}
		case "up", "k", "left":
			if m.phase == phaseBoard && m.boardIndex > 0 {
				m.boardIndex--
				return m, nil
			}
		case "down", "j", "right":
			if m.phase == phaseBoard && m.boardIndex < len(m.boardFindings)-1 {
				m.boardIndex++
				return m, nil
			}
		case "enter":
			if m.phase == phaseBoard && len(m.boardFindings) > 0 {
				return m.openDetail()
			}
			if m.phase == phaseDetail {
				m.phase = phaseBoard
				m.detail = nil
				return m, nil
			}
		}
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
		m.phase = phaseOverview
		m.events = nil
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.initStatus = fmt.Sprintf("%s init failed.", msg.provider.Label())
			return m, nil
		}

		m.initStatus = fmt.Sprintf("%s init completed.", msg.provider.Label())
		if err := m.reloadState(); err != nil {
			m.err = err
			m.lastError = err.Error()
		}
		return m, nil
	}

	var cmd tea.Cmd
	progressModel, cmd := m.progress.Update(msg)
	m.progress = progressModel.(progressbar.Model)
	return m, cmd
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cardStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	selectedCardStyle := cardStyle.Foreground(lipgloss.Color("205")).Bold(true)

	if m.err != nil {
		return titleStyle.Render("rdit") + "\n\n" +
			"Failed to inspect repository integrity.\n\n" +
			m.err.Error() + "\n\n" +
			sectionStyle.Render("Press q to quit.") + "\n"
	}

	lines := []string{
		titleStyle.Render("rdit"),
		"",
		fmt.Sprintf("Repository: %s", m.report.RootDir),
		fmt.Sprintf("Healthy: %t", m.report.Healthy()),
		fmt.Sprintf("Runtime dir: %s", m.report.Runtime.RuntimeDir.Status),
		fmt.Sprintf("Database: %s", m.report.Runtime.Database.Status),
		fmt.Sprintf(".gitignore: %s", m.report.Runtime.GitIgnore.Status),
	}

	if len(m.report.Runtime.MissingGitIgnores) > 0 {
		lines = append(lines, "Missing ignore entries: "+strings.Join(m.report.Runtime.MissingGitIgnores, ", "))
	}

	for _, provider := range []assetbundle.Provider{assetbundle.ProviderCodex, assetbundle.ProviderClaude} {
		status, ok := m.report.Providers[provider]
		if !ok {
			continue
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s installed: %t", provider.Label(), status.Healthy()))
		if missing := status.MissingPaths(); len(missing) > 0 {
			lines = append(lines, "Missing: "+strings.Join(missing, ", "))
		}
		if modified := status.ModifiedPaths(); len(modified) > 0 {
			lines = append(lines, "Modified: "+strings.Join(modified, ", "))
		}
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Pending audits: %d", m.pendingCount))

	switch m.phase {
	case phaseInitSelect:
		lines = append(lines, "This repository is not initialized correctly for rdit.")
		lines = append(lines, "Press c to run Codex init or l to run Claude init.")
		lines = append(lines, "", sectionStyle.Render("Press esc to cancel. Press q to quit."))
	case phaseInitRunning:
		lines = append(lines, m.initStatus)
		lines = append(lines, "", sectionStyle.Render("Init is running. Press q to quit."))
	case phaseAuditPrompt:
		lines = append(lines, "There are new unprocessed logs. Do you want to audit them now?")
		lines = append(lines, "", sectionStyle.Render("Press y to continue or n to skip for now."))
	case phaseProviderSelect:
		lines = append(lines, "Select the judge provider:")
		lines = append(lines, "c. Codex")
		lines = append(lines, "l. Claude Code")
		lines = append(lines, "", sectionStyle.Render("Press esc to go back."))
	case phaseModelSelect:
		lines = append(lines, fmt.Sprintf("Select the model for %s:", m.selectedProvider))
		for index, modelName := range providerModels[m.selectedProvider] {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, modelName))
		}
		lines = append(lines, "", sectionStyle.Render("Press the number for the model. Press esc to go back."))
	case phaseProcessing:
		lines = append(lines, fmt.Sprintf("Judge: %s / %s", m.selectedProvider, m.selectedModel))
		lines = append(lines, fmt.Sprintf("Current file: %s", m.currentFile))
		lines = append(lines, m.progress.View())
		lines = append(lines, fmt.Sprintf("Completed: %d/%d", m.completed, m.total))
		lines = append(lines, fmt.Sprintf("Succeeded: %d  Failed: %d", m.succeeded, m.failedCount))
		if m.statusLine != "" {
			lines = append(lines, m.statusLine)
		}
		if m.lastError != "" {
			lines = append(lines, "Last error: "+m.lastError)
		}
		lines = append(lines, "", sectionStyle.Render("Processing is running. Press q to quit."))
	case phaseDetail:
		lines = append(lines, m.renderBoardCards(cardStyle, selectedCardStyle)...)
		if m.detail != nil {
			lines = append(lines, "")
			lines = append(lines, "Title")
			lines = append(lines, m.detail.Title)
			lines = append(lines, "")
			lines = append(lines, "Score")
			lines = append(lines, fmt.Sprintf("%.2f", m.detail.Score))
			lines = append(lines, "")
			lines = append(lines, "Issue")
			lines = append(lines, m.detail.Issue)
			lines = append(lines, "")
			lines = append(lines, "Why")
			lines = append(lines, m.detail.Why)
			lines = append(lines, "")
			lines = append(lines, "How")
			lines = append(lines, m.detail.How)
			lines = append(lines, "")
			lines = append(lines, "Source")
			lines = append(lines, m.detail.SourcePath)
			lines = append(lines, "")
			lines = append(lines, "Judge")
			lines = append(lines, fmt.Sprintf("%s / %s", m.detail.JudgeProvider, m.detail.JudgeModel))
		}
		lines = append(lines, "", sectionStyle.Render("Press esc or enter to close the detail view."))
	case phaseBoard:
		lines = append(lines, m.renderBoardCards(cardStyle, selectedCardStyle)...)
		if m.pendingCount > 0 {
			lines = append(lines, "Press a to audit remaining logs.")
		}
		if m.lastError != "" {
			lines = append(lines, "Last error: "+m.lastError)
		}
		lines = append(lines, "", sectionStyle.Render("Use arrows to navigate. Press enter to inspect. Press q to quit."))
	default:
		if !m.report.Healthy() {
			lines = append(lines, "Press i to initialize this repository now.")
		} else {
			lines = append(lines, "No unprocessed audit files were found.")
		}
		if m.initStatus != "" {
			lines = append(lines, m.initStatus)
		}
		if m.lastError != "" {
			lines = append(lines, "Last error: "+m.lastError)
		}
		lines = append(lines, "", sectionStyle.Render("Press q to quit."))
	}

	return strings.Join(lines, "\n") + "\n"
}

func (m model) renderBoardCards(cardStyle, selectedCardStyle lipgloss.Style) []string {
	if len(m.boardFindings) == 0 {
		return []string{"No findings to display yet."}
	}

	lines := []string{"Board"}
	for index, finding := range m.boardFindings {
		cardText := fmt.Sprintf("%s\nScore: %.2f", finding.Title, finding.Score)
		if index == m.boardIndex {
			lines = append(lines, selectedCardStyle.Render(cardText))
		} else {
			lines = append(lines, cardStyle.Render(cardText))
		}
	}
	return lines
}

func (m model) startProcessing() (model, tea.Cmd) {
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

	go func() {
		result, err := m.processor.ProcessUnprocessed(ctx, m.selectedProvider, m.selectedModel, func(update processing.ProgressUpdate) {
			events <- progressMsg{update: update}
		})
		events <- doneMsg{result: result, err: err}
		close(events)
	}()

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

	detail, err := m.store.GetFindingDetail(m.boardFindings[m.boardIndex].ID)
	if err != nil {
		m.lastError = err.Error()
		return m, nil
	}

	m.detail = &detail
	m.phase = phaseDetail
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

	if !report.Healthy() {
		m.phase = phaseOverview
		return nil
	}

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
	if err := m.loadBoardFindings(); err != nil {
		_ = store.Close()
		m.store = nil
		return err
	}
	if pendingCount > 0 && !m.auditPromptDismissed {
		m.phase = phaseAuditPrompt
	} else {
		m.phase = phaseBoard
	}
	return nil
}

func (m *model) loadBoardFindings() error {
	if m.store == nil {
		m.boardFindings = nil
		m.boardIndex = 0
		return nil
	}

	findings, err := m.store.ListBoardFindings()
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
