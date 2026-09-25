package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// App is the GUI backend bound to the frontend. It owns the workspace list and
// the running panes; the frontend owns layout, selection and focus.
type App struct {
	ctx    context.Context
	ready  chan struct{} // closed once ctx is set
	cfg    *Config
	cfgErr string
	sess   *sessions

	mu        sync.Mutex
	ws        []Workspace
	initial   string   // workspace to select on first load
	menuNames []string // workspace names in the Go menu, to rebuild it on change
}

// WorkspaceInfo is one sidebar entry.
type WorkspaceInfo struct {
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Dir      string `json:"dir"`
	Repo     string `json:"repo"`     // display repo name, for the window title
	RepoPath string `json:"repoPath"` // "" for a manual [[workspace]] (not deletable)
	Open     bool   `json:"open"`
	Claude   string `json:"claude"` // "working" | "waiting" | "idle" | ""
	Panes    []Pane `json:"panes"`
}

// Snapshot is everything the frontend renders.
type Snapshot struct {
	Error      string          `json:"error"` // config failed to load
	Workspaces []WorkspaceInfo `json:"workspaces"`
	CanCreate  bool            `json:"canCreate"` // a [[repo]] exists to create into
	Terminal   bool            `json:"terminal"`  // `terminal` is configured
	FontFamily string          `json:"fontFamily"`
	FontSize   int             `json:"fontSize"`
	Shortcuts  []ShortcutInfo  `json:"shortcuts"`
	Version    string          `json:"version"`
}

func newApp(initial string, setup bool) *App {
	a := &App{ready: make(chan struct{}), initial: initial}
	cfg, err := loadConfig()
	if err != nil {
		a.cfgErr = err.Error()
		cfg = &Config{}
	}
	a.cfg = cfg
	a.sess = newSessions(cfg.socketPath(), a.emit)
	a.ws = cfg.resolve()
	if setup && initial != "" {
		// `grove new` launched us for a fresh worktree: queue its setup command.
		if ws, ok := a.find(initial); ok {
			a.sess.ensure(ws, a.repoFor(ws).Setup)
		}
	}
	return a
}

func (a *App) emit(event string, data ...any) {
	select {
	case <-a.ready:
		wr.EventsEmit(a.ctx, event, data...)
	default: // before startup: the frontend fetches state when it loads
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	close(a.ready)
	a.setMenu()
}

// beforeClose confirms quitting while panes are running, since quitting stops
// them.
func (a *App) beforeClose(ctx context.Context) bool {
	n := a.sess.count()
	if n == 0 {
		return false
	}
	msg := fmt.Sprintf("%d worktree%s running. Quitting stops %s panes.", n, plural(n, " is", "s are"), plural(n, "its", "their"))
	choice, err := wr.MessageDialog(ctx, wr.MessageDialogOptions{
		Type:          wr.QuestionDialog,
		Title:         "Quit grove?",
		Message:       msg,
		Buttons:       []string{"Quit", "Cancel"},
		DefaultButton: "Quit",
		CancelButton:  "Cancel",
	})
	return err == nil && choice != "Quit"
}

func (a *App) shutdown(context.Context) { a.sess.closeAll() }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// show brings the window to the front.
func (a *App) show() {
	wr.WindowUnminimise(a.ctx)
	wr.WindowShow(a.ctx)
}

// reload re-discovers worktrees (re-running `git worktree list`), so worktrees
// created outside grove appear, and tells the frontend.
func (a *App) reload() {
	ws := a.cfg.resolve()
	a.mu.Lock()
	a.ws = ws
	a.mu.Unlock()
	a.setMenu()
	a.emit("changed")
}

func (a *App) find(name string) (Workspace, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ws := range a.ws {
		if ws.Name == name {
			return ws, true
		}
	}
	return Workspace{}, false
}

// repoFor returns the [[repo]] a discovered workspace belongs to.
func (a *App) repoFor(ws Workspace) Repo {
	for _, r := range a.cfg.Repo {
		if expandPath(r.Path) == ws.RepoPath {
			return r
		}
	}
	return Repo{}
}

// repo is the [[repo]] new worktrees are created in.
func (a *App) repo() (Repo, error) {
	if len(a.cfg.Repo) == 0 {
		return Repo{}, errors.New("no [[repo]] configured to create into")
	}
	return a.cfg.Repo[0], nil
}

// handle answers requests from other grove processes (see ipc.go).
func (a *App) handle(req ipcReq) error {
	<-a.ready
	switch req.Op {
	case "ping":
	case "focus":
		a.show()
	case "open":
		a.reload()
		ws, ok := a.find(req.Name)
		if !ok {
			return fmt.Errorf("no workspace named %q", req.Name)
		}
		setup := ""
		if req.Setup {
			setup = a.repoFor(ws).Setup
		}
		a.sess.ensure(ws, setup)
		a.emit("open", ws.Name)
		a.show()
	case "close":
		err := a.sess.close(req.Name)
		a.reload()
		return err
	case "state":
		return a.sess.setClaude(req.Pane, req.State)
	default:
		return fmt.Errorf("unknown request %q", req.Op)
	}
	return nil
}

// ---- bound to the frontend ----

// State returns the current snapshot.
func (a *App) State() Snapshot {
	open, claude := a.sess.snapshot()
	a.mu.Lock()
	infos := make([]WorkspaceInfo, len(a.ws))
	for i, ws := range a.ws {
		infos[i] = WorkspaceInfo{
			Name: ws.Name, Branch: ws.Branch, Dir: ws.Dir, Repo: ws.RepoName, RepoPath: ws.RepoPath,
			Claude: claude[ws.Name], Panes: open[ws.Name],
		}
		_, infos[i].Open = open[ws.Name]
	}
	a.mu.Unlock()
	return Snapshot{
		Error:      a.cfgErr,
		Workspaces: infos,
		CanCreate:  len(a.cfg.Repo) > 0,
		Terminal:   strings.TrimSpace(a.cfg.Terminal) != "",
		FontFamily: a.cfg.FontFamily,
		FontSize:   a.cfg.FontSize,
		Shortcuts:  shortcutInfos(),
		Version:    version,
	}
}

// Reload rescans worktrees and returns the new snapshot.
func (a *App) Reload() Snapshot {
	a.reload()
	return a.State()
}

// TakeInitial returns the workspace to select at startup, once.
func (a *App) TakeInitial() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.initial
	a.initial = ""
	return n
}

