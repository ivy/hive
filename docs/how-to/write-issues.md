# How to Write Issues That Work as Agent Prompts

GitHub issues are the primary interface between you and hive's agents. The issue body becomes the agent's prompt verbatim — what you write is what the agent reads as its instruction.

## Why issue quality matters

Hive uses the issue body as the prompt for Claude Code. The agent receives the text of `.hive/prompt.md` (which is the issue body) and works from there. No separate prompt engineering is needed if the issue is well-written.

From real usage: a well-structured issue for adding `fetch.prune = true` to git config produced correct code, tests, and conventional commits in 70 seconds at $0.45 (see `docs/prototype-learnings.md`).

## Structure of a good issue

### Title: what, not how

The title should describe the outcome, not the implementation steps. The agent uses the title as context but works from the body.

Good: `Add fetch.prune = true to git config template`
Bad: `Edit chezmoi dot_gitconfig.toml.tmpl to add a line`

### Body: context, then instruction

Start with enough context for the agent to understand the codebase area, then state what you want done.

```markdown
## Context

The git config template is managed by chezmoi at
`home/dot_gitconfig.toml.tmpl`. It currently configures
user identity, default branch, and aliases.

## Task

Add `prune = true` under the `[fetch]` section. If the section
doesn't exist, create it.

## Acceptance criteria

- `chezmoi apply` succeeds
- `git config --get fetch.prune` returns `true`
- Existing git config entries are unchanged
```

### Be explicit about commit expectations

The agent receives a system prompt that says "Commit your work before finishing. Use /commit for each logical change." But you can reinforce this in the issue body for complex tasks:

```markdown
Split this into two commits:
1. The implementation change
2. The test additions
```

## Patterns that work

### Reference existing patterns

Agents follow existing code patterns well. Point them at examples:

```markdown
Add a `hive status` command that shows the current config and
active sessions. Follow the same cobra command pattern as
`cmd/hive/ls.go`.
```

### Specify test expectations

Specs are both verification and documentation. Tell the agent what to test:

```markdown
Write Ginkgo specs that cover:
- Happy path: config file exists and is valid
- Missing config file returns graceful error
- Malformed TOML returns parse error
```

### Scope to one concern

Each issue should be one logical change. Hive creates one branch and one PR per issue. Mixing concerns leads to unfocused PRs.

Good: One issue per concern, each producing a focused PR.
Bad: "Refactor the config system, add validation, and update the docs."

### Include file paths

When the agent needs to modify specific files, name them:

```markdown
Modify `internal/session/session.go` to add a `Duration` field
that tracks how long the session ran. Update `cmd/hive/ls.go`
to display it in the output table.
```

## Patterns that don't work

### Vague instructions

```markdown
Make the code better.
```

The agent has no clear goal and will make arbitrary changes.

### Implementation-only (no "why")

```markdown
Add `ProtectHome=tmpfs` to the systemd unit.
```

Without context, the agent can't evaluate whether this is correct. The prototype learned that `ProtectHome` and `ProtectSystem=strict` conflict (see `docs/prototype-learnings.md`). A better issue explains the goal:

```markdown
## Context

The systemd sandbox needs to isolate the agent's home directory.
Currently using `TemporaryFileSystem=$HOME:mode=0755` because
`ProtectHome=tmpfs` conflicts with `ProtectSystem=strict`
(causes exit code 226).

## Task

Evaluate whether ProtectHome=read-only could replace the
TemporaryFileSystem approach while still allowing bind mounts.
```

### Multiple unrelated tasks

```markdown
1. Fix the reap command crash
2. Add a new "archive" board status
3. Update the README
```

Split these into three issues. Each gets its own session, branch, and PR.

## Authorization

Issues must be authored by a user in the `security.allowed-users` config list. Hive is fail-closed: if the author isn't in the list, the issue is silently skipped during poll and explicitly rejected during manual runs.

## Board workflow

1. Create the issue with a clear body
2. Add it to the GitHub Projects board
3. Move it to the "Ready" column (must match `github.ready-status` in config, e.g., "Ready 🤖")
4. `hive poll` picks it up, claims it, and dispatches a run
5. The board item moves to "In Progress" automatically
6. On success, it moves to "In Review" with a PR link
