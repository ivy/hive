# Hive

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests
by dispatching Claude Code agents in isolated workspaces on a home server.

## How It Works

```
GitHub Projects board          Hive                          Agent (Claude Code)
─────────────────────          ────                          ───────────────────
Ready 🤖  ──poll──→  prepare → worktree + metadata
                               exec ──────────────→  edit, test, commit
                               publish ← result
In Review 👀 ←────── push branch, open PR
```

1. Drag an issue to **Ready** on your project board
2. `hive poll` picks it up, creates an isolated workspace, and launches an agent
3. The agent implements the change, tests it, and commits
4. Hive pushes the branch and opens a PR
5. You review when ready

## Requirements

- Linux with systemd (process-level sandboxing via `systemd-run`)
- Go 1.25+
- [gh](https://cli.github.com/) CLI (authenticated)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI
- [mise](https://mise.jdx.dev/) (tool version management)

## Setup

### Build

```bash
go build -o hive ./cmd/hive/
```

### Repo Discovery

Hive finds repos on disk by convention:

```
~/src/github.com/<owner>/<repo>/
```

An issue on `ivy/dotfiles#143` maps to `~/src/github.com/ivy/dotfiles/`. If the
repo isn't at that path, `prepare` will fail.

### Configuration

Create `.hive.toml` in the repo you're running hive from (or pass `--config`):

```toml
[jail]
backend = "systemd-run"    # default, only backend currently

[github]
project-id = "10"                              # GitHub project number
status-field-id = "PVTSSF_..."                 # Status field ID
in-progress-option-id = "f75ad846"             # "In Progress" option ID
in-review-option-id = "47fc9ee4"               # "In Review" option ID
```

To find your project field IDs:

```bash
# List project fields
gh project field-list <project-number> --owner @me

# The status field and its options will show IDs you need
```

### Environment

```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # required for exec
```

## Usage

### Full Pipeline

```bash
# Workspace → agent → PR (the common case)
hive run ivy/dotfiles#143

# Skip PR creation (review the workspace first)
hive run --no-publish ivy/dotfiles#143
```

### Manual Stages

Each stage is independently invocable and re-entrant:

```bash
# Create workspace from issue
hive prepare ivy/dotfiles#143

# Launch agent in sandboxed workspace
hive exec /tmp/hive/dotfiles-143-1738900000

# Push branch and open PR
hive publish /tmp/hive/dotfiles-143-1738900000
```

### Resume With Feedback

```bash
# Re-run agent with reviewer feedback
hive exec --resume "fix the test assertion" /tmp/hive/dotfiles-143-1738900000
```

### Polling

```bash
# Find Ready items on the board and dispatch runs
hive poll
```

Requires `github.project-id` in `.hive.toml`. Designed to run on a systemd
timer.

### Inspect and Manage

```bash
# List all workspaces
hive list

# Attach to running agent's tmux session
hive attach dotfiles-143

# Clean up a workspace
hive cleanup dotfiles-143

# Clean up all workspaces
hive cleanup --all
```

### Global Flags

```
--config    config file path (default: .hive.toml in current dir)
--verbose   enable debug logging
```

## Workspace Layout

Each workspace is a git worktree with `.hive/` metadata:

```
/tmp/hive/<project>-<issue>-<timestamp>/
├── .hive/
│   ├── repo              # e.g. "ivy/dotfiles"
│   ├── issue-number      # e.g. "143"
│   ├── session-id        # Claude session UUID
│   ├── status            # prepared|running|stopped|published|failed
│   ├── issue.json        # full issue payload
│   ├── prompt.md         # issue body as agent prompt
│   ├── board-item-id     # project board item ID (if from poll)
│   └── tmux-session      # tmux session name (if attached)
└── <repo contents>
```

The workspace directory is the contract between stages. If a stage crashes,
re-run it against the same workspace.

## Sandboxing

Agents run inside `systemd-run --user --pty` with:

- `ProtectSystem=strict` — system paths read-only
- `TemporaryFileSystem=$HOME` — fresh home directory
- `PrivateTmp=yes` — isolated `/tmp`
- `NoNewPrivileges=yes` — no privilege escalation

The agent gets:

| Mount | Access | Purpose |
|-------|--------|---------|
| Worktree | read-write | Edit, test, commit |
| Repo `.git` | read-write | Git operations |
| `~/.claude` | read-write | Session persistence |
| `~/.local/bin` | read-only | Tool binaries |
| `~/.local/share/mise` | read-only | mise installs + shims |
| `~/.local/share/claude` | read-only | Claude binary |

The agent **cannot** access SSH keys, GitHub tokens, 1Password, or other repos.

## Development

```bash
go test ./...       # run all specs (Ginkgo BDD)
go vet ./...        # static analysis
go build ./...      # build
```

Tests use [Ginkgo/Gomega](https://onsi.github.io/ginkgo/) BDD style. Specs
double as living requirements documentation.

## Architecture

See `docs/` for detailed design:

- [`docs/vision.md`](docs/vision.md) — why this project exists
- [`docs/core-principles.md`](docs/core-principles.md) — 6 decision filters
- [`docs/architecture.md`](docs/architecture.md) — component design and data flow
- [`docs/adrs/`](docs/adrs/) — architecture decision records
