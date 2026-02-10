# CLI Reference

Complete reference for every `hive` command, its flags, arguments, and behavior.

## Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--config` | string | `~/.config/hive/config.toml` | Config file path |
| `--verbose` | bool | `false` | Enable debug logging (sets log level to DEBUG) |

## hive poll

Query the source for ready work items, claim them locally, and dispatch systemd units.

### Synopsis

```
hive poll [flags]
```

### Description

Polls a configured source (GitHub Projects board) for items in the Ready column. For each ready item, poll:

1. Checks the `poll.max-concurrent` limit against active `hive-run@*` units
2. Creates a local claim file to prevent duplicate dispatch
3. Writes a session JSON file with status `dispatching`
4. Starts a `hive-run@{uuid}.service` systemd user unit
5. Marks the item as taken on the source (moves to In Progress)

With `--interval`, runs as a long-lived daemon. Without it, runs once and exits.

### Flags

| Flag | Type | Default | Config key | Description |
|------|------|---------|------------|-------------|
| `--interval` | duration | `0` (single-shot) | `poll.interval` | Poll interval (e.g. `5m`); if unset, run once and exit |

### Config Keys Used

- `poll.interval` — poll frequency
- `poll.max-concurrent` — maximum active `hive-run@*` units (0 = unlimited)
- `poll.instance` — instance name for session tracking (default: `"default"`)
- `github.project-id` — GitHub Projects board number (required)
- `github.project-node-id` — GitHub Projects GraphQL node ID (required)
- `github.ready-status` — exact status name to filter (e.g. `"Ready 🤖"`)
- `github.status-field-id` — project status field ID
- `github.in-progress-option-id` — option ID for In Progress column
- `github.in-review-option-id` — option ID for In Review column
- `github.ready-option-id` — option ID for Ready column
- `security.allowed-users` — authorized issue authors (required, fail-closed)

### Credentials

- GitHub token (via `gh` CLI authentication) — read board, fetch issues, update status

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (single-shot) or clean shutdown (daemon) |
| 1 | Configuration error or source build failure |

### Examples

```bash
# Single-shot: poll once and exit
hive poll

# Daemon mode: poll every 5 minutes
hive poll --interval 5m

# With explicit config
hive poll --config ~/.config/hive/production.toml
```

---

## hive run

Orchestrate the full pipeline: prepare → exec → publish.

### Synopsis

```
hive run <uuid | owner/repo#issue> [flags]
```

### Description

Runs the complete agent pipeline. Accepts either:

- **UUID** — systemd dispatch path. Loads an existing session JSON, creates workspace from session data (prompt and title already fetched by poll).
- **Issue reference** (`owner/repo#123`) — manual workflow. Creates a new session, fetches issue from GitHub, validates author against `security.allowed-users`, then proceeds.

Both paths produce identical workspace layouts. The pipeline stages are:

1. **Prepare** — create git worktree, write `.hive/` metadata and prompt
2. **Exec** — launch Claude Code agent inside jail
3. **Publish** — push branch, draft PR, open PR, update board

Session status transitions: `dispatching` → `prepared` → `running` → `stopped` → `published` (or `failed` on error).

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<uuid \| owner/repo#issue>` | Yes | Session UUID or issue reference |

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-publish` | bool | `false` | Skip the publish step |
| `--model` | string | `"sonnet"` | Claude model to use |
| `--board-item-id` | string | `""` | GitHub Projects board item ID (manual workflow) |

### Credentials

- GitHub token (via `gh` CLI) — fetch issues, push branches, create PRs, update board
- `ANTHROPIC_API_KEY` (optional) — passed to agent; if empty, subscription auth via `~/.claude/`

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Pipeline completed successfully |
| 1 | Any stage failed (session marked as `failed`) |

### Examples

```bash
# Systemd dispatch (called by hive-run@{uuid}.service)
hive run a1b2c3d4-e5f6-7890-abcd-ef1234567890

# Manual run from issue reference
hive run ivy/hive#42

# Manual run, skip publish for local review
hive run ivy/hive#42 --no-publish

