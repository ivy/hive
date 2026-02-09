# AGENTS.md

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests by dispatching Claude Code agents in isolated workspaces on a home server.

See `docs/vision.md` for motivation, `docs/core-principles.md` for decision filters.

## Repo Map

```
cmd/hive/               # CLI entry point (cobra commands)
internal/
  authz/                # Issue author authorization
  github/               # Board status, issue fetch, PR creation, push
  jail/                 # Execution environment (systemd-run, swappable)
  prdraft/              # Structured-output PR drafting via Claude
  workspace/            # Git worktree creation, metadata, lifecycle
docs/
  vision.md             # Why this project exists
  core-principles.md    # Decision filters (6 principles)
  architecture.md       # Component design, data flow, trust boundaries
  prototype-learnings.md # Constraints from the shell prototype
  adrs/                 # Architecture Decision Records
```

## Project Board

[GitHub Projects board](https://github.com/users/ivy/projects/26) drives the pipeline. Board columns and field option IDs:

| Column | Option ID | Role |
|--------|-----------|------|
| Triage 📥 | `0282c225` | New issues land here |
| Icebox 🧊 | `8305ec23` | Parked/deferred |
| Planning 🧠 | `6d145703` | Needs design/breakdown |
| Ready 🤖 | `11a2b218` | `hive poll` picks these up |
| In Progress 🚧 | `dacd8d8c` | Agent is working |
| In Review 👀 | `2f8088e7` | PR opened, awaiting review |
| Done ✅ | `8ee47ba7` | Merged |

Status field ID: `PVTSSF_lAHOADgWUM4BOoACzg9RZqI`. Project node ID: `PVT_kwHOADgWUM4BOoAC`. Configured in `~/.config/hive/config.toml` — see `hive.example.toml` for format.

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
| Config | viper (`~/.config/hive/config.toml`) |
| Logging | `log/slog` + `systemd/slog-journal` |
| GitHub | `gh` CLI via `os/exec` |
| Sandbox | `systemd-run` via `os/exec` (behind `Jail` interface) |
| Testing | Ginkgo/Gomega (BDD) |

See `docs/adrs/002-tech-stack.md` for rationale.

## How to Work Here

```bash
go test ./...       # run all specs (Ginkgo BDD via go test)
go vet ./...        # static analysis
make build          # build binary to ./hive
make install        # install to ~/.local/bin/hive
```

Specs use Ginkgo/Gomega BDD style (`Describe`/`Context`/`It`). Write specs that read like requirements — agents use them to understand expected behavior.

Tool versions managed by mise — see `mise.toml`. Pre-commit hooks managed by hk — see `hk.pkl`.

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
