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