// Open ensures a workspace's panes exist and returns them.
func (a *App) Open(name string) ([]Pane, error) {
	ws, ok := a.find(name)
	if !ok {
		a.reload()
		if ws, ok = a.find(name); !ok {
			return nil, fmt.Errorf("no workspace named %q", name)
		}
	}
	return a.sess.ensure(ws, ""), nil
}

// Size sets a pane's terminal size, starting its process on the first call.
func (a *App) Size(id string, cols, rows int) error { return a.sess.start(id, cols, rows) }

// Write sends keyboard input to a pane.
func (a *App) Write(id, data string) { a.sess.write(id, data) }

// AddShell appends a shell pane to an open workspace.
func (a *App) AddShell(name string) (Pane, error) { return a.sess.addShell(name) }

// Close stops a workspace's panes; the worktree stays on disk.
func (a *App) Close(name string) error { return a.sess.close(name) }

// Create makes a worktree on a new branch and opens it (setup queued). It
// returns the new workspace's name.
func (a *App) Create(intention, branch, base string) (string, error) {
	r, err := a.repo()
	if err != nil {
		return "", err
	}
	ws, err := createWorktree(r, intention, branch, base)
	if err != nil {
		return "", err
	}
	return a.opened(ws, r), nil
}

// Checkout makes a worktree on an existing branch and opens it (setup queued).
func (a *App) Checkout(branch, name string) (string, error) {
	r, err := a.repo()
	if err != nil {
		return "", err
	}
	ws, err := checkoutWorktree(r, branch, name)
	if err != nil {
		return "", err
	}
	return a.opened(ws, r), nil
}

func (a *App) opened(ws Workspace, r Repo) string {
	a.reload()
	a.sess.ensure(ws, r.Setup)
	return ws.Name
}

// Remove removes a workspace's worktree, then stops its panes. It returns
// "dirty" (and removes nothing) when there are uncommitted changes and force is
// false. The list is not rescanned, so the frontend can still offer to delete
// the branch; it reloads when the flow ends.
func (a *App) Remove(name string, force bool) (string, error) {
	ws, ok := a.find(name)
	if !ok {
		return "", fmt.Errorf("no workspace named %q", name)
	}
	if ws.RepoPath == "" {
		return "", fmt.Errorf("%s is not a git worktree", name)
	}
	err := removeWorktree(ws.RepoPath, ws.Dir, force)
	if !force && errors.Is(err, errWorktreeDirty) {
		return "dirty", nil
	}
	if err != nil {
		return "", err
	}
	if a.sess.isOpen(name) {
		_ = a.sess.close(name)
	}
	return "", nil
}

// DeleteBranch safely deletes a branch (git branch -d). It returns "unmerged"
// when git refused because the branch isn't fully merged.
func (a *App) DeleteBranch(repoPath, branch string) (string, error) {
	err := removeBranch(repoPath, branch)
	if errors.Is(err, errBranchUnmerged) {
		return "unmerged", nil
	}
	return "", err
}

