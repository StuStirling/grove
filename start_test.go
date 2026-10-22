package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// startRepo makes a repo and a clone of it, so the clone has an origin, and
// returns the clone as a Repo whose new worktrees go under root.
func startRepo(t *testing.T) (origin string, r Repo, root string) {
	t.Helper()
	origin = gitRepo(t)
	clone := filepath.Join(filepath.Dir(origin), "clone")
	gitT(t, "", "clone", "-q", origin, clone)
	root = filepath.Join(filepath.Dir(origin), "wt")
	return origin, Repo{Path: clone, WorktreeRoot: root, Base: "origin/trunk"}, root
}

func TestPlanWorktree(t *testing.T) {
	origin, r, root := startRepo(t)
	gitT(t, r.Path, "branch", "stu/local")
	gitT(t, origin, "branch", "stu/remote")
	gitT(t, r.Path, "fetch", "-q", "origin")
	other := filepath.Join(filepath.Dir(origin), "elsewhere")
	gitT(t, r.Path, "worktree", "add", "-q", "-b", "stu/reuse", other)
	if err := os.MkdirAll(filepath.Join(root, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := func(p string) string { p, _ = filepath.EvalSymlinks(p); return p }
	for _, c := range []struct{ branch, name, action, path, reason string }{
		{"stu/new", "eng-1", "create", filepath.Join(root, "eng-1"), ""},
		{"stu/local", "local", "checkout", filepath.Join(root, "local"), ""},
		{"stu/remote", "remote", "checkout", filepath.Join(root, "remote"), ""},
		{"stu/reuse", "anything", "reuse", real(other), ""},
		{"trunk", "main", "reuse", real(r.Path), ""}, // the main checkout is on it
		{"stu/new", "taken", "conflict", "", "already exists"},
		{"stu/new", "a/b", "conflict", "", "path separator"},
		{"stu/bad..name", "bad", "conflict", "", "not a valid branch name"},
	} {
		p := planWorktree(r, c.branch, c.name)
		if p.Action != c.action || (c.path != "" && p.Path != c.path) || !strings.Contains(p.Reason, c.reason) {
			t.Errorf("planWorktree(%q, %q) = %+v, want %s %s %q", c.branch, c.name, p, c.action, c.path, c.reason)
		}
	}
}

func TestStartWorktreeFetchesPushedBranch(t *testing.T) {
	origin, r, _ := startRepo(t)
	gitT(t, origin, "checkout", "-q", "-b", "stu/pushed")
	writeT(t, filepath.Join(origin, "b.txt"), "b")
	gitT(t, origin, "add", ".")
	gitT(t, origin, "commit", "-qm", "pushed")
	gitT(t, origin, "checkout", "-q", "trunk")
	// The clone hasn't fetched since, so only a fetch finds the branch.
	ws, plan, err := startWorktree(r, "stu/pushed", "pushed")
	if err != nil || plan.Action != "checkout" || ws.Branch != "stu/pushed" {
		t.Fatalf("startWorktree = %+v, %+v, %v", ws, plan, err)
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "b.txt")); err != nil {
		t.Errorf("the pushed branch's file is missing: %v", err)
	}
}

func TestStartWorktreeCreateReuseConflict(t *testing.T) {
	_, r, root := startRepo(t)
	ws, plan, err := startWorktree(r, "stu/eng-2", "eng-2")
	if err != nil || plan.Action != "create" || ws.Dir != realPath(filepath.Join(root, "eng-2")) {
		t.Fatalf("create: %+v, %+v, %v", ws, plan, err)
	}
	// Starting the same ticket again reuses its worktree.
	again, plan, err := startWorktree(r, "stu/eng-2", "eng-2")
	if err != nil || plan.Action != "reuse" || again.Dir != ws.Dir {
		t.Errorf("again: %+v, %+v, %v", again, plan, err)
	}
	if _, _, err := startWorktree(r, "stu/eng-3", "eng-2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("same folder, other branch: %v", err)
	}
}

func TestStartRepo(t *testing.T) {
	_, r, root := startRepo(t)
	r.Panes, r.Setup = []string{"claude", ""}, "make setup"
	a := newTestApp(&Config{Repo: []Repo{r}})

	// The lead: seeded, with the other repos' instructions loaded.
	dir, err := a.StartRepo(r.Path, "stu/lead", "lead", Seed{Prompt: "plan it", AddDirs: []string{"/other"}})
	if err != nil || dir != realPath(filepath.Join(root, "lead")) {
		t.Fatalf("StartRepo = %q, %v", dir, err)
	}
	ps := a.sess.open[dir]
	args, env := seedArgs("plan it", []string{"/other"})
	if ps[0].args != args || !slices.Equal(ps[0].env, append(env, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1")) || ps[1].setup != "make setup" {
		t.Errorf("lead panes: args %q env %q setup %q", ps[0].args, ps[0].env, ps[1].setup)
	}
	// Another repo: no seed, setup queued.
	dir, _ = a.StartRepo(r.Path, "stu/other", "other", Seed{})
	if ps := a.sess.open[dir]; ps[0].args != "" || ps[0].env != nil || ps[1].setup != "make setup" {
		t.Errorf("non-lead panes: %+v %+v", ps[0], ps[1])
	}
	// A lead alone: no other repos' instructions to load.
	dir, _ = a.StartRepo(r.Path, "stu/solo", "solo", Seed{Prompt: "p"})
	if env := a.sess.open[dir][0].env; slices.Contains(env, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1") {
		t.Errorf("solo lead env = %q", env)
	}
	if p, err := a.PlanRepo(r.Path, "stu/lead", "x"); err != nil || p.Action != "reuse" {
		t.Errorf("PlanRepo = %+v, %v", p, err)
	}
	if !slices.ContainsFunc(shortcutInfos(), func(s ShortcutInfo) bool { return s.Label == "Start Work from Linear" }) {
		t.Error("no Start Work from Linear shortcut")
	}
}
