package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// Pane is one terminal in a workspace's layout, as the frontend sees it.
type Pane struct {
	ID  string `json:"id"`
	Cmd string `json:"cmd"` // configured command; "" = login shell
}

// pane is a Pane plus its process. The process starts lazily, on the frontend's
// first Start call, so it is spawned at the size the pane is actually drawn.
type pane struct {
	Pane
	ws    string
	dir   string
	setup string   // typed into the pane once it starts (the repo's setup command)
	f     *os.File // pty master; nil until started
	cmd   *exec.Cmd
}

// sessions owns every running pane. A workspace is "open" while it has an entry
// in open; it closes when its last pane exits or on Close.
type sessions struct {
	mu     sync.Mutex
	open   map[string][]*pane // workspace name -> panes, in layout order
	panes  map[string]*pane   // pane id -> pane
	claude map[string]claudeMark
	nextID int
	sock   string // exported to panes as GROVE_SOCK, for `grove state`
	emit   func(event string, data ...any)
}

// claudeMark is a workspace's Claude Code state and the pane that reported it,
// so the mark clears when that pane exits.
type claudeMark struct {
	state string // "working" | "waiting" | "idle"
	pane  string
}

func newSessions(sock string, emit func(string, ...any)) *sessions {
	return &sessions{
		open:   map[string][]*pane{},
		panes:  map[string]*pane{},
		claude: map[string]claudeMark{},
		sock:   sock,
		emit:   emit,
	}
}

// changed tells the frontend to refetch state (open workspaces, panes, marks).
func (s *sessions) changed() { s.emit("changed") }

// ensure opens a workspace's panes (without starting them) if it isn't open, and
// returns its panes. setup, when set, is typed into the shell pane on start.
func (s *sessions) ensure(ws Workspace, setup string) []Pane {
	s.mu.Lock()
	if ps, ok := s.open[ws.Name]; ok {
		s.mu.Unlock()
		return publicPanes(ps)
	}
	cmds := ws.Panes
	if len(cmds) == 0 {
		cmds = []string{""}
	}
	ps := make([]*pane, len(cmds))
	for i, c := range cmds {
		ps[i] = s.newPane(ws.Name, ws.Dir, c)
	}
	if strings.TrimSpace(setup) != "" {
		ps[shellPaneIndex(cmds)].setup = setup
	}
	s.open[ws.Name] = ps
	s.mu.Unlock()
	s.changed()
	return publicPanes(ps)
}

// newPane registers a pane; callers hold s.mu.
func (s *sessions) newPane(ws, dir, cmd string) *pane {
	s.nextID++
	p := &pane{Pane: Pane{ID: fmt.Sprintf("p%d", s.nextID), Cmd: cmd}, ws: ws, dir: dir}
	s.panes[p.ID] = p
	return p
}

// addShell appends a plain shell pane to an open workspace.
func (s *sessions) addShell(ws string) (Pane, error) {
	s.mu.Lock()
	ps, ok := s.open[ws]
	if !ok {
		s.mu.Unlock()
		return Pane{}, fmt.Errorf("%s is not open", ws)
	}
	p := s.newPane(ws, ps[0].dir, "")
	s.open[ws] = append(ps, p)
	s.mu.Unlock()
	s.changed()
	return p.Pane, nil
}

func publicPanes(ps []*pane) []Pane {
	out := make([]Pane, len(ps))
	for i, p := range ps {
		out[i] = p.Pane
	}
	return out
}

// start spawns a pane's process at the given size; a started pane just resizes.
func (s *sessions) start(id string, cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.panes[id]
	if !ok {
		return fmt.Errorf("no pane %s", id)
	}
	size := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	if p.f != nil {
		return pty.Setsize(p.f, size)
	}
	c := paneCommand(p.Cmd)
	c.Dir = p.dir
	c.Env = paneEnv(p.ws, p.ID, s.sock)
	f, err := pty.StartWithSize(c, size)
	if err != nil {
		return fmt.Errorf("starting %q: %w", p.Cmd, err)
	}
	p.f, p.cmd = f, c
	if p.setup != "" {
		// Typed ahead: the shell reads it once it's ready.
		_, _ = f.WriteString(p.setup + "\r")
	}
	go s.pump(p)
	go func() {
		_ = c.Wait()
		s.drop(p)
	}()
	return nil
}

