---
status: accepted
date: 2026-02-07
---

# Swappable jail backends with systemd-run as initial implementation

## Context and Problem Statement

Hive's `exec` command runs agents inside isolated workspaces. The isolation mechanism (the "jail") must enforce credential isolation, filesystem sandboxing, and environment parity. Different repositories have different parity targets: dotfiles needs host tool parity, while home-ops needs container environment parity. How should Hive support multiple isolation backends without coupling the pipeline to any single one?

## Decision Drivers

- **Environment Parity** (Principle 4): the agent's environment MUST match the environment the work targets, not a one-size-fits-all sandbox
- **Single Responsibility** (Principle 3): the jail module owns isolation; the rest of Hive MUST NOT know which backend runs
- **Credential Isolation** (Principle 2): every backend MUST enforce the same trust boundaries — the agent gets an API key and workspace write access, nothing else
- **Ship and Iterate** (Principle 6): build the minimum that works now; container support comes when real usage demands it
- **Existing validation**: the shell prototype proved systemd-run works for dotfiles (50ms startup, effective isolation)

## Considered Options

1. Swappable jail interface with systemd-run first, Podman next
2. systemd-run only, no abstraction
3. Podman containers only

## Decision Outcome

Chosen option: **Swappable jail interface with systemd-run first, Podman next**, because it delivers immediate value for dotfiles while designing for the known next step (home-ops containers) without building it yet.

The jail module MUST expose a backend-agnostic interface. Backend selection MUST be per-repo via `.hive.toml` configuration read by Viper. The initial implementation MUST be systemd-run. The interface SHOULD accommodate Podman as the next backend without requiring changes to callers.

### Consequences

- **Good**: dotfiles pipeline works immediately with prototype-validated systemd-run
- **Good**: per-repo config means adding a backend never changes existing repo configurations
- **Good**: the interface forces clean separation — no systemd-run assumptions leak into `exec` or `run`
- **Bad**: the interface must be designed speculatively against a second backend that doesn't exist yet; may require adjustment when Podman is actually implemented
- **Neutral**: Viper becomes a required dependency for jail backend selection (already planned for the tech stack)

## Pros and Cons of the Options

### Swappable jail interface with systemd-run first, Podman next

A Go interface (`Jail`) with a `Run(ctx, workspace, cmd)` method. A factory reads the repo's `.hive.toml` to select the backend. systemd-run is implemented now; Podman is added when home-ops needs it.

- **Good**: matches the architecture doc's stated design ("The implementation is swappable")
- **Good**: per-repo config aligns with how repos already differ (dotfiles = host tools, home-ops = Ansible in containers)
- **Good**: both backends use `os/exec` — no SDK dependencies
- **Bad**: interface may need revision once the second backend reveals requirements the first didn't

### systemd-run only, no abstraction

Hardcode systemd-run as the only jail. No interface, no config. Refactor later when containers are needed.

- **Good**: simplest possible implementation; zero speculative design
- **Good**: fully validated by the shell prototype
- **Bad**: home-ops is a known near-term need, not hypothetical — skipping the interface now means a refactor soon
- **Bad**: systemd-run assumptions would spread through the codebase, making the refactor harder

### Podman containers only

Use Podman for all repos. Mount host tools into the container for repos that need host parity.

- **Good**: single backend to maintain
- **Bad**: adds image management complexity for repos that don't need it (dotfiles works fine with systemd-run)
- **Bad**: host tool mounting into containers reintroduces the version drift problem Principle 4 exists to prevent
- **Bad**: slower startup than systemd-run for the common case

## More Information

- [Core Principles — Environment Parity](../core-principles.md) (revised from "Host Parity" as part of this decision)
- [Architecture — jail module](../architecture.md)
- [Prototype Learnings — systemd-run validation](../prototype-learnings.md)
- [Prototype Mechanics — why systemd-run over containers](../prototype/mechanics.md)

Revisit this decision when the Podman backend is implemented. The interface may need adjustment based on container-specific concerns (image selection, volume mounts, network policy) that systemd-run doesn't surface.
