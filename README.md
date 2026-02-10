# Hive

Personal agent orchestrator. Turns a GitHub Projects backlog into pull requests by dispatching Claude Code agents in isolated workspaces on a home server.

> [!CAUTION]
> Early experiment — built for personal use on a single home server.
> APIs, config formats, and workspace conventions will change without notice.

## How It Works

```
GitHub Projects board          Hive                          Agent (Claude Code)
─────────────────────          ────                          ───────────────────
Ready 🤖  ──poll──→  claim + session + systemd dispatch
                     prepare → worktree + metadata
                     exec ──────────────→  edit, test, commit
                     publish ← result
In Review 👀 ←────── push branch, open PR
                     reap: cleanup expired sessions
```

1. Drag an issue to **Ready** on your project board
2. `hive poll` picks it up, creates an isolated workspace, and launches an agent
3. The agent implements the change, tests it, and commits
4. Hive pushes the branch and opens a PR
5. You review when ready

## Quick Start

```bash
# Build and install
git clone https://github.com/ivy/hive.git
cd hive
mise install          # install toolchain
make install          # binary → ~/.local/bin/hive

# Create a minimal config
mkdir -p ~/.config/hive
cp hive.example.toml ~/.config/hive/config.toml
# Edit config.toml with your GitHub Projects IDs and allowed-users

# Run a single issue end-to-end
hive run your-org/your-repo#42
```

`hive run` chains prepare → exec → publish into one command. For step-by-step control, run each stage independently — see the [tutorial](docs/tutorial/first-run.md).

## Requirements

| Dependency | Purpose |
|---|---|
| Linux + systemd | Process sandboxing via `systemd-run --user` |
| [Go](https://go.dev/) 1.25+ | Build from source |
| [gh](https://cli.github.com/) CLI | GitHub API access (authenticated) |
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | Agent runtime |
| [mise](https://mise.jdx.dev/) | Tool version management |

## Commands

| Command | Does |
|---|---|
| `hive poll` | Query board for ready items, claim and dispatch |
| `hive run` | Orchestrate prepare → exec → publish |
| `hive prepare` | Create workspace from issue |
| `hive exec` | Launch agent in sandboxed workspace |
| `hive publish` | Push branch, open PR |
| `hive reap` | Clean up expired sessions, recover stuck items |
| `hive ls` | List sessions |
| `hive cd` | Shell into session workspace |
| `hive attach` | Attach to running agent's tmux session |

## Documentation

### [Tutorial](docs/tutorial/)

- [First run](docs/tutorial/first-run.md) — from issue to pull request, step by step

### [How-to guides](docs/how-to/)

- [Install](docs/how-to/install.md) — build, install binary and systemd units
- [Configure](docs/how-to/configure.md) — set up config.toml with GitHub Projects IDs
- [Deploy](docs/how-to/deploy.md) — run as a persistent systemd service
- [Write issues](docs/how-to/write-issues.md) — craft issues that produce good PRs
- [Debug a session](docs/how-to/debug-session.md) — inspect and troubleshoot agent runs

### [Reference](docs/reference/)

- [CLI](docs/reference/cli.md) — all commands, flags, and environment variables
- [Configuration](docs/reference/config.md) — config.toml keys and defaults
- [Session & data](docs/reference/session.md) — session lifecycle, data layout, status transitions
- [Systemd units](docs/reference/systemd-units.md) — unit templates and instance naming
- [Jail interface](docs/reference/jail-interface.md) — sandbox backend contract
- [Source interface](docs/reference/source-interface.md) — work-item source abstraction

### [Explanation](docs/explanation/)

- [Vision](docs/vision.md) — why this project exists
- [Architecture](docs/architecture.md) — component design, data flow, trust boundaries
- [Core principles](docs/core-principles.md) — 6 decision filters
- [Security model](docs/explanation/security-model.md) — isolation, credential boundaries, threat model
- [ADRs](docs/adrs/) — architecture decision records

## Development

```bash
go test ./...       # run all specs (Ginkgo BDD)
go vet ./...        # static analysis
make build          # build binary
make install        # install to ~/.local/bin
make install-units  # install systemd unit templates
```

Tests use [Ginkgo/Gomega](https://onsi.github.io/ginkgo/) BDD style. Pre-commit hooks managed by [hk](https://github.com/jdx/hk). Tool versions managed by [mise](https://mise.jdx.dev/) — see `mise.toml`.
