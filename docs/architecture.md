# Architecture

## Pipeline

```
 ① poll       Find Ready 🤖 items on the project board
 ② prepare    Create workspace from issue (worktree, prompt, metadata)
 ③ exec       Launch agent in sandboxed workspace
 ④ publish    Push branch, open PR, update board → In Review 👀
```

`hive run` orchestrates ②③④ in sequence. `hive poll` dispatches `hive run` for each Ready item.

## Components

```
┌──────────────────────────────────────────────────────┐
│  hive (CLI)                                          │
│                                                      │
│  Commands:                                           │
│  run           orchestrate prepare + exec + publish  │
│  poll          find Ready items, dispatch run        │
│  prepare       create workspace from issue           │
│  exec          launch agent in workspace             │
│  publish       push branch, open PR, update board    │
│  list          show workspaces (running/done/failed) │
│  attach        tmux attach to running agent          │
│  cleanup       tear down workspace(s)                │
│                                                      │
│  Internal modules:                                   │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐  │
│  │ workspace │  │   jail    │  │  github          │  │
│  │           │  │           │  │                  │  │
│  │ worktree  │  │ systemd   │  │ board read/write │  │
│  │ metadata  │  │ podman    │  │ push + PR        │  │
│  │ lifecycle │  │ unshare   │  │ issue fetch      │  │
│  │           │  │ (swap)    │  │                  │  │
│  └───────────┘  └───────────┘  └──────────────────┘  │
└──────────────────────────────────────────────────────┘
```

### workspace

Creates and manages git worktrees and the `.hive/` metadata directory. Pure local git operations. No network access, no credentials.

### jail

The execution environment. Provides credential isolation and filesystem sandboxing. The interface is: "run this command in this workspace with these constraints." The implementation is swappable — `systemd-run`, Podman, `unshare`, or something else. The rest of Hive doesn't care which.

### github

All GitHub API interactions. Board status changes, issue fetching, PR creation, branch pushing. This is the only module with GitHub credentials. Uses `gh` CLI.

## Trust Boundaries

| Component | GitHub credentials      | Repo write             | Claude API key |
|-----------|-------------------------|------------------------|----------------|
| poll      | Read (board)            | No                     | No             |
| prepare   | Read (issue)            | Yes (worktree)         | No             |
| exec      | No                      | Yes (workspace only)   | Yes            |
| publish   | Write (push, PR, board) | Read (branch)          | No             |

The agent inside `exec` can edit, test, and commit — but cannot push, open PRs, or interact with GitHub. The `publish` command runs outside the jail with GitHub credentials.

## Workspace Layout

The workspace directory is the contract between components. Each stage reads and writes files here.

```
/tmp/hive/<project>-<issue>-<timestamp>/
├── .hive/
│   ├── issue.json         # written by: prepare (full issue payload)
│   ├── prompt.md          # written by: prepare (issue body, extracted)
│   ├── session-id         # written by: prepare (uuidgen)
│   ├── repo               # written by: prepare (e.g. "ivy/dotfiles")
│   ├── issue-number       # written by: prepare (e.g. "132")
│   ├── tmux-session       # written by: exec (tmux session name)
│   ├── status             # written by: exec, publish (see below)
│   └── result.json        # written by: exec (claude output, if headless)
├── <worktree contents>    # the repo checkout
└── .git                   # → main repo's .git/worktrees/<name>
```

### Status values

```
prepared    → workspace created, agent has not started
running     → agent is executing
stopped     → agent exited (success or failure)
published   → branch pushed, PR opened
failed      → a stage failed (details in .hive/error)
```

## Dispatch: Poll Loop

```
every N minutes (systemd timer):
  items = github.ready_items(project_id)
  for item in items:
    if item.is_draft:
      log.warn("skipping draft: %s", item.title)
      continue
    github.move_to_in_progress(item)
    hive run <repo>#<issue>   # background
```

The poll loop is intentionally trivial. It connects the board to `hive run`. It doesn't know about workspaces, jails, or Claude.

Implemented as a systemd timer + service on the server. No GitHub Actions runner, no webhook infrastructure.

## Repo Discovery

Repos are found on disk by convention:

```
~/src/github.com/<owner>/<repo>/
```

An issue on `ivy/dotfiles#132` maps to `~/src/github.com/ivy/dotfiles/`. No configuration needed. If the repo isn't on disk, `prepare` clones it.

## CLI Examples

```bash
# Full pipeline: workspace → agent → PR
hive run ivy/dotfiles#132

# Manual stages
hive prepare ivy/dotfiles#132
hive exec /tmp/hive/dotfiles-132-1738900000
hive publish /tmp/hive/dotfiles-132-1738900000

# Re-run agent on existing workspace
hive exec /tmp/hive/dotfiles-132-1738900000

# Resume with feedback
hive exec --resume /tmp/hive/dotfiles-132-1738900000 "fix the test assertion"

# Skip PR creation (I'll review first)
hive run --no-publish ivy/dotfiles#132

# Inspect running agent
hive attach dotfiles-132

# Show all workspaces
hive list

# Tear down
hive cleanup dotfiles-132
hive cleanup --all
```
