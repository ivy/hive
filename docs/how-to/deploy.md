# Deploy Hive as a Systemd Service

Run hive as a persistent systemd user service that starts at boot.

## Prerequisites

- Hive binary installed (`make install`)
- Systemd units installed (`make install-units`)
- Config file at `~/.config/hive/config.toml` (or named instance config)
- `gh` CLI authenticated

## Start the hive target

The `hive@.target` groups the poll service and reap timer for a named instance. For the default instance:

```bash
systemctl --user start hive@default.target
```

This starts:

- `hive-poll@default.service` — long-running scheduler that polls the board
- `hive-reap@default.timer` — hourly trigger for session cleanup

Verify both are running:

```bash
systemctl --user status hive@default.target
systemctl --user status hive-poll@default.service
systemctl --user list-timers 'hive-reap*'
```

## Enable at boot with loginctl

Systemd user services only run while a user session exists. To keep hive running after logout, enable lingering:

```bash
loginctl enable-linger $USER
```

Then enable the target to start automatically:

```bash
systemctl --user enable hive@default.target
```

## Check logs

All hive components log to the systemd user journal.

### Poll logs

```bash
journalctl --user -u hive-poll@default.service -f
```

### Run logs (specific session)

```bash
journalctl --user -u hive-run@SESSION_UUID.service
```

Replace `SESSION_UUID` with the UUID from `hive ls`.

### Reap logs

```bash
journalctl --user -u hive-reap@default.service
```

### Combined hive logs

```bash
journalctl --user -u 'hive-*' --since "1 hour ago"
```

## Monitor active runs

```bash
# List all active hive-run units
systemctl --user list-units 'hive-run@*'

# List sessions with status
hive ls
```

`hive ls` shows all tracked sessions sorted by creation time:

```
UUID                                  REF                      STATUS      CREATED
a1b2c3d4-e5f6-7890-abcd-ef1234567890  github:ivy/hive#132     published   2h ago
f9e8d7c6-b5a4-3210-fedc-ba9876543210  github:ivy/hive#145     running     5m ago
```

## Zero-downtime binary upgrades

To upgrade hive without interrupting active agent runs:

1. Build and install the new binary:

   ```bash
   cd ~/src/github.com/ivy/hive
   git pull
   make install
   ```

2. Restart the poll service (picks up new binary):

   ```bash
   systemctl --user restart hive-poll@default.service
   ```

Active `hive-run@*` units continue running with the old binary until they complete. New dispatches use the updated binary. The reap timer doesn't need a restart — the next invocation picks up the new binary automatically.

## Stop hive

```bash
# Stop the target (stops poll + reap timer, leaves active runs alone)
systemctl --user stop hive@default.target

# Stop a specific run
systemctl --user stop hive-run@SESSION_UUID.service
```

## Multi-instance deployment

For multiple project boards, start separate targets with named configs:

```bash
systemctl --user start hive@projectA.target
systemctl --user start hive@projectB.target
```

Each instance loads `~/.config/hive/projectA.toml` or `projectB.toml` respectively. Enable both for boot persistence:

```bash
systemctl --user enable hive@projectA.target hive@projectB.target
```
