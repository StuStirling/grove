package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// syncRepo is startRepo plus a worktree of the clone on a new branch feat,
// returning the origin, the clone and the worktree's dir.
func syncRepo(t *testing.T) (origin, clone, dir string) {
	t.Helper()
	origin, r, _ := startRepo(t)
	dir = filepath.Join(filepath.Dir(origin), "feat")
	gitT(t, r.Path, "worktree", "add", "-q", "--no-track", "-b", "feat", dir, "origin/trunk")
	return origin, r.Path, dir
}

// commitT writes file=body in dir and commits it.
func commitT(t *testing.T, dir, file, body string) {
	t.Helper()
	writeT(t, filepath.Join(dir, file), body)
	gitT(t, dir, "add", file)
	gitT(t, dir, "commit", "-qm", file+"="+body)
}

func headT(t *testing.T, dir string) string {
	t.Helper()
	return revParse(dir, "HEAD")
}

// conflictRepo makes feat and origin's trunk change a.txt differently, with
// the change on trunk fetched.
func conflictRepo(t *testing.T) (origin, clone, dir string) {
	t.Helper()
	origin, clone, dir = syncRepo(t)
	commitT(t, dir, "a.txt", "feat")
	commitT(t, origin, "a.txt", "trunk")
	gitT(t, clone, "fetch", "-q", "origin")
	return origin, clone, dir
}

func TestOperation(t *testing.T) {
	_, clone, dir := conflictRepo(t)
	if got := operation(dir); got != "" {
		t.Fatalf("clean: %q", got)
	}
	if got := operation(clone); got != "" {
		t.Fatalf("main checkout: %q", got)
	}
	if inProgress(dir, "feat") != nil {
		t.Fatal("clean worktree reports progress")
	}
	if runGit(dir, "rebase", "origin/trunk") == nil {
		t.Fatal("rebase should conflict")
	}
	if got := operation(dir); got != "rebase" {
		t.Fatalf("mid rebase: %q", got)
	}
	p := inProgress(dir, "feat")
	if p == nil || p.Kind != "rebase" || !slices.Equal(p.Files, []string{"a.txt"}) {
		t.Fatalf("rebase progress: %+v", p)
	}
	gitT(t, dir, "rebase", "--abort")
	if runGit(dir, "merge", "--no-edit", "origin/trunk") == nil {
		t.Fatal("merge should conflict")
	}
	if got := operation(dir); got != "merge" {
		t.Fatalf("mid merge: %q", got)
	}
	// The main checkout's own state is separate from its linked worktree's.
	if got := operation(clone); got != "" {
		t.Fatalf("main checkout during worktree merge: %q", got)
	}
}

func TestPushKind(t *testing.T) {
	_, _, dir := syncRepo(t)
	if got := pushKind(dir); got != "upstream" {
		t.Fatalf("no upstream: %q", got)
	}
	if err := setUpstream(dir, "feat", "origin"); err != nil {
		t.Fatal(err)
	}
	if got := pushKind(dir); got != "upstream" {
		t.Fatalf("upstream configured, never pushed: %q", got)
	}
	commitT(t, dir, "b.txt", "b")
	gitT(t, dir, "push", "-q", "-u", "origin", "feat")
	if got := pushKind(dir); got != "" {
		t.Fatalf("pushed: %q", got)
	}
	commitT(t, dir, "c.txt", "c")
	if got := pushKind(dir); got != "push" {
		t.Fatalf("one new commit: %q", got)
	}
	gitT(t, dir, "push", "-q")
	gitT(t, dir, "commit", "-q", "--amend", "-m", "amended")
	if got := pushKind(dir); got != "force" {
		t.Fatalf("amended pushed commit: %q", got)
	}
}

func TestGitDir(t *testing.T) {
	_, clone, dir := syncRepo(t)
	if g := gitDir(clone); !strings.HasSuffix(g, ".git") {
		t.Errorf("main checkout git dir: %q", g)
	}
	if g := gitDir(dir); !strings.Contains(g, filepath.Join(".git", "worktrees")) {
		t.Errorf("linked worktree git dir: %q", g)
	}
	if g := gitDir(t.TempDir()); g != "" {
		t.Errorf("not a repo: %q", g)
	}
}

