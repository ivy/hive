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

const agentSystemPrompt = `Commit your work before finishing. Use /commit for each logical change — small, focused commits, not one big batch at the end. Don't leave uncommitted changes in the worktree.

IMPORTANT: Every Ready item must produce code changes. If the issue is a design proposal or analysis task that cannot be implemented, set your completion status to false and explain why in blockers. Do not simply analyze or plan without implementing.`

const maxCommitRetries = 3

const commitNudge = `You left uncommitted changes in the worktree. Please either:

- Use /commit to commit them (split into logical commits if needed)
- Add generated/temporary files to .gitignore
- Explain why the changes should not be committed

Then exit.`

const completionSchema = `{"type":"object","properties":{"completed":{"type":"boolean","description":"Whether the task was fully implemented and committed"},"summary":{"type":"string","description":"Brief description of what was done or attempted"},"blockers":{"type":"string","description":"What prevented completion, if anything"}},"required":["completed","summary"]}`

const validationPrompt = `Please provide a completion report for this task. Use the structured output tool to indicate whether you completed the implementation (with commits) or encountered blockers.`

const implementationNudge = `You reported the task as completed, but there are no commits on the branch. The expectation is that Ready items produce code changes. Please implement the requested changes and commit your work. If the issue is not implementable (e.g. it's a design discussion), set completed to false and explain in blockers.`

// completionReport is the structured output from validation.
type completionReport struct {
	Completed bool   `json:"completed"`
	Summary   string `json:"summary"`
	Blockers  string `json:"blockers,omitempty"`
}

// claudeResponse is the top-level JSON from `claude -p --output-format json`.
type claudeResponse struct {
	SessionID        string            `json:"session_id"`
	StructuredOutput *completionReport `json:"structured_output"`
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

	// Check for zero commits and validate completion
	hasCommits, err := workspace.HasNewCommits(cmd.Context(), ws)
	if err != nil {
		slog.Warn("failed to check for commits", "error", err)
	} else if !hasCommits {
		slog.Warn("agent produced zero commits, validating completion status")
		if validationErr := validateCompletion(cmd.Context(), j, ws, model); validationErr != nil {
			if setErr := workspace.SetStatus(ws, workspace.StatusFailed); setErr != nil {
				slog.Error("failed to set status", "error", setErr)
			}
			return validationErr
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

// validateCompletion checks whether the agent completed the task when no commits exist.
// It uses structured output to get a completion report from the agent, then either
// retries with a nudge (if agent claims completion) or fails with the blocker reason.
func validateCompletion(ctx context.Context, j jail.Jail, ws *workspace.Workspace, model string) error {
	const maxAttempts = 3
	const tools = "Glob,Read,Bash"
	const allowedTools = "Glob,Read,Bash(git diff *),Bash(git log *),Bash(git show *),Bash(git status *)"

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var prompt string
		if attempt == 1 {
			prompt = validationPrompt
		} else {
			prompt = implementationNudge
		}

		cmdArgs := []string{
			"claude", "-p",
			"--dangerously-skip-permissions",
			"--resume", ws.SessionID,
			"--output-format", "json",
			"--json-schema", completionSchema,
			"--tools", tools,
			"--allowedTools", allowedTools,
			"--model", model,
			prompt,
		}

		slog.Debug("validating completion status", "attempt", attempt)

		out, err := j.RunCapture(ctx, jail.RunOpts{
			Workspace: ws,
			Command:   cmdArgs,
		})
		if err != nil {
			slog.Warn("validation call failed", "attempt", attempt, "error", err)
			if attempt == maxAttempts {
				return fmt.Errorf("validation failed after %d attempts: %w", maxAttempts, err)
			}
			continue
		}

		report, parseErr := parseCompletionReport(out)
		if parseErr != nil {
			slog.Warn("failed to parse completion report", "attempt", attempt, "error", parseErr)
			if attempt == maxAttempts {
				return fmt.Errorf("structured output not received after %d attempts: %w", maxAttempts, parseErr)
			}
			continue
		}

		if report == nil {
			slog.Warn("structured output missing, will retry", "attempt", attempt)
			if attempt == maxAttempts {
				return fmt.Errorf("structured output not received after %d attempts", maxAttempts)
			}
			continue
		}

		// Agent claims it completed, but there are no commits — retry with nudge
		if report.Completed {
			slog.Warn("agent claims completion but no commits exist, retrying",
				"attempt", attempt, "summary", report.Summary)
			if attempt == maxAttempts {
				return fmt.Errorf("agent reported completion but produced no commits after %d attempts: %s",
					maxAttempts, report.Summary)
			}
			continue
		}

		// Agent reports blockers — fail with the reason
		slog.Info("agent reported task not completable", "summary", report.Summary, "blockers", report.Blockers)
		return fmt.Errorf("task not completable: %s (reason: %s)", report.Summary, report.Blockers)
	}

	return fmt.Errorf("validation did not converge after %d attempts", maxAttempts)
}

// parseCompletionReport unmarshals the claude JSON response.
// Returns (nil, nil) when structured_output is missing (signals retry).
// Returns (nil, err) on malformed JSON.
func parseCompletionReport(data []byte) (*completionReport, error) {
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
