# Configure Hive

Set up `config.toml` to connect hive to a GitHub Projects board.

## Create the config file

Copy the example config to the default location:

```bash
mkdir -p ~/.config/hive
cp hive.example.toml ~/.config/hive/config.toml
```

Hive looks for config in this order:

1. `--config <path>` flag (overrides everything)
2. `./config.toml` (current directory)
3. `$XDG_CONFIG_HOME/hive/config.toml`
4. `~/.config/hive/config.toml`

## Find your GitHub Projects IDs

Hive needs three GraphQL identifiers from your project board. Use the `gh` CLI to find them.

### Project node ID

```bash
gh api graphql -f query='
{
  viewer {
    projectsV2(first: 20) {
      nodes {
        id
        title
        number
      }
    }
  }
}'
```

Find your project in the output. The `id` field (starts with `PVT_`) is your `project-node-id`. The `number` is your `project-id`.

### Status field ID and option IDs

```bash
gh api graphql -f query='
{
  viewer {
    projectV2(number: YOUR_PROJECT_NUMBER) {
      field(name: "Status") {
        ... on ProjectV2SingleSelectField {
          id
          options {
            id
            name
          }
        }
      }
    }
  }
}'
```

Replace `YOUR_PROJECT_NUMBER` with the number from the previous step. This returns:

- The `id` of the Status field (starts with `PVTSSF_`) — this is your `status-field-id`
- Each option's `id` and `name` — match these to your board columns

### Map option IDs to config keys

| Board column | Config key |
|---|---|
| Ready | `ready-option-id` |
| In Progress | `in-progress-option-id` |
| In Review | `in-review-option-id` |

The `ready-status` value must match the exact column name including emoji (e.g., `"Ready 🤖"`).

## Fill in the config

```toml
[github]
project-id = "29"
project-node-id = "PVT_kwHOADgWUM4BOsG7"
status-field-id = "PVTSSF_lAHOADgWUM4BOsG7zg9UQiM"

in-progress-option-id = "6e4a1660"
in-review-option-id = "1b866fe9"
ready-option-id = "abc12345"

ready-status = "Ready 🤖"
```

## Configure authorization

Hive is fail-closed: if `allowed-users` is empty, nothing runs.

```toml
[security]
allowed-users = ["your-github-username"]
```

Only issues authored by users in this list will be picked up by poll or accepted by manual runs.

## Configure the jail backend

```toml
[jail]
backend = "systemd-run"
```

`systemd-run` is the only supported backend. It provides credential isolation via `ProtectSystem=strict` and a fresh `TemporaryFileSystem=$HOME`.

## Tune poll behavior

```toml
[poll]
# How often to check for ready items. Unset = single-shot mode.
interval = "5m"

# Maximum concurrent hive-run@* units. 0 = unlimited.
max-concurrent = 4

# Instance name for logging and session tracking.
instance = "default"
```

- **Single-shot mode** (no `interval`): `hive poll` runs once and exits. Useful for testing and cron.
- **Daemon mode** (`interval` set): polls immediately, then repeats on each tick. Used by the systemd service.
- **`max-concurrent`** prevents resource exhaustion by counting active `hive-run@*` systemd units before dispatching new work.

## Tune reap retention

```toml
[reap]
# How long to keep published sessions before cleanup.
published-retention = "24h"

# How long to keep failed sessions (longer for debugging).
failed-retention = "72h"
```

Reap also detects stale sessions (non-terminal status with no active systemd unit) and marks them failed, releasing their claims back to the board.

## Per-instance config for multi-project setups

To run hive against multiple project boards on the same machine, create named config files:

```bash
~/.config/hive/projectA.toml
~/.config/hive/projectB.toml
```

Each file contains a full config with different `[github]` settings. Start separate systemd instances:

```bash
systemctl --user start hive@projectA.target
systemctl --user start hive@projectB.target
```

The systemd unit templates use `%I` (instance name) to load the matching config file:

```
ExecStart=%h/.local/bin/hive poll --config %h/.config/hive/%I.toml
```

## Verify the config

Test that hive can read the board:

```bash
hive poll --verbose
```

With `--verbose`, hive logs at debug level. You should see:

```
level=INFO msg="loaded config" file=/home/you/.config/hive/config.toml
level=INFO msg="polling for ready items"
```

If there are ready items, they'll be listed. If not, you'll see `no ready items found`.

## Repo discovery convention

Hive resolves repositories to local paths by convention:

```
~/src/github.com/{owner}/{repo}/
```

Clone any repos you want hive to work on into this structure before running.
