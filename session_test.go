package main

import (
	"encoding/base64"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recorder collects emitted events: pane output (decoded) and change counts.
type recorder struct {
	mu      sync.Mutex
	out     strings.Builder
	changes int
}

func (r *recorder) emit(event string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.HasPrefix(event, "pty:") {
		b, _ := base64.StdEncoding.DecodeString(data[0].(string))
		r.out.Write(b)
	} else if event == "changed" {
		r.changes++
	}
}

func (r *recorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSessionsLifecycle(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("TMUX_PANE", "%9") // must not leak into panes
	r := &recorder{}
	s := newSessions("/tmp/grove-test.sock", r.emit)
	dir := t.TempDir()
	ws := Workspace{Name: "wt", Dir: dir, Panes: []string{`printf "big:%s:%s" "$GROVE_WORKSPACE" "${TMUX_PANE:-none}"; sleep 30`, ""}}

	panes := s.ensure(ws, "echo setup-$((40+2)); pwd")
	if len(panes) != 2 || panes[0].Cmd != ws.Panes[0] {
		t.Fatalf("panes = %+v", panes)
	}
	if again := s.ensure(ws, ""); again[0].ID != panes[0].ID {
		t.Fatalf("ensure is not idempotent: %+v vs %+v", again, panes)
	}
	for _, p := range panes {
		if err := s.start(p.ID, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	// The command pane sees its workspace and no outer TMUX_PANE; the setup command is typed
	// into the shell pane (the first ""), which runs in the worktree.
	waitFor(t, "pane output", func() bool {
		o := r.output()
		return strings.Contains(o, "big:wt:none") && strings.Contains(o, "setup-42") && strings.Contains(o, dir)
	})

	if err := s.setClaude(panes[0].ID, "waiting"); err != nil {
		t.Fatal(err)
	}
	if _, cl := s.snapshot(); cl["wt"] != "waiting" {
		t.Fatalf("claude = %q", cl["wt"])
	}

	// Exiting the shell pane drops only that pane.
	s.write(panes[1].ID, "exit\r")
	waitFor(t, "shell pane to drop", func() bool {
		open, _ := s.snapshot()
		return len(open["wt"]) == 1
	})
	if _, cl := s.snapshot(); cl["wt"] != "waiting" {
		t.Fatalf("mark from a live pane was cleared: %q", cl["wt"])
	}

	// Close stops the rest and forgets the mark.
	if err := s.close("wt"); err != nil {
		t.Fatal(err)
	}
	if s.isOpen("wt") {
		t.Fatal("still open after close")
	}
	if _, cl := s.snapshot(); cl["wt"] != "" {
		t.Fatalf("mark survived close: %q", cl["wt"])
	}
	if err := s.close("wt"); err == nil {
		t.Fatal("closing a closed workspace should fail")
	}
}

func TestSessionsCloseWithLastPane(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	s := newSessions("", (&recorder{}).emit)
	ps := s.ensure(Workspace{Name: "w", Dir: t.TempDir(), Panes: []string{"true"}}, "")
	if err := s.start(ps[0].ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	// The workspace closes when its last pane exits.
	waitFor(t, "workspace to close", func() bool { return !s.isOpen("w") })
}

func TestTabsOpenAndCloseWorkspace(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	s := newSessions("", (&recorder{}).emit)
	ws := Workspace{Name: "w", Dir: t.TempDir(), Panes: []string{"claude", ""}}

	// A tab on a closed workspace opens it with just that tab.
	a := s.newTab(ws, "")
	if !s.isOpen("w") || a.Name != "zsh" || a.Kind != "shell" {
		t.Fatalf("after newTab: open=%v pane=%+v", s.isOpen("w"), a)
	}
	b := s.newTab(ws, "claude --model opus")
	if open, _ := s.snapshot(); len(open["w"]) != 2 || b.Name != "claude" || b.Kind != "claude" {
		t.Fatalf("panes = %+v", open["w"])
	}

	if err := s.closeTab(a.ID); err != nil {
		t.Fatal(err)
	}
	if !s.isOpen("w") {
		t.Fatal("closed with a tab left")
	}
	// Closing the last tab closes the workspace.
	if err := s.closeTab(b.ID); err != nil {
		t.Fatal(err)
	}
	if s.isOpen("w") {
		t.Fatal("still open after its last tab closed")
	}
	if err := s.closeTab(b.ID); err == nil {
		t.Fatal("closing a closed tab should fail")
	}
}

func TestClaudeMarksPerPane(t *testing.T) {
	r := &recorder{}
	s := newSessions("", r.emit)
	ps := s.ensure(Workspace{Name: "w", Dir: t.TempDir(), Panes: []string{"claude", "claude", "claude"}}, "")
	summary := func() string { _, cl := s.snapshot(); return cl["w"] }
	mark := func(i int) string { open, _ := s.snapshot(); return open["w"][i].Claude }

	// The workspace's mark is the one that most needs you: waiting > idle > working.
	for i, st := range []string{"working", "idle", "waiting"} {
		if err := s.setClaude(ps[i].ID, st); err != nil {
			t.Fatal(err)
		}
		if got := summary(); got != st {
			t.Fatalf("after %s: summary = %q", st, got)
		}
	}
	if mark(0) != "working" || mark(1) != "idle" || mark(2) != "waiting" {
		t.Fatalf("marks = %q %q %q", mark(0), mark(1), mark(2))
	}

	// Looking at a tab clears waiting and idle, not working, and only a real
	// change tells the frontend.
	s.seen(ps[2].ID)
	s.seen(ps[1].ID)
	before := r.changes
	s.seen(ps[0].ID)
	s.seen(ps[1].ID)
	if mark(0) != "working" || mark(1) != "" || mark(2) != "" || r.changes != before {
		t.Fatalf("after seen: marks = %q %q %q, changes %d -> %d", mark(0), mark(1), mark(2), before, r.changes)
	}

	// A pane's mark goes with it.
	if err := s.closeTab(ps[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := summary(); got != "" {
		t.Fatalf("mark survived its pane: %q", got)
	}
}

// atPrompt waits until a shell pane has run its startup files and reads
// commands, so nothing from a login profile is in the foreground.
func atPrompt(t *testing.T, s *sessions, r *recorder, id string) {
	t.Helper()
	s.write(id, "echo ready-$((40+2))\r")
	waitFor(t, "the shell's prompt", func() bool { return strings.Contains(r.output(), "ready-42") })
}

func TestTabBusy(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	r := &recorder{}
	s := newSessions("", r.emit)
	dir := t.TempDir()
	ws := Workspace{Name: "w", Dir: dir}

	sh := s.newTab(ws, "")
	if s.busy(sh.ID) {
		t.Fatal("a pane that hasn't started is busy")
	}
	if err := s.start(sh.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	atPrompt(t, s, r, sh.ID)
	if s.busy(sh.ID) {
		t.Fatal("an idle shell is busy")
	}
	s.write(sh.ID, "sleep 5\r")
	waitFor(t, "sleep to be in the foreground", func() bool { return s.busy(sh.ID) })
	s.write(sh.ID, "\x03") // Ctrl-C: back to the prompt
	waitFor(t, "the shell to be back at its prompt", func() bool { return !s.busy(sh.ID) })

	// A Claude tab is busy while its mark says it's working.
	fake := dir + "/claude"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cl := s.newTab(ws, fake)
	if err := s.start(cl.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	for st, want := range map[string]bool{"working": true, "idle": false, "waiting": false, "": false} {
		_ = s.setClaude(cl.ID, st)
		if got := s.busy(cl.ID); got != want {
			t.Errorf("claude %q: busy = %v, want %v", st, got, want)
		}
	}
	_ = s.close("w")
}

func TestCloseTabEndsItsProcesses(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	r := &recorder{}
	s := newSessions("", r.emit)
	p := s.newTab(Workspace{Name: "w", Dir: t.TempDir()}, "")
	if err := s.start(p.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	atPrompt(t, s, r, p.ID)
	s.write(p.ID, "sleep 30\r")
	waitFor(t, "sleep to be in the foreground", func() bool { return s.busy(p.ID) })
	s.mu.Lock()
	pid := s.panes[p.ID].cmd.Process.Pid
	job, err := foreground(s.panes[p.ID].f)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.closeTab(p.ID); err != nil {
		t.Fatal(err)
	}
	// Both the shell and the program it was running exit (and are reaped).
	waitFor(t, "the shell to exit", func() bool { return syscall.Kill(pid, 0) != nil })
	waitFor(t, "the foreground job to exit", func() bool { return syscall.Kill(-job, 0) != nil })
	if s.isOpen("w") {
		t.Fatal("still open after its only tab closed")
	}
}

func TestPaneName(t *testing.T) {
	t.Setenv("SHELL", "/usr/local/bin/fish")
	for cmd, want := range map[string]string{"": "fish", "claude --model opus": "claude", "/opt/bin/claude": "claude", "lazygit": "lazygit"} {
		if got := paneName(cmd); got != want {
			t.Errorf("paneName(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestUserShellFallback(t *testing.T) {
	t.Setenv("SHELL", "")
	want := "/bin/sh"
	if runtime.GOOS == "darwin" {
		want = "/bin/zsh" // macOS's /bin/sh is bash
	}
	if got := userShell(); got != want {
		t.Errorf("userShell() = %q, want %q", got, want)
	}
}

func TestPaneEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GROVE_PANE", "p99")
	t.Setenv("HOME", "/home/x")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	env := strings.Join(paneEnv("ws", "p1", "/s.sock"), "\n") + "\n"
	exe, _ := groveExe()
	for _, want := range []string{"HOME=/home/x\n", "TERM=xterm-256color\n", "TERM_PROGRAM=grove\n", "GROVE_PANE=p1\n", "GROVE_SOCK=/s.sock\n", "GROVE_WORKSPACE=ws\n", "GROVE_BIN=" + exe + "\n", "CLAUDE_CODE_USE_BEDROCK=1\n"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
	for _, bad := range []string{"TMUX=", "TERM_PROGRAM=ghostty", "GROVE_PANE=p99", "CLAUDE_CODE_CHILD_SESSION="} {
		if strings.Contains(env, bad) {
			t.Errorf("env leaked %q", bad)
		}
	}
}

func TestPaneKind(t *testing.T) {
	for cmd, want := range map[string]string{"claude": "claude", " claude --model opus": "claude", "/opt/bin/claude": "claude", "": "shell", "lazygit": "shell", "claude-foo": "shell"} {
		if got := paneKind(cmd); got != want {
			t.Errorf("paneKind(%q) = %q, want %q", cmd, got, want)
		}
	}
}
