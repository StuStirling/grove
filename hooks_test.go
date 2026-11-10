package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooks(t *testing.T) {
	for name, tc := range map[string]struct {
		before  string // "" = no file
		wantErr bool
	}{
		"no file":          {},
		"empty object":     {before: `{}`},
		"other settings":   {before: `{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`},
		"partly installed": {before: `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state idle || true"}]}]}}`},
		"invalid json":     {before: `{"model":`, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if tc.before != "" {
				if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if ok, _ := hooksInstalled(path); ok {
				t.Fatal("installed before installing")
			}
			err := installHooks(path)
			if tc.wantErr {
				got, _ := os.ReadFile(path)
				if err == nil || string(got) != tc.before {
					t.Fatalf("err = %v, file = %s: want an error and the file untouched", err, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := hooksInstalled(path); !ok || err != nil {
				t.Fatalf("hooksInstalled = %v, %v after installing", ok, err)
			}
			// Installing again adds nothing.
			once, _ := os.ReadFile(path)
			if err := installHooks(path); err != nil {
				t.Fatal(err)
			}
			if twice, _ := os.ReadFile(path); string(twice) != string(once) {
				t.Errorf("second install changed the file:\n%s\n%s", once, twice)
			}
			var s map[string]any
			_ = json.Unmarshal(once, &s)
			if tc.before != "" && tc.before != "{}" {
				if b, _ := os.ReadFile(path + ".grove.bak"); string(b) != tc.before {
					t.Errorf("backup = %s, want %s", b, tc.before)
				}
			}
			if strings.Contains(tc.before, "say done") && !strings.Contains(string(once), "say done") {
				t.Error("an existing hook was dropped")
			}
			if strings.Contains(tc.before, "opus") && s["model"] != "opus" {
				t.Error("another setting was dropped")
			}
			if n := strings.Count(string(once), "state idle"); n != 2 { // SessionStart and Stop, once each
				t.Errorf("%d idle hooks, want 2:\n%s", n, once)
			}
			if fi, _ := os.Stat(path); tc.before != "" && fi.Mode().Perm() != 0o600 {
				t.Errorf("mode = %v, want the file's own 0600", fi.Mode().Perm())
			}
		})
	}
}
