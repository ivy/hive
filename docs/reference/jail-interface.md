# Jail Interface Reference

Complete reference for the `Jail` interface, `RunOpts` struct, and the systemd-run backend.

## Jail Interface

Defined in `internal/jail/jail.go`. Provides sandboxed command execution with pluggable backends.

```go
type Jail interface {
    Run(ctx context.Context, opts RunOpts) error
    RunCapture(ctx context.Context, opts RunOpts) ([]byte, error)
}
```

### Methods

| Method | Used By | Description |
|--------|---------|-------------|
| `Run` | `exec` | Executes a command inside the jail with stdout/stderr connected to the current terminal. Stdin is not connected. |
| `RunCapture` | `publish` (PR drafting) | Executes a command inside the jail and captures stdout as `[]byte`. Stderr still goes to `os.Stderr`. |

### Constructors

```go
func New(backend string) (Jail, error)
func NewWithRunner(backend string, runner CommandRunner) (Jail, error)
```

- `New` creates a jail with the default `exec.Command` runner
- `NewWithRunner` accepts a custom `CommandRunner` for testing

### Supported Backends

| Backend | Description |
|---------|-------------|
| `"systemd-run"` | SystemdJail — sandboxes via `systemd-run --user` |

Unknown backend names return an error.

## RunOpts

Configures a jail execution. Defined in `internal/jail/jail.go`.

```go
type RunOpts struct {
    Workspace *workspace.Workspace
    Command   []string
    Env       []string
    APIKey    string
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Workspace` | `*workspace.Workspace` | The workspace to run inside. Provides `Path` (working directory) and `RepoPath` (for `.git` bind mount). |
| `Command` | `[]string` | Command and arguments to execute (e.g. `["claude", "-p", ...]`). |
| `Env` | `[]string` | Additional environment variables in `KEY=VALUE` format. |
| `APIKey` | `string` | Anthropic API key. Optional — if empty, the agent authenticates via `~/.claude/` (subscription auth). |

## CommandRunner

```go
type CommandRunner func(name string, args ...string) *exec.Cmd
```

Injectable function for creating `*exec.Cmd`. Production uses `exec.Command`; tests inject a recording runner.

## systemd-run Backend

The `SystemdJail` backend (`internal/jail/systemd.go`) runs commands inside a `systemd-run --user` transient scope with strict sandboxing.

### systemd-run Arguments

```
systemd-run --user --pipe \
  -p TemporaryFileSystem=$HOME:mode=0755 \
  -p ProtectSystem=strict \
  -p PrivateTmp=yes \
  -p NoNewPrivileges=yes \
  -p BindPaths={workspace.Path} \
  -p BindPaths={workspace.RepoPath}/.git \
  -p BindPaths=$HOME/.claude \
  -p BindReadOnlyPaths=$HOME/.local/bin \
  -p BindReadOnlyPaths=$HOME/.local/share/mise \
  -p BindReadOnlyPaths=$HOME/.local/share/claude \
  -- /bin/bash -c '{bootstrap script}'
```

### Sandbox Properties

| Property | Value | Purpose |
|----------|-------|---------|
| `TemporaryFileSystem` | `$HOME:mode=0755` | Overlays a fresh tmpfs on `$HOME`. Agent cannot see or modify user files outside explicit bind mounts. |
| `ProtectSystem` | `strict` | Makes the entire filesystem read-only except for explicitly allowed paths. |
| `PrivateTmp` | `yes` | Gives the unit its own `/tmp` namespace. |
| `NoNewPrivileges` | `yes` | Prevents privilege escalation via setuid/setgid binaries. |

### Bind Mounts

| Type | Path | Purpose |
|------|------|---------|
| Read-write | `{workspace.Path}` | Workspace directory — agent's working directory |
| Read-write | `{workspace.RepoPath}/.git` | Main repo's `.git` — required for worktree operations |
| Read-write | `$HOME/.claude` | Claude configuration and session data |
| Read-only | `$HOME/.local/bin` | User binaries (hive, claude, etc.) |
| Read-only | `$HOME/.local/share/mise` | mise tool shims and installs |
| Read-only | `$HOME/.local/share/claude` | Claude application data |

### Bootstrap Script

The bootstrap script runs inside the sandbox as `/bin/bash -c '...'`. It sets up the environment before executing the command:

```bash
# PATH setup
export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"

# Git identity for agent commits
git config --global user.name "Claude (hive)"
git config --global user.email "claude-hive@localhost"

# mise auto-trust for workspace tools
export MISE_YES=1
export MISE_TRUSTED_CONFIG_PATHS="{workspace.Path}"

# API key (only if provided)
export ANTHROPIC_API_KEY="{apiKey}"

# Additional env vars from RunOpts.Env
export KEY="value"

# Execute command in workspace
cd "{workspace.Path}"
exec {command}
```

### Credential Isolation

The sandbox achieves credential isolation through its tmpfs overlay:

- `$HOME` is a fresh tmpfs — no SSH keys, AWS credentials, etc.
- Only `$HOME/.claude` is bind-mounted (read-write) for session persistence
- `ANTHROPIC_API_KEY` is passed via environment variable, not filesystem
- `gh` CLI is not available inside the jail (not in bind-mounted paths)
- The agent can only interact with the repo through git operations in the workspace
