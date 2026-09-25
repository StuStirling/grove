package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// gitRepo makes a real repo with one commit, isolated from the user's git
// config (signing, hooks), for the removal tests.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	// A git hook exports these for its own repo; left set, they'd aim every git
	// command here at it. Setenv first so the test restores them.
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	gitT(t, "", "init", "-q", "--initial-branch=trunk", repo)
	writeT(t, filepath.Join(repo, "a.txt"), "a")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "init")
	return repo
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeT(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addWorktree adds a worktree on a new branch, and an App that lists it.
func addWorktree(t *testing.T, repo, branch string) (*App, Workspace) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(repo), branch)
	gitT(t, repo, "worktree", "add", "-q", "-b", branch, dir)
	ws := Workspace{Name: branch, Dir: dir, Branch: branch, RepoPath: repo}
	return &App{ws: []Workspace{ws}, sess: newSessions("", func(string, ...any) {})}, ws
}

func TestRemoveDirtyThenForce(t *testing.T) {
	repo := gitRepo(t)
	a, ws := addWorktree(t, repo, "wip")
	writeT(t, filepath.Join(ws.Dir, "a.txt"), "changed")
	writeT(t, filepath.Join(ws.Dir, "new.txt"), "new")

	r := a.Remove(ws.Name, false)
	if r.Status != "dirty" || r.Reason != "2 uncommitted changes" {
		t.Fatalf("dirty removal = %+v", r)
	}
	if !strings.Contains(r.Detail, "?? new.txt") || !strings.Contains(r.Detail, " M a.txt") {
		t.Errorf("detail lacks the changes: %q", r.Detail)
	}
	if !fileExists(ws.Dir) || !branchExists(repo, "wip") {
		t.Fatal("a dirty removal removed something")
	}

	// Forced, it goes; the branch has no commits of its own, so it goes too.
	if r := a.Remove(ws.Name, true); r.Status != "removed" || r.BranchKept != "" {
		t.Fatalf("forced removal = %+v", r)
	}
	if fileExists(ws.Dir) || branchExists(repo, "wip") {
		t.Error("forced removal left the worktree or its merged branch")
	}
}

func TestRemoveCleanDeletesMergedBranch(t *testing.T) {
	repo := gitRepo(t)
	a, ws := addWorktree(t, repo, "done")
	if r := a.Remove(ws.Name, false); r.Status != "removed" || r.BranchKept != "" {
		t.Fatalf("clean removal = %+v", r)
	}
	if fileExists(ws.Dir) || branchExists(repo, "done") {
		t.Error("clean removal left the worktree or its merged branch")
	}
}

func TestRemoveKeepsUnmergedBranch(t *testing.T) {
	repo := gitRepo(t)
	a, ws := addWorktree(t, repo, "feature")
	writeT(t, filepath.Join(ws.Dir, "b.txt"), "b")
	gitT(t, ws.Dir, "add", ".")
	gitT(t, ws.Dir, "commit", "-qm", "work")

	r := a.Remove(ws.Name, false)
	if r.Status != "removed" || r.BranchKept != "unmerged" {
		t.Fatalf("removal = %+v, want removed with the branch kept unmerged", r)
	}
	if fileExists(ws.Dir) || !branchExists(repo, "feature") {
		t.Fatal("want the worktree gone and the branch kept")
	}
	if r := a.DeleteBranch(repo, "feature", false); r.Kept != "unmerged" || !branchExists(repo, "feature") {
		t.Fatalf("safe delete of an unmerged branch = %+v", r)
	}
	if r := a.DeleteBranch(repo, "feature", true); r.Kept != "" || branchExists(repo, "feature") {
		t.Fatalf("forced delete = %+v, branch still there: %v", r, branchExists(repo, "feature"))
	}
	// Any other refusal keeps git's full text for the tooltip.
	if r := a.DeleteBranch(repo, "trunk", true); r.Kept == "" || strings.Contains(r.Kept, "error:") || !strings.Contains(r.Detail, "error:") {
		t.Fatalf("deleting the checked-out branch = %+v", r)
	}
}

func TestRemoveFailureReason(t *testing.T) {
	repo := gitRepo(t)
	// The main worktree can't be removed, even forced.
	a := &App{ws: []Workspace{{Name: "repo", Dir: repo, RepoPath: repo}}, sess: newSessions("", func(string, ...any) {})}
	r := a.Remove("repo", true)
	if r.Status != "failed" {
		t.Fatalf("removing the main worktree = %+v", r)
	}
	if r.Reason == "" || strings.Contains(r.Reason, "\n") || strings.Contains(r.Reason, "fatal:") || strings.Contains(r.Reason, "exit status") {
		t.Errorf("reason should be git's one line without its prefix: %q", r.Reason)
	}
	if !strings.Contains(r.Detail, "fatal:") {
		t.Errorf("detail should keep git's full text: %q", r.Detail)
	}
}

func TestRemoveRefusedForAnotherReason(t *testing.T) {
	// Refusals --force doesn't override are reported as they are, not as dirty,
	// even with changes: Force remove would only fail again.
	repo := gitRepo(t)
	writeT(t, filepath.Join(repo, "a.txt"), "changed")
	a := &App{ws: []Workspace{{Name: "repo", Dir: repo, RepoPath: repo}}, sess: newSessions("", func(string, ...any) {})}
	if r := a.Remove("repo", false); r.Status != "failed" || !strings.Contains(r.Reason, "is a main working tree") {
		t.Errorf("removing the dirty main worktree = %+v", r)
	}

	a, ws := addWorktree(t, repo, "locked")
	writeT(t, filepath.Join(ws.Dir, "a.txt"), "changed")
	gitT(t, repo, "worktree", "lock", ws.Dir)
	if r := a.Remove(ws.Name, false); r.Status != "failed" || !strings.Contains(r.Reason, "locked working tree") {
		t.Errorf("removing a dirty locked worktree = %+v", r)
	}
	if !fileExists(ws.Dir) {
		t.Error("the locked worktree was removed")
	}
}

func TestRemoveWithSubmodules(t *testing.T) {
	// Populated submodules alone make git refuse; that's forced through when
	// clean, and reported as dirty when there are real changes.
	repo := gitRepo(t)
	sub := filepath.Join(filepath.Dir(repo), "sub")
	gitT(t, "", "init", "-q", "--initial-branch=trunk", sub)
	gitT(t, sub, "commit", "-q", "--allow-empty", "-m", "s")
	gitT(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sm")
	gitT(t, repo, "commit", "-qm", "sm")
	for name, want := range map[string]string{"dirty": "dirty", "clean": "removed"} {
		a, ws := addWorktree(t, repo, name)
		gitT(t, ws.Dir, "-c", "protocol.file.allow=always", "submodule", "update", "-q", "--init")
		if name == "dirty" {
			writeT(t, filepath.Join(ws.Dir, "a.txt"), "changed")
		}
		if r := a.Remove(ws.Name, false); r.Status != want {
			t.Errorf("%s: removal = %+v, want %s", name, r, want)
		}
	}
}

func TestExplain(t *testing.T) {
	err := &gitError{"git branch -d x", "error: the branch 'x' is not fully merged\nhint: If you are sure you want to delete it, run 'git branch -D x'"}
	if reason, detail := explain(err); reason != "the branch 'x' is not fully merged" || detail != err.out {
		t.Errorf("explain = %q, %q", reason, detail)
	}
}
