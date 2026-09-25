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
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Pane is one terminal (a tab) of a workspace, as the frontend sees it.
type Pane struct {
	ID     string `json:"id"`
	Cmd    string `json:"cmd"`    // configured command; "" = login shell
	Kind   string `json:"kind"`   // "claude" | "shell" (a login shell or any other command)
	Name   string `json:"name"`   // base tab label: "claude", "zsh", "lazygit", ...
	Claude string `json:"claude"` // this pane's Claude Code mark: "working" | "waiting" | "idle" | ""
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
	done  chan struct{} // closed once the process has exited and been reaped
}

// sessions owns every running pane. A workspace is "open" while it has an entry
// in open; it closes when its last pane exits or on Close.
type sessions struct {
	mu     sync.Mutex
	open   map[string][]*pane // workspace name -> panes, in the order they opened
	panes  map[string]*pane   // pane id -> pane
	nextID int
	sock   string // exported to panes as GROVE_SOCK, for `grove state`
	emit   func(event string, data ...any)
}

func newSessions(sock string, emit func(string, ...any)) *sessions {
	return &sessions{
		open:  map[string][]*pane{},
		panes: map[string]*pane{},
		sock:  sock,
		emit:  emit,
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
	p := &pane{Pane: Pane{ID: fmt.Sprintf("p%d", s.nextID), Cmd: cmd, Kind: paneKind(cmd), Name: paneName(cmd)}, ws: ws, dir: dir}
	s.panes[p.ID] = p
	return p
}

// newTab adds a pane running cmd to a workspace. A workspace that isn't open
// opens with just this pane, not its configured set.
func (s *sessions) newTab(ws Workspace, cmd string) Pane {
	s.mu.Lock()
	p := s.newPane(ws.Name, ws.Dir, cmd)
	s.open[ws.Name] = append(s.open[ws.Name], p)
	s.mu.Unlock()
	s.changed()
	return p.Pane
}

// closeTab stops one pane, as closing a terminal tab does. The workspace closes
// with its last pane.
func (s *sessions) closeTab(id string) error {
	s.mu.Lock()
	p, ok := s.panes[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no pane %s", id)
	}
	s.forget(p)
	hangup(p)
	s.mu.Unlock()
	s.changed()
	return nil
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
	p.f, p.cmd, p.done = f, c, make(chan struct{})
	if p.setup != "" {
		// Typed ahead: the shell reads it once it's ready.
		_, _ = f.WriteString(p.setup + "\r")
	}
	go s.pump(p)
	go func() {
		_ = c.Wait()
		close(p.done)
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

// drop removes a pane whose process exited.
func (s *sessions) drop(p *pane) {
	s.mu.Lock()
	if s.panes[p.ID] != p {
		s.mu.Unlock()
		return // already stopped by close
	}
	s.forget(p)
	_ = p.f.Close()
	s.mu.Unlock()
	s.changed()
}

// forget unregisters a pane, and with it its Claude mark; the workspace closes
// with its last pane. Callers hold s.mu.
func (s *sessions) forget(p *pane) {
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
	} else {
		s.open[p.ws] = ps
	}
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

// hangupGrace is how long a hung-up pane's programs get to exit before they are
// killed. A var so tests can shorten it.
var hangupGrace = 2 * time.Second

// hangup does what closing a terminal tab does: SIGHUP the pane's process group
// and close the pty. A program that ignores or handles SIGHUP (`trap "" HUP`,
// gunicorn) would outlive that, keeping the pty and its goroutines, so after
// hangupGrace it is SIGKILLed: the pane's group unless its process has exited by
// then (its pid may be reused once reaped), and the tty's foreground job, if in
// another group, either way, since shells pass SIGHUP on to it and exit.
func hangup(p *pane) {
	if p.f == nil {
		return
	}
	pid := p.cmd.Process.Pid
	job, err := foreground(p.f)
	if err != nil {
		job = pid
	}
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	_ = p.f.Close()
	time.AfterFunc(hangupGrace, func() {
		select {
		case <-p.done:
		default:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		if job != pid {
			_ = syscall.Kill(-job, syscall.SIGKILL)
		}
	})
}

// setClaude records the Claude Code state reported by a pane's hook (`grove
// state`). An empty state clears the mark.
func (s *sessions) setClaude(id, state string) error {
	s.mu.Lock()
	p, ok := s.panes[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no pane %s", id)
	}
	p.Claude = state
	s.mu.Unlock()
	s.changed()
	return nil
}

// seen clears a pane's mark once you look at it, if it was asking for you
// (finished or waiting). A working mark stays.
func (s *sessions) seen(id string) {
	s.mu.Lock()
	p, ok := s.panes[id]
	cleared := ok && (p.Claude == "idle" || p.Claude == "waiting")
	if cleared {
		p.Claude = ""
	}
	s.mu.Unlock()
	if cleared {
		s.changed()
	}
}

// busy reports whether closing a pane would interrupt something: Claude Code
// working, a configured command (the shell runs it in its own place, so it is
// running for as long as the tab is), or a program in the foreground of a shell.
func (s *sessions) busy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.panes[id]
	if !ok || p.f == nil {
		return false
	}
	if p.Kind == "claude" {
		return p.Claude == "working"
	}
	if strings.TrimSpace(p.Cmd) != "" {
		return true
	}
	pgrp, err := foreground(p.f)
	return err == nil && pgrp != p.cmd.Process.Pid
}

// foreground returns the pty's foreground process group. It goes through
// SyscallConn rather than f.Fd(), though the fd is in blocking mode anyway (pty
// opens it so on macOS, and pty.Setsize calls Fd). So hangup's Close can't
// interrupt the pump's Read: that ends once the pane's processes are gone.
func foreground(f *os.File) (int, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pgrp int
	var ioErr error
	if err := rc.Control(func(fd uintptr) { pgrp, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) }); err != nil {
		return 0, err
	}
	return pgrp, ioErr
}

// markRank orders Claude marks for a workspace's summary: the one that most
// needs you wins.
var markRank = map[string]int{"working": 1, "idle": 2, "waiting": 3}

// snapshot returns the open workspaces' panes and each workspace's Claude mark,
// summarised over its panes.
func (s *sessions) snapshot() (map[string][]Pane, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	open := make(map[string][]Pane, len(s.open))
	claude := map[string]string{}
	for ws, ps := range s.open {
		open[ws] = publicPanes(ps)
		for _, p := range ps {
			if markRank[p.Claude] > markRank[claude[ws]] {
				claude[ws] = p.Claude
			}
		}
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

// paneKind is "claude" for a command that runs Claude Code (e.g. "claude",
// "claude --model opus", "/usr/local/bin/claude"), else "shell".
func paneKind(cmd string) string {
	if f := strings.Fields(cmd); len(f) > 0 && filepath.Base(f[0]) == "claude" {
		return "claude"
	}
	return "shell"
}

// paneName is a pane's base tab label: its program's name ("claude",
// "lazygit"), or the login shell's ("zsh") for "".
func paneName(cmd string) string {
	if f := strings.Fields(cmd); len(f) > 0 {
		return filepath.Base(f[0])
	}
	return filepath.Base(userShell())
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
