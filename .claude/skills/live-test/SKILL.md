---
name: live-test
description: Use when the user wants to run an end-to-end test of hive against real GitHub infrastructure.
argument-hint: "[issue title and body]"
model: sonnet
allowed-tools:
  - Read
  - Glob
  - Grep
  - Bash(go install ./cmd/hive/:*)
  - Bash(go build ./...:*)
  - Bash(gh project item-list 29:*)
  - Bash(gh project field-list 29:*)
  - Bash(gh issue list --repo ivy/hive-sandbox:*)
  - Bash(gh pr list --repo ivy/hive-sandbox:*)
  - Bash(gh pr view * --repo ivy/hive-sandbox:*)
  - Bash(gh pr diff * --repo ivy/hive-sandbox:*)
  - Bash(ps aux:*)
  - Bash(ls /tmp/hive:*)
  - Bash(hive poll:*)
  - Bash(hive list:*)
---

# Live E2E Test

Run the full hive pipeline (`poll → prepare → exec → publish`) against the dedicated test infrastructure.

## Arguments

```
$ARGUMENTS
```

## Test Infrastructure

| Resource | Value |
|----------|-------|
| Test repo | `ivy/hive-sandbox` |
| Project board | "Hive Test 🧪" (#29) |
| Project node ID | `PVT_kwHOADgWUM4BOsG3` |
| Status field ID | `PVTSSF_lAHOADgWUM4BOsG3zg9UQiM` |
| Ready 🤖 option | `fd7146e3` |
| In Progress 🚧 option | `6e4a1660` |
| In Review 👀 option | `1b866fe9` |
| Done ✅ option | `5a45bee9` |
| Default test issue | `ivy/hive-sandbox#1` — "feat: add greeting function to hello.py" |

## Instructions

### 1. Verify Prerequisites

- Confirm `.hive.toml` points at project 29 (`project-id = "29"`)
- Run `go install ./cmd/hive/` to ensure latest binary

### 2. Reset Sandbox

Clean up artifacts from any previous run so the default issue can be reused:

```bash
# Close open PRs targeting the test issue
gh pr list --repo ivy/hive-sandbox --json number,headRefName --state open
gh pr close <n> --repo ivy/hive-sandbox --delete-branch
```

If `hello.py` was modified by a previous agent run, reset it on main:

```bash
cd ~/src/github.com/ivy/hive-sandbox
git checkout main && git pull
# Restore hello.py to baseline if needed
```

Baseline `hello.py`:

```python
"""A simple hello module."""


def main():
    print("Hello, world!")


if __name__ == "__main__":
    main()
```

Remove any `test_hello.py` or other agent-created files, commit, and push.

### 3. Queue Issue

**If arguments provided:** Create a new issue with them as the title/body.

**If no arguments:** Reuse `ivy/hive-sandbox#1`.

Check if the issue is already on the board:

```bash
gh project item-list 29 --owner @me --format json
```

- If the issue is already "Ready 🤖" → skip to step 4
- If the issue is on the board in another status → move it back to "Ready 🤖"
- If the issue is not on the board → add it

```bash
# Add to board (if needed)
gh project item-add 29 --owner @me --url https://github.com/ivy/hive-sandbox/issues/1 --format json

# Set to Ready 🤖
gh project item-edit --project-id PVT_kwHOADgWUM4BOsG3 \
  --id <item-id> \
  --field-id PVTSSF_lAHOADgWUM4BOsG3zg9UQiM \
  --single-select-option-id fd7146e3
```

### 4. Run Pipeline

```bash
hive poll --verbose
```

This dispatches `hive run` in the background. The command returns after dispatch.

### 5. Monitor

Watch the dispatched process:

- Check `ps aux | grep hive` for the running subprocess
- Check `/tmp/hive/` for the workspace directory
- Read `.hive/status` in the workspace to track progress (`preparing` → `executing` → `publishing` → `published`)

Wait for the status to reach `published` or for the process to exit. Use a timeout of 10 minutes.

### 6. Verify Results

Run in parallel:

- `gh pr list --repo ivy/hive-sandbox --json number,title,state,url` — confirm PR was created
- `gh pr diff <pr-number> --repo ivy/hive-sandbox` — show what the agent implemented
- `gh project item-list 29 --owner @me --format json` — confirm item moved to "In Review 👀"

Report a summary:

- PR URL and title
- Files changed
- Board item status
- Any errors encountered

## Examples

```
/live-test
→ Resets sandbox, re-queues issue #1, runs full pipeline

/live-test feat: add multiply function — Add multiply(a, b) to hello.py with tests
→ Resets sandbox, creates new issue, runs full pipeline
```
