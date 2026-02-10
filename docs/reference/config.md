# Configuration Reference

Complete reference for all hive configuration keys, environment variables, and resolution order.

## Config Resolution Order

Hive uses [viper](https://github.com/spf13/viper) for configuration. Resolution order (highest priority first):

1. **Explicit `--config` flag** — loads exactly that file
2. **Environment variables** — `viper.AutomaticEnv()` maps env vars to config keys
3. **Config file search path** (when `--config` is not set):
   1. Current working directory (`./config.toml`)
   2. `$XDG_CONFIG_HOME/hive/config.toml`
   3. `~/.config/hive/config.toml`

Config files use TOML format. See `hive.example.toml` for a complete template.

### Named Instances

Systemd unit templates pass `--config %h/.config/hive/%I.toml`, where `%I` is the unescaped instance name. This allows multiple hive instances with separate configs (e.g. `production.toml`, `staging.toml`).

## Config Sections

### [poll]

Controls the poll daemon behavior.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `poll.interval` | duration | `0` (single-shot) | How often to check for ready items. Set to a Go duration string (e.g. `"5m"`, `"30s"`). If unset or zero, poll runs once and exits. |
| `poll.max-concurrent` | int | `0` (unlimited) | Maximum concurrent `hive-run@*` systemd units. Poll skips remaining items when this limit is reached. |
| `poll.instance` | string | `"default"` | Instance name written to `poll_instance` field in session JSON. Used for tracking which poll daemon dispatched a session. |

### [reap]

Controls cleanup retention periods.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `reap.published-retention` | duration | `24h` | How long to keep published sessions before removing workspace and session file. |
| `reap.failed-retention` | duration | `72h` | How long to keep failed sessions before cleanup. Longer than published to allow debugging. |

### [security]

Authorization controls. **Fail-closed:** if `allowed-users` is empty or missing, all operations that check authorization will refuse to run.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `security.allowed-users` | string[] | `[]` (nobody) | GitHub usernames authorized to trigger runs. Issue authors not in this list are rejected. Comparison is case-insensitive. |

### [jail]

Sandbox backend configuration.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `jail.backend` | string | `"systemd-run"` | Sandbox backend for agent execution. Currently supported: `"systemd-run"`. |

### [github]

GitHub Projects board integration. All IDs come from the GitHub GraphQL API.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `github.project-id` | string | *required* | GitHub Projects board number (e.g. `"29"`). |
| `github.project-node-id` | string | *required* | GraphQL node ID for the project (e.g. `"PVT_kwHOADgWUM4BOsG3"`). |
| `github.status-field-id` | string | `""` | GraphQL field ID for the Status single-select field. |
| `github.ready-status` | string | `""` | Exact status name to filter for ready items (e.g. `"Ready 🤖"`). |
| `github.in-progress-option-id` | string | `""` | Single-select option ID for the In Progress column. |
| `github.in-review-option-id` | string | `""` | Single-select option ID for the In Review column. |
| `github.ready-option-id` | string | `""` | Single-select option ID for the Ready column (used by reap to release items). |

## Environment Variables

| Variable | Used By | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | `exec`, `publish` | Anthropic API key passed to the agent inside the jail. Optional — if empty, the agent authenticates via `~/.claude/` (subscription auth). |
| `XDG_CONFIG_HOME` | config resolution | Overrides the config search path (default: `~/.config`). |
| `XDG_DATA_HOME` | session/workspace data | Overrides the data directory (default: `~/.local/share`). Data lives at `$XDG_DATA_HOME/hive/`. |
| `HOME` | jail, workspace | User home directory. The jail overlays a tmpfs on `$HOME` and bind-mounts selected paths. |
| `SHELL` | `cd` | Shell to spawn for `hive cd` (default: `/bin/sh`). |
| `PATH` | systemd units | Systemd unit templates set `PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin`. |

## Example Config

```toml
[poll]
interval = "5m"
max-concurrent = 4
instance = "default"

[reap]
published-retention = "24h"
failed-retention = "72h"

[security]
allowed-users = ["ivy"]

[jail]
backend = "systemd-run"

[github]
project-id = "29"
project-node-id = "PVT_kwHOADgWUM4BOsG3"
status-field-id = "PVTSSF_lAHOADgWUM4BOsG3zg9UQiM"
ready-status = "Ready 🤖"
in-progress-option-id = "6e4a1660"
in-review-option-id = "1b866fe9"
ready-option-id = "abc12345"
```
