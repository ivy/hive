# Ephemeral Workspace Tool: Technical Specification

## What This Is

A CLI tool that runs Claude Code headless inside a sandboxed environment against an isolated git worktree. The human writes a prompt describing what to build or fix. The tool handles everything else: worktree creation, sandboxing, session management, and cleanup.

```
work-on prompt.md                                    # agent implements the task
work-on --resume /tmp/ws-1234 "fix the test"         # reviewer sends feedback
work-on --cleanup                                    # remove all worktrees
```

The tool is not Claude Code. It's the safety and orchestration layer that makes `--dangerously-skip-permissions` responsible.

## Design Constraints (from prototype)

These are learned from building and testing the shell prototype against 5 real projects (dotfiles, home-ops, kb, subslice, pyrite).

### 1. Inherit, Don't Reproduce

Every project has different tools. dotfiles needs chezmoi+bats+hk. kb needs go+buf+protoc-gen-*. subslice needs just Go. pyrite will need Rust. home-ops needs ansible+shellcheck+yamlfmt.

The sandbox must not try to install or manage any of these. It inherits the host's tools by running on the host kernel with read-only access to system paths. This is why `systemd-run` beats containers — exact host parity with zero maintenance.

### 2. Projects Don't Need Configuration

The prototype worked on dotfiles without any project-specific sandbox config. The same approach (worktree + hidden credentials + host tools) works for every project in the inventory. The tool should work on any git repo with zero project-side setup.

If a project *wants* to customize behavior (e.g., run a setup step after worktree creation), that's opt-in, not required.

### 3. Credentials Are the Threat Model

The sandbox exists to prevent an agent from:
- Opening a PR on the wrong repo
- Pushing to a remote
- Reading SSH keys, API tokens, or auth cookies
- Accidentally modifying the user's real home directory

It does *not* defend against malicious code execution, kernel exploits, or network exfiltration. Those are different problems with different solutions.

### 4. Sessions Are First-Class

The review cycle is: run → inspect → resume with feedback → inspect → repeat. Session persistence is not optional. The tool must:
- Generate and pin a session ID per worktree
- Save it alongside the worktree
- Support resuming with a new prompt that has full prior context

### 5. Worktrees Are the Unit of Work

Each run gets a fresh git worktree branched from the project's default branch. The worktree is the agent's workspace. When the human is satisfied, they cherry-pick or merge. When they're not, they `--cleanup`.

## Architecture

```
┌────────────────────────────────────────────────────┐
│  work-on CLI (Go binary)                           │
│                                                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │ Worktree │  │ Sandbox  │  │ Session          │ │
│  │ Manager  │  │ Backend  │  │ Manager          │ │
│  └──────────┘  └──────────┘  └──────────────────┘ │
│                     │                              │
│              ┌──────┴──────┐                       │
│              │  systemd    │                       │
│              │  (Linux)    │                       │
│              └─────────────┘                       │
└────────────────────────────────────────────────────┘
```

Three components, each with a clear boundary:

### Worktree Manager

Creates, lists, and removes git worktrees in a known location.

```
/tmp/work-on-<project>-<timestamp>/
├── .work-on-session          # session ID (UUID)
├── .work-on-prompt.md        # original prompt (for reference)
├── <repo contents>           # worktree checkout
└── .git                      # points to main repo's .git/worktrees/
```

**Worktree lifecycle:**
1. `git worktree add -b work-on/<timestamp> <path> <default-branch>`
2. Agent works in the worktree
3. Human inspects with `git log`, `git diff`
4. Human runs `--cleanup` or `git worktree remove`

**Branch naming:** `work-on/<timestamp>` — predictable, sortable, no conflicts.

### Sandbox Backend

Provides the execution environment. On Linux, this is `systemd-run`. The interface is simple: "run this command with these mount rules."

**Mount rules (universal):**

| Path | Mount | Why |
|------|-------|-----|
| `/` | RO | `ProtectSystem=strict` |
| `$HOME` | tmpfs (RW) | Fresh home, no credential leakage |
| Worktree dir | RW | Agent's workspace |
| Repo `.git/` | RW | Worktree needs git metadata |
| `~/.claude` | RW | Session data, todos, settings |
| `~/.local/bin` | RO | Tool binaries (claude, mise, etc.) |
| `~/.local/share/mise` | RO | mise installs and shims |
| `~/.local/share/claude` | RO | Claude Code binary |
| `~/.ssh` | -- | Hidden |
| `~/.config/gh` | -- | Hidden |
| `~/.1password` | -- | Hidden |

**Bootstrap steps (run inside sandbox before claude):**
1. `mkdir -p ~/.config/chezmoi` (or equivalent project setup)
2. Set `PATH` to include mise shims and local bin
3. Configure minimal git identity (name + email, no credential helpers)
4. Set `MISE_YES=1` and `MISE_TRUSTED_CONFIG_PATHS=<worktree>`