// pump streams a pane's output to the frontend. Output is base64 so a UTF-8
// sequence split across reads survives the JSON hop; xterm.js reassembles it.
// ponytail: no backpressure. Measured fine (1M lines, ~7 MB, drawn in ~1.4s with
// the UI responsive); add xterm write-callback acks if bigger floods lag.
func (s *sessions) pump(p *pane) {
	buf := make([]byte, 64*1024)
	event := "pty:" + p.ID
	for {
		n, err := p.f.Read(buf)
		if n > 0 {
			s.emit(event, base64.StdEncoding.EncodeToString(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// drop removes a pane whose process exited; the workspace closes with its last
// pane.
func (s *sessions) drop(p *pane) {
	s.mu.Lock()
	if s.panes[p.ID] != p {
		s.mu.Unlock()
		return // already stopped by close
	}
	delete(s.panes, p.ID)
	ps := s.open[p.ws]
	for i, q := range ps {
		if q == p {
			ps = append(ps[:i:i], ps[i+1:]...)
			break
		}
	}
	if len(ps) == 0 {
		delete(s.open, p.ws)
		delete(s.claude, p.ws)
	} else {
		s.open[p.ws] = ps
		if s.claude[p.ws].pane == p.ID {
			delete(s.claude, p.ws)
		}
	}
	_ = p.f.Close()
	s.mu.Unlock()
	s.changed()
}

func (s *sessions) write(id, data string) {
	s.mu.Lock()
	var f *os.File
	if p := s.panes[id]; p != nil {
		f = p.f
	}
	s.mu.Unlock()
	if f != nil {
		_, _ = f.WriteString(data)
	}
}

// close stops every pane of a workspace. The worktree on disk is untouched.
func (s *sessions) close(ws string) error {
	s.mu.Lock()
	ps, ok := s.open[ws]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%s is not open", ws)
	}
	for _, p := range ps {
		delete(s.panes, p.ID)
		hangup(p)
	}
	delete(s.open, ws)
	delete(s.claude, ws)
	s.mu.Unlock()
	s.changed()
	return nil
}

// closeAll stops every pane, on quit.
func (s *sessions) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.panes {
		hangup(p)
	}
	s.panes = map[string]*pane{}
	s.open = map[string][]*pane{}
}

// hangup does what closing a terminal tab does: SIGHUP the pane's process group
// and close the pty. The pump goroutine then reaps the process.
func hangup(p *pane) {
	if p.f == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
	_ = p.f.Close()
}

// setClaude records the Claude Code state reported by a pane's hook (`grove
// state`) against its workspace. An empty state clears the mark.
func (s *sessions) setClaude(id, state string) error {
	s.mu.Lock()
	p, ok := s.panes[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no pane %s", id)
	}
	if state == "" {
		delete(s.claude, p.ws)
	} else {
		s.claude[p.ws] = claudeMark{state: state, pane: id}
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

// snapshot returns the open workspaces' panes and their Claude marks.
func (s *sessions) snapshot() (map[string][]Pane, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	open := make(map[string][]Pane, len(s.open))
	for ws, ps := range s.open {
		open[ws] = publicPanes(ps)
	}
	claude := make(map[string]string, len(s.claude))
	for ws, m := range s.claude {
		claude[ws] = m.state
	}
	return open, claude
}

func (s *sessions) isOpen(ws string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.open[ws]
	return ok
}

func (s *sessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.open)
}

// userShell is the login shell panes run: $SHELL, else zsh on macOS (its
// default shell; /bin/sh there is bash).
func userShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// paneCommand builds a pane's process: a login shell for "", else the command
// run through the shell.
func paneCommand(cmd string) *exec.Cmd {
	sh := userShell()
	if strings.TrimSpace(cmd) == "" {
		c := exec.Command(sh)
		c.Args[0] = "-" + filepath.Base(sh) // leading dash = login shell
		return c
	}
	return exec.Command(sh, "-c", cmd)
}

// scrubbed are env prefixes of the terminal grove was launched from. Panes are
// grove's own terminals, so leaking these would make tools think they run
// inside tmux, Ghostty, iTerm2, etc. The Claude
// Code session identity is dropped too: launched from inside a Claude session,
// a pane's own `claude` would otherwise run as that session's child (no
// transcript). User config such as CLAUDE_CODE_USE_BEDROCK passes through.
var scrubbed = []string{"TMUX", "TERM", "COLORTERM", "ITERM", "KITTY", "GHOSTTY",
	"WEZTERM", "ALACRITTY", "VSCODE", "LC_TERMINAL", "GROVE_",
	"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_", "CLAUDE_CODE_SESSION_", "CLAUDE_PID", "CLAUDE_EFFORT", "AI_AGENT"}

// paneEnv is the environment of a pane: grove's own, minus the outer terminal's
// identity, plus the pane's grove identity for `grove state`. GROVE_BIN lets hooks
// call this exact grove whatever is (or isn't) on PATH.
func paneEnv(ws, id, sock string) []string {
	bin, _ := groveExe()
	var env []string
	hasLocale := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if hasAnyPrefix(k, scrubbed) {
			continue
		}
		if k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE" {
			hasLocale = true
		}
		env = append(env, kv)
	}
	if !hasLocale {
		// Launched from Finder there is no locale; without one shells mangle UTF-8.
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env,
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"TERM_PROGRAM=grove", "TERM_PROGRAM_VERSION="+version,
		"GROVE_PANE="+id, "GROVE_SOCK="+sock, "GROVE_WORKSPACE="+ws, "GROVE_BIN="+bin)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// shellPaneIndex returns the index in panes of the first plain-shell pane, where
// the setup command runs; it falls back to the big pane (0).
func shellPaneIndex(panes []string) int {
	for i, c := range panes {
		if strings.TrimSpace(c) == "" {
			return i
		}
	}
	return 0
}
