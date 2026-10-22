package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// The OS keychain holds secrets under this service. Secrets go to the tools on
// stdin, never in argv, so they don't show in a process listing.
const secretService = "grove"

const securityBin = "/usr/bin/security"

var errNoSecretTool = errors.New("secret-tool not found: install libsecret-tools to connect Linear")

// secretGet returns the secret stored for account, or "" when there is none.
func secretGet(account string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command(securityBin, "find-generic-password", "-s", secretService, "-a", account, "-w").Output()
		if exitCode(err) == 44 { // errSecItemNotFound
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("reading the keychain: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return "", errNoSecretTool
		}
		out, err := exec.Command("secret-tool", "lookup", "service", secretService, "account", account).Output()
		if err != nil && len(bytes.TrimSpace(out)) == 0 {
			return "", nil // nothing stored
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", fmt.Errorf("no keychain support on %s", runtime.GOOS)
}

// secretSet stores secret for account, replacing any already there.
func secretSet(account, secret string) error {
	switch runtime.GOOS {
	case "darwin":
		// security -i reads the command from stdin; quoting there is plain, so
		// refuse anything a key never contains rather than escape it.
		if secret == "" || strings.ContainsAny(secret, " \t\r\n\"'\\") {
			return errors.New("that doesn't look like a Linear API key")
		}
		cmd := exec.Command(securityBin, "-i")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w \"%s\"\n", secretService, account, secret))
		out, err := cmd.CombinedOutput()
		// security -i can exit 0 after a failed command, so read the item back.
		if got, _ := secretGet(account); err != nil || got != secret {
			return fmt.Errorf("saving to the keychain: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return errNoSecretTool
		}
		cmd := exec.Command("secret-tool", "store", "--label=grove Linear key", "service", secretService, "account", account)
		cmd.Stdin = strings.NewReader(secret)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("saving to the keyring: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return fmt.Errorf("no keychain support on %s", runtime.GOOS)
}

// secretDelete removes account's secret; there being none is fine.
func secretDelete(account string) error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command(securityBin, "delete-generic-password", "-s", secretService, "-a", account).CombinedOutput()
		if err != nil && exitCode(err) != 44 {
			return fmt.Errorf("removing from the keychain: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return errNoSecretTool
		}
		if out, err := exec.Command("secret-tool", "clear", "service", secretService, "account", account).CombinedOutput(); err != nil {
			return fmt.Errorf("removing from the keyring: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return fmt.Errorf("no keychain support on %s", runtime.GOOS)
}

// exitCode is a command's exit status, or -1 when it didn't run to an exit.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
