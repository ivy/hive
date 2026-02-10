package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach <ref|uuid>",
	Short: "Attach to running agent's tmux session",
	Long:  "Resolves the session by ref or UUID and attaches to its tmux session.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttach,
}

func init() {
	rootCmd.AddCommand(attachCmd)
}

func runAttach(cmd *cobra.Command, args []string) error {
	sess, err := resolveSession(session.DataDir(), args[0])
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	wsPath := filepath.Join(workspace.BaseDir(), sess.ID)
	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		return fmt.Errorf("workspace not found: %s", wsPath)
	}

	// Read tmux session name
	sessionFile := filepath.Join(wsPath, ".hive", "tmux-session")
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return fmt.Errorf("read tmux session: %w (is the agent running?)", err)
	}
	sessionName := strings.TrimSpace(string(data))

	// Attach to tmux session
	tmuxCmd := exec.CommandContext(cmd.Context(), "tmux", "attach-session", "-t", sessionName)
	tmuxCmd.Stdin = os.Stdin
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr
	return tmuxCmd.Run()
}
