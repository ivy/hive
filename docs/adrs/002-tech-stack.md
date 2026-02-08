---
status: accepted
date: 2026-02-07
---

# Core tech stack: cobra, viper, slog, os/exec

## Context and Problem Statement

Hive is a greenfield Go CLI with a validated shell prototype. It needs a CLI framework, configuration management, structured logging, and patterns for interacting with external tools (gh, systemd-run, tmux). Which libraries should form the foundation, and where should Hive rely on the standard library instead?

## Decision Drivers

- **Ship and Iterate** (Principle 6): minimize dependency count; every library must earn its place
- **Embrace systemd**: logging MUST integrate natively with journald for operational observability
- **Host Parity** (Principle 4): external tools (gh, systemd-run, tmux) are invoked as host binaries, not wrapped in Go SDKs
- **Per-repo configuration**: different repos will need different jail backends, tool mounts, and settings — configuration MUST support per-repo overrides

## Considered Options

1. cobra + pflag + viper + slog + systemd/slog-journal
2. cobra + pflag + os.Getenv + logf + logfjournald
3. Minimal (stdlib flag + slog + no config framework)

## Decision Outcome

Chosen option: **cobra + pflag + viper + slog + systemd/slog-journal**, because it covers known requirements (8 CLI commands, per-repo config files, structured journald logging) with four well-maintained dependencies and no speculative additions.

### External dependencies

| Library | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI command structure and flag parsing |
| `github.com/spf13/pflag` | POSIX flag parsing (cobra dependency) |
| `github.com/spf13/viper` | Per-repo `.hive.toml` and global config |
| `github.com/systemd/slog-journal` | slog handler writing structured fields to journald |

### Standard library usage

| Concern | Approach |
|---------|----------|
| Logging | `log/slog` with journald handler |
| GitHub | `gh` CLI via `os/exec` |
| Sandbox | `systemd-run` / `podman` via `os/exec` |
| tmux | `tmux` via `os/exec` |
| Testing | `testing` package |

### Consequences

- **Good**: four external dependencies for the entire project — small supply chain surface
- **Good**: slog is stdlib with a universal handler interface; journald handler is maintained by the systemd project
- **Good**: viper reads `.hive.toml` per-repo, enabling jail backend selection and future per-repo settings without code changes
- **Good**: `os/exec` for external tools maintains host parity and avoids SDK version drift
- **Bad**: viper pulls in transitive dependencies (mapstructure, etc.) that add weight for what starts as simple config
- **Neutral**: cobra is heavyweight for 8 commands, but the team knows it and migration cost from alternatives is not worth the marginal benefit

## Pros and Cons of the Options

### cobra + pflag + viper + slog + systemd/slog-journal

- **Good**: cobra handles the full command tree (run, poll, prepare, exec, publish, list, attach, cleanup) with subcommand routing, flag inheritance, and help generation
- **Good**: viper supports the known near-term need for per-repo `.hive.toml` config
- **Good**: slog is stdlib, guaranteed to track Go releases, universal ecosystem support
- **Good**: systemd/slog-journal maps slog levels to journald PRIORITY and structured attrs to journal fields — `journalctl -u hive --json` works out of the box
- **Bad**: viper's transitive dependency tree is non-trivial

### cobra + pflag + os.Getenv + logf + logfjournald

The initially considered stack with logf for zero-allocation logging and no config framework.

- **Good**: logf claims zero-allocation performance
- **Bad**: logf benchmarks undocumented (README says "TODO"), claims unverifiable
- **Bad**: logfjournald last updated 2021, 3 GitHub stars, untested on recent Go versions
- **Bad**: custom logging API — nothing else in the Go ecosystem speaks logf
- **Bad**: os.Getenv is insufficient once per-repo config is needed

### Minimal (stdlib flag + slog + no config framework)

Standard library only, no CLI framework, no config library.

- **Good**: zero external dependencies
- **Bad**: stdlib `flag` doesn't support subcommands — would require manual routing for 8 commands
- **Bad**: no config file support without manual TOML parsing
- **Bad**: reimplements what cobra and viper already provide, violating "ship and iterate"

## More Information

- [ADR-001: Swappable jail backends](001-swappable-jail-backends.md) — the decision that validates Viper's inclusion for per-repo config
- [Architecture](../architecture.md) — CLI commands and module structure

Revisit Viper if per-repo config stays trivial after 3 months of daily use. If `.hive.toml` only ever contains `[jail] backend = "systemd-run"`, the complexity isn't earning its keep.