// Branches lists local and remote branches, for autocompletion.
func (a *App) Branches() []string {
	r, err := a.repo()
	if err != nil {
		return nil
	}
	return branchList(expandPath(r.Path))
}

// DefaultBase is the prefilled start-point for new branches.
func (a *App) DefaultBase() string {
	r, err := a.repo()
	if err != nil {
		return ""
	}
	return defaultBase(r)
}

// OpenInTerminal opens a workspace in the configured external terminal.
func (a *App) OpenInTerminal(name string) error {
	ws, ok := a.find(name)
	if !ok {
		return fmt.Errorf("no workspace named %q", name)
	}
	return openInTerminal(ws, a.cfg.Terminal)
}

// OpenURL opens a link clicked in a pane in the default browser.
func (a *App) OpenURL(url string) { wr.BrowserOpenURL(a.ctx, url) }

// ---- menu and shortcuts ----

// shortcut is one menu command. The menu is the single source of shortcuts: its
// accelerators fire even while a terminal pane has focus, and the help overlay
// lists this same table.
type shortcut struct {
	menu, label, action, key string
	mods                     []keys.Modifier
}

var (
	cmdMod   = []keys.Modifier{}
	shiftMod = []keys.Modifier{keys.ShiftKey}
	altMod   = []keys.Modifier{keys.OptionOrAltKey}
)

var shortcuts = []shortcut{
	{"File", "New Worktree…", "new", "n", cmdMod},
	{"File", "New Worktree from Branch…", "checkout", "n", shiftMod},
	{"File", "", "", "", nil},
	{"File", "Open in Terminal", "terminal", "t", shiftMod},
	{"File", "", "", "", nil},
	{"File", "Close Worktree", "close", "w", cmdMod},
	{"File", "Delete Worktree…", "delete", "backspace", cmdMod},
	{"File", "", "", "", nil},
	{"File", "Reload Worktrees", "reload", "r", cmdMod},

	{"View", "Zoom Pane", "zoom", "return", shiftMod},
	{"View", "", "", "", nil},
	{"View", "Bigger Text", "font-up", "=", cmdMod},
	{"View", "Smaller Text", "font-down", "-", cmdMod},
	{"View", "Actual Size", "font-reset", "0", cmdMod},

	{"Go", "Go to Worktree…", "goto", "p", cmdMod},
	{"Go", "Next Worktree", "next-ws", "]", shiftMod},
	{"Go", "Previous Worktree", "prev-ws", "[", shiftMod},
	{"Go", "", "", "", nil},
	{"Go", "Next Pane", "next-pane", "]", cmdMod},
	{"Go", "Previous Pane", "prev-pane", "[", cmdMod},
	{"Go", "Pane Left", "pane-left", "left", altMod},
	{"Go", "Pane Right", "pane-right", "right", altMod},
	{"Go", "Pane Above", "pane-up", "up", altMod},
	{"Go", "Pane Below", "pane-down", "down", altMod},
	{"Go", "", "", "", nil},
	{"Go", "Add Shell Pane", "add-shell", "d", cmdMod},

	{"Help", "Keyboard Shortcuts", "help", "/", cmdMod},
}

// accel is a shortcut's accelerator: ⌘ combos on macOS; elsewhere Ctrl+Shift
// (plus Alt when the Mac binding already uses Shift), since plain Ctrl combos
// belong to the terminal panes.
func accel(key string, mods []keys.Modifier) *keys.Accelerator {
	all := append([]keys.Modifier{keys.CmdOrCtrlKey}, mods...)
	if runtime.GOOS != "darwin" {
		if slices.Contains(mods, keys.ShiftKey) {
			all = append(all, keys.OptionOrAltKey)
		} else {
			all = append(all, keys.ShiftKey)
		}
	}
	return &keys.Accelerator{Key: key, Modifiers: all}
}

// ShortcutInfo is a shortcut as the help overlay shows it.
type ShortcutInfo struct {
	Group string `json:"group"`
	Label string `json:"label"`
	Keys  string `json:"keys"`
}

func shortcutInfos() []ShortcutInfo {
	var out []ShortcutInfo
	for _, s := range shortcuts {
		if s.action != "" {
			out = append(out, ShortcutInfo{s.menu, strings.TrimSuffix(s.label, "…"), accelLabel(accel(s.key, s.mods))})
		}
	}
	return append(out, ShortcutInfo{"Go", "Worktree 1–9", accelLabel(accel("1", cmdMod)) + "–9"})
}

