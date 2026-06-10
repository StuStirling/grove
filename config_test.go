package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"~":        home,
		"~/x":      filepath.Join(home, "x"),
		"/abs/dir": "/abs/dir",
		"rel/dir":  "rel/dir",
	}
	for in, want := range cases {
		if got := expandPath(in); got != want {
			t.Errorf("expandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gitRoot(child); got != root {
		t.Errorf("gitRoot(child) = %q, want %q", got, root)
	}
	if got := gitRoot(t.TempDir()); got != "" {
		t.Errorf("gitRoot(non-repo) = %q, want empty", got)
	}
}

func TestFindLocalConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, localConfigName)
	if err := os.WriteFile(cfg, []byte("terminal=\"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(child)
	got := findLocalConfig()
	// macOS /var -> /private/var symlink: compare resolved paths.
	gotR, _ := filepath.EvalSymlinks(got)
	wantR, _ := filepath.EvalSymlinks(cfg)
	if gotR != wantR {
		t.Errorf("findLocalConfig() = %q, want %q", got, cfg)
	}
}

func TestFindLocalConfigStopsAtRepoRoot(t *testing.T) {
	// A .grove.toml above the repo root must NOT be found.
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, localConfigName), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	if got := findLocalConfig(); got != "" {
		t.Errorf("findLocalConfig() = %q, want empty (must not climb past repo root)", got)
	}
}
