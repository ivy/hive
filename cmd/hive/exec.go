package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var execCmd = &cobra.Command{
	Use:   "exec <workspace-path>",
	Short: "Launch agent in sandboxed workspace",
	Long:  "Loads a prepared workspace, sets status to running, launches Claude Code inside the jail, then sets status to stopped.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExec,
}

func init() {
	execCmd.Flags().String("resume", "", "resume session with feedback message")
	execCmd.Flags().String("model", "sonnet", "Claude model to use")
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) error {
	wsPath := args[0]
	resume, _ := cmd.Flags().GetString("resume")
	model, _ := cmd.Flags().GetString("model")

	ws, err := workspace.Load(cmd.Context(), wsPath)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	slog.Info("executing agent", "workspace", ws.Path, "issue", ws.IssueNumber)

	// Set status to running
	if err := workspace.SetStatus(ws, workspace.StatusRunning); err != nil {
		return fmt.Errorf("set status: %w", err)
	}

	// Build claude command
	claudeCmd, err := buildClaudeCommand(ws, resume, model)
	if err != nil {
		return err
	}

	// API key is optional — subscription auth uses ~/.claude/ instead
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	// Get jail backend from config
	backend := viper.GetString("jail.backend")
	if backend == "" {
		backend = "systemd-run"
	}

	j, err := jail.New(backend)
	if err != nil {
		return fmt.Errorf("create jail: %w", err)
	}

	opts := jail.RunOpts{
		Workspace: ws,
		Command:   claudeCmd,
		APIKey:    apiKey,
	}

	slog.Info("launching agent in jail", "backend", backend)

	err = j.Run(cmd.Context(), opts)

	// Set status based on result
	if err != nil {
		if setErr := workspace.SetStatus(ws, workspace.StatusFailed); setErr != nil {
			slog.Error("failed to set status", "error", setErr)
		}
		return fmt.Errorf("agent execution: %w", err)
	}

	if err := workspace.SetStatus(ws, workspace.StatusStopped); err != nil {
		return fmt.Errorf("set status: %w", err)
	}

	fmt.Printf("Agent finished: %s\n", ws.Path)
	return nil
}

func buildClaudeCommand(ws *workspace.Workspace, resume string, model string) ([]string, error) {
	if resume != "" {
		// Resume existing session with feedback
		sessionID, err := workspace.ReadSessionID(ws)
		if err != nil {
			slog.Warn("could not read session ID, starting new session", "error", err)
			return buildNewClaudeCommand(ws, model, resume)
		}
		return []string{
			"claude", "-p",
			"--dangerously-skip-permissions",
			"--resume", sessionID,
			"--model", model,
			"--output-format", "json",
			resume,
		}, nil
	}

	// Read prompt for new session
	prompt, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "prompt.md"))
	if err != nil {
		return nil, fmt.Errorf("reading prompt: %w", err)
	}

	return buildNewClaudeCommand(ws, model, string(prompt))
}

func buildNewClaudeCommand(ws *workspace.Workspace, model string, prompt string) ([]string, error) {
	return []string{
		"claude", "-p",
		"--dangerously-skip-permissions",
		"--session-id", ws.SessionID,
		"--model", model,
		"--output-format", "json",
		prompt,
	}, nil
}
