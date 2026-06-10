package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// worktree is one entry from `git worktree list --porcelain`.
type worktree struct {
	Path   string
	Branch string // empty if detached
	Bare   bool
}

// worktreesOf returns the worktrees of the repo containing repoPath.
func worktreesOf(repoPath string) ([]worktree, error) {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, err
	}
	return parseWorktrees(string(out)), nil
}

// parseWorktrees parses porcelain output. Blocks are separated by blank lines:
//
//	worktree /abs/path
//	HEAD <sha>
//	branch refs/heads/<name>     (or "detached", or "bare")
func parseWorktrees(out string) []worktree {
	var wts []worktree
	var cur *worktree
	flush := func() {
		if cur != nil && cur.Path != "" {
			wts = append(wts, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			// skip
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "": // blank = end of block
			flush()
		}
	}
	flush()
	return wts
}

// sanitizeName makes a string safe as a tmux session name (no "." or ":").
func sanitizeName(s string) string {
	r := strings.NewReplacer(".", "-", ":", "-", " ", "-")
	return r.Replace(s)
}

// branchList returns local + remote branch names of the repo, for base-branch
// autocompletion (e.g. "f/MON-3446-halfscreen/2-discounts", "origin/develop").
func branchList(repoPath string) []string {
	out, err := exec.Command("git", "-C", repoPath,
		"for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes").Output()
	if err != nil {
		return nil
	}
	var bs []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasSuffix(l, "/HEAD") {
			continue
		}
		bs = append(bs, l)
	}
	return bs
}

// defaultBase is the configured start-point for new branches, falling back to
// origin/develop.
func defaultBase(r Repo) string {
	if b := strings.TrimSpace(r.Base); b != "" {
		return b
	}
	return "origin/develop"
}

// isRemote reports whether name is a configured git remote of the repo.
func isRemote(repoPath, name string) bool {
	out, err := exec.Command("git", "-C", repoPath, "remote").Output()
	if err != nil {
		return false
	}
	for _, r := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(r) == name {
			return true
		}
	}
	return false
}

// branchExists reports whether a local branch already exists in the repo.
func branchExists(repoPath, branch string) bool {
	return exec.Command("git", "-C", repoPath,
		"show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// workspaceFor builds the Workspace for a worktree at path on branch, matching
// how expandRepo derives names/panes from a repo.
func workspaceFor(r Repo, path, branch string) Workspace {
	name := sanitizeName(filepath.Base(path))
	if r.Prefix != "" {
		name = sanitizeName(r.Prefix) + "/" + name
	}
	return Workspace{Name: name, Dir: path, Panes: r.Panes, Branch: branch}
}

// createWorktree creates a new git worktree at <worktree_root>/<intention> on a
// new branch, fetching the base start-point first. It fails if the branch
// already exists. Returns the Workspace for the new worktree.
func createWorktree(r Repo, intention, branch, base string) (Workspace, error) {
	intention = strings.TrimSpace(intention)
	branch = strings.TrimSpace(branch)
	switch {
	case intention == "":
		return Workspace{}, fmt.Errorf("intention (worktree name) is required")
	case branch == "":
		return Workspace{}, fmt.Errorf("branch name is required")
	case strings.ContainsAny(intention, "/\\"):
		return Workspace{}, fmt.Errorf("intention must not contain a path separator")
	}

	repoPath := expandPath(r.Path)
	root := expandPath(r.WorktreeRoot)
	if root == "" {
		return Workspace{}, fmt.Errorf("worktree_root is not set for repo %s", r.Path)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("creating worktree root: %w", err)
	}
	path := filepath.Join(root, intention)
	if _, err := os.Stat(path); err == nil {
		return Workspace{}, fmt.Errorf("path already exists: %s", path)
	}
	if branchExists(repoPath, branch) {
		return Workspace{}, fmt.Errorf("branch already exists: %s", branch)
	}

	base = strings.TrimSpace(base)
	if base == "" {
		base = defaultBase(r)
	}
	// Fetch only when the start-point is a remote-tracking ref (<remote>/<ref>
	// where <remote> is an actual configured remote). Local branch names also
	// contain "/", so we must not treat those as remotes.
	if remote, ref, ok := strings.Cut(base, "/"); ok && isRemote(repoPath, remote) {
		if out, err := exec.Command("git", "-C", repoPath, "fetch", remote, ref).CombinedOutput(); err != nil {
			return Workspace{}, fmt.Errorf("git fetch %s %s: %v: %s", remote, ref, err, strings.TrimSpace(string(out)))
		}
	}

	if out, err := exec.Command("git", "-C", repoPath,
		"worktree", "add", "-b", branch, path, base).CombinedOutput(); err != nil {
		return Workspace{}, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return workspaceFor(r, path, branch), nil
}

// expandRepo turns a [[repo]] entry into one workspace per worktree.
func expandRepo(r Repo) ([]Workspace, error) {
	wts, err := worktreesOf(expandPath(r.Path))
	if err != nil {
		return nil, err
	}
	var out []Workspace
	for _, wt := range wts {
		if wt.Bare {
			continue
		}
		out = append(out, workspaceFor(r, wt.Path, wt.Branch))
	}
	return out, nil
}
