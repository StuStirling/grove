package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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

	// git
	if p, err := exec.LookPath("git"); err == nil {
		check("git", p, true)
	} else {
		check("git", "not found: install git", false)
	}

	// config (repo-local .grove.toml wins, else global)
	cfgPath, isLocal := resolveConfigPath()
	scope := "global"
	if isLocal {
		scope = "local"
	}
	check("config", fmt.Sprintf("%s (%s)", cfgPath, scope), fileExists(cfgPath))

	cfg, err := loadConfig()
	if err != nil {
		cfg = &Config{}
	}
	// pane commands: the first word of each must resolve on PATH.
	seen := map[string]bool{}
	for _, r := range cfg.Repo {
		for _, c := range r.Panes {
			f := strings.Fields(c)
			if len(f) == 0 || seen[f[0]] {
				continue
			}
			seen[f[0]] = true
			if p, err := exec.LookPath(f[0]); err == nil {
				check("pane", p, true)
			} else {
				check("pane", f[0]+" not found on PATH", false)
			}
		}
	}

	// terminal (optional: only needed for Open in Terminal / `grove open -w`)
	if cfg.Terminal != "" {
		check("terminal", cfg.Terminal, true)
	} else {
		fmt.Printf("[opt ] %-12s %s\n", "terminal", "unset: Open in Terminal (`grove open -w`) disabled")
	}

	if ok {
		fmt.Println("\nAll good.")
		return 0
	}
	fmt.Println("\nSome checks failed, see above.")
	return 1
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
