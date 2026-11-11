package main

import (
	"encoding/json"
	"errors"
	"os"
)

// The tabs file: the open worktrees' tabs, written on quit and read (then
// removed) on the next launch, so tabs come back under the same ids and each
// Claude tab resumes its conversation.
type savedTabs struct {
	Next      int             `json:"next"` // nextID, so new tabs never reuse a restored id
	Worktrees []savedWorktree `json:"worktrees"`
}

type savedWorktree struct {
	Dir  string     `json:"dir"`
	WS   string     `json:"ws"`
	Tabs []savedTab `json:"tabs"`
}

type savedTab struct {
	ID      string `json:"id"`
	Cmd     string `json:"cmd"`
	Session string `json:"session,omitempty"`
}

// save writes the open worktrees' tabs to path; with none open it removes the
// file. Worktree order is not kept: the sidebar orders worktrees from config.
func (s *sessions) save(path string) error {
	s.mu.Lock()
	f := savedTabs{Next: s.nextID}
	for dir, ps := range s.open {
		w := savedWorktree{Dir: dir}
		for _, p := range ps {
			w.WS = p.ws
			w.Tabs = append(w.Tabs, savedTab{ID: p.ID, Cmd: p.Cmd, Session: p.session})
		}
		f.Worktrees = append(f.Worktrees, w)
	}
	s.mu.Unlock()
	if len(f.Worktrees) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// restore reopens the worktrees saved at path, unstarted, and removes the file
// first, so a bad or used one never applies twice. A worktree whose dir is gone
// is skipped. Restored tabs get no setup and no seed: a Claude tab with a
// session resumes it, falling back to a fresh Claude if it no longer exists.
func (s *sessions) restore(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	_ = os.Remove(path)
	if err != nil {
		return err
	}
	var f savedTabs
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	s.mu.Lock()
	opened := false
	for _, w := range f.Worktrees {
		if fi, err := os.Stat(w.Dir); err != nil || !fi.IsDir() || len(w.Tabs) == 0 {
			continue
		}
		ps := make([]*pane, 0, len(w.Tabs))
		for _, t := range w.Tabs {
			p := &pane{Pane: Pane{ID: t.ID, Cmd: t.Cmd, Kind: paneKind(t.Cmd), Name: paneName(t.Cmd)}, ws: w.WS, dir: w.Dir}
			if validSession(t.Session) {
				p.session = t.Session
				if p.Kind == "claude" {
					p.args = "--resume " + t.Session + " || " + t.Cmd
				}
			}
			s.panes[p.ID] = p
			ps = append(ps, p)
		}
		s.open[w.Dir] = ps
		s.restored[w.Dir] = true
		opened = true
	}
	s.nextID = max(s.nextID, f.Next)
	s.mu.Unlock()
	if opened {
		s.changed()
	}
	return nil
}

// wasRestored reports whether the worktree at dir was reopened by restore and
// has stayed open since: the frontend then starts it from its saved layout.
func (s *sessions) wasRestored(dir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restored[dir]
}

// resumable reports whether any tab has a Claude session to resume next launch.
func (s *sessions) resumable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.panes {
		if p.session != "" {
			return true
		}
	}
	return false
}