### Session Manager

Tracks the mapping between worktrees and Claude Code sessions.

**Storage:** A single file in the worktree (`.work-on-session`) containing the session UUID. No external database, no state directory.

**Operations:**
- `new`: Generate UUID, pass to `claude --session-id <uuid>`
- `resume`: Read UUID from worktree, pass to `claude --resume <uuid>`

## CLI Interface

```
work-on <prompt-file> [flags]      Run agent on a prompt
work-on --resume <dir> <feedback>  Resume with reviewer feedback
work-on --list                     List active worktrees
work-on --cleanup [dir]            Remove worktree(s)
work-on --version                  Print version
```

### Flags

```
--repo <path>       Target repo (default: current directory)
--branch <name>     Base branch (default: repo's default branch)
--model <model>     Claude model (default: sonnet)
--budget <usd>      Max spend per run (passed to --max-budget-usd)
--verbose           Stream claude's output
--dry-run           Show what would happen without executing
```

### Output

Default output is the JSON result from `claude -p --output-format json`, which includes:
- `session_id` — for resuming
- `total_cost_usd` — what the run cost
- `num_turns` — how many tool-use cycles
- `duration_ms` — wall clock time
- `result` — Claude's final text summary

The tool also prints human-readable instructions:
```
==> Agent finished ($0.45, 14 turns, 70s)
==> Inspect:  cd /tmp/work-on-kb-1770529040
              git log --oneline main..HEAD
==> Resume:   work-on --resume /tmp/work-on-kb-1770529040 "feedback"
==> Cleanup:  work-on --cleanup /tmp/work-on-kb-1770529040
```

## Project-Optional Configuration

A project can optionally include `.work-on.toml` (or similar) at the repo root:

```toml
# Extra paths to bind-mount read-only (e.g., shared proto definitions)
[sandbox]
bind_ro = ["~/.config/some-tool"]

# Extra paths to bind-mount read-write
bind_rw = []

# Commands to run inside sandbox before claude starts
[setup]
pre = ["go generate ./..."]

# Commands to run after claude finishes (inside sandbox)
[setup]
post = ["go test ./..."]

# Default claude flags
[claude]
model = "sonnet"
budget = 1.00
```

This is entirely optional. The tool works without it.

## Implementation Plan

### Phase 1: Port the Prototype (Go)

Rewrite the shell prototype ([`dotfiles-work-on`](dotfiles-work-on)) in Go. Same behavior, better error handling, proper argument parsing.

- [ ] `cmd/work-on/main.go` — CLI entry point (use `pflag` or `cobra`)
- [ ] `internal/worktree/` — Create, list, remove worktrees
- [ ] `internal/sandbox/` — systemd-run invocation
- [ ] `internal/session/` — Session ID generation and persistence
- [ ] Cross-compile for linux/amd64

### Phase 2: Polish

- [ ] `--list` command showing active worktrees with branch, age, session status
- [ ] `--budget` flag wired to `claude --max-budget-usd`
- [ ] `--dry-run` flag
- [ ] Better error messages (e.g., "systemd-run not found — are you on Linux?")
- [ ] `.work-on.toml` support for project-specific config

### Phase 3: Distribution

- [ ] `mise use` installation
- [ ] Renovate-managed version pinning
- [ ] GitHub releases with goreleaser

## Non-Goals (For Now)

- **macOS support.** The tool is Linux-only until there's a real user on macOS. When that happens, the sandbox backend becomes the abstraction point.
- **Multi-repo orchestration.** One prompt, one repo, one worktree. Cross-repo work is a human coordination problem.
- **PR automation.** The tool creates worktrees with commits. Publishing those commits as a PR is a separate concern (and a separate tool).
- **Custom Claude system prompts.** The tool passes the prompt to Claude Code as-is. If you want system prompt customization, put it in the project's AGENTS.md or CLAUDE.md.
- **Container-based sandboxing.** systemd-run works. If it stops working, containers are the fallback, but there's no reason to build that now.

## Open Questions

1. **Where should the Go module live?** Options: new repo (`ivy/work-on`), or inside dotfiles as a `tools/work-on/` subdirectory during dogfooding.

2. **Should the tool manage the prompt file?** Currently the human writes the prompt. A future version could accept a GitHub issue URL and generate the prompt. But that's scope creep for v1.

3. **What about `.git` write scope?** The prototype mounts the entire `.git` directory RW. A tighter mount would be `.git/objects` and `.git/refs` as RO, `.git/worktrees/<name>` as RW. Worth the complexity?

4. **Pre-commit hook policy?** The prototype skips hk hooks with `HK=0`. Should the tool run hooks by default and provide a flag to skip? Or skip by default since the sandbox may not have all linting tools configured?
