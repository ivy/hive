package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner is a function that creates an *exec.Cmd.
// It exists for testability — specs inject a recording runner.
type CommandRunner func(name string, args ...string) *exec.Cmd

// Client wraps the gh CLI for GitHub operations.
type Client struct {
	runner CommandRunner

	// StatusFieldID is the project field ID for the "Status" field.
	StatusFieldID string

	// InProgressOptionID is the single-select option ID for "In Progress".
	InProgressOptionID string

	// InReviewOptionID is the single-select option ID for "In Review".
	InReviewOptionID string
}

// NewClient creates a Client that uses the gh CLI on PATH.
func NewClient() (*Client, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not found on PATH: %w", err)
	}
	return &Client{runner: exec.Command}, nil
}

// NewClientWithRunner creates a Client with a custom command runner
// for testing.
func NewClientWithRunner(runner CommandRunner) *Client {
	return &Client{runner: runner}
}

// FetchIssue retrieves issue details via gh issue view --json.
func (c *Client) FetchIssue(ctx context.Context, repo string, number int) (*Issue, error) {
	cmd := c.runner("gh", "issue", "view",
		fmt.Sprintf("%d", number),
		"--repo", repo,
		"--json", "number,title,body,state,url,author",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue view: %w", err)
	}
	var issue Issue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, fmt.Errorf("parsing issue JSON: %w", err)
	}
	return &issue, nil
}

// ReadyItems lists items in the "Ready" column of a project board.
func (c *Client) ReadyItems(ctx context.Context, projectID string) ([]BoardItem, error) {
	cmd := c.runner("gh", "project", "item-list", projectID,
		"--owner", "@me",
		"--format", "json",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh project item-list: %w", err)
	}
	var resp projectItemListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parsing project items JSON: %w", err)
	}
	var items []BoardItem
	for _, pi := range resp.Items {
		if !strings.Contains(pi.Status, "Ready") {
			continue
		}
		items = append(items, BoardItem{
			ID:      pi.ID,
			Title:   pi.Title,
			Number:  pi.Content.Number,
			Repo:    pi.Content.Repository,
			Status:  pi.Status,
			IsDraft: pi.Content.Type == "DraftIssue",
			Type:    pi.Content.Type,
		})
	}
	return items, nil
}

// MoveToInProgress moves a board item to the "In Progress" status.
func (c *Client) MoveToInProgress(ctx context.Context, projectID string, itemID string) error {
	return c.moveItem(ctx, projectID, itemID, c.InProgressOptionID)
}

// MoveToInReview moves a board item to the "In Review" status.
func (c *Client) MoveToInReview(ctx context.Context, projectID string, itemID string) error {
	return c.moveItem(ctx, projectID, itemID, c.InReviewOptionID)
}

func (c *Client) moveItem(_ context.Context, projectID, itemID, optionID string) error {
	cmd := c.runner("gh", "project", "item-edit",
		"--project-id", projectID,
		"--id", itemID,
		"--field-id", c.StatusFieldID,
		"--single-select-option-id", optionID,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh project item-edit: %s: %w", string(out), err)
	}
	return nil
}

// PushBranch pushes a branch to the origin remote.
func (c *Client) PushBranch(ctx context.Context, repoPath string, branch string) error {
	cmd := c.runner("git", "-C", repoPath, "push", "origin", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %s: %w", string(out), err)
	}
	return nil
}

// CreatePR opens a pull request via gh pr create, then fetches its
// details with gh pr view --json.
func (c *Client) CreatePR(ctx context.Context, repo string, branch string, title string, body string) (*PR, error) {
	cmd := c.runner("gh", "pr", "create",
		"--repo", repo,
		"--head", branch,
		"--title", title,
		"--body", body,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// gh pr create outputs the PR URL on success. Use gh pr view to get
	// structured JSON.
	prURL := strings.TrimSpace(string(out))
	cmd = c.runner("gh", "pr", "view", prURL,
		"--json", "number,title,url,headRefName",
	)
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}
	var pr PR
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, fmt.Errorf("parsing PR JSON: %w", err)
	}
	return &pr, nil
}
