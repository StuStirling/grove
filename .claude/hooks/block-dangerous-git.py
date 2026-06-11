#!/usr/bin/env python3
"""PreToolUse guardrail: block destructive git ops + hook-disabling tricks.

Triggered by Claude Code on every tool call. Works even under
--dangerously-skip-permissions / bypassPermissions because hooks are
evaluated independently of the permission system.

Bash blocks:
  1. git push --force / --force-with-lease / -f targeting main
  2. git push origin +main (refspec force)
  3. git reset --hard while currently on main
  4. git branch -D main
  5. git update-ref refs/heads/main
  6. git commit/push --no-verify (bypasses pre-commit/pre-push hooks)
  7. Shell writes to .git/hooks/* (rm/mv/cp/echo>/tee/chmod targeting hooks)
  8. git config core.hooksPath (redirects hook resolution)

Edit/Write/MultiEdit blocks:
  9. Any modification to **/.git/hooks/** or **/.git/config

Exit 0 = allow. Exit 2 = block (stderr shown to Claude).
"""

from __future__ import annotations

import json
import re
import subprocess
import sys

PROTECTED_BRANCHES = {"main"}
PROTECTED_RE = r"main"
PATH_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit"}


def current_branch(cwd: str) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", cwd or ".", "rev-parse", "--abbrev-ref", "HEAD"],
            capture_output=True,
            text=True,
            timeout=2,
        )
        return result.stdout.strip()
    except Exception:
        return ""


def has_force_flag(cmd: str) -> bool:
    return bool(
        re.search(r"--force(?:-with-lease)?(?:[=\s]|$)", cmd)
        or re.search(r"(?:^|\s)-[A-Za-z]*f[A-Za-z]*(?=\s|$)", cmd)
    )


def mentions_protected(cmd: str) -> bool:
    return bool(re.search(rf"(?:^|[\s/:+=]){PROTECTED_RE}(?:[\s:]|$)", cmd))


def has_protected_refspec_force(cmd: str) -> bool:
    return bool(re.search(rf"\+{PROTECTED_RE}(?:[\s:]|$)", cmd))


def push_specifies_branch(cmd: str) -> bool:
    """True if a `git push` subcommand has an explicit branch arg.

    A push with >=2 positional args (remote + ref) names its target. A push
    with <=1 positional arg pushes the current branch via upstream tracking.
    """
    match = re.search(r"\bgit\s+push\b([^;&|]*)", cmd)
    if not match:
        return False
    positional = [a for a in match.group(1).split() if not a.startswith("-")]
    return len(positional) >= 2


def block(reason: str) -> None:
    sys.stderr.write(f"BLOCKED by Claude guardrail: {reason}\n")
    sys.stderr.write(
        "If intentional, run the command yourself in your shell.\n"
    )
    sys.exit(2)


def check_bash(command: str, cwd: str) -> None:
    branch = current_branch(cwd)
    on_protected = branch in PROTECTED_BRANCHES

    is_push = bool(re.search(r"\bgit\s+push\b", command))

    if is_push and has_protected_refspec_force(command):
        block("git push with force-refspec '+main'")

    if is_push and has_force_flag(command):
        if mentions_protected(command):
            block("force-push references protected branch (main)")
        if on_protected and not push_specifies_branch(command):
            block(
                f"force-push of upstream-tracking on protected branch '{branch}'"
            )

    if re.search(r"\bgit\s+reset\s+(?:[^&|;]*\s)?--hard\b", command) and on_protected:
        block(f"git reset --hard while on protected branch '{branch}'")

    if re.search(r"\bgit\s+branch\s+(?:-D|--delete\s+--force)\b", command) and re.search(
        rf"(?:^|\s){PROTECTED_RE}(?:\s|$)", command
    ):
        block("deletion of protected branch (main)")

    if re.search(rf"\bgit\s+update-ref\b[^&|;]*refs/heads/{PROTECTED_RE}", command):
        block("git update-ref on refs/heads/main")

    if re.search(r"\bgit\s+(?:commit|push)\b[^&|;]*--no-verify\b", command):
        block("git commit/push --no-verify (bypasses pre-commit/pre-push hooks)")

    if re.search(r"\bgit\s+config\b[^&|;]*\bcore\.hooksPath\b", command):
        block("git config core.hooksPath (would redirect/disable git hooks)")

    if re.search(r"\.git/hooks(?:/|\b)", command):
        write_verbs = (
            r"(?:>>?|"
            r"\brm\b|\bmv\b|\bcp\b|\btee\b|\bsed\s+-i\b|"
            r"\btruncate\b|\bchmod\s+[+-]?[0-7uogax=+-]*[xX]?\b|"
            r"\bln\b|\btouch\b|\bcat\s*<<)"
        )
        if re.search(write_verbs, command):
            block("shell modification of .git/hooks/* (would disable git hooks)")


def check_path(tool_input: dict) -> None:
    candidates = []
    fp = tool_input.get("file_path")
    if fp:
        candidates.append(fp)
    # MultiEdit-style and Notebook variants
    for edit in tool_input.get("edits", []) or []:
        if isinstance(edit, dict) and edit.get("file_path"):
            candidates.append(edit["file_path"])
    nbp = tool_input.get("notebook_path")
    if nbp:
        candidates.append(nbp)

    for path in candidates:
        if not isinstance(path, str):
            continue
        if re.search(r"(?:^|/)\.git/hooks(?:/|$)", path):
            block(f"modification of .git/hooks: {path}")
        if re.search(r"(?:^|/)\.git/config$", path):
            block(f"modification of .git/config: {path}")


def main() -> None:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        sys.exit(0)

    tool_name = data.get("tool_name", "")
    tool_input = data.get("tool_input", {}) or {}

    if tool_name == "Bash":
        command = tool_input.get("command", "")
        if command:
            check_bash(command, data.get("cwd", ""))
    elif tool_name in PATH_TOOLS:
        check_path(tool_input)

    sys.exit(0)


if __name__ == "__main__":
    main()
