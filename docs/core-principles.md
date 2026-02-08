# Core Principles

Decision filters for Hive. Every principle has teeth — it should be possible to violate it, and doing so should feel wrong.

## 1. Workspace as Contract

The workspace directory is the interface between components. `prepare` creates it, `exec` runs inside it, `publish` reads from it. No in-memory state is passed between pipeline stages.

If a component crashes, the workspace is the recovery point. Re-run the failed stage against the same workspace — don't start over.

**Says no to**: Passing state through function arguments across stage boundaries. Requiring components to be co-resident in memory. Any design where a crash loses work.

## 2. Credential Isolation

Each component has exactly the credentials it needs, and no more. The agent has an API key and workspace write access. It does not have GitHub tokens, SSH keys, or access to other repos. The publisher has GitHub credentials. It does not have the API key.

The threat model is a helpful agent doing something unhelpful — not a malicious actor trying to escape. Mount-based isolation is sufficient.

**Says no to**: Shared credential stores. Agents that can push to remotes. Mounting `~/.ssh`, `~/.config/gh`, or `~/.1password` into any workspace.

## 3. Single Responsibility

`poll` finds work. `prepare` creates workspaces. `exec` runs agents. `publish` ships results. `run` orchestrates all four in sequence. Each is independently invocable.

When a new capability is needed, the question is "which component owns this?" — not "where should I add this code?"

**Says no to**: A monolithic `run` that can't be decomposed. Components that reach into each other's concerns. "Just add it to exec, it's easier."

## 4. Host Parity

The agent's execution environment has the same tools as the host — exact versions, same binaries. The sandbox inherits the host's tools by running on the host kernel with read-only access to system paths. No tool installation, no image rebuilds, no version drift.

**Says no to**: Container images that approximate the host. Maintaining a separate toolchain for sandboxed execution. "It works on the host but not in the sandbox."

## 5. Resumability

Every stage is re-entrant. If `exec` crashes, run `exec` again on the same workspace. If `publish` fails, run `publish` again. If the agent needs feedback, `exec --resume` picks up where it left off.

A human can also intervene: attach to the tmux session, finish the work manually, then run `publish` to ship it. The pipeline doesn't care who did the work — it just needs commits on the branch.

**Says no to**: Pipelines that must run start-to-finish. Stages that are idempotent only on the happy path. Designs where manual intervention requires restarting from scratch.

## 6. Ship and Iterate

A shell prototype already works. The Go rewrite exists because shell scripts compose poorly, error handling is fragile, and testing is painful — not because the prototype is missing features.

Build the minimum that works. Use it daily. Let real usage expose what's missing. Resist the urge to design for hypothetical future needs.

**Says no to**: Feature work before the basic pipeline runs daily. Abstractions without two concrete use cases. "We might need this later."
