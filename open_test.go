package main

import "testing"

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
