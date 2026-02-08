# Prototype Learnings

What was learned building and testing the shell prototype ([`prototype/dotfiles-work-on`](prototype/dotfiles-work-on)). These are constraints discovered through real usage, not theoretical concerns.

## What Worked

### systemd-run sandboxing

`systemd-run --user --pty` with `ProtectSystem=strict` and `TemporaryFileSystem=$HOME` provides effective credential isolation with ~50ms startup. The agent gets exact host tool parity (same binaries, same versions) with zero maintenance.

### Git worktrees

Worktrees are fast (shared object store, no network), visible from the main checkout for cherry-picking, and cheap to create/destroy. The worktree directory is a natural unit of work.

### Issue body as prompt

The prototype was tested on issue #136. The issue body contained enough context for the agent to implement the change, write tests, and commit. No separate prompt engineering needed — well-written issues are good prompts.

### Session resume

Claude Code's `--session-id` and `--resume` flags enable the review cycle: run → inspect → resume with feedback → inspect → repeat. Session IDs persisted in the worktree survive process restarts.

## What Broke

### ProtectHome + ProtectSystem conflict

`ProtectSystem=strict` makes everything read-only, including a `ProtectHome=tmpfs` overlay. `ReadWritePaths=$HOME` causes exit code 226 (mount conflict).

**Fix**: Use `TemporaryFileSystem=$HOME:mode=0755` instead of `ProtectHome=tmpfs`.

### Chezmoi state database

`~/.config/chezmoi/` mounted read-only blocks `chezmoistate.boltdb` writes.

**Fix**: Mount only `chezmoi.toml` as a source file, copy it into the writable tmpfs at boot. The state DB is ephemeral in a sandbox.

### Chezmoi install scripts

`chezmoi apply` runs `run_once` scripts that try to write to system paths.

**Fix**: Use `chezmoi apply --exclude scripts`. The sandbox has the tools already.

### Git worktree metadata

Worktree `.git` file points to main repo's `.git/worktrees/<name>/`. With `.git` mounted read-only, git can't write index or HEAD.

**Fix**: Mount `.git` as read-write via `BindPaths`. This means the sandbox could modify refs in the main repo — acceptable for a prototype, but Hive should scope `.git/worktrees/<name>` more tightly.

### ~/.claude must be read-write

Claude Code writes session data, todos, and logs to `~/.claude/`. Mounted read-only causes immediate crash.

**Fix**: `BindPaths` (read-write). In Hive, this becomes per-workspace: each workspace gets its own `.claude/` directory.

### mise trust prompt

mise shows an interactive TUI to trust config files in unfamiliar directories.

**Fix**: `MISE_YES=1` + `MISE_TRUSTED_CONFIG_PATHS=<worktree>`.

### Agent didn't commit

The prompt said "make small targeted commits" but the agent interpreted this as guidance for the human reviewer.

**Fix**: Be explicit — "You MUST commit your changes before finishing."

## Open Questions (inherited)

### .git write scope

The prototype mounts the entire `.git` directory read-write. A tighter mount: `.git/objects` and `.git/refs` as read-only, `.git/worktrees/<name>` as read-write. Worth the complexity? Probably yes for Hive.

### Pre-commit hooks

The prototype skips hk hooks with `HK=0`. Hive should either ensure hooks work inside the sandbox or have a deliberate skip policy. The host has all the tools, so hooks should work if the sandbox inherits them correctly.

### Network access

The sandbox has full network access (needed for Claude Code API calls). An agent could theoretically exfiltrate data. For the current threat model (prevent accidental credential use), this is acceptable.

## Prototype Cost Data

From issue #136 (add `fetch.prune = true` to git config):

| Metric | Run 1 (implement) | Run 2 (resume + feedback) |
|--------|-------------------|--------------------------|
| Duration | 70s | 30s |
| API cost | $0.45 | $0.21 |
| Turns | 14 | 7 |
| Model | Sonnet | Sonnet |

The agent read existing patterns, made the change, wrote BATS tests, ran the full suite (38/38 pass), and committed with proper conventional messages.

## Source Material

- [`prototype/dotfiles-work-on`](prototype/dotfiles-work-on) — orchestrator prototype
- [`prototype/dotfiles-sandbox`](prototype/dotfiles-sandbox) — low-level sandbox wrapper
- [`prototype/mechanics.md`](prototype/mechanics.md) — detailed mechanics report
- [`prototype/spec.md`](prototype/spec.md) — original spec (partially superseded by this project)