// accelLabel renders an accelerator the way the platform's menus do.
func accelLabel(a *keys.Accelerator) string {
	names := map[string]string{"backspace": "⌫", "return": "↩", "left": "←", "right": "→", "up": "↑", "down": "↓"}
	key := strings.ToUpper(a.Key)
	if n, ok := names[a.Key]; ok {
		key = n
	}
	if runtime.GOOS == "darwin" {
		var b strings.Builder
		for _, m := range []keys.Modifier{keys.ControlKey, keys.OptionOrAltKey, keys.ShiftKey, keys.CmdOrCtrlKey} {
			if slices.Contains(a.Modifiers, m) {
				b.WriteString(map[keys.Modifier]string{keys.ControlKey: "⌃", keys.OptionOrAltKey: "⌥", keys.ShiftKey: "⇧", keys.CmdOrCtrlKey: "⌘"}[m])
			}
		}
		return b.String() + key
	}
	var parts []string
	for _, m := range []keys.Modifier{keys.CmdOrCtrlKey, keys.OptionOrAltKey, keys.ShiftKey} {
		if slices.Contains(a.Modifiers, m) {
			parts = append(parts, map[keys.Modifier]string{keys.CmdOrCtrlKey: "Ctrl", keys.OptionOrAltKey: "Alt", keys.ShiftKey: "Shift"}[m])
		}
	}
	return strings.Join(append(parts, key), "+")
}

// buildMenu builds the app menu; the Go menu lists the first nine workspaces
// under ⌘1–⌘9, like a browser's tabs.
func (a *App) buildMenu(names []string) *menu.Menu {
	m := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		m.Append(menu.AppMenu())
	}
	subs := map[string]*menu.Menu{}
	sub := func(title string) *menu.Menu {
		if subs[title] == nil {
			subs[title] = m.AddSubmenu(title)
			if title == "File" {
				// Edit sits after File, as on every Mac app: copy/paste in panes.
				m.Append(menu.EditMenu())
			}
		}
		return subs[title]
	}
	for _, s := range shortcuts {
		if s.action == "" {
			sub(s.menu).AddSeparator()
			continue
		}
		action := s.action
		sub(s.menu).AddText(s.label, accel(s.key, s.mods), func(*menu.CallbackData) { a.emit("menu", action) })
	}
	gm := subs["Go"]
	if len(names) > 0 {
		gm.AddSeparator()
	}
	for i, n := range names {
		if i == 9 {
			break
		}
		action := fmt.Sprintf("ws:%d", i)
		gm.AddText(n, accel(fmt.Sprint(i+1), cmdMod), func(*menu.CallbackData) { a.emit("menu", action) })
	}
	if runtime.GOOS == "darwin" {
		m.Append(menu.WindowMenu())
	}
	// Help goes last on macOS; it was created in table order, so move it.
	for i, it := range m.Items {
		if it.Label == "Help" {
			m.Items = append(append(m.Items[:i:i], m.Items[i+1:]...), it)
			break
		}
	}
	return m
}

// setMenu installs the menu, rebuilding it only when the workspace names change.
func (a *App) setMenu() {
	a.mu.Lock()
	names := make([]string, len(a.ws))
	for i, ws := range a.ws {
		names[i] = ws.Name
	}
	same := slices.Equal(names, a.menuNames)
	a.menuNames = names
	a.mu.Unlock()
	if same || a.ctx == nil {
		return
	}
	wr.MenuSetApplicationMenu(a.ctx, a.buildMenu(names))
}

// runGUI runs the window for the resolved config. initial selects a workspace;
// setup queues its setup command (a worktree `grove new` just created).
func runGUI(initial string, setup bool) {
	fixPath()
	a := newApp(initial, setup)
	if a.cfgErr == "" {
		ln, err := ipcServe(a.cfg.socketPath(), a.handle)
		if err != nil {
			// Another window has this repo: hand it the request and bow out.
			if showInGUI(a.cfg, initial, setup) == nil {
				return
			}
			fmt.Fprintf(os.Stderr, "grove: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			_ = ln.Close()
			_ = os.Remove(a.cfg.socketPath())
		}()
	}

	title := "grove"
	if len(a.ws) > 0 && a.ws[0].RepoName != "" {
		title = "grove - " + a.ws[0].RepoName
	}
	err := wails.Run(&options.App{
		Title:            title,
		Width:            1440,
		Height:           900,
		MinWidth:         640,
		MinHeight:        400,
		Menu:             a.buildMenu(nil),
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 14, G: 17, B: 22, A: 1},
		OnStartup:        a.startup,
		OnBeforeClose:    a.beforeClose,
		OnShutdown:       a.shutdown,
		Bind:             []any{a},
		Mac: &mac.Options{
			About: &mac.AboutInfo{Title: "grove " + version, Message: "A git-worktree switcher.", Icon: icon},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "grove: %v\n", err)
		os.Exit(1)
	}
}
