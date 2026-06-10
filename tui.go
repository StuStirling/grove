package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13")).PaddingLeft(1)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	windowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green: visible window
	detachStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow: detached session
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).PaddingLeft(1)
	labelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).PaddingLeft(1)
)

const (
	modeList = iota
	modeForm
)

type model struct {
	cfg        *Config
	workspaces []Workspace
	cursor     int
	statuses   []string // live status per workspace, parallel to workspaces
	msg        string
	embedded   bool       // running as a pane inside the grove session
	chosen     *Workspace // launcher mode: workspace picked to attach after quit
	width      int        // pane width, for truncating long branch names

	mode         int
	intention    textinput.Model
	branch       textinput.Model
	base         textinput.Model
	formField    int  // 0 = intention, 1 = branch, 2 = base
	confirmClose bool // awaiting y/n to close the selected window
}

const numFormFields = 3

func newModel(cfg *Config, embedded bool) model {
	intention := textinput.New()
	intention.Placeholder = "worktree name"
	intention.CharLimit = 60
	intention.Width = 22
	intention.Focus()

	branch := textinput.New()
	branch.Placeholder = "branch name"
	branch.CharLimit = 120
	branch.Width = 22

	base := textinput.New()
	base.Placeholder = "base branch"
	base.CharLimit = 120
	base.Width = 22
	// Autocomplete base against the repo's branches. tab is taken by field-nav,
	// so accept the ghosted suggestion with → or ctrl+f; cycle with ctrl+n/p.
	base.ShowSuggestions = true
	base.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("right", "ctrl+f"))

	m := model{
		cfg:        cfg,
		workspaces: cfg.resolve(),
		embedded:   embedded,
		intention:  intention,
		branch:     branch,
		base:       base,
	}
	m.statuses = make([]string, len(m.workspaces))
	m.refresh()
	return m
}

func (m *model) refresh() {
	if len(m.statuses) != len(m.workspaces) {
		m.statuses = make([]string, len(m.workspaces))
	}
	for i, ws := range m.workspaces {
		m.statuses[i] = statusOf(ws.Name)
	}
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		if m.mode == modeForm {
			return m.updateForm(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resolve a pending close confirmation first.
	if m.confirmClose {
		m.confirmClose = false
		if msg.String() == "y" {
			ws := m.workspaces[m.cursor]
			if err := closeWindow(ws.Name); err != nil {
				m.msg = err.Error()
			} else {
				m.msg = "closed " + ws.Name
			}
			m.refresh()
		} else {
			m.msg = "cancelled"
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.workspaces)-1 {
			m.cursor++
		}
	case "r":
		m.refresh()
		m.msg = "refreshed"
	case "x":
		if len(m.workspaces) == 0 {
			return m, nil
		}
		ws := m.workspaces[m.cursor]
		if statusOf(ws.Name) == "" {
			m.msg = ws.Name + " is not open"
			return m, nil
		}
		m.confirmClose = true
		m.msg = "close " + ws.Name + "? (y/n)"
		return m, nil
	case "n":
		if len(m.cfg.Repo) == 0 {
			m.msg = "no [[repo]] configured to create into"
			return m, nil
		}
		repo := m.cfg.Repo[0]
		m.mode = modeForm
		m.msg = ""
		m.intention.SetValue("")
		m.branch.SetValue("")
		m.base.SetValue(defaultBase(repo)) // prefill, editable
		m.base.SetSuggestions(branchList(expandPath(repo.Path)))
		m.focusField(0)
		return m, textinput.Blink
	case "enter", " ":
		if len(m.workspaces) == 0 {
			return m, nil
		}
		ws := m.workspaces[m.cursor]
		if m.embedded {
			if err := prepare(ws); err != nil {
				m.msg = "error: " + err.Error()
			} else {
				m.msg = "→ " + ws.Name
			}
			m.refresh()
			return m, nil
		}
		m.chosen = &ws
		return m, tea.Quit
	}
	return m, nil
}

// focusField focuses form field i (0..numFormFields-1) and blurs the rest.
func (m *model) focusField(i int) {
	m.formField = i
	m.intention.Blur()
	m.branch.Blur()
	m.base.Blur()
	switch i {
	case 0:
		m.intention.Focus()
	case 1:
		m.branch.Focus()
	case 2:
		m.base.Focus()
	}
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.msg = ""
		return m, nil
	case "tab":
		m.focusField((m.formField + 1) % numFormFields)
		return m, textinput.Blink
	case "shift+tab":
		m.focusField((m.formField - 1 + numFormFields) % numFormFields)
		return m, textinput.Blink
	case "enter":
		return m.submitForm()
	}

	var cmd tea.Cmd
	switch m.formField {
	case 0:
		m.intention, cmd = m.intention.Update(msg)
	case 1:
		m.branch, cmd = m.branch.Update(msg)
	case 2:
		m.base, cmd = m.base.Update(msg)
	}
	return m, cmd
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	ws, err := createAndOpen(m.cfg.Repo[0], m.intention.Value(), m.branch.Value(), m.base.Value())
	if err != nil {
		m.msg = err.Error()
		return m, nil
	}
	if !m.embedded {
		m.chosen = &ws
		return m, tea.Quit
	}
	// Reload so the new worktree shows up, return to the list.
	m.workspaces = m.cfg.resolve()
	m.refresh()
	m.mode = modeList
	m.msg = "created " + ws.Name
	return m, nil
}

func (m model) View() string {
	if m.mode == modeForm {
		return m.formView()
	}
	return m.listView()
}

func (m model) listView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("workspaces") + "\n\n")

	for i, ws := range m.workspaces {
		cursor := "  "
		line := normalStyle
		if i == m.cursor {
			cursor = cursorStyle.Render("▸ ")
			line = selectedStyle
		}
		row := cursor + line.Render(ws.Name)

		switch m.statuses[i] {
		case "active":
			row += " " + windowStyle.Render("●")
		case "open":
			row += " " + detachStyle.Render("○")
		}
		b.WriteString(row + "\n")
		if ws.Branch != "" {
			b.WriteString("    " + dimStyle.Render(truncate(ws.Branch, m.width-5)) + "\n")
		}
	}

	b.WriteString("\n")
	if m.msg != "" {
		b.WriteString(dimStyle.PaddingLeft(1).Render(truncate(m.msg, m.width-2)) + "\n")
	}
	// Two short lines to fit the slim pane.
	b.WriteString(helpStyle.Render("⏎ open  n new  x close") + "\n")
	b.WriteString(helpStyle.Render("↑↓ move  r reload  q quit") + "\n")
	b.WriteString(helpStyle.Render(legend()))
	return b.String()
}

func (m model) formView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("new worktree") + "\n\n")

	b.WriteString(labelStyle.Render("intention") + "\n")
	b.WriteString("  " + m.intention.View() + "\n\n")
	b.WriteString(labelStyle.Render("branch") + "\n")
	b.WriteString("  " + m.branch.View() + "\n\n")
	b.WriteString(labelStyle.Render("base") + "\n")
	b.WriteString("  " + m.base.View() + "\n\n")

	if m.msg != "" {
		b.WriteString(errStyle.PaddingLeft(1).Render(truncate(m.msg, m.width-2)) + "\n\n")
	}
	b.WriteString(helpStyle.Render("⇥ field  → accept") + "\n")
	b.WriteString(helpStyle.Render("⏎ create  esc cancel"))
	return b.String()
}

// truncate shortens s to max runes with an ellipsis. max <= 0 means no limit.
func truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func legend() string {
	return fmt.Sprintf("%s active tab  %s open tab", windowStyle.Render("●"), detachStyle.Render("○"))
}
