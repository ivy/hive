package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const agentSystemPrompt = `Every Ready item should produce code changes. Implement the requested changes and commit your work. Use /commit for each logical change — small, focused commits, not one big batch at the end. Don't leave uncommitted changes in the worktree.

If the issue is not implementable (e.g., it's a design discussion or planning task), explain why in your response.`

const maxCommitRetries = 3

const commitNudge = `You left uncommitted changes in the worktree. Please either:

- Use /commit to commit them (split into logical commits if needed)
- Add generated/temporary files to .gitignore
- Explain why the changes should not be committed

Then exit.`

const completionSchema = `{"type":"object","properties":{"completed":{"type":"boolean","description":"Whether the task was fully implemented and committed"},"summary":{"type":"string","description":"Brief description of what was done or attempted"},"blockers":{"type":"string","description":"What prevented completion, if anything"}},"required":["completed","summary"]}`

const validationPrompt = `Check the status of your work on this issue. Have you implemented and committed the requested changes?`

const noCommitsNudge = `You reported the task as completed, but there are no commits on the branch. The expectation is that Ready items produce code changes. Please implement the requested changes and commit your work.

If the issue is not implementable (e.g., it's a design discussion or needs more clarification), set completed to false and explain in blockers.`

// CompletionReport is the structured output from the completion validation.
type CompletionReport struct {
	Completed bool   `json:"completed"`
	Summary   string `json:"summary"`
	Blockers  string `json:"blockers,omitempty"`
}

// claudeResponse is the top-level JSON from `claude -p --output-format json`.
type claudeResponse struct {
	SessionID        string            `json:"session_id"`
	StructuredOutput *CompletionReport `json:"structured_output"`
}

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

	// Nudge agent to commit any leftover changes
	for attempt := 1; attempt <= maxCommitRetries; attempt++ {
		hasChanges, err := workspace.HasUncommittedChanges(cmd.Context(), ws)
		if err != nil || !hasChanges {
			break
		}
		slog.Warn("agent left uncommitted changes, resuming session",
			"attempt", attempt, "max", maxCommitRetries)

		retryCmd := []string{
			"claude", "-p",
			"--dangerously-skip-permissions",
			"--resume", ws.SessionID,
			"--append-system-prompt", agentSystemPrompt,
			"--model", model,
			"--output-format", "json",
			commitNudge,
		}
		opts.Command = retryCmd
		if err := j.Run(cmd.Context(), opts); err != nil {
			slog.Error("commit retry failed", "attempt", attempt, "error", err)
			break
		}
	}

	// Validate that the agent produced commits
	hasCommits, err := workspace.HasNewCommits(cmd.Context(), ws)
	if err != nil {
		slog.Error("failed to check for commits", "error", err)
	} else if !hasCommits {
		slog.Warn("agent produced no commits, validating completion")
		if err := validateCompletion(cmd.Context(), j, opts, ws, model); err != nil {
			if setErr := workspace.SetStatus(ws, workspace.StatusFailed); setErr != nil {
				slog.Error("failed to set status", "error", setErr)
			}
			return fmt.Errorf("validation failed: %w", err)
		}
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
			"--append-system-prompt", agentSystemPrompt,
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
		"--append-system-prompt", agentSystemPrompt,
		"--model", model,
		"--output-format", "json",
		prompt,
	}, nil
}

// validateCompletion checks if the agent believes it completed the task,
// and retries with a nudge if it claims completion but produced no commits.
func validateCompletion(ctx context.Context, j jail.Jail, opts jail.RunOpts, ws *workspace.Workspace, model string) error {
	for attempt := 1; attempt <= maxCommitRetries; attempt++ {
		slog.Info("requesting completion report", "attempt", attempt)

		validationCmd := []string{
			"claude", "-p",
			"--resume", ws.SessionID,
			"--output-format", "json",
			"--json-schema", completionSchema,
			"--tools", "Glob,Read,Bash",
			"--allowedTools", "Glob,Read,Bash(git log *),Bash(git status *),Bash(git diff *)",
			"--model", model,
			validationPrompt,
		}

		opts.Command = validationCmd
		out, err := j.RunCapture(ctx, opts)
		if err != nil {
			return fmt.Errorf("validation command failed (attempt %d): %w", attempt, err)
		}

		report, parseErr := parseCompletionReport(out)
		if parseErr != nil {
			slog.Warn("failed to parse completion report", "attempt", attempt, "error", parseErr)
			if attempt == maxCommitRetries {
				return fmt.Errorf("completion report not received after %d attempts: %w", maxCommitRetries, parseErr)
			}
			continue
		}

		if report == nil {
			slog.Warn("completion report missing, will retry", "attempt", attempt)
			if attempt == maxCommitRetries {
				return fmt.Errorf("completion report not received after %d attempts", maxCommitRetries)
			}
			continue
		}

		slog.Info("received completion report", "completed", report.Completed, "summary", report.Summary)

		// If agent reports blocked or incomplete, fail with the reason
		if !report.Completed {
			msg := "agent reported task incomplete"
			if report.Blockers != "" {
				msg = fmt.Sprintf("agent reported blockers: %s", report.Blockers)
			}
			return fmt.Errorf("%s", msg)
		}

		// Agent claims completed but has no commits — nudge to actually implement
		slog.Warn("agent claims completion but has no commits, nudging to implement")
		retryCmd := []string{
			"claude", "-p",
			"--dangerously-skip-permissions",
			"--resume", ws.SessionID,
			"--append-system-prompt", agentSystemPrompt,
			"--model", model,
			"--output-format", "json",
			noCommitsNudge,
		}
		opts.Command = retryCmd
		if err := j.Run(ctx, opts); err != nil {
			slog.Error("implementation retry failed", "attempt", attempt, "error", err)
			if attempt == maxCommitRetries {
				return fmt.Errorf("agent failed to implement after %d attempts: %w", maxCommitRetries, err)
			}
			continue
		}

		// Check if commits were made after the nudge
		hasCommits, err := workspace.HasNewCommits(ctx, ws)
		if err != nil {
			return fmt.Errorf("failed to check for commits after retry: %w", err)
		}
		if hasCommits {
			slog.Info("agent produced commits after nudge")
			return nil
		}

		if attempt == maxCommitRetries {
			return fmt.Errorf("agent produced no commits after %d attempts", maxCommitRetries)
		}
	}

	return fmt.Errorf("validation loop completed without resolution")
}

// parseCompletionReport unmarshals the claude JSON response.
// Returns (nil, sessionID, nil) when structured_output is missing (signals retry).
// Returns (nil, "", err) on malformed JSON.
func parseCompletionReport(data []byte) (*CompletionReport, error) {
	var resp claudeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.StructuredOutput == nil {
		return nil, nil
	}

	// Validate required fields
	if resp.StructuredOutput.Summary == "" {
		return nil, nil
	}

	return resp.StructuredOutput, nil
}
