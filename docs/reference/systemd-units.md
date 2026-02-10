# Systemd Units Reference

Complete reference for all systemd unit templates in `contrib/systemd/`.

All units are **user units** (installed to `~/.config/systemd/user/`) and use systemd template syntax with instance specifiers.

## Instance Specifiers

| Specifier | Expansion | Example |
|-----------|-----------|---------|
| `%i` | Instance name (escaped) | `production`, `a1b2c3d4-e5f6-...` |
| `%I` | Instance name (unescaped) | Same, but with special characters preserved |
| `%h` | User home directory | `/home/ivy` |

For hive, `%i` and `%I` are typically identical (instance names contain only alphanumeric characters, hyphens, and dots).

## <hive-poll@.service>

Long-lived daemon that polls for ready work items.

### Unit File

```ini
[Unit]
Description=Hive poll: %I
PartOf=hive@%i.target

[Service]
Type=simple
ExecStart=%h/.local/bin/hive poll --config %h/.config/hive/%I.toml
Restart=on-failure
RestartSec=30
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

### Behavior

- **Type:** `simple` — runs as a long-lived process
- **Config:** uses `--config %h/.config/hive/%I.toml` (instance-specific config)
- **Restart:** restarts on failure after 30 seconds
- **PartOf:** bound to `hive@%i.target` — stops when the target stops
- **Note:** the poll interval is set in the config file (`poll.interval`), not in the unit

### Example Usage

```bash
# Start polling with the "production" config
systemctl --user start hive-poll@production.service

# Check status
systemctl --user status hive-poll@production.service

# View logs
journalctl --user -u hive-poll@production.service -f
```

## <hive-run@.service>

One-shot unit for running a dispatched work item. Started by `hive poll` via `systemctl --user start hive-run@{uuid}.service`.

### Unit File

```ini
[Unit]
Description=Hive run: %I

[Service]
Type=oneshot
ExecStart=%h/.local/bin/hive run %i
TimeoutStartSec=infinity
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

### Behavior

- **Type:** `oneshot` — runs to completion, then exits
- **Argument:** `%i` is the session UUID
- **Timeout:** `infinity` — agent runs may take hours; no timeout
- **Not PartOf target:** run units are independently started by poll, not tied to the target lifecycle
- **No Restart:** if the run fails, the session is marked as `failed` and reap handles recovery

### Example Usage

```bash
# Manually start a run (usually done by poll)
systemctl --user start hive-run@a1b2c3d4-e5f6-7890-abcd-ef1234567890.service

# Check if a run is active
systemctl --user is-active hive-run@a1b2c3d4-e5f6-7890-abcd-ef1234567890.service

# View logs for a specific run
journalctl --user -u hive-run@a1b2c3d4-e5f6-7890-abcd-ef1234567890.service
```

## <hive-reap@.service>

One-shot unit for cleaning up expired sessions and recovering stuck items.

### Unit File

```ini
[Unit]
Description=Hive reap: %I

[Service]
Type=oneshot
ExecStart=%h/.local/bin/hive reap --config %h/.config/hive/%I.toml
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin
```

### Behavior

- **Type:** `oneshot` — runs cleanup once, then exits
- **Config:** uses instance-specific config (same as poll)
- **Triggered by:** the companion timer unit

### Example Usage

```bash
# Manually trigger reap
systemctl --user start hive-reap@production.service

# View reap logs
journalctl --user -u hive-reap@production.service
```

## <hive-reap@.timer>

Timer unit that triggers `hive-reap@.service` on a schedule.

### Unit File

```ini
[Unit]
Description=Hive reap timer: %I
PartOf=hive@%i.target

[Timer]
OnCalendar=hourly
Persistent=true
```

### Behavior

- **Schedule:** runs hourly (`OnCalendar=hourly`)
- **Persistent:** `true` — if a scheduled run was missed (e.g. system was off), it runs on next boot/start
- **PartOf:** bound to `hive@%i.target` — stops when the target stops
- **Activates:** `hive-reap@%i.service` (implicit, same instance name)

### Example Usage

```bash
# Start the reap timer
systemctl --user start hive-reap@production.timer

# Check next scheduled run
systemctl --user list-timers hive-reap@production.timer

# View timer status
systemctl --user status hive-reap@production.timer
```

## <hive@.target>

Target unit that groups poll and reap services for a named instance.

### Unit File

```ini
[Unit]
Description=Hive orchestrator: %I
Wants=hive-poll@%i.service hive-reap@%i.timer
```

### Behavior

- **Wants:** starts `hive-poll@%i.service` and `hive-reap@%i.timer` when the target is started
- **PartOf (reverse):** poll and reap timer declare `PartOf=hive@%i.target`, so stopping the target stops them

### Example Usage

```bash
# Start the full orchestrator for a named instance
systemctl --user start hive@production.target

# Stop everything (poll + reap timer)
systemctl --user stop hive@production.target

# Check overall status
systemctl --user status hive@production.target

# Enable for auto-start on login
systemctl --user enable hive@production.target
```

## Unit Relationships

```
hive@{instance}.target
├── Wants: hive-poll@{instance}.service    (PartOf target)
└── Wants: hive-reap@{instance}.timer      (PartOf target)
                └── Activates: hive-reap@{instance}.service

hive-poll dispatches:
    └── hive-run@{uuid}.service            (independent, no target binding)
```

## Installation

```bash
make install-units
```

This copies the unit templates to `~/.config/systemd/user/` and reloads the systemd user daemon.

## Useful Commands

```bash
# Reload after editing unit files
systemctl --user daemon-reload

# Start the full stack
systemctl --user start hive@production.target

# Stop the full stack
systemctl --user stop hive@production.target

# List all active hive units
systemctl --user list-units 'hive-*'

# List active runs
systemctl --user list-units 'hive-run@*' --state=active,activating

# Follow logs for all hive units
journalctl --user -u 'hive-*' -f

# Follow logs for a specific instance
journalctl --user -u 'hive-poll@production.service' -u 'hive-reap@production.service' -f
```
