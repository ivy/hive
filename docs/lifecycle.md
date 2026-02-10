# Dispatch & Lifecycle

End-state specification for how hive dispatches work, manages agent lifecycles, and cleans up after itself. Supersedes the current fire-and-forget model where poll spawns child processes that die with it.

Motivated by [#43] (zero-downtime deploys) and [#28] (agents as systemd units). Informed by [#11] (recovery/state management), [#22] (Unix philosophy evaluation), and [#29] (multi-project support).

## Design Principles

- **systemd is the process supervisor.** Hive does not manage process lifecycles. Each run is its own systemd unit. Poll can restart freely without affecting running agents. ([#28], [#43])
- **No in-memory state crosses process boundaries.** Components communicate through the filesystem and systemd unit state. ([#11], Core Principle 1: Workspace as Contract)
- **The work source is an abstraction.** Hive's core (sessions, workspaces, claims, dispatch, reap) has no knowledge of where work items come from. GitHub Projects is one source implementation. The source could be Gitea, Linear, a directory of markdown files, or anything else. ([#29])
- **Session metadata is outside the agent's trust boundary.** The agent sees its workspace directory and nothing else. Dispatch metadata, claim state, and source references are not accessible to the agent. ([#50], Core Principle 2: Credential Isolation)

## Components

### hive poll (scheduler)

Long-lived daemon. Finds ready work items from a source, claims them locally, and starts systemd units. Does nothing else.

- Queries the configured source for ready items on an internal timer
- For each item, checks the local claim index — skips if already claimed
- Creates a session (UUID), writes session metadata, places a claim
- Starts `hive-run@{uuid}` via systemctl
- Marks the item as taken on the source (e.g., moves to In Progress)
- **Stateless between restarts** — all dispatch state is on disk (claims, sessions)

Poll is a daemon, not a systemd timer. An earlier design used an external timer + oneshot service, but a long-running daemon simplifies operations (one unit instead of two) and dispatches immediately on startup rather than waiting for the first timer activation. See [PR #40] (`a438ce0`), which closed [#35].

Does NOT monitor runs, track child processes, or manage any lifecycle beyond dispatch. ([#22], Core Principle 3: Single Responsibility)

### hive run (worker)

Runs as a oneshot systemd unit (`hive-run@{uuid}.service`). Orchestrates one work item end-to-end. ([#28])

- Reads session metadata from `~/.local/share/hive/sessions/{uuid}.json`
- Resolves the source ref, repo path, and any source-specific identifiers from the session
- Executes: prepare → exec → publish
- On success: session status → `published`, notifies source (e.g., board → In Review)
- On failure: session status → `failed`, notifies source (e.g., board → Ready), exit non-zero ([#39])
- **Self-contained** — owns its entire lifecycle from start to completion

The workspace persists after run exits for debugging. Reap handles cleanup. (Core Principle 5: Resumability)

### hive reap (janitor)

Runs on a systemd timer. Scans sessions and workspaces. Cleans up finished work. Recovers stuck items. ([#39], [#11])

- `published` sessions past retention: remove workspace (worktree + branch), remove session file, release claim
- `failed` sessions past retention: same (longer retention for debugging)
- Stale claims (session in non-terminal state, no active systemd unit): mark failed, notify source to release the item, release claim ([#39])
- **Idempotent** — safe to run at any frequency

## Work Source Interface

The source is the boundary between hive's core and wherever work items come from. It is an abstraction — hive core never imports source-specific packages. Same pattern as the swappable jail interface ([ADR 001]). ([#29])

```go
type Source interface {
    // Ready returns work items available for dispatch.
    Ready(ctx context.Context) ([]WorkItem, error)

    // Take marks an item as claimed on the remote side.
    Take(ctx context.Context, ref string) error

    // Complete marks an item as done on the remote side.
    Complete(ctx context.Context, ref string) error

    // Release marks an item as available again on the remote side.
    Release(ctx context.Context, ref string) error
}

type WorkItem struct {
    Ref      string            // opaque identifier (source-specific)
    Repo     string            // repository location (e.g., "ivy/hive")
    Title    string            // human-readable summary
    Prompt   string            // agent instructions (e.g., issue body)
    Metadata map[string]string // source-specific data (board-item-id, labels, etc.)
}
```

The `Ref` is an opaque string. Hive hashes it for the claim key but never parses it. The source adapter produces refs and knows how to act on them.

GitHub Projects is the initial implementation. The interface accommodates any source that can answer "what's ready?" and accept status updates.

## Directory Layout

All hive data lives under `~/.local/share/hive/` (XDG_DATA_HOME). Workspaces survive reboots. ([#21])

```
~/.local/share/hive/
├── claims/
│   └── <hash>                  # claim file (content: session UUID)
├── sessions/
│   └── <uuid>.json             # session metadata
└── workspaces/
    └── <uuid>/                 # git worktree (agent's sandbox)
```

### claims/

One file per active work item. The filename is a hash (SHA-256, truncated) of the source ref. The content is the session UUID.

- **Created by poll** with `O_EXCL` — atomic, race-free dedup
- **Removed by run** on completion, or by **reap** on expiry/failure
- `ls claims/` shows all in-flight work
- Existence check is O(1) — no scanning

### sessions/

One JSON file per session, keyed by UUID. Contains everything needed to run the work item:

```json
{
  "id": "00112233-4455-6677-8899-ccddeeffaabb",
  "ref": "github:ivy/hive#43",
  "repo": "ivy/hive",
  "title": "Zero-downtime deploys",
  "prompt": "...",
  "source_metadata": {
    "board_item_id": "PVTI_...",
    "project_node_id": "PVT_..."
  },
  "status": "running",
  "created_at": "2026-02-10T12:00:00Z",
  "poll_instance": "default"
}
```

This file is **outside the workspace directory** — the agent cannot read or modify it. The jail mounts only `workspaces/<uuid>/`. Session metadata includes source-specific fields (like board-item-id) that would be dangerous or confusing inside the agent's sandbox. ([#50], Core Principle 2: Credential Isolation)

### workspaces/

One directory per session, keyed by UUID. This is the git worktree where the agent works. The jail mounts this directory and nothing above it. ([#50])

```
workspaces/<uuid>/
├── <repo contents>             # git worktree
├── .git                        # → main repo's .git/worktrees/<name>
└── .claude/                    # agent session data (bind-mounted read-write)
```

## systemd Units

```
~/.config/systemd/user/
├── hive@.target                 # meta: groups poll + reap per instance
├── hive-poll@.service           # daemon: scheduler
├── hive-run@.service            # oneshot: worker (UUID instance)
├── hive-reap@.service           # oneshot: janitor
└── hive-reap@.timer             # periodic trigger for reap
```

### <hive@.target>

Groups poll and reap for a named instance. One command to start/stop everything. ([#29])

```ini
[Unit]
Description=Hive orchestrator: %I
Wants=hive-poll@%i.service hive-reap@%i.timer
```

`systemctl --user start hive@default.target` brings up everything.

### <hive-poll@.service>

```ini
[Unit]
Description=Hive poll: %I
PartOf=hive@%i.target

[Service]
Type=simple
ExecStart=%h/.local/bin/hive poll --config %h/.config/hive/%I.toml
Restart=on-failure
RestartSec=30
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

Reads instance-specific config from `~/.config/hive/<instance>.toml`. The config defines the source type, source settings (project ID, status field IDs, etc.), poll interval, and repo discovery rules.

Restartable at any time. No children to orphan — runs are separate units. ([#43])

Future: `Type=notify` with sd_notify for accurate readiness reporting. ([#9])

### <hive-run@.service>

```ini
[Unit]
Description=Hive run: %I

[Service]
Type=oneshot
ExecStart=%h/.local/bin/hive run %i
TimeoutStartSec=infinity
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

Not installed (no `[Install]` section) — started on-demand by poll. The `%i` is a UUID. `TimeoutStartSec=infinity` because agent runs have no predictable upper bound. ([#28])

Future: per-unit structured journal fields for filtering by repo/issue. ([#46])

### <hive-reap@.timer> / <hive-reap@.service>

```ini
# hive-reap@.timer
[Unit]
Description=Hive reap timer: %I
PartOf=hive@%i.target

[Timer]
OnCalendar=hourly
Persistent=true

# hive-reap@.service
[Unit]
Description=Hive reap: %I

[Service]
Type=oneshot
ExecStart=%h/.local/bin/hive reap --config %h/.config/hive/%I.toml
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

## Claim-Based Dedup

Two gates prevent duplicate dispatch: ([#11])

1. **Source gate** — only ready items are returned by the source. Items already taken (In Progress, In Review, Done) are filtered at the source.
2. **Claim gate** — local claim file, created atomically with `O_EXCL`.

### Claim lifecycle

```
poll: source returns ready item with ref R
poll: hash(R) → claim key K
poll: open("claims/K", O_CREATE|O_EXCL) → success or already-claimed
      if already-claimed → skip
poll: write UUID to claim file
poll: write sessions/<uuid>.json
poll: systemctl start hive-run@<uuid>
poll: source.Take(R)

run:  ... prepare → exec → publish ...
run:  source.Complete(R) or source.Release(R)
run:  update sessions/<uuid>.json status
run:  remove claims/K

reap: scan sessions/ for non-terminal status with no active unit
reap: for stale sessions: source.Release(R), remove claim, mark failed
reap: scan sessions/ for terminal status past retention
reap: for expired sessions: remove workspace, session file, claim (if still present)
```

### Crash scenarios

| Crash point | Claim state | Session state | Unit state | Recovery |
|---|---|---|---|---|
| After claim, before unit start | Claim exists | Session file written | No unit | Reap: stale session + no unit → release claim, notify source |
| After unit start, before source.Take | Claim exists | Running | Active | Next poll tick: claim gate blocks re-dispatch; source may return item again but claim prevents action |
| Run fails, no cleanup | Claim exists | Running | Failed | Reap: non-terminal session + no active unit → mark failed, release claim, source.Release ([#39]) |
| Run succeeds, claim removal fails | Claim exists | Published | Inactive | Reap: terminal session + claim still present → remove claim |
| Poll restarts mid-cycle | Some claims placed | Some sessions written | Some units started | Poll restarts: source gate + claim gate prevent re-dispatch of in-flight work ([#43]) |

## Session Lifecycle

```
                    ┌─ dispatching ─┐
                    │               │
              (poll creates)   (poll starts unit)
                    │               │
                    └──→ prepared ──→ running ──→ stopped ──→ published
                                        │                       │
                                        └──→ failed ←───────────┘
                                               │
                                          (reap cleans up)
```

Status transitions:

| Status | Set by | Meaning |
|--------|--------|---------|
| `dispatching` | poll | Session created, unit not yet started |
| `prepared` | run (prepare) | Workspace created, agent not started |
| `running` | run (exec) | Agent is executing |
| `stopped` | run (exec) | Agent exited |
| `published` | run (publish) | Branch pushed, PR opened, source notified |
| `failed` | run or reap | A stage failed or session was detected as stuck ([#39]) |

Terminal states: `published`, `failed`.

## Workspace Lifecycle

Workspaces are git worktrees created by the `prepare` stage inside `~/.local/share/hive/workspaces/<uuid>/`. ([#21])

```
prepare     creates worktree at workspaces/<uuid>/
exec        agent works inside the worktree
publish     pushes branch, opens PR from the worktree
reap        removes worktree, deletes branch, prunes git metadata
```

Reap replaces the current manual `hive cleanup` command. Retention periods are configurable:

- Published sessions: short retention (workspace is low-value after PR is open)
- Failed sessions: longer retention (workspace preserved for debugging)

## Concurrency

Poll respects a `max-concurrent` limit when dispatching. ([#41]) Before starting a new unit, poll counts active `hive-run@*` units via systemctl. If at capacity, remaining ready items are skipped and picked up on the next tick.

Future: auto-detect a sensible default based on system resources. ([#42])

## CLI Changes

| Command | Current | End state |
|---------|---------|-----------|
| `hive run <ref>` | Accepts `owner/repo#123`, runs inline | Also accepts `<uuid>`, reads session metadata |
| `hive cd <ref\|uuid>` | Does not exist | Spawns `$SHELL` in the workspace of the specified session |
| `hive attach <ref\|uuid>` | Uses tmux session name | Spawns `$SHELL` or attaches to the agent session in the workspace |
| `hive ls` | `hive list`, lists workspaces from `/tmp/hive/` | Lists sessions from `~/.local/share/hive/sessions/` |
| `hive cleanup` | Manual, removes all or by ID | Replaced by automated `hive reap` |

### Resolving `<ref|uuid>`

`hive cd`, `hive attach`, and other session-aware commands accept either a work item ref (e.g., `ivy/hive#43`) or a session UUID. Resolution order:

1. If the argument looks like a UUID, look up `sessions/<uuid>.json` directly.
2. Otherwise, treat it as a source ref — scan `claims/` for a matching claim to find the active session. If no active claim, find the most recent session matching that ref.

This means `hive cd ivy/hive#43` lands you in the workspace of the latest session for that ticket, and `hive cd 00112233-...` targets a specific session. Both spawn `$SHELL` in the workspace directory.

For manual use, `hive run owner/repo#123` generates its own UUID, creates the session, and runs inline (no systemd unit). This preserves the current manual workflow. ([#22])

## What Changes from Today

| Concern | Current | End state |
|---------|---------|-----------|
| Dispatch | `exec.Command` child process | `systemctl --user start hive-run@{uuid}` ([#28]) |
| Agent survival on deploy | Agents die with poll daemon | Agents in own units, unaffected ([#43]) |
| Lifecycle monitoring | None (fire-and-forget) | systemd unit state + session files ([#9], [#28]) |
| Workspace cleanup | Manual `hive cleanup` | Automated `hive reap` on timer |
| Workspace location | `/tmp/hive/` (lost on reboot) | `~/.local/share/hive/workspaces/` (persistent) ([#21]) |
| Session metadata | Individual files inside workspace (`.hive/`) | JSON file outside workspace, agent-inaccessible ([#50]) |
| Failure recovery | Items stuck In Progress forever ([#39]) | Run self-reports; reap catches crashes |
| Idempotent dispatch | None | Atomic claim files (`O_EXCL`) ([#11]) |
| Run discoverability | `ps aux \| grep hive` | `systemctl --user list-units 'hive-run@*'` ([#28]) |
| Logs | Mixed into poll's stderr | Per-unit journald (`journalctl --user -u hive-run@{uuid}`) ([#46]) |
| Source coupling | GitHub Projects hardcoded in poll | Source interface; GitHub is one implementation ([#29]) |
| Multi-instance | Single poll daemon | `hive@.target` per instance, each with own config ([#29]) |
| Config | Single `~/.config/hive/config.toml` | Per-instance `~/.config/hive/<instance>.toml` ([#29]) |
| Concurrency | Unbounded ([#41]) | `max-concurrent` limit, enforced by poll ([#41], [#42]) |

## References

Open issues addressed by this spec:

- [#9] — Integrate systemd notify and watchdog
- [#11] — Design recovery and state management strategy
- [#21] — Adopt XDG Base Directory Specification for all paths
- [#28] — Spawn each agent as a systemd unit
- [#29] — Support multiple projects and repositories
- [#39] — Move interrupted/failed items back to Ready on the board
- [#41] — Limit concurrent agent runs
- [#42] — Auto-detect sensible max-concurrent default
- [#43] — Zero-downtime deploys: don't kill in-flight agents on binary upgrade
- [#46] — Switch to slog-journal and add per-job journal fields
- [#50] — Audit filesystem security and workspace isolation

Closed issues and merged PRs that informed the design:

- [#35] — Convert `hive poll` to long-running daemon (closed, done)
- [PR #40] — feat(poll): convert to long-running daemon with --interval flag (merged)
- [ADR 001] — Swappable jail backends with systemd-run as initial implementation

Related but out of scope:

- [#8] — Implement jail backend for local server replica via Ansible
- [#22] — Evaluate Unix philosophy in subcommand design
- [#30] — Re-dispatch agent when PR changes are requested

[#8]: https://github.com/ivy/hive/issues/8
[#9]: https://github.com/ivy/hive/issues/9
[#11]: https://github.com/ivy/hive/issues/11
[#21]: https://github.com/ivy/hive/issues/21
[#22]: https://github.com/ivy/hive/issues/22
[#28]: https://github.com/ivy/hive/issues/28
[#29]: https://github.com/ivy/hive/issues/29
[#30]: https://github.com/ivy/hive/issues/30
[#35]: https://github.com/ivy/hive/issues/35
[#39]: https://github.com/ivy/hive/issues/39
[#41]: https://github.com/ivy/hive/issues/41
[#42]: https://github.com/ivy/hive/issues/42
[#43]: https://github.com/ivy/hive/issues/43
[#46]: https://github.com/ivy/hive/issues/46
[#50]: https://github.com/ivy/hive/issues/50
[PR #40]: https://github.com/ivy/hive/pull/40
[ADR 001]: adrs/001-swappable-jail-backends.md
