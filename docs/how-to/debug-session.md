# How to Debug a Failed Session

Investigate why a session failed and optionally resume it.

## List sessions

```bash
hive ls
```

Output shows all tracked sessions sorted by most recent first:

```
UUID                                  REF                      STATUS      CREATED
a1b2c3d4-e5f6-7890-abcd-ef1234567890  github:ivy/hive#132     failed      30m ago
f9e8d7c6-b5a4-3210-fedc-ba9876543210  github:ivy/hive#145     published   2h ago
```

Session statuses and what they mean:

| Status | Meaning |
|---|---|
| `dispatching` | Session created, systemd unit starting |
| `prepared` | Workspace created, agent hasn't started |
| `running` | Agent is executing |
| `stopped` | Agent exited normally |
| `published` | Branch pushed, PR opened |
| `failed` | Something went wrong (or stuck session detected by reap) |

## Read session metadata

Session JSON is stored at `~/.local/share/hive/sessions/{uuid}.json`:

```bash
cat ~/.local/share/hive/sessions/a1b2c3d4-e5f6-7890-abcd-ef1234567890.json | jq .
```

The JSON contains the ref, repo, title, prompt, status, source metadata (board item ID), and creation time.

## Inspect the workspace

Open a shell in the session's workspace directory:

```bash
hive cd a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

You can also use the issue reference (resolves to the most recent session for that ref):

```bash
hive cd ivy/hive#132
```

Once inside the workspace, inspect the `.hive/` metadata directory:

```bash
ls .hive/
```

| File | Contents |
|---|---|
| `repo` | Repository owner/name (e.g., `ivy/hive`) |
| `issue-number` | Issue number |
| `session-id` | Claude session UUID (for resume) |
| `status` | Current lifecycle state |
| `prompt.md` | Issue body that was given to the agent |
| `issue.json` | Full issue JSON payload |
| `tmux-session` | Tmux session name (only while agent is running) |
| `board-item-id` | GitHub Projects board item ID (if dispatched via poll) |

Check what the agent's status was:

```bash
cat .hive/status
```

Check the git log to see what the agent committed:

```bash
git log --oneline
```

Check for uncommitted changes:

```bash
git status
```

## Check journald logs

View logs for the specific run unit:

```bash
journalctl --user -u hive-run@a1b2c3d4-e5f6-7890-abcd-ef1234567890.service
```

This shows the full output from `hive run`, including prepare, exec, and publish stages. Add `--no-pager` for piping or `-f` for following live output.

If the session was dispatched via poll, also check the poll logs for dispatch errors:

```bash
journalctl --user -u hive-poll@default.service --since "1 hour ago"
```

## Attach to a running agent

If the session status is `running`, you can attach to the agent's tmux session:

```bash
hive attach a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

Or by issue reference:

```bash
hive attach ivy/hive#132
```

This reads the tmux session name from `.hive/tmux-session` and runs `tmux attach-session`. Detach with `Ctrl-b d` without interrupting the agent.

If the agent isn't running, you'll see an error:

```
Error: read tmux session: open .hive/tmux-session: no such file or directory (is the agent running?)
```

## Resume a failed session

If the agent failed partway through, you can resume it with feedback. The session ID in `.hive/session-id` allows Claude Code to continue where it left off.

```bash
hive exec --resume "The tests are failing because X, please fix." ~/.local/share/hive/workspaces/a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

The `--resume` flag reads the Claude session ID from `.hive/session-id` and passes your feedback as the resume prompt. The agent sees the full conversation history and your new instruction.

You can also specify a different model:

```bash
hive exec --resume "Try a different approach" --model opus ~/.local/share/hive/workspaces/SESSION_UUID
```

After the agent finishes, inspect the result and publish manually:

```bash
hive publish ~/.local/share/hive/workspaces/SESSION_UUID
```

## Manual run from an issue

To bypass the poll loop and run a specific issue directly:

```bash
hive run ivy/hive#132
```

Add `--no-publish` to stop before opening a PR, so you can inspect first:

```bash
hive run --no-publish ivy/hive#132
```

This creates a session, prepares the workspace, runs the agent, and stops. You can then inspect the workspace with `hive cd` and publish later with `hive publish`.

## Check claims

Claims prevent duplicate dispatch of the same work item. If a session is stuck and the claim isn't released, poll will skip that issue. Claims are stored in `~/.local/share/hive/claims/`:

```bash
ls ~/.local/share/hive/claims/
```

The `hive reap` command detects stale sessions and releases their claims automatically. You can also trigger it manually:

```bash
hive reap --verbose
```