func TestStateGitMark(t *testing.T) {
	_, clone, dir := conflictRepo(t)
	a := &App{ws: []Workspace{{Name: "feat", Dir: dir, Branch: "feat", RepoPath: clone}}, sess: newSessions("", func(string, ...any) {}), cfg: &Config{}, own: &Config{}}
	if g := a.State().Workspaces[0].Git; g != "" {
		t.Fatalf("clean: %q", g)
	}
	_ = runGit(dir, "rebase", "origin/trunk")
	if g := a.State().Workspaces[0].Git; g != "rebase" {
		t.Fatalf("mid rebase: %q", g)
	}
}

func TestParsePR(t *testing.T) {
	pr := func(state string, reviewers ...string) []byte {
		var rs []string
		for _, r := range reviewers {
			rs = append(rs, `{"author":{"login":"`+r+`"}}`)
		}
		return []byte(`{"number":12,"state":"` + state + `","author":{"login":"stu"},"reviews":[` + strings.Join(rs, ",") + `]}`)
	}
	ghErr := func(out string) error { return &gitError{"gh pr view", out} }
	for _, c := range []struct {
		name     string
		out      []byte
		err      error
		state    string
		reviewer string
	}{
		{"open, no reviews", pr("OPEN"), nil, "unreviewed", ""},
		{"open, reviewed by alice", pr("OPEN", "stu", "alice"), nil, "reviewed", "alice"},
		{"open, only the author", pr("OPEN", "stu"), nil, "unreviewed", ""},
		{"closed", pr("CLOSED", "alice"), nil, "none", ""},
		{"merged", pr("MERGED", "alice"), nil, "merged", ""},
		{"no PR", nil, ghErr(`no pull requests found for branch "feat"`), "none", ""},
		{"no gh", nil, exec.ErrNotFound, "unknown", ""},
		{"logged out", nil, ghErr("To get started with GitHub CLI, please run:  gh auth login"), "unknown", ""},
		{"bad json", []byte("{"), nil, "unknown", ""},
	} {
		got := parsePR(c.out, c.err)
		if got.State != c.state || got.Reviewer != c.reviewer {
			t.Errorf("%s: %+v, want %s/%q", c.name, got, c.state, c.reviewer)
		}
		if got.State == "unknown" && got.Detail == "" {
			t.Errorf("%s: unknown without a detail", c.name)
		}
		if (got.State == "unreviewed" || got.State == "reviewed" || got.State == "merged") && got.Number != 12 {
			t.Errorf("%s: number %d", c.name, got.Number)
		}
	}
	if d := parsePR(nil, exec.ErrNotFound).Detail; !strings.Contains(d, "gh") {
		t.Errorf("missing gh detail: %q", d)
	}
}

func TestStrategyFor(t *testing.T) {
	for state, want := range map[string]string{"none": "rebase", "unreviewed": "rebase", "merged": "rebase", "reviewed": "merge", "unknown": ""} {
		if got := strategyFor(state); got != want {
			t.Errorf("%s: %q, want %q", state, got, want)
		}
	}
}

// stubPR makes prView answer with out/err for the rest of the test.
func stubPR(t *testing.T, out string, err error) {
	t.Helper()
	old := prView
	prView = func(string, string) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { prView = old })
}

const noPR = `no pull requests found for branch "feat"`

// pushT pushes feat from dir and makes it track origin/feat.
func pushT(t *testing.T, dir string) {
	t.Helper()
	gitT(t, dir, "push", "-q", "-u", "origin", "feat")
}

// otherCommitT commits file=body onto origin's feat from a second clone.
func otherCommitT(t *testing.T, origin, file, body string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gitT(t, "", "clone", "-q", "-b", "feat", origin, other)
	commitT(t, other, file, body)
	gitT(t, other, "push", "-q", "origin", "feat")
}

