package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoPlan is what starting work on a branch would do in one repo, shown on the
// repo's row before anything is created.
type RepoPlan struct {
	Action string `json:"action"` // "create" | "checkout" | "reuse" | "conflict"
	Path   string `json:"path"`   // the worktree's dir ("" for some conflicts)
	Reason string `json:"reason"` // a conflict's one line
}

func conflict(format string, a ...any) RepoPlan {
	return RepoPlan{Action: "conflict", Reason: fmt.Sprintf(format, a...)}
}

// planWorktree decides how a worktree named name on branch would come about in
// r, without changing anything. A branch already checked out anywhere in the
// repo, the main checkout included, is reused: git won't check it out twice.
func planWorktree(r Repo, branch, name string) RepoPlan {
	name, branch = strings.TrimSpace(name), strings.TrimSpace(branch)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return conflict("worktree name must not be empty or contain a path separator")
	}
	if branch == "" || exec.Command("git", "check-ref-format", "--branch", branch).Run() != nil {
		return conflict("%s is not a valid branch name", branch)
	}
	repoPath, root, _ := resolveWorktreeRoot(r)
	wts, err := worktreesOf(repoPath)
	if err != nil {
		return conflict("can't read %s: %v", tildePath(repoPath), err)
	}
	for _, wt := range wts {
		if wt.Branch == branch {
			return RepoPlan{Action: "reuse", Path: wt.Path}
		}
	}
	path := filepath.Join(root, name)
	if _, err := os.Stat(path); err == nil {
		return conflict("%s already exists", tildePath(path))
	}
	if branchExists(repoPath, branch) || remoteBranchExists(repoPath, branch) {
		return RepoPlan{Action: "checkout", Path: path}
	}
	return RepoPlan{Action: "create", Path: path}
}

// remoteBranchExists reports whether origin/<branch> is known locally.
func remoteBranchExists(repoPath, branch string) bool {
	return exec.Command("git", "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch).Run() == nil
}

// startWorktree carries out the plan for branch in r. Before creating a new
// branch it fetches origin's, so a branch someone already pushed is checked out
// rather than started again.
func startWorktree(r Repo, branch, name string) (Workspace, RepoPlan, error) {
	branch, name = strings.TrimSpace(branch), strings.TrimSpace(name)
	plan := planWorktree(r, branch, name)
	repoPath := expandPath(r.Path)
	if plan.Action == "create" && isRemote(repoPath, "origin") {
		_ = exec.Command("git", "-C", repoPath, "fetch", "-q", "origin", branch).Run() // no such branch is fine
		plan = planWorktree(r, branch, name)
	}
	var ws Workspace
	var err error
	switch plan.Action {
	case "conflict":
		err = errors.New(plan.Reason)
	case "reuse":
		ws = workspaceFor(r, plan.Path, branch)
	case "checkout":
		ref := branch
		if !branchExists(repoPath, branch) {
			ref = "origin/" + branch
		}
		ws, err = checkoutWorktree(r, ref, name)
	default:
		ws, err = createWorktree(r, name, branch, "")
	}
	return ws, plan, err
}

// ---- bound to the frontend ----

// Seed starts the lead repo's Claude on a prompt; the zero Seed starts none.
type Seed struct {
	Prompt  string   `json:"prompt"`
	AddDirs []string `json:"addDirs"` // the other repos' worktrees
}

// PlanRepo says what starting work on branch would do in the repo at repoPath.
func (a *App) PlanRepo(repoPath, branch, name string) (RepoPlan, error) {
	r, err := a.repo(repoPath)
	if err != nil {
		return RepoPlan{}, err
	}
	return planWorktree(r, branch, name), nil
}

// StartRepo starts work on branch in the repo at repoPath and opens the
// worktree, its setup queued unless it already existed. The lead's seed loads
// the other worktrees' CLAUDE.md and rules too, which --add-dir alone doesn't.
func (a *App) StartRepo(repoPath, branch, name string, seed Seed) (string, error) {
	r, err := a.repo(repoPath)
	if err != nil {
		return "", err
	}
	ws, plan, err := startWorktree(r, branch, name)
	if err != nil {
		return "", err
	}
	a.reload()
	setup := r.Setup
	if plan.Action == "reuse" {
		setup = ""
	}
	if seed.Prompt == "" {
		a.sess.ensure(ws, setup)
		return ws.Dir, nil
	}
	args, env := seedArgs(seed.Prompt, seed.AddDirs)
	if len(seed.AddDirs) > 0 {
		env = append(env, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1")
	}
	a.sess.seed(ws, setup, args, env)
	return ws.Dir, nil
}
