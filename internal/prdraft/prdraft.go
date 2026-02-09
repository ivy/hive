// Package prdraft uses Claude Code headless mode to draft PR content
// by examining branch diffs and following repo PR templates.
package prdraft

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"text/template"

	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/workspace"
)

//go:embed prompt.tmpl
var promptTemplate string

// jsonSchema is the structured output schema for PR content.
const jsonSchema = `{"type":"object","properties":{"title":{"type":"string","description":"Conventional commit style PR title ending with a mood emoji"},"body":{"type":"string","description":"PR body in markdown"}},"required":["title","body"]}`

// tools restricts Claude to read-only operations.
const tools = "Glob,Read,Bash"

// allowedTools scopes Bash to git read commands only.
const allowedTools = `Glob,Read,Bash(git diff *),Bash(git log *),Bash(git show *),Bash(git status *)`

// nudgeMessage is sent on retry when Claude doesn't use the structured output tool.
const nudgeMessage = "Your previous response did not use the structured output tool. Please provide your PR title and body by calling the structured output tool with the \"title\" and \"body\" fields."

// maxAttempts is the number of times to try getting structured output.
const maxAttempts = 3

// PRContent holds the drafted PR title and body.
type PRContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// DraftParams configures a PR draft operation.
type DraftParams struct {
	// Workspace is the workspace containing the branch to draft for.
	Workspace *workspace.Workspace

	// Model is the Claude model to use (e.g. "sonnet").
	Model string

	// Resume resumes the agent's existing session for richer context.
	Resume bool
}

// claudeResponse is the top-level JSON from `claude -p --output-format json`.
type claudeResponse struct {
	SessionID        string     `json:"session_id"`
	StructuredOutput *PRContent `json:"structured_output"`
}

// Drafter drafts PR content using Claude Code headless mode.
type Drafter struct {
	jail jail.Jail
}

// New creates a Drafter that runs Claude inside the given jail.
func New(j jail.Jail) *Drafter {
	return &Drafter{jail: j}
}

// Draft runs Claude Code to produce PR content from branch diffs.
// It retries up to 3 times if Claude doesn't use the structured output tool.
func (d *Drafter) Draft(ctx context.Context, params DraftParams) (*PRContent, error) {
	prompt, err := renderPrompt(params.Workspace)
	if err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	var sessionID string
	if params.Resume {
		sessionID = params.Workspace.SessionID
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var cmdArgs []string
		if attempt == 1 {
			cmdArgs = buildCommand(prompt, params.Model, sessionID)
		} else {
			cmdArgs = buildCommand(nudgeMessage, params.Model, sessionID)
		}

		slog.Debug("drafting PR", "attempt", attempt, "resume", sessionID != "")

		out, err := d.jail.RunCapture(ctx, jail.RunOpts{
			Workspace: params.Workspace,
			Command:   cmdArgs,
		})
		if err != nil {
			return nil, fmt.Errorf("claude invocation (attempt %d): %w", attempt, err)
		}

		content, respSessionID, parseErr := parseOutput(out)
		if respSessionID != "" {
			sessionID = respSessionID
		}

		if parseErr != nil {
			slog.Warn("failed to parse claude output", "attempt", attempt, "error", parseErr)
			if attempt == maxAttempts {
				return nil, fmt.Errorf("structured output not received after %d attempts: %w", maxAttempts, parseErr)
			}
			continue
		}

		if content == nil {
			slog.Warn("structured output missing, will retry", "attempt", attempt)
			if attempt == maxAttempts {
				return nil, fmt.Errorf("structured output not received after %d attempts", maxAttempts)
			}
			continue
		}

		appendFooter(content, params.Workspace.IssueNumber)
		return content, nil
	}

	// unreachable — loop returns or errors on maxAttempts
	return nil, fmt.Errorf("structured output not received after %d attempts", maxAttempts)
}

// Fallback returns mechanical PR content for when drafting fails.
func Fallback(issueNumber int) *PRContent {
	return &PRContent{
		Title: fmt.Sprintf("hive: implement #%d", issueNumber),
		Body:  fmt.Sprintf("Automated by [hive](https://github.com/ivy/hive).\n\nCloses #%d", issueNumber),
	}
}

// renderPrompt renders the prompt template with workspace data.
func renderPrompt(ws *workspace.Workspace) (string, error) {
	tmpl, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	data := struct {
		IssueNumber int
		Repo        string
	}{
		IssueNumber: ws.IssueNumber,
		Repo:        ws.Repo,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// buildCommand assembles the claude CLI arguments.
func buildCommand(prompt, model, sessionID string) []string {
	args := []string{
		"claude", "-p",
		"--output-format", "json",
		"--json-schema", jsonSchema,
		"--tools", tools,
		"--allowedTools", allowedTools,
		"--model", model,
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, prompt)
	return args
}

// parseOutput unmarshals the claude JSON response and validates fields.
// Returns (nil, sessionID, nil) when structured_output is missing (signals retry).
// Returns (nil, "", err) on malformed JSON.
func parseOutput(data []byte) (*PRContent, string, error) {
	var resp claudeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "", fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.StructuredOutput == nil {
		return nil, resp.SessionID, nil
	}

	// Validate required fields — json.Unmarshal doesn't enforce JSON Schema.
	if resp.StructuredOutput.Title == "" || resp.StructuredOutput.Body == "" {
		return nil, resp.SessionID, nil
	}

	return resp.StructuredOutput, resp.SessionID, nil
}

// appendFooter adds the Hive footer with the Closes directive.
func appendFooter(content *PRContent, issueNumber int) {
	content.Body += fmt.Sprintf("\n\n---\nGenerated with [Hive](https://github.com/ivy/hive) | Closes #%d", issueNumber)
}
