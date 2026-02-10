# About Hive's Security Model

Hive runs AI agents that edit code, commit, and produce pull requests — autonomously, without a human in the loop. This document explains the security thinking behind that design: what we're protecting against, how isolation works in practice, and what's deliberately left out.

## Threat model

Hive's threat model is a **helpful agent doing something unhelpful** — not a malicious actor trying to escape a sandbox. The agent is Claude Code, a tool that follows instructions in good faith. The danger isn't adversarial intent; it's scope creep. An agent that can push to a remote, read credentials from `~/.config/gh/`, or modify another repo's worktree can cause real damage while trying to be helpful.

The goal is to limit blast radius: confine each agent to the workspace it was given and the API key it needs, so that mistakes stay local and reviewable.

This framing has practical consequences. It means Hive doesn't need to defeat a determined attacker — container escapes, kernel exploits, and side-channel attacks are out of scope. It also means the isolation doesn't need to be perfect, just sufficient to prevent the most likely accidental misuse. A mount-based sandbox that prevents credential access covers the realistic failure modes.

## Trust boundaries across the pipeline

Each pipeline stage has access to exactly the credentials and filesystem regions it needs. No stage has a superset of another's access.

| Stage | GitHub credentials | Repo write access | Claude API key |
|---------|-------------------------|------------------------|----------------|
| poll | Read (board queries) | None | None |
| prepare | Read (issue fetch) | Worktree creation | None |
| exec | None | Workspace directory only | Yes |
| publish | Write (push, PR, board) | Read (branch) | None |
| reap | Write (release items) | Worktree removal | None |

The key separation is between `exec` and `publish`. The agent inside `exec` can edit files, run tests, and commit — but it cannot push branches, open pull requests, or interact with GitHub at all. The `publish` stage runs *outside* the jail, with GitHub credentials, after the agent has exited. This means an agent that hallucinates a `git push` or tries to call the GitHub API simply fails — the credentials aren't available in its environment.

Session metadata (the JSON files tracking dispatch state, source refs, and board item IDs) lives outside the workspace directory at `~/.local/share/hive/sessions/`. The jail mounts only the workspace. The agent cannot read or modify its own dispatch state, which means it can't mark itself as published, manipulate claim files, or interfere with the pipeline's view of the world.

## Credential isolation in practice

The systemd-run jail (the current backend) enforces credential isolation through mount namespacing. When `exec` launches an agent, the sandbox starts with a fresh `tmpfs` overlay on `$HOME` — the agent's home directory is empty by default. Only specific paths are bind-mounted back in:

**Read-write mounts:**

- The workspace directory (`~/.local/share/hive/workspaces/<uuid>/`) — the agent needs to edit, build, and commit here
- The repo's `.git` directory — git operations (commit, log, diff) need write access to the worktree metadata under `.git/worktrees/<name>/`
- `~/.claude/` — Claude Code writes session data, todos, and logs here; read-only access causes it to crash

**Read-only mounts:**

- `~/.local/bin` — host-installed binaries (go, mise, etc.)
- `~/.local/share/mise` — mise shims and tool installations
- `~/.local/share/claude` — Claude Code's application data

**Not mounted (inaccessible):**

- `~/.config/gh/` — GitHub CLI credentials
- `~/.ssh/` — SSH keys
- `~/.config/hive/` — Hive's own configuration (contains project IDs, status field IDs)
- `~/.1password/` — password manager sockets
- `~/.local/share/hive/sessions/` — session metadata
- `~/.local/share/hive/claims/` — claim files
- Any other workspace directory — agents cannot see each other's work

The `ProtectSystem=strict` directive makes the entire system filesystem read-only. Combined with the `$HOME` tmpfs overlay, this means the agent can only write to its explicitly mounted paths. Stray writes to `/etc`, `/usr`, or any system path silently fail.

Additional hardening:

- `PrivateTmp=yes` — the agent gets its own `/tmp`, isolated from other processes
- `NoNewPrivileges=yes` — the agent cannot escalate privileges via setuid binaries or capabilities

