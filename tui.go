package main

import (
	"errors"
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
	modeForm     // new worktree: intention, branch, base
	modeCheckout // existing branch: branch, intention
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

	mode      int
	intention textinput.Model
	branch    textinput.Model
	base      textinput.Model
	formField int // 0 = intention, 1 = branch, 2 = base

	// confirm holds the pending y/n confirmation, "" when none. Stages:
	// "close", "delete", "forceDelete", "branch". pendingWS is the target,
	// captured at key-press so a list reload can't shift it.
	confirm   string
	pendingWS Workspace
}

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
	// Checkout mode autocompletes the branch field against existing branches; the
	// accept binding mirrors base. New-worktree mode leaves ShowSuggestions off.
	branch.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("right", "ctrl+f"))

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
		if m.mode == modeForm || m.mode == modeCheckout {
			return m.updateForm(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resolve a pending confirmation first.
	if m.confirm != "" {
		return m.resolveConfirm(msg)
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
		m.pendingWS = ws
		m.confirm = "close"
		m.msg = "close " + ws.Name + "? (y/n)"
		return m, nil
	case "d":
		if len(m.workspaces) == 0 {
			return m, nil
		}
		ws := m.workspaces[m.cursor]
		if ws.RepoPath == "" {
			m.msg = ws.Name + " is not a git worktree"
			return m, nil
		}
		if m.embedded && statusOf(ws.Name) == "active" {
			m.msg = "cannot delete the active workspace"
			return m, nil
		}
		m.pendingWS = ws
		m.confirm = "delete"
		m.msg = "delete " + ws.Name + "? (y/n)"
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
		m.branch.ShowSuggestions = false // new branch name: no autocomplete
		m.base.SetValue(defaultBase(repo)) // prefill, editable
		m.base.SetSuggestions(branchList(expandPath(repo.Path)))
		m.focusField(0)
		return m, textinput.Blink
	case "c":
		if len(m.cfg.Repo) == 0 {
			m.msg = "no [[repo]] configured to create into"
			return m, nil
		}
		repo := m.cfg.Repo[0]
		m.mode = modeCheckout
		m.msg = ""
		m.intention.SetValue("")
		m.branch.SetValue("")
		m.branch.SetSuggestions(branchList(expandPath(repo.Path)))
		m.branch.ShowSuggestions = true
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

// resolveConfirm handles the answer to a pending y/n confirmation, driving the
// close and (multi-step) delete flows.
func (m model) resolveConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	yes := msg.String() == "y"
	stage := m.confirm
	m.confirm = ""
	ws := m.pendingWS

	switch stage {
	case "close":
		if !yes {
			m.msg = "cancelled"
			return m, nil
		}
		if err := closeWindow(ws.Name); err != nil {
			m.msg = err.Error()
		} else {
			m.msg = "closed " + ws.Name
		}
		m.refresh()
		return m, nil

	case "delete":
		if !yes {
			m.msg = "cancelled"
			return m, nil
		}
		err := removeWorktree(ws.RepoPath, ws.Dir, false)
		if errors.Is(err, errWorktreeDirty) {
			m.pendingWS = ws
			m.confirm = "forceDelete"
			m.msg = ws.Name + " has uncommitted changes, force delete? (y/n)"
			return m, nil
		}
		if err != nil {
			m.msg = err.Error()
			return m, nil
		}
		return m.afterRemove(ws)

	case "forceDelete":
		if !yes {
			m.msg = "cancelled"
			return m, nil
		}
		if err := removeWorktree(ws.RepoPath, ws.Dir, true); err != nil {
			m.msg = err.Error()
			return m, nil
		}
		return m.afterRemove(ws)

	case "branch":
		if !yes {
			return m.finishRemove("removed " + ws.Name)
		}
		switch err := removeBranch(ws.RepoPath, ws.Branch); {
		case errors.Is(err, errBranchUnmerged):
			return m.finishRemove("removed " + ws.Name + ", branch " + ws.Branch + " unmerged, kept")
		case err != nil:
			return m.finishRemove(err.Error())
		default:
			return m.finishRemove("removed " + ws.Name + " and branch " + ws.Branch)
		}
	}
	return m, nil
}

// afterRemove kills the worktree's tmux window (if any), then either offers to
// delete its branch or finishes the removal.
func (m model) afterRemove(ws Workspace) (tea.Model, tea.Cmd) {
	if statusOf(ws.Name) != "" {
		_ = closeWindow(ws.Name)
	}
	if ws.Branch != "" {
		m.pendingWS = ws
		m.confirm = "branch"
		m.msg = "removed " + ws.Name + " — also delete branch " + ws.Branch + "? (y/n)"
		return m, nil
	}
	return m.finishRemove("removed " + ws.Name)
}

// finishRemove reloads the workspace list after a deletion, keeps the cursor in
// range, and shows the final message.
func (m model) finishRemove(msg string) (tea.Model, tea.Cmd) {
	m.workspaces = m.cfg.resolve()
	if m.cursor >= len(m.workspaces) {
		m.cursor = len(m.workspaces) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.refresh()
	m.msg = msg
	return m, nil
}

// formFields returns the focusable inputs for the active form, in tab order.
func (m *model) formFields() []*textinput.Model {
	if m.mode == modeCheckout {
		return []*textinput.Model{&m.branch, &m.intention}
	}
	return []*textinput.Model{&m.intention, &m.branch, &m.base}
}

// focusField focuses form field i (in formFields order) and blurs the rest.
func (m *model) focusField(i int) {
	m.formField = i
	m.intention.Blur()
	m.branch.Blur()
	m.base.Blur()
	m.formFields()[i].Focus()
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.msg = ""
		return m, nil
	case "tab":
		n := len(m.formFields())
		m.focusField((m.formField + 1) % n)
		return m, textinput.Blink
	case "shift+tab":
		n := len(m.formFields())
		m.focusField((m.formField - 1 + n) % n)
		return m, textinput.Blink
	case "enter":
		return m.submitForm()
	}

	var cmd tea.Cmd
	f := m.formFields()[m.formField]
	*f, cmd = f.Update(msg)
	return m, cmd
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	var ws Workspace
	var err error
	if m.mode == modeCheckout {
		ws, err = checkoutAndOpen(m.cfg.Repo[0], m.branch.Value(), m.intention.Value())
	} else {
		ws, err = createAndOpen(m.cfg.Repo[0], m.intention.Value(), m.branch.Value(), m.base.Value())
	}
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
	switch m.mode {
	case modeForm:
		return m.formView()
	case modeCheckout:
		return m.checkoutView()
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
	// Short lines to fit the slim pane.
	b.WriteString(helpStyle.Render("⏎ open  n new  c get") + "\n")
	b.WriteString(helpStyle.Render("d del  x close") + "\n")
	b.WriteString(helpStyle.Render("r reload  q quit  ↑↓ move") + "\n")
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

func (m model) checkoutView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("checkout branch") + "\n\n")

	b.WriteString(labelStyle.Render("branch") + "\n")
	b.WriteString("  " + m.branch.View() + "\n\n")
	b.WriteString(labelStyle.Render("name") + "\n")
	b.WriteString("  " + m.intention.View() + "\n\n")

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
