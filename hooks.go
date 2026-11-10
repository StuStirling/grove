package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeHooks are the Claude Code hooks that report a pane's state to grove
// (`grove state`), by event: the README's list. Outside a grove pane GROVE_BIN
// is unset and they do nothing.
var claudeHooks = []struct{ event, matcher, state string }{
	{"SessionStart", "", "idle"},
	{"UserPromptSubmit", "", "working"},
	{"Notification", "", "waiting"},
	{"PermissionRequest", "*", "waiting"},
	{"Stop", "", "idle"},
	{"SessionEnd", "", "clear"},
}

func hookCommand(state string) string {
	return `[ -n "$GROVE_BIN" ] && "$GROVE_BIN" state ` + state + ` || true`
}

// claudeSettingsPath is Claude Code's user settings file.
func claudeSettingsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json")
}

// readSettings parses a Claude settings file; a missing one is empty.
func readSettings(path string) (map[string]any, []byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	s := map[string]any{}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil, fmt.Errorf("%s is not valid JSON, so grove left it alone: %w", path, err)
	}
	return s, data, nil
}

// hasGroveHook reports whether an event's hook groups already call grove.
func hasGroveHook(groups any) bool {
	b, _ := json.Marshal(groups)
	return strings.Contains(string(b), "GROVE_BIN")
}

// hooksInstalled reports whether every event has a grove hook.
func hooksInstalled(path string) (bool, error) {
	s, _, err := readSettings(path)
	if err != nil {
		return false, err
	}
	hooks, _ := s["hooks"].(map[string]any)
	for _, h := range claudeHooks {
		if !hasGroveHook(hooks[h.event]) {
			return false, nil
		}
	}
	return true, nil
}

// installHooks adds grove's hook to each event that has none, keeping every
// other setting and hook. The file is backed up to .grove.bak first.
// ponytail: rewrites the file with sorted keys and 2-space indent (Go maps);
// keep key order with an ordered decoder if that churn ever matters.
func installHooks(path string) error {
	s, before, err := readSettings(path)
	if err != nil {
		return err
	}
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		if s["hooks"] != nil {
			return fmt.Errorf(`"hooks" in %s is not an object, so grove left it alone`, path)
		}
		hooks = map[string]any{}
		s["hooks"] = hooks
	}
	changed := false
	for _, h := range claudeHooks {
		if hasGroveHook(hooks[h.event]) {
			continue
		}
		groups, ok := hooks[h.event].([]any)
		if !ok && hooks[h.event] != nil {
			return fmt.Errorf(`"hooks.%s" in %s is not a list, so grove left it alone`, h.event, path)
		}
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand(h.state)}}}
		if h.matcher != "" {
			group["matcher"] = h.matcher
		}
		hooks[h.event] = append(groups, group)
		changed = true
	}
	if !changed {
		return nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep && as written
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if before != nil {
		if err := os.WriteFile(path+".grove.bak", before, mode); err != nil {
			return err
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, b.Bytes(), mode)
}

// ClaudeHooks reports whether grove's Claude Code hooks are installed, for
// Settings.
func (a *App) ClaudeHooks() (bool, error) { return hooksInstalled(claudeSettingsPath()) }

// InstallClaudeHooks adds grove's hooks to Claude Code's settings.
func (a *App) InstallClaudeHooks() error { return installHooks(claudeSettingsPath()) }