func TestPlanRefusals(t *testing.T) {
	stubPR(t, "", &gitError{"gh", noPR})

	_, _, dir := conflictRepo(t)
	_ = runGit(dir, "rebase", "origin/trunk")
	p := planUpdate(dir, "feat", "origin/trunk")
	if p.Progress == nil || p.Progress.Kind != "rebase" || len(p.Steps) != 0 {
		t.Errorf("in progress: %+v", p)
	}

	_, _, dir = syncRepo(t)
	writeT(t, filepath.Join(dir, "a.txt"), "changed")
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Refusal != "Commit or stash first" {
		t.Errorf("dirty: %+v", p)
	}
	gitT(t, dir, "checkout", "-q", "a.txt")
	writeT(t, filepath.Join(dir, "untracked.txt"), "x")
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Refusal != "" {
		t.Errorf("untracked files alone refused: %+v", p)
	}
	gitT(t, dir, "checkout", "-q", "--detach")
	if p := planUpdate(dir, "", "origin/trunk"); p.Refusal != "Not on a branch" {
		t.Errorf("detached: %+v", p)
	}

	repo := gitRepo(t)
	if p := planUpdate(repo, "trunk", "origin/trunk"); p.Refusal != "No origin/trunk to update from" {
		t.Errorf("no base ref: %+v", p)
	}
}

func TestPlanSteps(t *testing.T) {
	stubPR(t, "", &gitError{"gh", noPR})
	step := func(kind, ref string, n int) UpdateStep { return UpdateStep{Kind: kind, Ref: ref, Count: n} }

	origin, clone, dir := syncRepo(t)
	commitT(t, origin, "t1.txt", "t1") // not yet fetched: the plan fetches
	p := planUpdate(dir, "feat", "origin/trunk")
	if !slices.Equal(p.Steps, []UpdateStep{step("base", "origin/trunk", 1)}) || p.Head != headT(t, dir) {
		t.Errorf("behind base: %+v", p)
	}

	pushT(t, dir)
	otherCommitT(t, origin, "f1.txt", "f1")
	p = planUpdate(dir, "feat", "origin/trunk")
	if !slices.Equal(p.Steps, []UpdateStep{step("pull", "origin/feat", 1), step("base", "origin/trunk", 1)}) {
		t.Errorf("remote ahead and behind base: %+v", p.Steps)
	}

	gitT(t, dir, "pull", "-q", "--no-rebase", "--no-edit")
	gitT(t, dir, "merge", "-q", "--no-edit", "origin/trunk")
	gitT(t, dir, "push", "-q")
	if p := planUpdate(dir, "feat", "origin/trunk"); len(p.Steps) != 0 || p.Push != "" {
		t.Errorf("up to date: %+v", p)
	}
	commitT(t, dir, "l.txt", "l")
	if p := planUpdate(dir, "feat", "origin/trunk"); len(p.Steps) != 0 || p.Push != "push" {
		t.Errorf("up to date with a local commit: %+v", p)
	}

	commitT(t, origin, "t2.txt", "t2")
	// t1 and t2 are both unpulled on the clone's trunk.
	if p := planUpdate(clone, "trunk", "origin/trunk"); !slices.Equal(p.Steps, []UpdateStep{step("ff", "origin/trunk", 2)}) {
		t.Errorf("main checkout behind: %+v", p.Steps)
	}
}

func TestPlanStrategy(t *testing.T) {
	_, _, dir := syncRepo(t)
	open := func(reviewer string) string {
		rs := ""
		if reviewer != "" {
			rs = `{"author":{"login":"` + reviewer + `"}}`
		}
		return `{"number":7,"state":"OPEN","author":{"login":"stu"},"reviews":[` + rs + `]}`
	}
	stubPR(t, open("alice"), nil)
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Strategy != "merge" || p.PR.Reviewer != "alice" {
		t.Errorf("reviewed: %+v", p)
	}
	stubPR(t, open(""), nil)
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Strategy != "rebase" {
		t.Errorf("unreviewed: %+v", p)
	}
	stubPR(t, `{"number":7,"state":"MERGED","author":{"login":"stu"},"reviews":[]}`, nil)
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Strategy != "rebase" || len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "already merged") {
		t.Errorf("merged: %+v", p)
	}
	stubPR(t, "", exec.ErrNotFound)
	if p := planUpdate(dir, "feat", "origin/trunk"); p.Strategy != "" || p.PR.State != "unknown" {
		t.Errorf("unknown: %+v", p)
	}
}

