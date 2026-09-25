package main

import (
	"os"
	"path/filepath"
	"testing"
)

// groveRepo makes a repo at dir with a .grove.toml, and returns its config.
func groveRepo(t *testing.T, dir string) *Config {
	t.Helper()
	isolateGit(t)
	gitT(t, "", "init", "-q", dir)
	path := filepath.Join(dir, localConfigName)
	writeT(t, path, "[[repo]]\npanes = [\"\"]\n")
	cfg, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// shortHome points HOME at a short temp dir: the sockets live under it, and
// unix socket paths are capped near 104 bytes.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "g")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	home, _ = filepath.EvalSymlinks(home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	return home
}

func newTestApp(own *Config) *App {
	a := &App{ready: make(chan struct{}), own: own, cfg: own, sess: newSessions("", func(string, ...any) {})}
	a.ws = own.resolve()
	return a
}

func TestAddRepoRememberedAndRemoved(t *testing.T) {
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	other := filepath.Join(home, "b")
	groveRepo(t, other)

	a := newTestApp(own)
	if name, err := a.addRepo(other); err != nil || name != "b" {
		t.Fatalf("addRepo = %q, %v", name, err)
	}
	if len(a.ws) != 2 {
		t.Fatalf("workspaces = %+v, want both repos' main worktrees", a.ws)
	}
	if _, err := a.addRepo(other); err == nil {
		t.Fatal("adding a repo twice should fail")
	}
	if _, err := a.addRepo(own.Path); err == nil {
		t.Fatal("adding the window's own repo should fail")
	}
	if repos := a.State().Repos; len(repos) != 2 || repos[0].Added || !repos[1].Added {
		t.Fatalf("repos = %+v", repos)
	}
	a.saveRepos()
	a.closeRepos() // the window quits

	// Next launch shows it again.
	a = newTestApp(own)
	a.restoreRepos()
	if len(a.ws) != 2 {
		t.Fatalf("restored workspaces = %+v", a.ws)
	}
	if err := a.RemoveRepo(filepath.Dir(own.Path)); err == nil {
		t.Fatal("removing the window's own repo should fail")
	}
	if err := a.RemoveRepo(other); err != nil {
		t.Fatal(err)
	}
	if len(a.ws) != 1 || len(a.added) != 0 {
		t.Fatalf("after remove: ws %+v, added %d", a.ws, len(a.added))
	}
	if !fileExists(filepath.Join(other, localConfigName)) {
		t.Fatal("removing a repo from the window touched it on disk")
	}
	a = newTestApp(own)
	a.restoreRepos()
	if len(a.added) != 0 {
		t.Fatal("a removed repo came back on the next launch")
	}
}

func TestSameNamedWorktreesStayApart(t *testing.T) {
	// Two repos whose main worktrees are both named "app".
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a", "app"))
	dirA, dirB := filepath.Dir(own.Path), filepath.Join(home, "b", "app")
	groveRepo(t, dirB)
	a := newTestApp(own)
	if _, err := a.addRepo(dirB); err != nil {
		t.Fatal(err)
	}
	defer a.closeRepos()
	panes := func(dir string) int {
		for _, w := range a.State().Workspaces {
			if w.Dir == dir {
				return len(w.Panes)
			}
		}
		t.Fatalf("no workspace at %s", dir)
		return 0
	}
	if ws := a.State().Workspaces; len(ws) != 2 || ws[0].Name != "app" || ws[1].Name != "app" {
		t.Fatalf("workspaces = %+v", ws)
	}

	if _, err := a.Open(dirA); err != nil {
		t.Fatal(err)
	}
	if panes(dirA) != 1 || panes(dirB) != 0 {
		t.Fatalf("after opening a: panes %d, %d", panes(dirA), panes(dirB))
	}
	for range 2 {
		if _, err := a.NewTab(dirB, "shell"); err != nil {
			t.Fatal(err)
		}
	}
	if panes(dirA) != 1 || panes(dirB) != 2 {
		t.Fatalf("after b's tabs: panes %d, %d", panes(dirA), panes(dirB))
	}
	if err := a.Close(dirA); err != nil {
		t.Fatal(err)
	}
	if a.sess.isOpen(dirA) || panes(dirB) != 2 {
		t.Fatalf("closing a: a open %v, b panes %d", a.sess.isOpen(dirA), panes(dirB))
	}
	if err := a.Close(dirB); err != nil {
		t.Fatal(err)
	}
}
