package main

import "testing"

func TestShellPaneIndex(t *testing.T) {
	cases := []struct {
		panes []string
		want  int
	}{
		{[]string{"claude", "", "lazygit"}, 2}, // "" is config idx 1 -> pane 2
		{[]string{"", "claude"}, 1},            // "" first -> pane 1
		{[]string{"claude", "lazygit"}, 1},     // no empty -> fall back to big pane 1
		{nil, 1},
	}
	for _, c := range cases {
		if got := shellPaneIndex(c.panes); got != c.want {
			t.Errorf("shellPaneIndex(%v) = %d, want %d", c.panes, got, c.want)
		}
	}
}

func TestExpandTerminal(t *testing.T) {
	cases := []struct {
		tmpl, cmd, want string
	}{
		{"ghostty -e {cmd}", "tmux attach -t grove", "ghostty -e tmux attach -t grove"},
		{"wezterm start -- {cmd}", "tmux attach -t grove", "wezterm start -- tmux attach -t grove"},
		{"kitty", "tmux attach -t grove", "kitty tmux attach -t grove"}, // no placeholder -> append
	}
	for _, c := range cases {
		if got := expandTerminal(c.tmpl, c.cmd); got != c.want {
			t.Errorf("expandTerminal(%q,%q) = %q, want %q", c.tmpl, c.cmd, got, c.want)
		}
	}
}
