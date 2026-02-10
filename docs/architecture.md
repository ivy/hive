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

Session metadata stored as JSON at `~/.local/share/hive/sessions/{uuid}.json`. Lives outside the workspace — agent-inaccessible. Tracks status lifecycle: `dispatching` → `prepared` → `running` → `stopped` → `published`/`failed`.

### claim

Atomic claim files for work-item dedup at `~/.local/share/hive/claims/`. Filename is truncated SHA-256 of the source ref. Created with `O_EXCL` for race-free dedup. One file per active work item.

### source

Abstraction for where work items come from. Interface: `Ready()`, `Take()`, `Complete()`, `Release()`. Same pattern as the swappable jail interface. GitHub Projects is the initial adapter (`internal/source/ghprojects`).

### workspace

Creates and manages git worktrees and the `.hive/` metadata directory at `~/.local/share/hive/workspaces/{uuid}/`. Pure local git operations. No network access, no credentials.

### jail

The execution environment. Provides credential isolation and filesystem sandboxing. The interface is: "run this command in this workspace with these constraints." The implementation is swappable — `systemd-run`, Podman, `unshare`, or something else. The rest of Hive doesn't care which.

### github

All GitHub API interactions. Board status changes, issue fetching, PR creation, branch pushing. This is the only module with GitHub credentials. Uses `gh` CLI.

## Trust Boundaries

| Component | GitHub credentials      | Repo write             | Claude API key |
|-----------|-------------------------|------------------------|----------------|
| poll      | Read (board via source) | No                     | No             |
| prepare   | Read (issue)            | Yes (worktree)         | No             |
| exec      | No                      | Yes (workspace only)   | Yes            |
| publish   | Write (push, PR, board) | Read (branch)          | No             |
| reap      | Write (release items)   | Yes (remove worktree)  | No             |

The agent inside `exec` can edit, test, and commit — but cannot push, open PRs, or interact with GitHub. The `publish` command runs outside the jail with GitHub credentials. Session metadata is outside the workspace — the agent cannot read or modify dispatch state.

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

### Session status lifecycle

```
dispatching → prepared → running → stopped → published
                                      │          │
                                      └→ failed ←┘
```

| Status | Set by | Meaning |
|--------|--------|---------|
| `dispatching` | poll | Session created, unit not yet started |
| `prepared` | run (prepare) | Workspace created, agent not started |
| `running` | run (exec) | Agent is executing |
| `stopped` | run (exec) | Agent exited |
| `published` | run (publish) | Branch pushed, PR opened, source notified |
| `failed` | run or reap | A stage failed or session detected as stuck |

## Dispatch: Poll Loop

```
hive poll --interval 5m:
  loop every interval (or once if no interval):
    items = source.Ready()
    for item in items:
      if at max-concurrent: skip remaining
      claim.TryClaim(item.ref, uuid)    # atomic, skip if already claimed
      session.Create(uuid, item)        # write session JSON
      systemctl --user start hive-run@{uuid}
      source.Take(item.ref)             # mark taken on board
```

Poll is stateless between restarts — all dispatch state is on disk (claims, sessions). It doesn't know about workspaces, jails, or Claude.

With `--interval` (or `poll.interval` in config), poll runs as a long-lived daemon — polling immediately, then on each tick. Without it, poll runs once and exits (for use with external schedulers). Runs as `hive-poll@<instance>.service`.

## Repo Discovery

Repos are found on disk by convention:

```
~/src/github.com/<owner>/<repo>/
```

An issue on `ivy/dotfiles#132` maps to `~/src/github.com/ivy/dotfiles/`. No configuration needed. If the repo isn't on disk, `prepare` clones it.

## CLI Examples

```bash
# Full pipeline: workspace → agent → PR (manual path)
hive run ivy/dotfiles#132

# Run from session UUID (systemd-dispatched path)
hive run 00112233-4455-6677-8899-aabbccddeeff

# Skip PR creation (I'll review first)
hive run --no-publish ivy/dotfiles#132

# List all sessions
hive ls

# Open shell in a session's workspace
hive cd ivy/dotfiles#132
hive cd 00112233-4455-6677-8899-aabbccddeeff

# Inspect running agent
hive attach ivy/dotfiles#132

# Poll once (single-shot)
hive poll

# Poll as daemon
hive poll --interval 5m

# Clean up expired sessions and recover stuck items
hive reap
```
