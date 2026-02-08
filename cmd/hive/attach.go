package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach <workspace-id>",
	Short: "Attach to running agent's tmux session",
	Long:  "Reads the tmux session name from workspace metadata and attaches to it.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttach,
}

func init() {
	rootCmd.AddCommand(attachCmd)
}

func runAttach(cmd *cobra.Command, args []string) error {
	wsID := args[0]

	// Resolve workspace path — either a full path or a short ID
	wsPath := wsID
	if !filepath.IsAbs(wsPath) {
		// Treat as a short ID: scan workspaces
		workspaces, err := workspace.ListAll(cmd.Context())
		if err != nil {
			return fmt.Errorf("list workspaces: %w", err)
		}
		for _, ws := range workspaces {
			if strings.Contains(filepath.Base(ws.Path), wsID) {
				wsPath = ws.Path
				break
			}
		}
		if !filepath.IsAbs(wsPath) {
			return fmt.Errorf("workspace not found: %s", wsID)
		}
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
