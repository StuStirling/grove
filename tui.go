package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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

	// Claude Code state markers (set per-window by hooks; see liveStates).
	claudeWaitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // permission prompt: needs you now
	claudeIdleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true) // finished: your turn
	claudeWorkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // working
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
	cfg          *Config
	workspaces   []Workspace
	cursor       int
	statuses     []string // live tmux status per workspace, parallel to workspaces
	claudeStates []string // live @claude_state per workspace, parallel to workspaces
	msg        string
	embedded   bool       // running as a pane inside the grove session
	chosen     *Workspace // launcher mode: workspace picked to attach after quit
	width      int        // pane width, for truncating long branch names

	mode      int
	intention textinput.Model
	branch    textinput.Model
	base      textinput.Model
	formField int // 0 = intention, 1 = branch, 2 = base

	// busy is the label of an in-flight blocking git operation (create, remove,
	// branch delete), "" when idle. While set, the spinner animates and key input
	// is ignored so the op can't be re-triggered mid-flight.
	spin spinner.Model
	busy string

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

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))

	m := model{
		cfg:        cfg,
		workspaces: cfg.resolve(),
		embedded:   embedded,
		intention:  intention,
		branch:     branch,
		base:       base,
		spin:       sp,
	}
	m.statuses = make([]string, len(m.workspaces))
	m.refresh()
	return m
}

// reload re-discovers worktrees from the config (re-running `git worktree list`
// per repo), keeps the cursor in range, then refreshes live tmux state. Bound to
// the manual refresh key so worktrees created outside grove get picked up; the
// poll timer stays on the cheaper refresh() so it doesn't shell out every tick.
func (m *model) reload() {
	m.workspaces = m.cfg.resolve()
	if m.cursor >= len(m.workspaces) {
		m.cursor = len(m.workspaces) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.refresh()
}

func (m *model) refresh() {
	if len(m.statuses) != len(m.workspaces) {
		m.statuses = make([]string, len(m.workspaces))
	}
	if len(m.claudeStates) != len(m.workspaces) {
		m.claudeStates = make([]string, len(m.workspaces))
	}
	live := liveStates()
	for i, ws := range m.workspaces {
		ls := live[ws.Name]
		m.statuses[i] = ls.status
		m.claudeStates[i] = ls.claude
	}
}

// createResultMsg / removeResultMsg / branchResultMsg carry the outcome of a
// backgrounded blocking git operation back into Update, so the spinner can stop
// and the (multi-step) flow can resume on the main loop.
type createResultMsg struct {
	ws  Workspace
	err error
}
type removeResultMsg struct {
	ws     Workspace
	forced bool
	err    error
}
type branchResultMsg struct {
	ws  Workspace
	err error
}

// runCreate creates (or checks out) a worktree and opens it, off the main loop.
func runCreate(r Repo, intention, branch, base string, checkout bool) tea.Cmd {
	return func() tea.Msg {
		var ws Workspace
		var err error
		if checkout {
			ws, err = checkoutAndOpen(r, branch, intention)
		} else {
			ws, err = createAndOpen(r, intention, branch, base)
		}
		return createResultMsg{ws: ws, err: err}
	}
}

// runRemove removes a worktree off the main loop.
func runRemove(ws Workspace, force bool) tea.Cmd {
	return func() tea.Msg {
		return removeResultMsg{ws: ws, forced: force, err: removeWorktree(ws.RepoPath, ws.Dir, force)}
	}
}

// runBranch deletes a worktree's branch off the main loop.
func runBranch(ws Workspace) tea.Cmd {
	return func() tea.Msg {
		return branchResultMsg{ws: ws, err: removeBranch(ws.RepoPath, ws.Branch)}
	}
}

// startBusy sets the busy label and returns the command batch that kicks off the
// work and starts the spinner animation.
func (m *model) startBusy(label string, work tea.Cmd) tea.Cmd {
	m.busy = label
	m.msg = ""
	return tea.Batch(work, m.spin.Tick)
}

// pollMsg drives the periodic refresh of live statuses (tmux presence and
// @claude_state) so the list reflects Claude Code waiting for input without the
// user pressing reload.
type pollMsg struct{}

const pollInterval = 1500 * time.Millisecond

func pollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollMsg{} })
}

