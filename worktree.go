package main

import (
	"errors"
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
	repoName := strings.TrimSpace(r.Prefix)
	if repoName == "" {
		repoName = filepath.Base(expandPath(r.Path))
	}
	return Workspace{Name: name, Dir: path, Panes: r.Panes, Branch: branch, RepoPath: expandPath(r.Path), RepoName: repoName}
}

// resolveWorktreeRoot returns the repo path and the worktree root as absolute
// paths. A relative worktree_root (e.g. "../worktrees") is anchored to the repo,
// NOT the process cwd: grove creates worktrees from inside other worktrees'
// switcher panes, whose cwd is an unrelated worktree, so a cwd-relative root
// would resolve wrongly, land the tmux panes in a bogus directory, and leave the
// switcher unable to find the repo's config.
func resolveWorktreeRoot(r Repo) (repoPath, root string, err error) {
	repoPath = expandPath(r.Path)
	if abs, e := filepath.Abs(repoPath); e == nil {
		repoPath = abs
	}
	root = expandPath(r.WorktreeRoot)
	if root == "" {
		return "", "", fmt.Errorf("worktree_root is not set for repo %s", r.Path)
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(repoPath, root)
	}
	return repoPath, root, nil
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

	repoPath, root, err := resolveWorktreeRoot(r)
	if err != nil {
		return Workspace{}, err
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

	// Decide the remote whose namespace the new branch should track before adding
	// the worktree, so we know whether to suppress git's default tracking.
	remote := upstreamRemote(repoPath, base)

	addArgs := []string{"-C", repoPath, "worktree", "add", "-b", branch, path, base}
	if remote != "" {
		// --no-track stops git tracking the base start-point (e.g. origin/develop);
		// we point the branch at its own origin ref below instead.
		addArgs = []string{"-C", repoPath, "worktree", "add", "--no-track", "-b", branch, path, base}
	}
	if out, err := exec.Command("git", addArgs...).CombinedOutput(); err != nil {
		return Workspace{}, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}

	if remote != "" {
		if err := setUpstream(repoPath, branch, remote); err != nil {
			return Workspace{}, err
		}
	}
	return workspaceFor(r, path, branch), nil
}

// upstreamRemote picks the remote the new branch should track: the remote
// embedded in base when base is a remote-tracking ref (<remote>/<ref>), else
// "origin" when configured. Returns "" when no suitable remote exists.
func upstreamRemote(repoPath, base string) string {
	if remote, _, ok := strings.Cut(base, "/"); ok && isRemote(repoPath, remote) {
		return remote
	}
	if isRemote(repoPath, "origin") {
		return "origin"
	}
	return ""
}

// setUpstream configures branch to track <remote>/<branch>. The remote ref does
// not exist until the branch is first pushed, so we write the tracking config
// directly rather than via `git branch --set-upstream-to`, which requires the
// upstream ref to already exist.
func setUpstream(repoPath, branch, remote string) error {
	if out, err := exec.Command("git", "-C", repoPath, "config",
		"branch."+branch+".remote", remote).CombinedOutput(); err != nil {
		return fmt.Errorf("git config branch.%s.remote: %v: %s", branch, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repoPath, "config",
		"branch."+branch+".merge", "refs/heads/"+branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git config branch.%s.merge: %v: %s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// checkoutWorktree creates a worktree at <worktree_root>/<intention> checked out
// on an existing branch. branch may be a local name ("feature-x") or a
// remote-tracking ref ("origin/feature-x"); for a remote-only branch it fetches
// and creates a local branch tracking the remote. Returns the new Workspace.
func checkoutWorktree(r Repo, branch, intention string) (Workspace, error) {
	branch = strings.TrimSpace(branch)
	intention = strings.TrimSpace(intention)
	switch {
	case branch == "":
		return Workspace{}, fmt.Errorf("branch is required")
	case intention == "":
		return Workspace{}, fmt.Errorf("intention (worktree name) is required")
	case strings.ContainsAny(intention, "/\\"):
		return Workspace{}, fmt.Errorf("intention must not contain a path separator")
	}

	repoPath, root, err := resolveWorktreeRoot(r)
	if err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("creating worktree root: %w", err)
	}
	path := filepath.Join(root, intention)
	if _, err := os.Stat(path); err == nil {
		return Workspace{}, fmt.Errorf("path already exists: %s", path)
	}

	// Resolve which branch to check out. A remote-tracking ref (<remote>/<ref>
	// where <remote> is a configured remote) with no matching local branch is
	// fetched and checked out as a new local tracking branch; otherwise we check
	// out the existing local branch directly. Local names also contain "/", so we
	// must not treat those as remotes.
	local := branch
	addArgs := []string{"worktree", "add", path, branch}
	if remote, ref, ok := strings.Cut(branch, "/"); ok && isRemote(repoPath, remote) {
		local = ref
		if branchExists(repoPath, local) {
			addArgs = []string{"worktree", "add", path, local}
		} else {
			if out, err := exec.Command("git", "-C", repoPath, "fetch", remote, ref).CombinedOutput(); err != nil {
				return Workspace{}, fmt.Errorf("git fetch %s %s: %v: %s", remote, ref, err, strings.TrimSpace(string(out)))
			}
			addArgs = []string{"worktree", "add", "--track", "-b", local, path, branch}
		}
	}

	args := append([]string{"-C", repoPath}, addArgs...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return Workspace{}, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return workspaceFor(r, path, local), nil
}

// errWorktreeDirty signals that a plain `git worktree remove` was refused because
// the worktree has genuine uncommitted changes (not just submodules), so the
// caller must opt into a forced removal.
var errWorktreeDirty = errors.New("worktree has uncommitted changes")

// worktreeDirty reports real changes in the worktree, ignoring submodules. The
// submodule working dirs are exactly what make a plain `git worktree remove` fail
// in the first place, so we discount them when deciding whether forcing is safe.
func worktreeDirty(dir string) bool {
	out, err := exec.Command("git", "-C", dir,
		"status", "--porcelain", "--ignore-submodules=all").Output()
	if err != nil {
		return true // can't tell → treat as dirty, stay safe
	}
	return strings.TrimSpace(string(out)) != ""
}

// removeWorktree removes the linked worktree at dir, running git from repoPath
// (which must not be dir). It tries a plain remove first; if git refuses, it only
// retries with --force when the worktree is clean ignoring submodules, otherwise
// it returns errWorktreeDirty so the caller can ask for explicit confirmation.
func removeWorktree(repoPath, dir string, force bool) error {
	if _, err := exec.Command("git", "-C", repoPath,
		"worktree", "remove", dir).CombinedOutput(); err == nil {
		return nil
	}
	if !force && worktreeDirty(dir) {
		return errWorktreeDirty
	}
	if out, err := exec.Command("git", "-C", repoPath,
		"worktree", "remove", "--force", dir).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove --force: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// errBranchUnmerged signals that `git branch -d` was refused because the branch
// is not fully merged, so it was kept rather than force-deleted.
var errBranchUnmerged = errors.New("branch not fully merged")

// removeBranch safely deletes a local branch with -d (refuses unmerged branches).
// An unmerged branch is reported as errBranchUnmerged; an empty name is a no-op.
func removeBranch(repoPath, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", repoPath, "branch", "-d", branch).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "not fully merged") {
			return errBranchUnmerged
		}
		return fmt.Errorf("git branch -d %s: %v: %s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
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
