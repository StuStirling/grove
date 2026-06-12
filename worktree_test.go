package main

import "testing"

func TestParseWorktrees(t *testing.T) {
	out := `worktree /repo/main
HEAD aaaa
branch refs/heads/develop

worktree /repo/wt/feature
HEAD bbbb
branch refs/heads/f/MON-1/2-x

worktree /repo/bare
bare

worktree /repo/detached
HEAD cccc
detached
`
	got := parseWorktrees(out)
	if len(got) != 4 {
		t.Fatalf("want 4 worktrees, got %d", len(got))
	}
	if got[0].Path != "/repo/main" || got[0].Branch != "develop" {
		t.Errorf("main: %+v", got[0])
	}
	if got[1].Branch != "f/MON-1/2-x" {
		t.Errorf("feature branch: %q", got[1].Branch)
	}
	if !got[2].Bare {
		t.Errorf("bare not detected: %+v", got[2])
	}
	if got[3].Branch != "" {
		t.Errorf("detached should have empty branch: %q", got[3].Branch)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"kabul":        "kabul",
		"gradle-9.5.0": "gradle-9-5-0",
		"a:b c":        "a-b-c",
		"f/MON-3446/2": "f/MON-3446/2", // slashes preserved
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceFor(t *testing.T) {
	// Discovered worktrees carry the owning repo path (used by remove); the
	// prefix is folded into the name.
	ws := workspaceFor(Repo{Path: "/repo", Prefix: "android"}, "/repo/wt/feature", "f/x")
	if ws.Name != "android/feature" {
		t.Errorf("name = %q, want android/feature", ws.Name)
	}
	if ws.RepoPath != "/repo" {
		t.Errorf("RepoPath = %q, want /repo", ws.RepoPath)
	}
	if ws.Dir != "/repo/wt/feature" || ws.Branch != "f/x" {
		t.Errorf("dir/branch: %+v", ws)
	}
}

func TestDefaultBase(t *testing.T) {
	if got := defaultBase(Repo{Base: "origin/main"}); got != "origin/main" {
		t.Errorf("explicit base: %q", got)
	}
	if got := defaultBase(Repo{}); got != "origin/develop" {
		t.Errorf("empty base fallback: %q", got)
	}
	if got := defaultBase(Repo{Base: "  "}); got != "origin/develop" {
		t.Errorf("blank base fallback: %q", got)
	}
}
