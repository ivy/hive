package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var cdCmd = &cobra.Command{
	Use:   "cd <ref|uuid>",
	Short: "Open a shell in a session's workspace",
	Long:  "Resolves the session by ref or UUID and spawns $SHELL in its workspace directory.",
	Args:  cobra.ExactArgs(1),
	RunE:  runCd,
}

func init() {
	rootCmd.AddCommand(cdCmd)
}

func runCd(cmd *cobra.Command, args []string) error {
	sess, err := resolveSession(session.DataDir(), args[0])
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	wsPath := filepath.Join(workspace.BaseDir(), sess.ID)
	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		return fmt.Errorf("workspace not found: %s", wsPath)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	shellCmd := exec.CommandContext(cmd.Context(), shell)
	shellCmd.Dir = wsPath
	shellCmd.Stdin = os.Stdin
	shellCmd.Stdout = os.Stdout
	shellCmd.Stderr = os.Stderr
	return shellCmd.Run()
}
