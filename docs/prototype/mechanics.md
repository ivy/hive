# Ephemeral Workspace: Mechanics Report

How the prototype works, what we learned, and where it breaks down.

## What We Built

Two shell scripts that let a human hand a markdown prompt to Claude Code, which then implements the work inside a sandboxed environment and commits the result:

| Script | Purpose |
|--------|---------|
| [`dotfiles-sandbox`](dotfiles-sandbox) | Low-level wrapper — runs any command inside a `systemd-run` sandbox |
| [`dotfiles-work-on`](dotfiles-work-on) | Orchestrator — creates a worktree, launches Claude headless in the sandbox, manages sessions |

The system was tested end-to-end on [ivy/dotfiles#136](https://github.com/ivy/dotfiles/issues/136) (add `fetch.prune = true` to git config). The agent implemented the change, wrote BATS tests, and committed — across two sessions (initial run + resume with feedback).

## Sandboxing: systemd-run

The sandbox uses `systemd-run --user --pty` to create a transient systemd service with security directives. This is process-level sandboxing, not a container — the agent runs on the host kernel with the host's actual binaries.

### Mount Topology

```
/                           RO   (ProtectSystem=strict)
├── /usr/bin/*              RO   Host tools: zsh, nvim, chezmoi, git
├── /tmp                    RW   Private tmpfs (PrivateTmp=yes)
├── /home/ivy/              RW   Fresh tmpfs (TemporaryFileSystem)
│   ├── .claude/            RW   BindPaths — session data, todos, settings
│   ├── .local/bin/         RO   BindReadOnlyPaths — claude, mise symlinks
│   ├── .local/share/mise/  RO   BindReadOnlyPaths — mise installs + shims
│   ├── .local/share/claude/RO   BindReadOnlyPaths — claude binary
│   ├── .chezmoi.toml.src   RO   BindReadOnlyPaths — copied into config at boot
│   ├── .config/            RW   Created by bootstrap (chezmoi config, etc.)
│   ├── .ssh/               --   Does not exist
│   ├── .1password/         --   Does not exist
│   └── src/.../dotfiles/   --   Does not exist (main repo)
│       └── .git/           RW   BindPaths — shared git object store
└── /tmp/dotfiles-sandbox-* RW   BindPaths — the worktree
```

**Key insight**: `TemporaryFileSystem=$HOME:mode=0755` overlays a fresh tmpfs on the home directory. Everything underneath disappears. We then selectively bind-mount back only what the agent needs. Credentials (`.ssh`, `.1password`, `.config/gh`) are never mounted — they simply don't exist in the sandbox.

### What the Agent Can Do

- Read all system binaries and libraries (exact host versions)
- Write to the git worktree (edits persist on the host)
- Write to `~/.claude` (session persistence)
- Write to `~/` tmpfs (ephemeral — gone when process exits)
- Run `chezmoi apply` into the tmpfs home for verification
- Commit to the worktree branch

### What the Agent Cannot Do

- Write to `/usr`, `/etc`, `/boot` (read-only)
- Access SSH keys, 1Password socket, or `gh` auth tokens
- Push to any remote (no credentials)
- Modify the main repo checkout (only the worktree is mounted)
- Escalate privileges (`NoNewPrivileges=yes`)

### Why Not a Container?

| Property | systemd-run | Podman container |
|----------|-------------|-----------------|
| Startup time | ~50ms | ~2-5s |
| Host tool parity | Exact (same binaries) | Approximation (must rebuild image) |
| Maintenance | Zero | Containerfile + image builds |
| Isolation depth | Process-level (same PID namespace) | Container-level (separate namespaces) |
| Credential boundary | Mount-based (hidden, not isolated) | Filesystem-level (only mounted content exists) |
| Portability | Linux + systemd only | Cross-platform (macOS via Podman Machine) |

For the threat model — "prevent an eager agent from using credentials it shouldn't" — process-level sandboxing is sufficient. We're not defending against a malicious process trying to escape; we're preventing a helpful agent from finding `.ssh` keys and doing something unhelpful.

## Git Worktrees

Each run creates a git worktree branched from `main`:

```
git worktree add -b sandbox/work-<timestamp> /tmp/dotfiles-sandbox-<timestamp> main
```

**Why worktrees instead of clones:**

- Shares the object store with the main repo (fast, no network)
- Branch is visible from the main checkout for cherry-picking
- Cleanup is `git worktree remove` + `git branch -D`

**The `.git` problem:** A worktree's `.git` is a file containing `gitdir: /path/to/main/.git/worktrees/<name>`. The worktree needs RW access to that metadata directory (index, HEAD, logs). We mount `.git` as RW via `BindPaths`. This means the sandbox could theoretically modify refs in the main repo — acceptable for a prototype, but a real tool should scope this tighter.

## Claude Code Headless

Claude Code runs in print mode (`-p`) with `--dangerously-skip-permissions`:

```bash
claude -p \
    --dangerously-skip-permissions \
    --session-id <uuid> \
    --model sonnet \
    --output-format json \
    "$(cat prompt.md)"
```

### Session Management

- **`--session-id <uuid>`**: Pins the session to a known ID (generated by `uuidgen`)
- **`--output-format json`**: Returns structured result including `session_id`, `total_cost_usd`, `num_turns`, `duration_ms`
- **`--resume <session-id>`**: Resumes with full prior context for follow-up feedback

The session ID is saved to `.claude-session` in the worktree directory. This enables the resume workflow:

```bash
# Initial run
dotfiles-work-on prompt.md
# → saves session to /tmp/dotfiles-sandbox-<ts>/.claude-session

# Review, then resume with feedback
dotfiles-work-on --resume /tmp/dotfiles-sandbox-<ts> "fix the test assertion"
# → reads session ID, resumes with full context
```

### Prompt as Argument vs. Stdin

`systemd-run --pty` manages the TTY — stdin doesn't pass through to the inner process. The prompt must be passed as a command-line argument (`"$(cat prompt.md)"`), not via stdin redirection. This works but imposes a practical limit on prompt size (shell argument length, typically 2MB on Linux — well above any reasonable prompt).

## Prototype Results: Issue #136

| Metric | Run 1 (implement) | Run 2 (resume + commit) |
|--------|-------------------|------------------------|
| Duration | 70s | 30s |
| API cost | $0.45 | $0.21 |
| Turns | 14 | 7 |
| Model | Sonnet | Sonnet |

The agent:

1. Read the existing git config template and test patterns
2. Added `[fetch]\n\tprune = true` in the correct location
3. Created `test/git-config.bats` following established patterns
4. Ran all tests (38/38 pass)
5. On resume: committed with proper conventional messages, split into two commits

## What Broke During Prototyping

### 1. `ProtectHome=tmpfs` + `ProtectSystem=strict` conflict

`ProtectSystem=strict` makes everything RO, including the tmpfs home. `ReadWritePaths=$HOME` to fix it causes exit code 226 (mount conflict). **Fix**: Use `TemporaryFileSystem=$HOME:mode=0755` instead of `ProtectHome=tmpfs`, which creates a writable tmpfs without conflicting with `ProtectSystem`.

### 2. Chezmoi state database

`~/.config/chezmoi/` mounted RO blocks `chezmoistate.boltdb` writes. **Fix**: Mount only `chezmoi.toml` (as `.chezmoi.toml.src`) and copy it into the writable tmpfs at boot. The state DB is ephemeral — fine for a sandbox.

### 3. Chezmoi install scripts

`chezmoi apply` runs `run_once` scripts that try to write to system paths. **Fix**: Use `chezmoi apply --exclude scripts`. The sandbox has the tools already; it only needs the dotfile configs deployed.

### 4. Git worktree metadata

Worktree `.git` file points to main repo's `.git/worktrees/<name>/`. With `.git` mounted RO, git can't write index or HEAD. **Fix**: Mount `.git` as RW via `BindPaths`.

### 5. `~/.claude` must be RW

Claude Code writes session data, todos, and logs to `~/.claude/`. Initially mounted RO, causing immediate crash. **Fix**: `BindPaths` (RW) instead of `BindReadOnlyPaths`.

### 6. mise trust prompt

mise shows an interactive TUI to trust config files in unfamiliar directories. **Fix**: `MISE_YES=1` + `MISE_TRUSTED_CONFIG_PATHS=<worktree>`.

### 7. Agent didn't commit

The prompt said "make small targeted commits" but the agent interpreted this as guidance for the human reviewer. **Fix**: Be explicit — "You MUST commit your changes before finishing."

## Open Questions

### Credential Isolation

`~/.claude` is mounted RW and may contain API keys or auth tokens. A confused agent could theoretically read them. Need to audit what's in `~/.claude/` and whether sensitive content can be isolated from session data.

### `.git` Write Scope

The entire `.git` directory is RW. The agent could modify refs, tags, or the main branch's HEAD. A real tool should use `BindPaths` more surgically — mounting `.git/objects` and `.git/refs` as RO while keeping `.git/worktrees/<name>` as RW.

### Pre-commit Hooks

The prototype uses `HK=0` to skip `hk` pre-commit hooks because the sandbox environment may not have all linting tools configured. A production tool should either ensure hooks work inside the sandbox or have a deliberate policy on hook execution.

### macOS Support

`systemd-run` is Linux-only. macOS would need a different sandboxing mechanism — likely `sandbox-exec` (deprecated), a Podman container, or a Lima VM. This is the strongest argument for eventually having a container-based fallback.

### Network Access

The sandbox has full network access (needed for Claude Code's API calls). This means an agent could theoretically exfiltrate data. For the current threat model (prevent accidental credential use) this is fine. For stronger isolation, network namespacing with an allowlist proxy would be needed.

## Reference Implementation

- **[`dotfiles-sandbox`](dotfiles-sandbox)** — Low-level sandbox wrapper
- **[`dotfiles-work-on`](dotfiles-work-on)** — Full orchestrator with worktree + session management
