package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const testSession = "5f1c2b9e-0a1d-4c3e-9f00-1234567890ab"

func newTestSessions() *sessions { return newSessions("", (&recorder{}).emit) }

func tabsPath(t *testing.T) string { return filepath.Join(t.TempDir(), "w.tabs") }

func TestSaveRestoreRoundTrip(t *testing.T) {
	path, dir := tabsPath(t), t.TempDir()
	s1 := newTestSessions()
	ws := Workspace{Name: "a", Dir: dir, Panes: []string{"claude", ""}}
	ps := s1.ensure(ws, "make setup")
	_ = s1.setClaude(ps[0].ID, "idle", testSession)
	s1.newTab(ws, "lazygit")
	want := publicPanes(s1.open[dir])
	if err := s1.save(path); err != nil {
		t.Fatal(err)
	}

	s2 := newTestSessions()
	if err := s2.restore(path); err != nil {
		t.Fatal(err)
	}
	got := s2.open[dir]
	if len(got) != len(want) {
		t.Fatalf("restored %d tabs, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.ID != want[i].ID || p.Cmd != want[i].Cmd || p.Kind != want[i].Kind || p.Name != want[i].Name {
			t.Errorf("tab %d = %+v, want %+v", i, p.Pane, want[i])
		}
		if p.setup != "" || p.f != nil || p.ws != "a" || p.dir != dir {
			t.Errorf("tab %d: setup %q started %v ws %q dir %q", i, p.setup, p.f != nil, p.ws, p.dir)
		}
	}
	if a := got[0].args; a != "--resume "+testSession+" || claude" {
		t.Errorf("claude args = %q", a)
	}
	if got[0].session != testSession {
		t.Errorf("claude session = %q", got[0].session)
	}
	if got[1].args != "" || got[2].args != "" {
		t.Errorf("non-claude args = %q, %q", got[1].args, got[2].args)
	}
	if s2.nextID != s1.nextID {
		t.Errorf("nextID = %d, want %d", s2.nextID, s1.nextID)
	}
	if !s2.wasRestored(dir) {
		t.Error("not marked restored")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
}

func TestRestoreClaudeWithoutSession(t *testing.T) {
	path, dir := tabsPath(t), t.TempDir()
	s1 := newTestSessions()
	s1.ensure(Workspace{Name: "a", Dir: dir, Panes: []string{"claude"}}, "")
	_ = s1.save(path)
	s2 := newTestSessions()
	_ = s2.restore(path)
	if a := s2.open[dir][0].args; a != "" {
		t.Errorf("args = %q", a)
	}
}

func TestRestoreThenSaveKeepsSessions(t *testing.T) {
	path, dir := tabsPath(t), t.TempDir()
	s1 := newTestSessions()
	ps := s1.ensure(Workspace{Name: "a", Dir: dir, Panes: []string{"claude"}}, "")
	_ = s1.setClaude(ps[0].ID, "idle", testSession)
	_ = s1.save(path)
	s2 := newTestSessions()
	_ = s2.restore(path)
	_ = s2.save(path)
	var f savedTabs
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Worktrees) != 1 || f.Worktrees[0].Tabs[0].Session != testSession {
		t.Fatalf("saved again: %s", b)
	}
}

func TestRestoreSkipsMissingDir(t *testing.T) {
	path, keep, gone := tabsPath(t), t.TempDir(), t.TempDir()
	s1 := newTestSessions()
	s1.ensure(Workspace{Name: "keep", Dir: keep, Panes: []string{""}}, "")
	s1.ensure(Workspace{Name: "gone", Dir: gone, Panes: []string{""}}, "")
	_ = s1.save(path)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	s2 := newTestSessions()
	_ = s2.restore(path)
	if _, ok := s2.open[gone]; ok || len(s2.open) != 1 {
		t.Fatalf("open = %v", s2.open)
	}
}

func TestRestoreCorruptFile(t *testing.T) {
	path := tabsPath(t)
	_ = os.WriteFile(path, []byte("{"), 0o600)
	s := newTestSessions()
	if err := s.restore(path); err == nil {
		t.Error("no error for a corrupt file")
	}
	if len(s.open) != 0 {
		t.Errorf("open = %v", s.open)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
}

func TestRestoreNoFile(t *testing.T) {
	s := newTestSessions()
	if err := s.restore(tabsPath(t)); err != nil || len(s.open) != 0 {
		t.Fatalf("err %v open %v", err, s.open)
	}
}

func TestSaveNothingOpenRemovesFile(t *testing.T) {
	path := tabsPath(t)
	_ = os.WriteFile(path, []byte(`{"next":3,"worktrees":[]}`), 0o600)
	if err := newTestSessions().save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
}

func TestNewTabAfterRestoreGetsFreshID(t *testing.T) {
	path, dir := tabsPath(t), t.TempDir()
	s1 := newTestSessions()
	ws := Workspace{Name: "a", Dir: dir, Panes: []string{"claude", ""}}
	s1.ensure(ws, "")
	_ = s1.save(path)
	s2 := newTestSessions()
	_ = s2.restore(path)
	p := s2.newTab(ws, "")
	if slices.ContainsFunc(s2.open[dir][:2], func(q *pane) bool { return q.ID == p.ID }) {
		t.Fatalf("new tab reused id %s", p.ID)
	}
}

func TestRestoredClearsOnClose(t *testing.T) {
	path, dir := tabsPath(t), t.TempDir()
	s1 := newTestSessions()
	s1.ensure(Workspace{Name: "a", Dir: dir, Panes: []string{""}}, "")
	_ = s1.save(path)
	s2 := newTestSessions()
	_ = s2.restore(path)
	if err := s2.close(dir); err != nil {
		t.Fatal(err)
	}
	if s2.wasRestored(dir) {
		t.Error("still restored after close")
	}
}
