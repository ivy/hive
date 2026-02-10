# Dispatch & Lifecycle

How hive dispatches work, manages agent lifecycles, and cleans up after itself. This document focuses on the *why* behind the design — the rationale, trade-offs, and failure recovery thinking. For interface definitions and directory layouts, see the [Architecture](architecture.md), [Session & Data Reference](reference/session.md), [Jail Interface Reference](reference/jail-interface.md), and [Source Interface Reference](reference/source-interface.md).

Motivated by [#43] (zero-downtime deploys) and [#28] (agents as systemd units). Informed by [#11] (recovery/state management), [#22] (Unix philosophy evaluation), and [#29] (multi-project support).

## Design Principles

- **systemd is the process supervisor.** Hive does not manage process lifecycles. Each run is its own systemd unit. Poll can restart freely without affecting running agents. ([#28], [#43])
- **No in-memory state crosses process boundaries.** Components communicate through the filesystem and systemd unit state. ([#11], Core Principle 1: Workspace as Contract)
- **The work source is an abstraction.** Hive's core (sessions, workspaces, claims, dispatch, reap) has no knowledge of where work items come from. GitHub Projects is one source implementation. The source could be Gitea, Linear, a directory of markdown files, or anything else. ([#29])
- **Session metadata is outside the agent's trust boundary.** The agent sees its workspace directory and nothing else. Dispatch metadata, claim state, and source references are not accessible to the agent. ([#50], Core Principle 2: Credential Isolation)

## Why systemd owns process lifecycles

The shell prototype ran agents as child processes of the poll daemon. This meant agents died whenever poll restarted — a binary upgrade killed all in-flight work. Each run is now its own systemd unit (`hive-run@{uuid}.service`), completely independent of poll's lifecycle.

This separation has cascading benefits:

- **Zero-downtime deploys** — stop poll, upgrade the binary, restart poll. Running agents are unaffected because they're separate units. ([#43])
- **Crash isolation** — a failed run doesn't bring down poll or other runs. systemd reports the failure per-unit.
- **Observability** — `systemctl --user list-units 'hive-run@*'` shows all active runs. `journalctl --user -u hive-run@{uuid}` shows logs for a specific run. No more `ps aux | grep hive`.
- **Resource accounting** — systemd's cgroup integration gives per-unit CPU and memory tracking for free.

Poll is a daemon, not a systemd timer. An earlier design used an external timer + oneshot service, but a long-running daemon simplifies operations (one unit instead of two) and dispatches immediately on startup rather than waiting for the first timer activation. See [PR #40] (`a438ce0`), which closed [#35].

## Components

### hive poll (scheduler)

Long-lived daemon. Finds ready work items from a source, claims them locally, and starts systemd units. Does nothing else.

- Queries the configured source for ready items on an internal timer
- For each item, checks the local claim index — skips if already claimed
- Creates a session (UUID), writes session metadata, places a claim
- Starts `hive-run@{uuid}` via systemctl
- Marks the item as taken on the source (e.g., moves to In Progress)
- **Stateless between restarts** — all dispatch state is on disk (claims, sessions)

Does NOT monitor runs, track child processes, or manage any lifecycle beyond dispatch. ([#22], Core Principle 3: Single Responsibility)

### hive run (worker)

Runs as a oneshot systemd unit (`hive-run@{uuid}.service`). Orchestrates one work item end-to-end. ([#28])

- Reads session metadata from the session JSON file
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

## Claim-based dedup

Two gates prevent duplicate dispatch: ([#11])

1. **Source gate** — only ready items are returned by the source. Items already taken (In Progress, In Review, Done) are filtered at the source.
2. **Claim gate** — local claim file, created atomically with `O_EXCL`.

Why two gates? The source gate is necessary but not sufficient. Between the moment poll calls `source.Ready()` and the moment it calls `source.Take()`, the source could return the same item on a concurrent poll tick (or on a restart). The local claim file closes this window — `O_EXCL` guarantees only one process wins the race.

The claim key is a truncated SHA-256 hash of the source ref. Hive never parses the ref — it's opaque. The source adapter produces refs and knows how to act on them.

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

Every crash point has a recovery path. This is the core design constraint — no crash should leave work permanently stuck.

| Crash point | Claim state | Session state | Unit state | Recovery |
|---|---|---|---|---|
| After claim, before unit start | Claim exists | Session file written | No unit | Reap: stale session + no unit → release claim, notify source |
| After unit start, before source.Take | Claim exists | Running | Active | Next poll tick: claim gate blocks re-dispatch; source may return item again but claim prevents action |
| Run fails, no cleanup | Claim exists | Running | Failed | Reap: non-terminal session + no active unit → mark failed, release claim, source.Release ([#39]) |
| Run succeeds, claim removal fails | Claim exists | Published | Inactive | Reap: terminal session + claim still present → remove claim |
| Poll restarts mid-cycle | Some claims placed | Some sessions written | Some units started | Poll restarts: source gate + claim gate prevent re-dispatch of in-flight work ([#43]) |

The key insight is that reap is the safety net. It scans for inconsistencies between claim state, session state, and systemd unit state, and resolves them. This is why reap runs on a timer — it's the eventual consistency mechanism for the dispatch pipeline.

## Session lifecycle

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

For session JSON structure and storage details, see [Session & Data Reference](reference/session.md).

## Workspace lifecycle

Workspaces are git worktrees created by the `prepare` stage. ([#21])

```
prepare     creates worktree at workspaces/<uuid>/
exec        agent works inside the worktree
publish     pushes branch, opens PR from the worktree
reap        removes worktree, deletes branch, prunes git metadata
```

Reap replaces the current manual `hive cleanup` command. Retention periods are configurable:

- Published sessions: short retention (workspace is low-value after PR is open)
- Failed sessions: longer retention (workspace preserved for debugging)

For workspace directory structure and metadata files, see [Session & Data Reference](reference/session.md).

## Concurrency

Poll respects a `max-concurrent` limit when dispatching. ([#41]) Before starting a new unit, poll counts active `hive-run@*` units via systemctl. If at capacity, remaining ready items are skipped and picked up on the next tick.

Future: auto-detect a sensible default based on system resources. ([#42])

## What changed from the prototype

| Concern | Prototype | Current design |
|---------|-----------|----------------|
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
