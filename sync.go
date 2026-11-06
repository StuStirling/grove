package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// InProgress is a rebase or merge a worktree is stopped in the middle of.
type InProgress struct {
	Kind   string   `json:"kind"`   // "rebase" | "merge"
	Files  []string `json:"files"`  // conflicted files; may be empty
	Prompt string   `json:"prompt"` // what Ask Claude types into the worktree's Claude pane
}

// gitDir is the worktree's own git dir: dir/.git itself, or where a linked
// worktree's .git file points. Read directly, so the sidebar can check every
// worktree on each snapshot without a git process per row.
func gitDir(dir string) string {
	p := filepath.Join(dir, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return ""
	}
	if !filepath.IsAbs(g) {
		g = filepath.Join(dir, g)
	}
	return g
}

// operation is "rebase" or "merge" while the worktree at dir is in the middle
// of one, else "".
func operation(dir string) string {
	g := gitDir(dir)
	if g == "" {
		return ""
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(g, name)); return err == nil }
	switch {
	case exists("rebase-merge"), exists("rebase-apply"):
		return "rebase"
	case exists("MERGE_HEAD"):
		return "merge"
	}
	return ""
}

// inProgress describes the operation the worktree at dir (on branch) is
// stopped in, or nil if none.
func inProgress(dir, branch string) *InProgress {
	kind := operation(dir)
	if kind == "" {
		return nil
	}
	return &InProgress{Kind: kind, Files: gitLines(dir, "diff", "--name-only", "--diff-filter=U")}
}

// pushKind is the push the branch checked out at dir needs: "upstream" when it
// has no pushed upstream, "force" when its upstream isn't in its history (a
// rebase or amend), "push" when it's ahead, else "".
func pushKind(dir string) string {
	if revParse(dir, "@{u}") == "" {
		return "upstream"
	}
	if exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", "@{u}", "HEAD").Run() != nil {
		return "force"
	}
	if count(dir, "@{u}..HEAD") > 0 {
		return "push"
	}
	return ""
}

// revParse resolves rev at dir, or "" if it doesn't resolve.
func revParse(dir, rev string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", rev).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// count is the number of commits in a rev-list range, 0 if it fails.
func count(dir, rng string) int {
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", rng).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// gitLines runs a git read in dir and returns its non-empty output lines.
func gitLines(dir string, args ...string) []string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// PRInfo is what grove knows about the branch's pull request.
type PRInfo struct {
	Number   int    `json:"number"`
	State    string `json:"state"`    // "none" | "unreviewed" | "reviewed" | "merged" | "unknown"
	Reviewer string `json:"reviewer"` // first reviewer who isn't the author, when reviewed
	Detail   string `json:"detail"`   // why the state is unknown
}

// prView asks gh about branch's PR from dir. A failure is a *gitError holding
// gh's own words. A var so tests can stand in for gh.
var prView = func(dir, branch string) ([]byte, error) {
	c := exec.Command("gh", "pr", "view", branch, "--json", "number,state,author,reviews")
	c.Dir = dir
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, err
		}
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = err.Error()
		}
		return nil, &gitError{"gh pr view " + branch, text}
	}
	return out, nil
}

// parsePR reads prView's result. A PR counts as reviewed once anyone but its
// author has submitted a review of any kind; a closed one counts as none.
func parsePR(out []byte, err error) PRInfo {
	if errors.Is(err, exec.ErrNotFound) {
		return PRInfo{State: "unknown", Detail: "gh is not installed"}
	}
	if err != nil {
		// ponytail: matches gh's English message, as refusedAsDirty does git's.
		if strings.Contains(err.Error(), "no pull requests found") {
			return PRInfo{State: "none"}
		}
		reason, _ := explain(err)
		return PRInfo{State: "unknown", Detail: reason}
	}
	var pr struct {
		Number  int
		State   string
		Author  struct{ Login string }
		Reviews []struct{ Author struct{ Login string } }
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return PRInfo{State: "unknown", Detail: "couldn't read gh's answer: " + err.Error()}
	}
	info := PRInfo{Number: pr.Number}
	switch pr.State {
	case "MERGED":
		info.State = "merged"
	case "OPEN":
		info.State = "unreviewed"
		for _, r := range pr.Reviews {
			if r.Author.Login != pr.Author.Login {
				info.State, info.Reviewer = "reviewed", r.Author.Login
				break
			}
		}
	default:
		return PRInfo{State: "none"}
	}
	return info
}

// strategyFor is how a branch with a PR in state is brought up to date: merge
// once reviewed, so reviewers keep their place; rebase otherwise; "" when
// unknown, for the user to pick.
func strategyFor(state string) string {
	switch state {
	case "reviewed":
		return "merge"
	case "unknown":
		return ""
	}
	return "rebase"
}

