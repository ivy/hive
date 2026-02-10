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

IMPORTANT: Every Ready item should produce code changes and commits. If the issue is purely a design proposal or discussion (not an implementation task), set completed to false and explain in the blockers field.`

const maxCommitRetries = 3

const commitNudge = `You left uncommitted changes in the worktree. Please either:

- Use /commit to commit them (split into logical commits if needed)
- Add generated/temporary files to .gitignore
- Explain why the changes should not be committed

Then exit.`

// completionReportSchema is the JSON schema for the completion report.
const completionReportSchema = `{"type":"object","properties":{"completed":{"type":"boolean","description":"Whether the task was fully implemented and committed"},"summary":{"type":"string","description":"Brief description of what was done or attempted"},"blockers":{"type":"string","description":"What prevented completion, if anything"}},"required":["completed","summary"]}`

// completionValidationPrompt asks the agent to report on completion status.
const completionValidationPrompt = `Please provide a completion report for this task using the structured output tool. Set "completed" to true if you fully implemented the requested changes, or false if there were blockers.`

// noCommitsNudge is sent when the agent claims completion but produced no commits.
const noCommitsNudge = `You reported the task as completed, but there are no commits on the branch. The expectation is that Ready items produce code changes. Please implement the requested changes and commit your work. If the issue is not implementable (e.g. it's a design discussion), set completed to false and explain in blockers.`

// CompletionReport holds the agent's structured completion status.
type CompletionReport struct {
	Completed bool   `json:"completed"`
	Summary   string `json:"summary"`
	Blockers  string `json:"blockers,omitempty"`
}

// claudeValidationResponse is the top-level JSON from validation runs.
type claudeValidationResponse struct {
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

	// Validate that the agent produced commits (happy path check first)
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

// validateCompletion asks the agent for a structured completion report when
// zero commits were produced. It retries with a nudge if the agent claims
// completion despite having no commits.
func validateCompletion(ctx context.Context, j jail.Jail, ws *workspace.Workspace, model string) error {
	for attempt := 1; attempt <= maxCommitRetries; attempt++ {
		report, err := getCompletionReport(ctx, j, ws, model, attempt == 1)
		if err != nil {
			slog.Warn("failed to get completion report", "attempt", attempt, "error", err)
			if attempt == maxCommitRetries {
				return fmt.Errorf("completion validation failed after %d attempts: %w", maxCommitRetries, err)
			}
			continue
		}

		if report.Completed {
			// Agent claims completion but has no commits — nudge to actually implement
			slog.Warn("agent claims completion but produced no commits, nudging", "attempt", attempt)
			if err := nudgeForImplementation(ctx, j, ws, model); err != nil {
				slog.Error("implementation nudge failed", "attempt", attempt, "error", err)
				if attempt == maxCommitRetries {
					return fmt.Errorf("agent claims completion but produced no commits after %d nudges", maxCommitRetries)
				}
			}
			// Check if the nudge produced commits
			hasCommits, err := workspace.HasNewCommits(ctx, ws)
			if err == nil && hasCommits {
				return nil // Success!
			}
			continue
		}

		// Agent reported blockers
		return fmt.Errorf("agent reported blockers: %s (summary: %s)", report.Blockers, report.Summary)
	}

	return fmt.Errorf("completion validation failed after %d attempts", maxCommitRetries)
}

// getCompletionReport runs a validation command to get structured completion status.
func getCompletionReport(ctx context.Context, j jail.Jail, ws *workspace.Workspace, model string, firstAttempt bool) (*CompletionReport, error) {
	prompt := completionValidationPrompt
	if !firstAttempt {
		prompt = "Your previous response did not use the structured output tool. " + completionValidationPrompt
	}

	cmd := []string{
		"claude", "-p",
		"--output-format", "json",
		"--json-schema", completionReportSchema,
		"--tools", "Glob,Read,Bash",
		"--allowedTools", "Glob,Read,Bash(git log *),Bash(git diff *),Bash(git status *)",
		"--model", model,
		"--resume", ws.SessionID,
		prompt,
	}

	out, err := j.RunCapture(ctx, jail.RunOpts{
		Workspace: ws,
		Command:   cmd,
	})
	if err != nil {
		return nil, fmt.Errorf("claude invocation: %w", err)
	}

	var resp claudeValidationResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.StructuredOutput == nil {
		return nil, fmt.Errorf("structured output missing")
	}

	if resp.StructuredOutput.Summary == "" {
		return nil, fmt.Errorf("summary field missing")
	}

	return resp.StructuredOutput, nil
}

// nudgeForImplementation resumes the agent with a message to actually implement the changes.
func nudgeForImplementation(ctx context.Context, j jail.Jail, ws *workspace.Workspace, model string) error {
	cmd := []string{
		"claude", "-p",
		"--dangerously-skip-permissions",
		"--resume", ws.SessionID,
		"--model", model,
		"--output-format", "json",
		noCommitsNudge,
	}

	return j.Run(ctx, jail.RunOpts{
		Workspace: ws,
		Command:   cmd,
	})
}
