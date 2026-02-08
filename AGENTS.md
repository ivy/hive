# AGENTS.md

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests by dispatching Claude Code agents in isolated workspaces on a home server.

See `docs/vision.md` for motivation, `docs/core-principles.md` for philosophy.

## Repo Map

```
cmd/hive/           # CLI entry point
internal/
  workspace/        # Git worktree creation, metadata, lifecycle
  jail/             # Execution environment (systemd-run, swappable)
  github/           # Board status, issue fetch, PR creation, push
docs/
  vision.md         # Why this project exists
  core-principles.md # Decision filters
  architecture.md   # Component decomposition, data flow, trust boundaries
  prototype-learnings.md # What we learned from the shell prototype
```

## How It Works

```
GitHub Projects board          Hive (this tool)              Agent (Claude Code)
─────────────────────          ────────────────              ───────────────────
Ready 🤖  ──poll──→  prepare → worktree + metadata
                               exec ──────────────→  edit, test, commit
                               publish ← result
In Review 👀 ←────── push branch, open PR
```

Five commands, each with a single responsibility:

| Command | Does | Credentials |
|---------|------|-------------|
| `hive poll` | Finds Ready items on the board, dispatches `run` | GitHub (read board) |
| `hive prepare` | Creates worktree, writes metadata, extracts prompt | GitHub (read issue) |
| `hive exec` | Launches agent in jailed workspace | Claude API key |
| `hive publish` | Pushes branch, opens PR, updates board | GitHub (write) |
| `hive run` | Orchestrates prepare → exec → publish | All of the above |

## Language & Tools

- **Go** — single binary, good CLI libraries, easy cross-compile
- **mise** — tool version management (see `mise.toml`)

## Change Expectations

- Follow existing patterns over introducing new ones
- Keep diffs focused
- Test with `go test ./...`
- One change per commit

## Key Docs

- `docs/architecture.md` — component decomposition, workspace layout, trust boundaries
- `docs/prototype-learnings.md` — constraints discovered through real usage
- `docs/core-principles.md` — decision filters (workspace as contract, credential isolation, etc.)
