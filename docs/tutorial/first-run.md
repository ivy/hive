# First Run: From Issue to Pull Request

This tutorial walks you through manually dispatching a single GitHub issue
to a completed pull request using hive. By the end you will have built hive,
configured it, watched an agent implement a change, and seen the resulting PR.

## Prerequisites

You need:

- **Go** (managed by mise) to build hive
- **mise** for tool management (`curl https://mise.run | sh`)
- **gh** (GitHub CLI), authenticated (`gh auth login`)
- **Claude Code** installed (`npm install -g @anthropic-ai/claude-code` or via mise)
- **An Anthropic API key** (set as `ANTHROPIC_API_KEY` in your environment)
- **A test repository** cloned at `~/src/github.com/<owner>/<repo>/`
- **A GitHub issue** in that repository with a clear, implementable task in the body

The local clone path matters. Hive resolves repos by convention:
`~/src/github.com/<owner>/<repo>/`. If your repo lives elsewhere, hive
will not find it.

## Build and install hive

Clone the hive repository and build the binary:

```bash
cd ~/src/github.com/ivy/hive
mise install        # install toolchain (Go, etc.)
make install        # builds ./hive and installs to ~/.local/bin/hive
```

Verify the binary is on your PATH:

```bash
hive --help
```

You should see the top-level help with subcommands like `poll`, `run`,
`prepare`, `exec`, `publish`, `reap`, and `ls`.

## Create a configuration file

Hive reads configuration from `~/.config/hive/config.toml`. Create a
minimal config for manual runs:

```bash
mkdir -p ~/.config/hive
```

```toml
# ~/.config/hive/config.toml

[security]
# Your GitHub username. Hive only processes issues authored by these users.
# Fail-closed: if this list is empty, hive refuses to run.
allowed-users = ["your-github-username"]

[jail]
backend = "systemd-run"
```

That is enough for the manual workflow. The `[github]` section with project
board IDs is only needed for `hive poll` (automated dispatch). We will skip
it for now.

## Step 1: Prepare a workspace

The `prepare` command fetches an issue from GitHub, creates a git worktree,
and writes metadata the agent needs. The argument is an issue reference in
`owner/repo#number` format:

```bash
hive prepare your-org/your-repo#42
```

Output:

```
Workspace ready: /home/you/.local/share/hive/workspaces/a1b2c3d4-...
Branch: hive/a1b2c3d4-...
Issue: Add retry logic to API client
```

### Inspect the workspace

Take a look at what hive created:

```bash
ls ~/.local/share/hive/workspaces/
```

Each workspace is a UUID-named directory. Inside it, you will find the full
repository checkout (a git worktree) plus a `.hive/` metadata directory:

```bash
ls <workspace-path>/.hive/
```

```
issue-number   prompt.md    session-id
issue.json     repo         status
```

The two files you'll interact with most are `prompt.md` (the issue body
that becomes the agent's instructions) and `status` (the current lifecycle
stage — `prepared` right now). For the full metadata layout, see
[Session & Data Reference](../reference/session.md#workspace-layout).

The workspace is a real git worktree branched from `main`. The agent will
work here exactly as if it cloned the repo fresh.

## Step 2: Run the agent

The `exec` command launches Claude Code inside a
[sandboxed workspace](../explanation/security-model.md#credential-isolation-in-practice).

```bash
hive exec <workspace-path>
```

For example:

```bash
hive exec ~/.local/share/hive/workspaces/a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

The agent reads `.hive/prompt.md`, implements the requested change, runs
tests, and commits its work. You will see output as the agent works. This
can take a few minutes depending on the complexity of the issue.

When the agent finishes:

```
Agent finished: /home/you/.local/share/hive/workspaces/a1b2c3d4-...
```

If the agent leaves uncommitted changes, hive retries automatically
(see [lifecycle](../lifecycle.md#session-lifecycle)). Check the workspace status:

```bash
cat <workspace-path>/.hive/status
```

It should say `stopped` — the agent finished successfully.

### Inspect the agent's work

You can review what the agent did before publishing:

```bash
cd <workspace-path>
git log --oneline main..HEAD
git diff main..HEAD --stat
```

This is a good time to verify the changes look reasonable.

## Step 3: Publish the result

The `publish` command pushes the branch and opens a pull request:

```bash
hive publish <workspace-path>
```

Hive will:

1. Auto-commit any remaining uncommitted changes (as a safety net)
2. Push the branch to the remote
3. Draft a PR title and body using Claude (falls back to a simple template if drafting fails)
4. Create the pull request via the GitHub API

Output:

```
Published: https://github.com/your-org/your-repo/pull/99
```

Open the URL to review the PR on GitHub.

## Shortcut: `hive run`

The three steps above (prepare, exec, publish) are what you do when you want
to inspect each stage manually. For day-to-day use, `hive run` chains them
into a single command:

```bash
hive run your-org/your-repo#42
```

This runs the full pipeline: prepare the workspace, launch the agent, and
publish the PR. One command, one issue, one PR.

Add `--no-publish` to stop after the agent finishes so you can review
before publishing, then run `hive publish <workspace-path>` when ready.

## Monitor sessions

List all sessions with `hive ls`:

```bash
hive ls
```

```
UUID                                  REF                          STATUS      CREATED
a1b2c3d4-e5f6-7890-abcd-ef1234567890  github:your-org/your-repo#42  published   5m ago
```

To jump into a session's workspace:

```bash
hive cd your-org/your-repo#42
```

This spawns a shell in the workspace directory so you can inspect files,
run tests, or check git history.

## Clean up

Once you are done, `hive reap` removes expired sessions and their
workspaces:

```bash
hive reap
```

Reap also recovers stuck sessions. See the [CLI reference](../reference/cli.md#hive-reap)
for retention flags.

## What you just did

You took a single GitHub issue through hive's full pipeline:

1. **prepare** — Created an isolated git worktree with the issue as a prompt
2. **exec** — Ran Claude Code in a sandbox to implement the change
3. **publish** — Pushed the branch and opened a pull request

Or you did all three at once with `hive run`.

## Next steps

- **Automate dispatch** — Configure the `[github]` section in `config.toml`
  with your project board IDs, install the systemd units with
  `make install-units`, and let `hive poll` pick up Ready items
  automatically.
- **Review the architecture** — Read `docs/architecture.md` to understand
  how the pipeline stages, trust boundaries, and credential isolation work.
- **Write good issues** — The issue body *is* the agent's prompt. Clear,
  specific issues with acceptance criteria produce better results.
