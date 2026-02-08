// Package jail provides sandboxed execution of commands in workspaces.
// The Jail interface is backend-agnostic — callers don't know whether
// the command runs under systemd-run, Podman, or something else.
package jail

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/ivy/hive/internal/workspace"
)

// CommandRunner is a function that creates an *exec.Cmd.
// It exists for testability — specs inject a recording runner
// instead of actually invoking systemd-run.
type CommandRunner func(name string, args ...string) *exec.Cmd

// RunOpts configures a jail execution.
type RunOpts struct {
	// Workspace is the workspace to run inside.
	Workspace *workspace.Workspace

	// Command is the command and arguments to execute.
	Command []string

	// Env is additional environment variables (KEY=VALUE).
	Env []string

	// APIKey is the Anthropic API key passed to the agent.
	APIKey string
}

// Jail is the interface for sandboxed command execution.
type Jail interface {
	// Run executes a command inside the jail with the given options.
	// It connects stdin/stdout/stderr to the current terminal.
	Run(ctx context.Context, opts RunOpts) error
}

// New creates a Jail for the given backend name.
// Supported backends: "systemd-run".
func New(backend string) (Jail, error) {
	return NewWithRunner(backend, exec.Command)
}

// NewWithRunner creates a Jail for the given backend with a custom
// CommandRunner. This is the primary constructor for testing — inject
// a recording runner to capture the command without invoking systemd-run.
func NewWithRunner(backend string, runner CommandRunner) (Jail, error) {
	switch backend {
	case "systemd-run":
		return &SystemdJail{runner: runner}, nil
	default:
		return nil, fmt.Errorf("unknown jail backend: %q", backend)
	}
}
