# Session and Data Reference

Complete reference for session JSON, workspace layout, claim files, and data directory structure.

## Data Directory

All persistent data lives under `$XDG_DATA_HOME/hive/` (default: `~/.local/share/hive/`).

```
~/.local/share/hive/
├── sessions/          # Session JSON files
│   ├── {uuid}.json
│   └── ...
├── claims/            # Claim lock files
│   ├── {hash}
│   └── ...
└── workspaces/        # Git worktrees
    ├── {uuid}/
    │   ├── .hive/     # Workspace metadata
    │   └── ...        # Repository files (git worktree)
    └── ...
```

## Session JSON

Each dispatched work item is tracked as `{dataDir}/sessions/{uuid}.json`.

### Schema

```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "ref": "github:ivy/hive#42",
  "repo": "ivy/hive",
  "title": "Add dark mode support",
  "prompt": "Issue body text used as the agent prompt...",
  "source_metadata": {
    "board_item_id": "PVTI_abc123",
    "project_node_id": "PVT_kwHOADgWUM4BOsG3"
  },
  "status": "running",
  "created_at": "2025-01-15T10:30:00Z",
  "poll_instance": "default"
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | UUIDv4 identifier. Used as directory name for workspace and filename for session JSON. |
| `ref` | string | Source reference. Format: `github:{owner}/{repo}#{number}` (e.g. `github:ivy/hive#42`). |
| `repo` | string | Repository identifier (e.g. `ivy/hive`). |
| `title` | string | Human-readable issue title. |
| `prompt` | string | Agent prompt text (typically the issue body). |
| `source_metadata` | map[string]string | Source-specific data. For GitHub Projects: `board_item_id` and `project_node_id`. |
| `status` | string | Lifecycle status (see state machine below). |
| `created_at` | string (RFC 3339) | UTC timestamp when the session was created. |
| `poll_instance` | string | Name of the poll instance that dispatched this session (default: `"default"`). |

### Status State Machine

```
dispatching ──→ prepared ──→ running ──→ stopped ──→ published
     │              │            │           │
     └──────────────┴────────────┴───────────┴──→ failed
```

| Status | Set By | Description |
|--------|--------|-------------|
| `dispatching` | `poll` | Session created, systemd unit starting. |
| `prepared` | `run` | Workspace created, metadata written, prompt ready. |
| `running` | `run` (exec) | Agent is executing inside the jail. |
| `stopped` | `run` (exec) | Agent finished, workspace has commits. |
| `published` | `run` (publish) | Branch pushed, PR created, board updated. |
| `failed` | `run`, `reap` | Any stage failed, or reap detected a stale session. |

**Terminal states:** `published` and `failed`. Reap cleans up terminal sessions after their retention period. Reap marks non-terminal sessions as `failed` if their systemd unit is no longer active.

## Claim Files

Claims prevent duplicate dispatch of the same work item. Located at `{dataDir}/claims/`.

### Format

- **Filename:** first 12 hex characters of SHA-256 hash of the source ref
- **Content:** session UUID (plain text)

### Operations

| Operation | Behavior |
|-----------|----------|
| `TryClaim` | Atomically creates claim file using `O_EXCL`. Returns `(true, nil)` on success, `(false, nil)` if already claimed. |
| `Release` | Removes the claim file. Idempotent — returns nil if file doesn't exist. |
| `Exists` | Checks if a claim file exists for the given ref. |
| `SessionForRef` | Reads the session UUID from the claim file. |
| `ListAll` | Returns all active claims. |

### Example

For ref `github:ivy/hive#42`:

```
# SHA-256("github:ivy/hive#42") = a9f3e7b2c1d4...
# Filename: a9f3e7b2c1d4

~/.local/share/hive/claims/a9f3e7b2c1d4
# Content: a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

## Workspace Layout

Each workspace is a git worktree at `{dataDir}/workspaces/{uuid}/` with a `.hive/` metadata directory.

### Directory Structure

```
{dataDir}/workspaces/{uuid}/
├── .hive/
│   ├── repo              # "ivy/hive"
│   ├── issue-number      # "42"
│   ├── session-id        # UUID
│   ├── status            # "prepared", "running", "stopped", "published", "failed"
│   ├── prompt.md         # Issue body (agent prompt)
│   ├── issue.json        # Full issue JSON (prepare path only)
│   ├── board-item-id     # GitHub Projects item ID (optional)
│   └── tmux-session      # tmux session name (written during exec)
├── .git                  # Worktree git link
└── ...                   # Repository source files
```

### Metadata Files

| File | Written By | Content |
|------|-----------|---------|
| `repo` | `prepare` | Repository identifier (e.g. `ivy/hive`) |
| `issue-number` | `prepare` | Issue number as string (e.g. `42`) |
| `session-id` | `prepare` | Session UUID, also used as Claude session ID for resume |
| `status` | `prepare`, `exec`, `publish` | Current workspace lifecycle status |
| `prompt.md` | `prepare` | Issue body text used as the agent prompt |
| `issue.json` | `prepare` (direct path) | Full GitHub issue JSON. Only written by `hive prepare`, not by the session-based path in `hive run`. |
| `board-item-id` | `run` | GitHub Projects board item ID for status updates |
| `tmux-session` | jail/exec | tmux session name for `hive attach` |

### Workspace Status Values

| Status | Description |
|--------|-------------|
| `prepared` | Worktree created, metadata written, ready for exec |
| `running` | Agent is executing |
| `stopped` | Agent finished execution |
| `published` | Branch pushed, PR created |
| `failed` | Execution or publish failed |

### Git Conventions

- **Worktree base:** branches from `main`
- **Branch name:** `hive/{uuid}`
- **Base directory:** `{dataDir}/workspaces/`
- **Uncommitted changes:** `.hive/` files are excluded from uncommitted-changes detection and auto-commits

### Workspace Struct

The Go struct used internally:

```go
type Workspace struct {
    Path        string  // Absolute path to worktree directory
    RepoPath    string  // Absolute path to main repository
    Repo        string  // GitHub owner/name (e.g. "ivy/hive")
    IssueNumber int     // GitHub issue number
    Branch      string  // Git branch name (e.g. "hive/{uuid}")
    SessionID   string  // Claude session UUID for resume
    Status      Status  // Current lifecycle state
}
```