// UpdateStep is one git command an update runs.
type UpdateStep struct {
	Kind  string `json:"kind"`  // "pull" (own upstream) | "base" | "ff" (pull --ff-only on the base branch)
	Ref   string `json:"ref"`   // e.g. "origin/feat", "origin/main"
	Count int    `json:"count"` // commits it brings in
}

// UpdatePlan is what updating a worktree from its base would do, shown before
// anything changes.
type UpdatePlan struct {
	Branch   string       `json:"branch"`
	Base     string       `json:"base"` // e.g. "origin/main"
	Head     string       `json:"head"` // the SHA planned against
	PR       PRInfo       `json:"pr"`
	Strategy string       `json:"strategy"` // "rebase" | "merge" | "" (unknown: the user picks)
	Steps    []UpdateStep `json:"steps"`
	Push     string       `json:"push"` // pushKind, only when Steps is empty
	Warnings []string     `json:"warnings"`
	Refusal  string       `json:"refusal"`  // set: nothing else applies
	Progress *InProgress  `json:"progress"` // set: stopped mid-operation
}

// stepsFor is what bringing branch (checked out at dir) up to date needs, from
// the refs already fetched: its upstream's new commits first, so a stale copy
// is never rebased and force-pushed over them, then the base's. The base
// branch itself only fast-forwards from its upstream.
func stepsFor(dir, branch, base string) []UpdateStep {
	var steps []UpdateStep
	up := upstreamName(dir)
	_, baseBranch, _ := strings.Cut(base, "/")
	if branch == baseBranch {
		if n := count(dir, "HEAD..@{u}"); up != "" && n > 0 {
			steps = append(steps, UpdateStep{"ff", up, n})
		}
		return steps
	}
	if n := count(dir, "HEAD..@{u}"); up != "" && n > 0 {
		steps = append(steps, UpdateStep{"pull", up, n})
	}
	if n := count(dir, "HEAD.."+base); n > 0 {
		steps = append(steps, UpdateStep{"base", base, n})
	}
	return steps
}

