# Install Hive

Build hive from source, install the binary, and set up systemd units.

## Prerequisites

- Go (managed by mise — see `mise.toml` for version)
- `gh` CLI authenticated (`gh auth login`)
- systemd with user session support (`systemctl --user`)
- tmux
- Claude Code CLI (`claude`)
- mise for tool management

## Build from source

```bash
git clone https://github.com/ivy/hive.git
cd hive
make build
```

This compiles `./cmd/hive/` and writes the binary to `./hive` in the repo root.

## Install the binary

```bash
make install
```

Installs to `~/.local/bin/hive` with mode 755. Make sure `~/.local/bin` is on your `PATH`.

To verify:

```bash
hive --help
```

You should see:

```
Hive dispatches Claude Code agents in isolated workspaces.
It polls a GitHub Projects board for ready items, creates git worktrees,
runs agents inside sandboxed environments, and opens PRs with the results.
```

## Install systemd units

```bash
make install-units
```

This copies the unit templates from `contrib/systemd/` into `~/.config/systemd/user/` and runs `systemctl --user daemon-reload`. The installed units are:

| Unit | Type | Role |
|------|------|------|
| `hive@.target` | target | Groups poll + reap for an instance |
| `hive-poll@.service` | simple | Long-running scheduler |
| `hive-run@.service` | oneshot | Per-session worker |
| `hive-reap@.service` | oneshot | Cleanup janitor |
| `hive-reap@.timer` | timer | Triggers reap hourly |

To verify:

```bash
systemctl --user list-unit-files 'hive*'
```

## Uninstall systemd units

```bash
make uninstall-units
```

Removes all hive unit files and reloads the daemon.

## Verify the tool chain

Check that all required tools are available:

```bash
gh auth status          # GitHub CLI authenticated
claude --version        # Claude Code available
systemd-run --version   # systemd available
tmux -V                 # tmux available
```

## Run the test suite

```bash
go test ./...
```

Specs use Ginkgo/Gomega BDD style. All tests should pass before you proceed to configuration.