func (m model) Init() tea.Cmd { return tea.Batch(textinput.Blink, pollTick()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case pollMsg:
		// Keep the list live, but don't disturb an in-flight op or a form.
		if m.mode == modeList && m.busy == "" {
			m.refresh()
		}
		return m, pollTick()
	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case createResultMsg:
		return m.onCreated(msg)
	case removeResultMsg:
		return m.onRemoved(msg)
	case branchResultMsg:
		return m.onBranchRemoved(msg)
	case tea.KeyMsg:
		if m.busy != "" {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit // always allow bailing out of a stuck op
			}
			return m, nil // otherwise ignore input while an operation is in flight
		}
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
		m.reload()
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
		cmd := m.startBusy("removing "+ws.Name, runRemove(ws, false))
		return m, cmd

	case "forceDelete":
		if !yes {
			m.msg = "cancelled"
			return m, nil
		}
		cmd := m.startBusy("removing "+ws.Name, runRemove(ws, true))
		return m, cmd

	case "branch":
		if !yes {
			return m.finishRemove("removed " + ws.Name)
		}
		cmd := m.startBusy("deleting branch "+ws.Branch, runBranch(ws))
		return m, cmd
	}
	return m, nil
}

// onCreated resumes after a backgrounded create/checkout completes.
func (m model) onCreated(msg createResultMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.err != nil {
		m.msg = msg.err.Error() // stay in the form so the user can fix and retry
		return m, nil
	}
	if !m.embedded {
		m.chosen = &msg.ws
		return m, tea.Quit
	}
	m.reload()
	m.mode = modeList
	m.msg = "created " + msg.ws.Name
	return m, nil
}

// onRemoved resumes after a backgrounded worktree removal completes, branching
// into the force-delete prompt or the post-removal (branch) flow.
func (m model) onRemoved(msg removeResultMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if !msg.forced && errors.Is(msg.err, errWorktreeDirty) {
		m.pendingWS = msg.ws
		m.confirm = "forceDelete"
		m.msg = msg.ws.Name + " has uncommitted changes, force delete? (y/n)"
		return m, nil
	}
	if msg.err != nil {
		m.msg = msg.err.Error()
		return m, nil
	}
	return m.afterRemove(msg.ws)
}

// onBranchRemoved resumes after a backgrounded branch deletion completes.
func (m model) onBranchRemoved(msg branchResultMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	switch {
	case errors.Is(msg.err, errBranchUnmerged):
		return m.finishRemove("removed " + msg.ws.Name + ", branch " + msg.ws.Branch + " unmerged, kept")
	case msg.err != nil:
		return m.finishRemove(msg.err.Error())
	default:
		return m.finishRemove("removed " + msg.ws.Name + " and branch " + msg.ws.Branch)
	}
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
	m.reload()
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
	checkout := m.mode == modeCheckout
	label := "creating " + strings.TrimSpace(m.intention.Value())
	if checkout {
		label = "checking out " + strings.TrimSpace(m.branch.Value())
	}
	work := runCreate(m.cfg.Repo[0], m.intention.Value(), m.branch.Value(), m.base.Value(), checkout)
	cmd := m.startBusy(label, work)
	return m, cmd
}

func (m model) View() string {
	if m.busy != "" {
		return m.busyView()
	}
	switch m.mode {
	case modeForm:
		return m.formView()
	case modeCheckout:
		return m.checkoutView()
	}
	return m.listView()
}

func (m model) busyView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("workspaces") + "\n\n")
	b.WriteString("  " + m.spin.View() + " " + truncate(m.busy, m.width-5) + "…\n")
	return b.String()
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
		// Indicators live in a fixed-width gutter on the left so they stay on
		// screen even when a long name gets truncated. Absent glyphs are a
		// space, so names stay aligned regardless of state.
		status := " "
		switch m.statuses[i] {
		case "active":
			status = windowStyle.Render("●")
		case "open":
			status = detachStyle.Render("○")
		}
		claude := " "
		switch m.claudeStates[i] {
		case "waiting":
			claude = claudeWaitStyle.Render("◆") // permission prompt
		case "idle":
			claude = claudeIdleStyle.Render("◆") // finished, your turn
		case "working":
			claude = claudeWorkStyle.Render("◌")
		}
		// cursor(2) + status(1) + claude(1) + space(1) = 5 cells of gutter.
		name := truncate(ws.Name, m.width-6)
		row := cursor + status + claude + " " + line.Render(name)
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
	return fmt.Sprintf("%s active  %s open\n %s%s claude wants you  %s busy",
		windowStyle.Render("●"), detachStyle.Render("○"),
		claudeWaitStyle.Render("◆"), claudeIdleStyle.Render("◆"), claudeWorkStyle.Render("◌"))
}