# Use a specific model
hive run ivy/hive#42 --model opus
```

---

## hive prepare

Create a workspace from a GitHub issue.

### Synopsis

```
hive prepare <owner/repo#issue>
```

### Description

Fetches issue data from GitHub, validates the issue author against `security.allowed-users`, then creates a git worktree with `.hive/` metadata. Writes:

- `.hive/repo` — repository identifier
- `.hive/issue-number` — issue number
- `.hive/session-id` — UUID
- `.hive/status` — set to `prepared`
- `.hive/issue.json` — full issue JSON payload
- `.hive/prompt.md` — issue body as agent prompt

The workspace is created at `{dataDir}/workspaces/{uuid}` as a git worktree branching from `main` with branch name `hive/{uuid}`.

The local repo is resolved by convention: `~/src/github.com/{owner}/{repo}/`.

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<owner/repo#issue>` | Yes | Issue reference (e.g. `ivy/hive#42`) |

### Credentials

- GitHub token (via `gh` CLI) — fetch issue data

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Workspace created successfully |
| 1 | Issue fetch failed, author not allowed, or repo not found on disk |

### Examples

```bash
hive prepare ivy/hive#42
# Output:
# Workspace ready: /home/user/.local/share/hive/workspaces/abc123...
# Branch: hive/abc123...
# Issue: Add dark mode support
```

---

## hive exec

Launch an agent in a sandboxed workspace.

### Synopsis

```
hive exec <workspace-path> [flags]
```

### Description

Loads a prepared workspace, sets status to `running`, and launches Claude Code inside the configured jail backend. The agent runs with:

- `--dangerously-skip-permissions` — headless mode
- `--append-system-prompt` — instructs agent to commit work before finishing
- `--output-format json` — structured output
- `--session-id` — workspace session UUID (for resume support)

After the agent exits, if uncommitted changes remain, exec retries up to 3 times by resuming the Claude session with a commit nudge message. Finally sets status to `stopped`.

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<workspace-path>` | Yes | Absolute path to workspace directory |

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--resume` | string | `""` | Resume session with a feedback message |
| `--model` | string | `"sonnet"` | Claude model to use |

### Config Keys Used

- `jail.backend` — sandbox backend (default: `"systemd-run"`)

### Credentials

- `ANTHROPIC_API_KEY` (optional) — passed to agent inside jail

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Agent finished successfully |
| 1 | Workspace load failed or agent execution failed |

### Examples

```bash
# Run agent in a prepared workspace
hive exec /home/user/.local/share/hive/workspaces/abc123

# Resume with feedback
hive exec /home/user/.local/share/hive/workspaces/abc123 --resume "Fix the failing test"
```

---

## hive publish

Push branch, open PR, and update the project board.

### Synopsis

```
hive publish <workspace-path>
```

### Description

Loads a workspace and performs the publish pipeline:

1. **Auto-commit** — if uncommitted changes remain after exec retries, stages all non-`.hive/` files and commits with a generic message
2. **Push branch** — pushes the worktree branch to origin
3. **Draft PR** — uses Claude Code (via jail, model `sonnet`) to generate a PR title and body from the branch diff. Falls back to a mechanical title/body if drafting fails
4. **Create PR** — opens a pull request via `gh pr create`
5. **Update board** — moves the board item to In Review (requires `board-item-id` from session metadata or `.hive/board-item-id` file)
6. **Set status** — marks workspace as `published`

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<workspace-path>` | Yes | Absolute path to workspace directory |

### Config Keys Used

- `jail.backend` — sandbox backend for PR drafting (default: `"systemd-run"`)
- `github.project-node-id` — for board item update
- `github.status-field-id` — project status field ID
- `github.in-progress-option-id` — In Progress option ID
- `github.in-review-option-id` — In Review option ID

### Credentials

- GitHub token (via `gh` CLI) — push branch, create PR, update board
- `ANTHROPIC_API_KEY` (optional) — for PR drafting

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | PR created and board updated |
| 1 | Push, PR creation, or workspace load failed |

### Examples

```bash
hive publish /home/user/.local/share/hive/workspaces/abc123
# Output:
# Published: https://github.com/ivy/hive/pull/99
```

---

## hive reap

Clean up finished sessions and recover stuck items.

### Synopsis

```
hive reap [flags]
```

### Description

Scans all sessions and performs two kinds of cleanup:

**Expired terminal sessions** — published or failed sessions past their retention period:

- Removes the git worktree and workspace directory
- Deletes the session JSON file
- Releases the claim file

**Stale non-terminal sessions** — sessions whose `hive-run@{uuid}.service` unit is no longer active:

- Marks the session as `failed`
- Releases the item on the source board (moves back to Ready)
- Releases the claim file

### Flags

| Flag | Type | Default | Config key | Description |
|------|------|---------|------------|-------------|
| `--published-retention` | duration | `24h` | `reap.published-retention` | Retention for published sessions |
| `--failed-retention` | duration | `72h` | `reap.failed-retention` | Retention for failed sessions |

### Config Keys Used

- `reap.published-retention` — how long to keep published sessions
- `reap.failed-retention` — how long to keep failed sessions
- All `github.*` and `security.*` keys (for building source to release items)

### Credentials

- GitHub token (via `gh` CLI) — release items back to Ready on the board

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Reap completed (errors on individual sessions are logged, not fatal) |
| 1 | Failed to list sessions |

### Examples

```bash
# Default retention periods
hive reap

