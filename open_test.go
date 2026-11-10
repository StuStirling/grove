package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellPaneIndex(t *testing.T) {
	cases := []struct {
		panes []string
		want  int
	}{
		{[]string{"claude", "", "lazygit"}, 1}, // first "" is the shell pane
		{[]string{"", "claude"}, 0},
		{[]string{"claude", "lazygit"}, 0}, // no shell -> fall back to the big pane
		{nil, 0},
	}
	for _, c := range cases {
		if got := shellPaneIndex(c.panes); got != c.want {
			t.Errorf("shellPaneIndex(%v) = %d, want %d", c.panes, got, c.want)
		}
	}
}

func TestExpandTerminal(t *testing.T) {
	cases := []struct {
		tmpl, want string
	}{
		{"ghostty -e {cmd}", "ghostty -e SHELL"},
		{"wezterm start -- {cmd}", "wezterm start -- SHELL"},
		{"kitty", "kitty SHELL"}, // no placeholder -> append
		{"open -na Ghostty --args --working-directory={dir}", "open -na Ghostty --args --working-directory='/w/it'\\''s'"},
	}
	for _, c := range cases {
		if got := expandTerminal(c.tmpl, "SHELL", "/w/it's"); got != c.want {
			t.Errorf("expandTerminal(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

func TestXcodeTarget(t *testing.T) {
	dir := t.TempDir()
	if got := xcodeTarget("Xcode", dir); got != dir {
		t.Errorf("no project: got %q, want the dir", got)
	}
	proj := filepath.Join(dir, "App.xcodeproj")
	_ = os.Mkdir(proj, 0o755)
	if got := xcodeTarget("Xcode", dir); got != proj {
		t.Errorf("project: got %q, want %q", got, proj)
	}
	work := filepath.Join(dir, "App.xcworkspace")
	_ = os.Mkdir(work, 0o755)
	for _, app := range []string{"Xcode", "xcode", "/Applications/Xcode.app"} {
		if got := xcodeTarget(app, dir); got != work {
			t.Errorf("%s with a workspace: got %q, want %q", app, got, work)
		}
	}
	if got := xcodeTarget("Visual Studio Code", dir); got != dir {
		t.Errorf("other app: got %q, want the dir", got)
	}
}