The API key (for Claude Code's own API calls) is passed as an environment variable inside the bootstrap script, not mounted from a file. It exists only in the agent's process environment and disappears when the process exits.

## The .git mount: a conscious trade-off

The repo's `.git` directory is mounted read-write into the sandbox. This is necessary — git needs to update the worktree's index, HEAD, and reflog under `.git/worktrees/<name>/`. But it also means the agent could theoretically modify refs in the main repository checkout.

The prototype discovered this constraint: mounting `.git` read-only breaks basic git operations (commit, checkout, reset). The current implementation mounts the entire `.git` directory rather than scoping the mount to `.git/worktrees/<name>/` specifically. This is acceptable under the threat model (helpful-not-malicious) but is a known area for tightening. A more precise mount that gives write access only to `.git/worktrees/<name>/` while keeping `.git/objects`, `.git/refs`, and `.git/config` read-only would reduce the blast radius further.

See [Prototype Learnings](../prototype-learnings.md) for the full story of how this constraint was discovered.

## Network access: current state

The sandbox has **full network access**. This is a deliberate choice, not an oversight. Claude Code makes API calls to Anthropic's servers during execution — blocking network access would prevent the agent from functioning at all.

This means an agent could, in theory, make arbitrary HTTP requests — exfiltrating workspace contents, downloading unexpected dependencies, or contacting services it shouldn't. Under the current threat model, this is acceptable: the concern is accidental credential use, not data exfiltration by a cooperative agent.

Network restriction is technically possible (systemd's `IPAddressAllow`/`IPAddressDeny` directives, or iptables rules scoping traffic to Anthropic's API endpoints), but the trade-offs are unfavorable today:

- API endpoint IPs change; maintaining an allowlist is fragile
- Some legitimate agent operations need network access (fetching documentation, running package managers)
- The threat model doesn't include adversarial exfiltration

If the threat model evolves — for instance, to support untrusted agents or third-party code execution — network isolation would become a priority.

## Issue author authorization

Hive includes a gate before any agent work begins: the `authz` package checks whether the issue author is in a configured allowlist. If an issue in the Ready column was filed by an unknown user, Hive skips it. The check is case-insensitive (GitHub usernames are case-insensitive) and fail-closed — an empty allowlist means no issues are processed.

This prevents a class of attack where someone files a malicious issue (with a crafted prompt) on a public repo and waits for Hive to pick it up. The authorization check happens early in the `poll` → `Ready()` path, before any workspace is created or agent is launched.

## Backend-agnostic isolation

The jail module is an interface, not an implementation. The current systemd-run backend was validated in the shell prototype and provides effective isolation for repos developed directly on the host (like dotfiles). But different repos have different needs — a repo whose work targets a container environment should run the agent in a matching container.

The `Jail` interface (`Run(ctx, opts)`) abstracts this choice. Backend selection is per-repo via configuration. The rest of Hive — `exec`, `run`, the entire pipeline — doesn't know or care whether the agent ran under systemd-run or in a Podman container. Both backends must enforce the same trust boundaries: the agent gets an API key and workspace write access, nothing else.

This design means adding a new isolation backend doesn't require changes to the pipeline. It also means security properties are defined by the interface contract, not by any single implementation. See [ADR 001](../adrs/001-swappable-jail-backends.md) for the decision record.

## What's explicitly out of scope

These concerns are acknowledged but deliberately not addressed:

- **Container escapes and kernel exploits.** The threat model assumes a cooperative agent. Hardening against a determined attacker would require a fundamentally different isolation approach (VMs, gVisor, etc.) and isn't justified for a personal tool.
- **Side-channel attacks.** The agent runs on the same host as the operator. Timing attacks, cache side channels, and similar concerns aren't relevant to the "helpful agent doing something unhelpful" model.
- **Supply chain attacks via dependencies.** If the agent runs `go get` or `npm install` inside the workspace, it's pulling packages from the public internet. Hive doesn't validate, pin, or audit these. This is the same risk as running those commands manually.
- **Agent-to-agent interference.** Each agent runs in its own systemd transient unit with its own workspace mount. They can't see each other's workspaces. But they share the host's CPU, memory, and I/O — there's no resource isolation beyond what systemd's default cgroup provides. A runaway agent could starve others of resources.
- **Persistence after exit.** The sandbox is transient — it exists only while the agent runs. But the workspace persists on disk after the agent exits (for debugging and PR creation). The workspace contents are visible to the host user and to `publish`. This is by design, not a gap.
- **Audit logging.** There's no detailed record of what the agent did inside the sandbox beyond git commits and journald logs. Adding `strace`-level auditing or command logging is possible but adds complexity that isn't justified yet.
