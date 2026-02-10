# AGENTS.md

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests by dispatching Claude Code agents in isolated workspaces on a home server.

See `docs/vision.md` for motivation, `docs/core-principles.md` for decision filters.

## Repo Map

```
cmd/hive/               # CLI entry point (cobra commands)
internal/
  authz/                # Issue author authorization
  claim/                # Atomic claim files for work-item dedup
  github/               # Board status, issue fetch, PR creation, push
  jail/                 # Execution environment (systemd-run, swappable)
  prdraft/              # Structured-output PR drafting via Claude
  session/              # Session struct + JSON CRUD (~/.local/share/hive/sessions/)
  source/               # Source interface (Ready, Take, Complete, Release)
  source/ghprojects/    # GitHub Projects adapter implementing Source
  workspace/            # Git worktree creation, metadata, lifecycle
contrib/
  systemd/              # systemd unit templates (poll, run, reap, target)
docs/
  vision.md             # Why this project exists
  core-principles.md    # Decision filters (6 principles)
  architecture.md       # Component design, data flow, trust boundaries
  lifecycle.md          # Dispatch, session lifecycle, systemd units, claims
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
Source (e.g. GitHub Projects)  Hive                          Agent (Claude Code)
─────────────────────────────  ────                          ───────────────────
Ready item  ──poll──→  claim + session + systemd dispatch
                       run: prepare → worktree + metadata
                            exec ──────────────→  edit, test, commit
                            publish ← result
In Review ←──────────  push branch, open PR
                       reap: cleanup expired sessions, recover stuck items
```

| Command | Does | Credentials |
|---------|------|-------------|
| `hive poll` | Queries source for ready items, claims, dispatches `hive-run@{uuid}` units | GitHub (read board) |
| `hive run` | Orchestrates prepare → exec → publish (accepts UUID or `owner/repo#issue`) | All of the above |
| `hive prepare` | Creates worktree, writes metadata, extracts prompt | GitHub (read issue) |
| `hive exec` | Launches agent in jailed workspace | Claude API key |
| `hive publish` | Pushes branch, opens PR, updates board | GitHub (write) |
| `hive reap` | Cleans up expired sessions, recovers stuck items | GitHub (write) |
| `hive ls` | Lists sessions (UUID, ref, status, created_at) | None |
| `hive cd` | Spawns shell in session workspace | None |
| `hive attach` | Attaches to agent tmux session | None |

Deprecated: `hive list` (alias for `ls`), `hive cleanup` (use `reap`). See `docs/architecture.md`.

## Tech Stack

| Concern | Choice |
|---------|--------|
| CLI | cobra + pflag |
| Config | viper (`~/.config/hive/config.toml` or per-instance `<name>.toml`) |
| Dispatch | systemd user units (`hive-run@{uuid}.service`) |
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
make install-units  # install systemd unit templates
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
- `docs/lifecycle.md` — dispatch model, session lifecycle, systemd units, claim-based dedup
- `docs/core-principles.md` — 6 decision filters (workspace as contract, credential isolation, environment parity, etc.)
- `docs/prototype-learnings.md` — constraints discovered through real usage
- `docs/adrs/` — architecture decision records (start with `001-swappable-jail-backends.md`)
