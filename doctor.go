package main

import (
	"fmt"
	"os"
	"os/exec"
)

// doctor checks the runtime prerequisites and prints a report.
func doctor() int {
	ok := true
	check := func(name, detail string, pass bool) {
		mark := "OK  "
		if !pass {
			mark = "FAIL"
			ok = false
		}
		fmt.Printf("[%s] %-12s %s\n", mark, name, detail)
	}

	// tmux
	if p, err := exec.LookPath("tmux"); err == nil {
		check("tmux", p, true)
	} else {
		check("tmux", "not found — install tmux", false)
	}

	// git
	if p, err := exec.LookPath("git"); err == nil {
		check("git", p, true)
	} else {
		check("git", "not found — install git", false)
	}

	// config (repo-local .grove.toml wins, else global)
	cfgPath, isLocal := resolveConfigPath()
	scope := "global"
	if isLocal {
		scope = "local"
	}
	check("config", fmt.Sprintf("%s (%s)", cfgPath, scope), fileExists(cfgPath))

	// terminal (optional: only needed for `grove open <name> -w`)
	if cfg, err := loadConfig(); err == nil && cfg.Terminal != "" {
		check("terminal", cfg.Terminal, true)
	} else {
		fmt.Printf("[opt ] %-12s %s\n", "terminal", "unset — `grove open -w` (new window) disabled; same-tab attach works")
	}

	if ok {
		fmt.Println("\nAll good.")
		return 0
	}
	fmt.Println("\nSome checks failed — see above.")
	return 1
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
