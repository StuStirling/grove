package main

import (
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