// upstreamName is the branch's pushed upstream, e.g. "origin/feat", or "".
func upstreamName(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "@{u}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// planUpdate works out how to bring branch, checked out at dir, up to date
// with base. It fetches, and asks gh about the PR, but changes nothing else.
func planUpdate(dir, branch, base string) UpdatePlan {
	p := UpdatePlan{Branch: branch, Base: base}
	if p.Progress = inProgress(dir, branch); p.Progress != nil {
		return p
	}
	if branch == "" {
		p.Refusal = "Not on a branch"
		return p
	}
	changes, _ := worktreeChanges(dir)
	for _, c := range changes {
		if !strings.HasPrefix(c, "??") { // untracked files don't stop a rebase or merge
			p.Refusal = "Commit or stash first"
			return p
		}
	}
	remotes := []string{upstreamRemote(dir, base)}
	if r := gitLines(dir, "config", "branch."+branch+".remote"); len(r) == 1 && r[0] != remotes[0] {
		remotes = append(remotes, r[0])
	}
	for _, r := range remotes {
		if r == "" {
			continue
		}
		if err := runGit(dir, "fetch", "-q", r); err != nil {
			reason, _ := explain(err)
			p.Warnings = append(p.Warnings, "Couldn't fetch "+r+": "+reason+"; using last known state")
		}
	}
	if revParse(dir, base) == "" {
		p.Refusal = "No " + base + " to update from"
		return p
	}
	p.PR = parsePR(prView(dir, branch))
	p.Strategy = strategyFor(p.PR.State)
	if p.PR.State == "merged" {
		p.Warnings = append(p.Warnings, "PR #"+strconv.Itoa(p.PR.Number)+" is already merged")
	}
	p.Head = revParse(dir, "HEAD")
	if p.Steps = stepsFor(dir, branch, base); len(p.Steps) == 0 {
		p.Push = pushKind(dir)
	}
	return p
}

// ---- bound to the frontend ----

// worktreeAt resolves a worktree's dir to its branch and base.
func (a *App) worktreeAt(dir string) (ws Workspace, base string, err error) {
	ws, ok := a.find(dir)
	if !ok {
		return ws, "", fmt.Errorf("no workspace at %s", dir)
	}
	return ws, defaultBase(a.repoFor(ws)), nil
}

// PlanUpdate says what updating the worktree at dir from its base would do.
func (a *App) PlanUpdate(dir string) (UpdatePlan, error) {
	ws, base, err := a.worktreeAt(dir)
	if err != nil {
		return UpdatePlan{}, err
	}
	return planUpdate(ws.Dir, ws.Branch, base), nil
}

// UpdateResult is how running, continuing or pushing an update went.
type UpdateResult struct {
	Status   string      `json:"status"`   // "done" | "stopped" | "failed"
	Push     string      `json:"push"`     // when done: pushKind
	Progress *InProgress `json:"progress"` // when stopped
	Reason   string      `json:"reason"`   // when failed
	Detail   string      `json:"detail"`
}

// noEditor keeps git from opening an editor, which would hang with no
// terminal. GIT_EDITOR beats core.editor and the user's own GIT_EDITOR.
var noEditor = []string{"GIT_EDITOR=true"}

// stepArgs is the git command for step under strategy.
func stepArgs(s UpdateStep, strategy string) []string {
	switch {
	case s.Kind == "ff":
		return []string{"pull", "--ff-only"}
	case s.Kind == "pull" && strategy == "merge":
		return []string{"pull", "--no-rebase", "--no-edit"}
	case s.Kind == "pull":
		return []string{"pull", "--rebase"}
	case strategy == "merge":
		return []string{"merge", "--no-edit", s.Ref}
	}
	return []string{"rebase", s.Ref}
}

// after is the result of a git command that may have stopped mid-operation.
func after(dir, branch string, err error) UpdateResult {
	if p := inProgress(dir, branch); p != nil {
		return UpdateResult{Status: "stopped", Progress: p}
	}
	if err != nil {
		reason, detail := explain(err)
		return UpdateResult{Status: "failed", Reason: reason, Detail: detail}
	}
	return UpdateResult{Status: "done", Push: pushKind(dir)}
}

// runUpdate carries out the plan made at head with strategy, stopping at the
// first step that fails or stops on conflicts. Steps are worked out again from
// the fetched refs, so the strategy picked in the dialog applies.
func runUpdate(dir, branch, base, head, strategy string) UpdateResult {
	if revParse(dir, "HEAD") != head {
		return UpdateResult{Status: "failed", Reason: "Branch changed since planning; check again"}
	}
	for _, s := range stepsFor(dir, branch, base) {
		if r := after(dir, branch, runGitEnv(dir, noEditor, stepArgs(s, strategy)...)); r.Status != "done" {
			return r
		}
	}
	return after(dir, branch, nil)
}

// continueUpdate carries on the rebase or merge stopped at dir, once its
// conflicts are resolved and staged. "done" means the caller should re-plan,
// as later steps haven't run.
func continueUpdate(dir, branch string) UpdateResult {
	args := []string{"rebase", "--continue"}
	switch operation(dir) {
	case "":
		return after(dir, branch, nil)
	case "merge":
		args = []string{"commit", "--no-edit"}
	}
	return after(dir, branch, runGitEnv(dir, noEditor, args...))
}

// abortUpdate abandons the rebase or merge stopped at dir.
func abortUpdate(dir string) error {
	switch operation(dir) {
	case "rebase":
		return runGit(dir, "rebase", "--abort")
	case "merge":
		return runGit(dir, "merge", "--abort")
	}
	return nil
}

// errStaleLease is a force push refused because the remote moved on.
var errStaleLease = errors.New("Remote changed since you fetched. Run Update again.")

// pushUpdate pushes branch the way kind (from pushKind) says.
func pushUpdate(dir, branch, base, kind string) error {
	var err error
	switch kind {
	case "force":
		err = runGit(dir, "push", "--force-with-lease")
	case "upstream":
		remote := upstreamRemote(dir, base)
		if r := gitLines(dir, "config", "branch."+branch+".remote"); len(r) == 1 {
			remote = r[0]
		}
		err = runGit(dir, "push", "-u", remote, branch)
	default:
		err = runGit(dir, "push")
	}
	// ponytail: matches git's English "(stale info)", as refusedAsDirty does.
	var ge *gitError
	if errors.As(err, &ge) && strings.Contains(ge.out, "stale info") {
		return errStaleLease
	}
	return err
}

// RunUpdate carries out the update planned at head, with strategy.
func (a *App) RunUpdate(dir, head, strategy string) (UpdateResult, error) {
	ws, base, err := a.worktreeAt(dir)
	if err != nil {
		return UpdateResult{}, err
	}
	return runUpdate(ws.Dir, ws.Branch, base, head, strategy), nil
}

// ContinueUpdate carries on the stopped rebase or merge at dir.
func (a *App) ContinueUpdate(dir string) (UpdateResult, error) {
	ws, _, err := a.worktreeAt(dir)
	if err != nil {
		return UpdateResult{}, err
	}
	return continueUpdate(ws.Dir, ws.Branch), nil
}

// AbortUpdate abandons the stopped rebase or merge at dir.
func (a *App) AbortUpdate(dir string) error { return abortUpdate(dir) }

// PushUpdate pushes the worktree at dir's branch the way kind says.
func (a *App) PushUpdate(dir, kind string) error {
	ws, base, err := a.worktreeAt(dir)
	if err != nil {
		return err
	}
	return pushUpdate(ws.Dir, ws.Branch, base, kind)
}
