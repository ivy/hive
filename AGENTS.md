# AGENTS.md

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests by dispatching Claude Code agents in isolated workspaces on a home server.

See `docs/vision.md` for motivation, `docs/core-principles.md` for decision filters.

## Status

Greenfield — Go module declared, no source code yet. Docs and a validated shell prototype exist.

## Repo Map

```
docs/
  vision.md             # Why this project exists
  core-principles.md    # Decision filters (6 principles)
  architecture.md       # Component design, data flow, trust boundaries
  prototype-learnings.md # Constraints from the shell prototype
  prototype/            # Shell prototype scripts and specs
  adrs/                 # Architecture Decision Records
go.mod                  # Module: github.com/ivy/hive, Go 1.25.7
```

**Planned structure** (not yet created):

```
cmd/hive/               # CLI entry point
internal/
  workspace/            # Git worktree creation, metadata, lifecycle
  jail/                 # Execution environment (systemd-run, swappable)
  github/               # Board status, issue fetch, PR creation, push
```

## Pipeline

```
GitHub Projects board          Hive (this tool)              Agent (Claude Code)
─────────────────────          ────────────────              ───────────────────
Ready 🤖  ──poll──→  prepare → worktree + metadata
                               exec ──────────────→  edit, test, commit
                               publish ← result
In Review 👀 ←────── push branch, open PR
```

| Command | Does | Credentials |
|---------|------|-------------|
| `hive poll` | Finds Ready items on the board, dispatches `run` | GitHub (read board) |
| `hive prepare` | Creates worktree, writes metadata, extracts prompt | GitHub (read issue) |
| `hive exec` | Launches agent in jailed workspace | Claude API key |
| `hive publish` | Pushes branch, opens PR, updates board | GitHub (write) |
| `hive run` | Orchestrates prepare → exec → publish | All of the above |

Additional commands: `list`, `attach`, `cleanup` — see `docs/architecture.md`.

## Tech Stack

| Concern | Choice |
|---------|--------|
| CLI | cobra + pflag |
| Config | viper (per-repo `.hive.toml`) |
| Logging | `log/slog` + `systemd/slog-journal` |
| GitHub | `gh` CLI via `os/exec` |
| Sandbox | `systemd-run` via `os/exec` (behind `Jail` interface) |
| Testing | stdlib `testing` |

See `docs/adrs/002-tech-stack.md` for rationale.

## How to Work Here

```bash
go test ./...       # run tests
go vet ./...        # static analysis
go build ./...      # build
```

Tool versions managed by mise — see `.mise.toml` (when created).

## Change Expectations

- Follow existing patterns over introducing new ones
- Keep diffs focused — one logical change per commit
- Test with `go test ./...`
- Read the relevant principle in `docs/core-principles.md` before making architectural choices
- Don't add dependencies without checking `docs/adrs/002-tech-stack.md`

## Key Docs

- `docs/architecture.md` — component decomposition, workspace layout, trust boundaries, CLI examples
- `docs/core-principles.md` — 6 decision filters (workspace as contract, credential isolation, environment parity, etc.)
- `docs/prototype-learnings.md` — constraints discovered through real usage
- `docs/adrs/` — architecture decision records (start with `001-swappable-jail-backends.md`)
