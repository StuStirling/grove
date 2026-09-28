package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// groveRepo makes a repo at dir with a .grove.toml, and returns its config.
func groveRepo(t *testing.T, dir string) *Config {
	t.Helper()
	return groveConfig(t, dir, "[[repo]]\npanes = [\"\"]\n")
}

// groveConfig makes a repo at dir with body as its .grove.toml.
func groveConfig(t *testing.T, dir, body string) *Config {
	t.Helper()
	isolateGit(t)
	gitT(t, "", "init", "-q", dir)
	path := filepath.Join(dir, localConfigName)
	writeT(t, path, body)
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

func TestCreateUnderSymlinkedRoot(t *testing.T) {
	// git lists worktrees by their real path, which keys them.
	repo := gitRepo(t)
	real, link := filepath.Join(filepath.Dir(repo), "real"), filepath.Join(filepath.Dir(repo), "link")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(&Config{Repo: []Repo{{Path: repo, WorktreeRoot: link, Base: "trunk", Setup: "true"}}})
	dir, err := a.Create(repo, "feat", "feat", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(a.State().Workspaces, func(w WorkspaceInfo) bool { return w.Dir == dir && w.Open }) {
		t.Fatalf("Create = %s, not an open workspace: %+v", dir, a.State().Workspaces)
	}
	if _, err := a.Open(dir); err != nil {
		t.Fatal(err)
	}
}

func TestAddRepoAlreadyListed(t *testing.T) {
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	dirA := filepath.Dir(own.Path)
	gitT(t, dirA, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(home, "a-fix")
	gitT(t, dirA, "worktree", "add", "-q", "-b", "fix", wt)
	writeT(t, filepath.Join(wt, localConfigName), "[[repo]]\n") // as if committed
	a := newTestApp(own)
	defer a.closeRepos()

	// The window's repo again, by a linked worktree's config or by path.
	for _, dir := range []string{wt, filepath.Dir(groveConfig(t, filepath.Join(home, "c"), "[[repo]]\npath = \""+dirA+"\"\n").Path)} {
		if _, err := a.addRepo(dir); err == nil || !strings.Contains(err.Error(), "already in this window") {
			t.Fatalf("adding %s = %v, want already in this window", dir, err)
		}
	}
	// No [[repo]]: nothing to take it out of the window by.
	onlyManual := filepath.Join(home, "d")
	groveConfig(t, onlyManual, "[[workspace]]\nname = \"notes\"\ndir = \""+home+"\"\n")
	if _, err := a.addRepo(onlyManual); err == nil || !strings.Contains(err.Error(), "no [[repo]]") {
		t.Fatalf("adding a config without a [[repo]] = %v", err)
	}
	if len(a.added) != 0 {
		t.Fatalf("added %d", len(a.added))
	}

	// A manual entry already listed is dropped, and stays the window's own.
	b := filepath.Join(home, "b")
	groveConfig(t, b, "[[repo]]\n\n[[workspace]]\nname = \"dup\"\ndir = \""+dirA+"\"\n")
	if _, err := a.addRepo(b); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, w := range a.State().Workspaces {
		dirs = append(dirs, w.Dir)
	}
	if want := []string{dirA, wt, b}; !slices.Equal(dirs, want) {
		t.Fatalf("dirs = %q, want %q", dirs, want)
	}
	if _, err := a.Open(dirA); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveRepo(b); err != nil {
		t.Fatal(err)
	}
	if !a.sess.isOpen(dirA) {
		t.Fatal("removing the added repo closed the window's own worktree")
	}
}

func TestSkippedRepoStaysSaved(t *testing.T) {
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	b, c := filepath.Join(home, "b"), filepath.Join(home, "c")
	groveRepo(t, b)
	cfgC := groveRepo(t, c)
	saved := func() string {
		data, _ := os.ReadFile(own.reposFile())
		return string(data)
	}
	a := newTestApp(own)
	for _, dir := range []string{c, b} {
		if _, err := a.addRepo(dir); err != nil {
			t.Fatal(err)
		}
	}
	a.saveRepos()
	a.closeRepos()

	// Next launch c is open in another window, so it isn't restored, but it
	// stays saved when the list is saved again.
	ln, err := ipcServe(cfgC.socketPath(), func(ipcReq) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	a = newTestApp(own)
	defer a.closeRepos()
	a.restoreRepos()
	if len(a.added) != 1 {
		t.Fatalf("restored %d repos, want b only", len(a.added))
	}
	a.saveRepos()
	if got := saved(); got != b+"\n"+c {
		t.Fatalf("saved %q", got)
	}
	// Added once that window has gone, it is saved once.
	_ = ln.Close()
	if _, err := a.addRepo(c); err != nil {
		t.Fatal(err)
	}
	a.saveRepos()
	if got := saved(); got != b+"\n"+c {
		t.Fatalf("saved %q", got)
	}
}

func TestRemoveMovedRepo(t *testing.T) {
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	b := filepath.Join(home, "b")
	groveRepo(t, b)
	a := newTestApp(own)
	if _, err := a.addRepo(b); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Open(b); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(b, b+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveRepo(b); err != nil {
		t.Fatal(err)
	}
	if n := a.sess.count(); n != 0 {
		t.Fatalf("%d worktrees still open", n)
	}
}

func TestRelativeRepoPath(t *testing.T) {
	// From the config's dir, not grove's cwd (this repo, in a test).
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	b := filepath.Join(home, "b")
	groveConfig(t, b, "[[repo]]\npath = \".\"\n")
	a := newTestApp(own)
	defer a.closeRepos()
	if _, err := a.addRepo(b); err != nil {
		t.Fatal(err)
	}
	if ws := a.State().Workspaces; len(ws) != 2 || ws[1].Dir != b || a.State().Repos[1].Path != b {
		t.Fatalf("workspaces = %+v, repos = %+v", ws, a.State().Repos)
	}
}

func TestMenuLabels(t *testing.T) {
	a := &App{cfg: &Config{Repo: make([]Repo, 3)}, ws: []Workspace{
		{Name: "fix", RepoName: "app"}, {Name: "fix", RepoName: "api"}, {Name: "android/x", RepoName: "android"}, {Name: "notes"},
	}}
	if got, want := a.menuLabels(), []string{"app/fix", "api/fix", "android/x", "notes"}; !slices.Equal(got, want) {
		t.Errorf("several repos: %q, want %q", got, want)
	}
	a.cfg.Repo = a.cfg.Repo[:1]
	if got, want := a.menuLabels(), []string{"fix", "fix", "android/x", "notes"}; !slices.Equal(got, want) {
		t.Errorf("one repo: %q, want %q", got, want)
	}
}

func TestInitialInAddedRepo(t *testing.T) {
	// `grove gui --setup b`: b is a worktree of a repo restored after newApp.
	home := shortHome(t)
	own := groveRepo(t, filepath.Join(home, "a"))
	b := filepath.Join(home, "b")
	groveConfig(t, b, "[[repo]]\nsetup = \"make\"\n")
	a := newTestApp(own)
	a.initial, a.setup = "b", true
	defer a.closeRepos()
	if _, err := a.addRepo(b); err != nil {
		t.Fatal(err)
	}
	if got := a.TakeInitial(); got != b {
		t.Fatalf("TakeInitial = %q, want %s", got, b)
	}
	if ps := a.sess.open[b]; len(ps) != 1 || ps[0].setup != "make" {
		t.Fatalf("setup not queued: %+v", ps)
	}
	if got := a.TakeInitial(); got != "" {
		t.Fatalf("second TakeInitial = %q", got)
	}
}
