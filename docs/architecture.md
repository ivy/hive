# Architecture

## Pipeline

```
 ① poll       Query source for ready items, claim, dispatch systemd units
 ② prepare    Create workspace from issue (worktree, prompt, metadata)
 ③ exec       Launch agent in sandboxed workspace
 ④ publish    Push branch, open PR, update board → In Review 👀
 ⑤ reap       Clean up expired sessions, recover stuck items
```

`hive run` orchestrates ②③④ in sequence. `hive poll` dispatches `hive run` as systemd units for each Ready item. `hive reap` runs on a timer to handle cleanup and failure recovery.

Each stage is independently invocable because each communicates through the filesystem, not in-memory state. This decomposition exists so that any stage can crash and be re-run without starting over — the workspace is the recovery point. See [Core Principles](core-principles.md) (Principle 1: Workspace as Contract, Principle 5: Resumability).

## Components

```
┌────────────────────────────────────────────────────────────────┐
│  hive (CLI)                                                    │
│                                                                │
│  Commands:                                                     │
│  poll          query source, claim, dispatch systemd units     │
│  run           orchestrate prepare + exec + publish            │
│  prepare       create workspace from issue                     │
│  exec          launch agent in workspace                       │
│  publish       push branch, open PR, update board              │
│  reap          clean up expired sessions, recover stuck items  │
│  ls            list sessions (UUID, ref, status, age)          │
│  cd            spawn shell in session workspace                │
│  attach        tmux attach to running agent                    │
│                                                                │
│  Internal modules:                                             │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐            │
│  │ session   │  │ workspace │  │  source           │            │
│  │           │  │           │  │                   │            │
│  │ JSON CRUD │  │ worktree  │  │ Ready/Take/       │            │
│  │ status    │  │ metadata  │  │ Complete/Release   │            │
│  │ lifecycle │  │ lifecycle │  │ (interface)        │            │
│  └───────────┘  └───────────┘  └──────────────────┘            │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐            │
│  │  claim    │  │   jail    │  │  github           │            │
│  │           │  │           │  │                   │            │
│  │ atomic    │  │ systemd   │  │ board read/write  │            │
│  │ dedup     │  │ podman    │  │ push + PR         │            │
│  │ O_EXCL    │  │ unshare   │  │ issue fetch       │            │
│  │           │  │ (swap)    │  │                   │            │
│  └───────────┘  └───────────┘  └──────────────────┘            │
└────────────────────────────────────────────────────────────────┘
```

### session

Session metadata stored as JSON at `~/.local/share/hive/sessions/{uuid}.json`. Lives outside the workspace — agent-inaccessible. This separation is deliberate: session files contain source-specific identifiers (board item IDs, project node IDs) that would be dangerous or confusing inside the agent's sandbox. Tracks status lifecycle: `dispatching` → `prepared` → `running` → `stopped` → `published`/`failed`. See [Lifecycle](lifecycle.md) for status transitions and crash recovery.

### claim

Atomic claim files for work-item dedup at `~/.local/share/hive/claims/`. Filename is truncated SHA-256 of the source ref. Created with `O_EXCL` for race-free dedup. One file per active work item. Claims exist because poll is stateless between restarts — all dispatch state must survive on disk.

### source

Abstraction for where work items come from. Interface: `Ready()`, `Take()`, `Complete()`, `Release()`. Same pattern as the swappable jail interface — hive core never imports source-specific packages. GitHub Projects is the initial adapter (`internal/source/ghprojects`). The interface accommodates any source that can answer "what's ready?" and accept status updates. See [ADR 001](adrs/001-swappable-jail-backends.md) for the swappable-interface pattern.

### workspace

Creates and manages git worktrees and the `.hive/` metadata directory at `~/.local/share/hive/workspaces/{uuid}/`. Pure local git operations. No network access, no credentials. The workspace is the contract between pipeline stages — `prepare` writes it, `exec` runs inside it, `publish` reads from it.

### jail

The execution environment. Provides credential isolation and filesystem sandboxing. The interface is: "run this command in this workspace with these constraints." The implementation is swappable — `systemd-run`, Podman, `unshare`, or something else. The rest of Hive doesn't care which. Backend selection is per-repo via configuration because different repos have different parity targets (host tools vs. container images). See [Security Model](explanation/security-model.md) for isolation details, [ADR 001](adrs/001-swappable-jail-backends.md) for the backend decision.

### github

All GitHub API interactions. Board status changes, issue fetching, PR creation, branch pushing. This is the only module with GitHub credentials. Uses `gh` CLI via `os/exec`. Concentrating GitHub access here means the credential boundary is a module boundary, not a runtime enforcement — but the jail's mount namespacing enforces it at runtime too.

### authz

Issue author authorization. Checks whether an issue's author is in the configured allowlist before any agent work begins. Fail-closed: an empty allowlist means no issues are processed.

## Trust Boundaries

| Component | GitHub credentials      | Repo write             | Claude API key |
|-----------|-------------------------|------------------------|----------------|
| poll      | Read (board via source) | No                     | No             |
| prepare   | Read (issue)            | Yes (worktree)         | No             |
| exec      | No                      | Yes (workspace only)   | Yes            |
| publish   | Write (push, PR, board) | Read (branch)          | No             |
| reap      | Write (release items)   | Yes (remove worktree)  | No             |

The agent inside `exec` can edit, test, and commit — but cannot push, open PRs, or interact with GitHub. The `publish` command runs outside the jail with GitHub credentials. Session metadata is outside the workspace — the agent cannot read or modify dispatch state. See [Security Model](explanation/security-model.md) for the full threat model and isolation mechanisms.

## Data Flow

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

## Data Layout

All hive data lives under `~/.local/share/hive/` (respects `XDG_DATA_HOME`). Workspaces survive reboots.

```
~/.local/share/hive/
├── claims/
│   └── <hash>                  # claim file (content: session UUID)
├── sessions/
│   └── <uuid>.json             # session metadata (status, ref, prompt, etc.)
└── workspaces/
    └── <uuid>/                 # git worktree (agent's sandbox)
        ├── <repo contents>
        ├── .git                # → main repo's .git/worktrees/<name>
        └── .hive/              # workspace metadata
            ├── issue.json
            ├── prompt.md
            ├── session-id
            ├── repo
            ├── issue-number
            ├── tmux-session
            └── status
```

## Repo Discovery

Repos are found on disk by convention:

```
~/src/github.com/<owner>/<repo>/
```

An issue on `ivy/dotfiles#132` maps to `~/src/github.com/ivy/dotfiles/`. No configuration needed. If the repo isn't on disk, `prepare` clones it.