func TestPlanFetchFailure(t *testing.T) {
	stubPR(t, "", &gitError{"gh", noPR})
	origin, clone, dir := syncRepo(t)
	commitT(t, origin, "t1.txt", "t1")
	gitT(t, clone, "fetch", "-q")
	gitT(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing"))
	p := planUpdate(dir, "feat", "origin/trunk")
	if len(p.Warnings) != 1 || !strings.HasPrefix(p.Warnings[0], "Couldn't fetch origin") {
		t.Errorf("warnings: %q", p.Warnings)
	}
	if len(p.Steps) != 1 || p.Steps[0].Kind != "base" {
		t.Errorf("steps from known refs: %+v", p.Steps)
	}
}

func TestRunRefusesMovedHead(t *testing.T) {
	_, _, dir := syncRepo(t)
	r := runUpdate(dir, "feat", "origin/trunk", "0000", "rebase")
	if r.Status != "failed" || r.Reason != "Branch changed since planning; check again" {
		t.Errorf("%+v", r)
	}
}

// behindT pushes a commit on feat, then adds a fetched commit to trunk.
func behindT(t *testing.T) (origin, clone, dir string) {
	t.Helper()
	origin, clone, dir = syncRepo(t)
	commitT(t, dir, "f.txt", "f")
	pushT(t, dir)
	commitT(t, origin, "t.txt", "t")
	gitT(t, clone, "fetch", "-q")
	return origin, clone, dir
}

func TestRunRebaseThenPush(t *testing.T) {
	_, _, dir := behindT(t)
	r := runUpdate(dir, "feat", "origin/trunk", headT(t, dir), "rebase")
	if r.Status != "done" || r.Push != "force" {
		t.Fatalf("%+v", r)
	}
	if err := pushUpdate(dir, "feat", "origin/trunk", "force"); err != nil {
		t.Fatal(err)
	}
	if k := pushKind(dir); k != "" {
		t.Errorf("after force push: %q", k)
	}
}

func TestRunMergeNoEditor(t *testing.T) {
	t.Setenv("GIT_EDITOR", "false") // an editor launch fails the merge
	_, _, dir := behindT(t)
	r := runUpdate(dir, "feat", "origin/trunk", headT(t, dir), "merge")
	if r.Status != "done" || r.Push != "push" {
		t.Fatalf("%+v", r)
	}
	if revParse(dir, "HEAD^2") == "" {
		t.Error("HEAD is not a merge commit")
	}
}

func TestRunConflictContinueAbort(t *testing.T) {
	t.Setenv("GIT_EDITOR", "false")
	for _, strategy := range []string{"rebase", "merge"} {
		_, _, dir := conflictRepo(t)
		pre := headT(t, dir)
		r := runUpdate(dir, "feat", "origin/trunk", pre, strategy)
		if r.Status != "stopped" || r.Progress == nil || r.Progress.Kind != strategy || !slices.Equal(r.Progress.Files, []string{"a.txt"}) {
			t.Fatalf("%s: run %+v", strategy, r)
		}
		if r := continueUpdate(dir, "feat"); r.Status != "stopped" || !slices.Equal(r.Progress.Files, []string{"a.txt"}) {
			t.Fatalf("%s: continue unresolved %+v", strategy, r)
		}
		writeT(t, filepath.Join(dir, "a.txt"), "both")
		gitT(t, dir, "add", "a.txt")
		if r := continueUpdate(dir, "feat"); r.Status != "done" || operation(dir) != "" {
			t.Fatalf("%s: continue resolved %+v", strategy, r)
		}

		gitT(t, dir, "reset", "-q", "--hard", pre)
		if r := runUpdate(dir, "feat", "origin/trunk", pre, strategy); r.Status != "stopped" {
			t.Fatalf("%s: second run %+v", strategy, r)
		}
		if err := abortUpdate(dir); err != nil {
			t.Fatal(err)
		}
		if operation(dir) != "" || headT(t, dir) != pre {
			t.Errorf("%s: abort left %q at %s", strategy, operation(dir), headT(t, dir))
		}
	}
}

func TestRunFFOnlyDiverged(t *testing.T) {
	origin, clone, _ := syncRepo(t)
	commitT(t, clone, "local.txt", "l")
	commitT(t, origin, "t.txt", "t")
	gitT(t, clone, "fetch", "-q")
	r := runUpdate(clone, "trunk", "origin/trunk", headT(t, clone), "rebase")
	if r.Status != "failed" || r.Progress != nil || r.Reason == "" {
		t.Errorf("%+v", r)
	}
}

func TestPushUpstream(t *testing.T) {
	_, _, dir := syncRepo(t)
	commitT(t, dir, "f.txt", "f")
	if err := pushUpdate(dir, "feat", "origin/trunk", "upstream"); err != nil {
		t.Fatal(err)
	}
	if u := upstreamName(dir); u != "origin/feat" {
		t.Errorf("upstream %q", u)
	}
}

func TestPushStaleLease(t *testing.T) {
	origin, _, dir := syncRepo(t)
	commitT(t, dir, "f.txt", "f")
	pushT(t, dir)
	otherCommitT(t, origin, "o.txt", "o") // not fetched here
	gitT(t, dir, "commit", "-q", "--amend", "-m", "amended")
	err := pushUpdate(dir, "feat", "origin/trunk", "force")
	if err == nil || err.Error() != "Remote changed since you fetched. Run Update again." {
		t.Errorf("%v", err)
	}
}

func TestConflictPrompt(t *testing.T) {
	for _, c := range []struct {
		kind  string
		files []string
		want  string
	}{
		{"rebase", []string{"a.txt", "b.txt"}, "A rebase of feat stopped on conflicts in a.txt, b.txt. Resolve them keeping the intent of both sides, stage the files, and run `GIT_EDITOR=true git rebase --continue`. Repeat if later commits conflict. Don't push."},
		{"merge", []string{"a.txt"}, "A merge into feat stopped on conflicts in a.txt. Resolve them keeping the intent of both sides, stage the files, and run `GIT_EDITOR=true git commit --no-edit`. Don't push."},
		{"rebase", nil, "A rebase of feat stopped with no conflicted files listed. Check git status, fix what stopped it, and run `GIT_EDITOR=true git rebase --continue`. Repeat if later commits conflict. Don't push."},
	} {
		if got := conflictPrompt(c.kind, "feat", c.files); got != c.want {
			t.Errorf("%s %v:\n got %q\nwant %q", c.kind, c.files, got, c.want)
		}
	}
	_, _, dir := conflictRepo(t)
	_ = runGit(dir, "rebase", "origin/trunk")
	if p := inProgress(dir, "feat"); p.Prompt != conflictPrompt("rebase", "feat", []string{"a.txt"}) {
		t.Errorf("inProgress prompt: %q", p.Prompt)
	}
}

func TestAskClaude(t *testing.T) {
	_, clone, dir := conflictRepo(t)
	_ = runGit(dir, "rebase", "origin/trunk")
	prompt := inProgress(dir, "feat").Prompt
	ws := Workspace{Name: "feat", Dir: dir, Branch: "feat", RepoPath: clone, Panes: []string{"claude", ""}}
	newApp := func() *App { return &App{ws: []Workspace{ws}, sess: newSessions("", func(string, ...any) {})} }
	seeded := func(a *App, id string) bool {
		p := a.sess.panes[id]
		return p != nil && p.Kind == "claude" && strings.Contains(p.args, "GROVE_SEED_PROMPT") && slices.Contains(p.env, "GROVE_SEED_PROMPT="+prompt)
	}

	// Not open: its own Claude pane starts on the prompt.
	a := newApp()
	id, err := a.AskClaude(dir)
	if err != nil || !seeded(a, id) || a.sess.panes[id].Cmd != "claude" {
		t.Fatalf("not open: %q %v", id, err)
	}

	// Open, Claude pane not started yet: same.
	a = newApp()
	ps := a.sess.ensure(ws, "")
	if id, err := a.AskClaude(dir); err != nil || id != ps[0].ID || !seeded(a, id) {
		t.Fatalf("open, unstarted: %q %v", id, err)
	}

	// Open and running: typed in, not submitted.
	a = newApp()
	ps = a.sess.ensure(ws, "")
	r, w, _ := os.Pipe()
	a.sess.panes[ps[0].ID].f = w
	if id, err := a.AskClaude(dir); err != nil || id != ps[0].ID {
		t.Fatalf("running: %q %v", id, err)
	}
	w.Close()
	if got, _ := io.ReadAll(r); string(got) != prompt {
		t.Errorf("typed %q", got)
	}

	// Not mid-operation: nothing to ask.
	gitT(t, dir, "rebase", "--abort")
	if _, err := newApp().AskClaude(dir); err == nil {
		t.Error("no operation: want an error")
	}
}

func TestUpdateShortcut(t *testing.T) {
	seen := map[string]string{}
	found := false
	for _, s := range shortcuts {
		if s.key == "" {
			continue
		}
		k := accelLabel(accel(s.key, s.mods))
		if prev, dup := seen[k]; dup {
			t.Errorf("%s is both %s and %s", k, prev, s.action)
		}
		seen[k] = s.action
		found = found || (s.action == "update" && s.key == "u" && len(s.mods) == 0)
	}
	if !found {
		t.Error("no ⌘U update shortcut")
	}
}

// Review fixes.

func TestReplanAfterRebaseOfPushedBranch(t *testing.T) {
	_, _, dir := conflictRepo(t)
	pushT(t, dir)
	if r := runUpdate(dir, "feat", "origin/trunk", headT(t, dir), "rebase"); r.Status != "stopped" {
		t.Fatalf("run %+v", r)
	}
	writeT(t, filepath.Join(dir, "a.txt"), "both")
	gitT(t, dir, "add", "a.txt")
	if r := continueUpdate(dir, "feat"); r.Status != "done" {
		t.Fatalf("continue %+v", r)
	}
	if s := stepsFor(dir, "feat", "origin/trunk"); len(s) != 0 {
		t.Errorf("the pre-rebase commits came back as a pull: %+v", s)
	}
	if k := pushKind(dir); k != "force" {
		t.Errorf("push kind %q", k)
	}
}

func TestReplanStillPullsTeammateCommits(t *testing.T) {
	origin, clone, dir := behindT(t)
	if r := runUpdate(dir, "feat", "origin/trunk", headT(t, dir), "rebase"); r.Status != "done" {
		t.Fatalf("run %+v", r)
	}
	otherCommitT(t, origin, "o.txt", "o")
	gitT(t, clone, "fetch", "-q")
	if s := stepsFor(dir, "feat", "origin/trunk"); len(s) != 1 || s[0].Kind != "pull" || s[0].Count != 1 {
		t.Errorf("teammate's commit: %+v", s)
	}
}

func TestForcePushRefusesFetchedButUnseenCommits(t *testing.T) {
	origin, clone, dir := syncRepo(t)
	commitT(t, dir, "f.txt", "f")
	pushT(t, dir)
	otherCommitT(t, origin, "o.txt", "o")
	gitT(t, clone, "fetch", "-q") // a background fetch: the lease alone now passes
	gitT(t, dir, "commit", "-q", "--amend", "-m", "amended")
	if err := pushUpdate(dir, "feat", "origin/trunk", "force"); err != errStaleLease {
		t.Errorf("want stale lease, got %v", err)
	}
}

func TestWorktreesOfMidRebaseKeepsBranch(t *testing.T) {
	_, clone, dir := conflictRepo(t)
	_ = runGit(dir, "rebase", "origin/trunk")
	wts, err := worktreesOf(clone)
	if err != nil {
		t.Fatal(err)
	}
	for _, wt := range wts {
		if filepath.Base(wt.Path) == "feat" && wt.Branch != "feat" {
			t.Errorf("mid-rebase worktree branch %q", wt.Branch)
		}
	}
}

func TestStoppedResultSaysWhy(t *testing.T) {
	_, _, dir := conflictRepo(t)
	_ = runUpdate(dir, "feat", "origin/trunk", headT(t, dir), "merge")
	if r := continueUpdate(dir, "feat"); r.Status != "stopped" || r.Reason == "" {
		t.Errorf("continue with unresolved files: %+v", r)
	}
}

func TestUpstreamOnBaseIsNotOwnUpstream(t *testing.T) {
	origin, clone, dir := syncRepo(t)
	gitT(t, dir, "branch", "-q", "--set-upstream-to", "origin/trunk")
	commitT(t, origin, "t.txt", "t")
	gitT(t, clone, "fetch", "-q")
	if s := stepsFor(dir, "feat", "origin/trunk"); len(s) != 1 || s[0].Kind != "base" {
		t.Errorf("base duplicated as a pull: %+v", s)
	}
	if k := pushKind(dir); k != "upstream" {
		t.Errorf("push kind %q, want upstream (own branch not pushed yet)", k)
	}
}