# Custom retention
hive reap --published-retention 48h --failed-retention 168h

# With config file
hive reap --config ~/.config/hive/production.toml
```

---

## hive ls

List all sessions.

### Synopsis

```
hive ls
```

### Description

Lists all sessions from `{dataDir}/sessions/` in a table sorted by creation time (most recent first). Columns:

| Column | Description |
|--------|-------------|
| UUID | Session identifier |
| REF | Source reference (e.g. `github:ivy/hive#42`) |
| STATUS | Lifecycle status |
| CREATED | Relative time (e.g. `5m ago`, `2h ago`, `3d ago`) |

### Arguments

None.

### Credentials

None.

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Sessions listed (or "No sessions found.") |
| 1 | Failed to read sessions directory |

### Examples

```bash
hive ls
# UUID                                  REF                   STATUS      CREATED
# a1b2c3d4-e5f6-7890-abcd-ef1234567890  github:ivy/hive#42    published   2h ago
# f0e1d2c3-b4a5-6789-0123-456789abcdef  github:ivy/hive#43    running     5m ago
```

---

## hive cd

Open a shell in a session's workspace.

### Synopsis

```
hive cd <ref|uuid>
```

### Description

Resolves the session by ref or UUID and spawns `$SHELL` (falling back to `/bin/sh`) in the workspace directory. Useful for inspecting agent work in progress.

Session resolution order:

1. If the argument is a UUID, loads `sessions/{uuid}.json` directly
2. Otherwise, normalizes as `github:{ref}` and checks claims for an active session
3. Falls back to scanning all sessions for matching ref (most recent wins)

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<ref\|uuid>` | Yes | Session UUID or issue ref (e.g. `ivy/hive#42`) |

### Credentials

None.

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Shell exited normally |
| 1 | Session not found or workspace missing |

### Examples

```bash
# By UUID
hive cd a1b2c3d4-e5f6-7890-abcd-ef1234567890

# By issue ref
hive cd ivy/hive#42
```

---

## hive attach

Attach to a running agent's tmux session.

### Synopsis

```
hive attach <ref|uuid>
```

### Description

Resolves the session, reads the tmux session name from `.hive/tmux-session`, and runs `tmux attach-session -t {name}`. Use this to observe a running agent in real time.

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<ref\|uuid>` | Yes | Session UUID or issue ref |

### Credentials

None.

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Detached from tmux normally |
| 1 | Session not found, workspace missing, or no tmux session file (agent may not be running) |

### Examples

```bash
hive attach ivy/hive#42
```

---

## hive cleanup

> **Deprecated:** use `hive reap` instead.

Remove workspace(s) by ID or all at once.

### Synopsis

```
hive cleanup [workspace-id] [flags]
```

### Description

Removes a workspace by ID (matches partial UUID against workspace directory names), or all workspaces with `--all`. Each removal tears down the git worktree, deletes the branch, and prunes worktree metadata.

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `[workspace-id]` | No (if `--all`) | Full or partial workspace ID |

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | `false` | Remove all workspaces |

### Credentials

None.

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Workspace(s) removed |
| 1 | Workspace not found or removal failed |

---

## hive list

> **Deprecated:** use `hive ls` instead.

Alias for `hive ls`. Same behavior and output.
