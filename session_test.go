package main

import (
	"encoding/base64"
	"runtime"
	"strings"
	"sync"
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
