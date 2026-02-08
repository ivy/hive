package jail

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// SystemdJail runs commands inside a systemd-run sandbox.
type SystemdJail struct {
	runner CommandRunner
}

// Run builds and executes a systemd-run command that sandboxes the given
// command inside the workspace. It overlays a fresh tmpfs on $HOME and
// selectively bind-mounts only what the agent needs.
func (j *SystemdJail) Run(ctx context.Context, opts RunOpts) error {
	home := os.Getenv("HOME")
	bootstrap := buildBootstrap(home, opts)

	args := []string{
		"--user", "--pty",
		"-p", fmt.Sprintf("TemporaryFileSystem=%s:mode=0755", home),
		"-p", "ProtectSystem=strict",
		"-p", "PrivateTmp=yes",
		"-p", "NoNewPrivileges=yes",
		"-p", fmt.Sprintf("BindPaths=%s", opts.Workspace.Path),
		"-p", fmt.Sprintf("BindPaths=%s/.git", opts.Workspace.RepoPath),
		"-p", fmt.Sprintf("BindPaths=%s/.claude", home),
		"-p", fmt.Sprintf("BindReadOnlyPaths=%s/.local/bin", home),
		"-p", fmt.Sprintf("BindReadOnlyPaths=%s/.local/share/mise", home),
		"-p", fmt.Sprintf("BindReadOnlyPaths=%s/.local/share/claude", home),
		"--", "/bin/bash", "-c", bootstrap,
	}

	cmd := j.runner("systemd-run", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// buildBootstrap returns the shell script that runs inside the sandbox.
func buildBootstrap(home string, opts RunOpts) string {
	var b strings.Builder

	// PATH setup
	fmt.Fprintf(&b, "export PATH=\"%s/.local/bin:%s/.local/share/mise/shims:$PATH\"\n", home, home)

	// Git identity
	b.WriteString("git config --global user.name \"Claude (hive)\"\n")
	b.WriteString("git config --global user.email \"claude-hive@localhost\"\n")

	// mise trust
	b.WriteString("export MISE_YES=1\n")
	fmt.Fprintf(&b, "export MISE_TRUSTED_CONFIG_PATHS=%q\n", opts.Workspace.Path)

	// API key
	fmt.Fprintf(&b, "export ANTHROPIC_API_KEY=%q\n", opts.APIKey)

	// Additional env
	for _, env := range opts.Env {
		if k, v, ok := strings.Cut(env, "="); ok {
			fmt.Fprintf(&b, "export %s=%q\n", k, v)
		}
	}

	// cd + exec
	fmt.Fprintf(&b, "cd %q\n", opts.Workspace.Path)
	fmt.Fprintf(&b, "exec %s", shellJoin(opts.Command))

	return b.String()
}

// shellJoin quotes each argument for safe embedding in a shell script.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
