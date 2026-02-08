package github

import (
	"context"
	"fmt"
	"os/exec"
)

// CommandRunner is a function that creates an *exec.Cmd.
// It exists for testability — specs inject a recording runner.
type CommandRunner func(name string, args ...string) *exec.Cmd

// Client wraps the gh CLI for GitHub operations.
type Client struct {
	runner CommandRunner
}

// NewClient creates a Client that uses the gh CLI on PATH.
func NewClient() (*Client, error) {
	return nil, fmt.Errorf("not implemented")
}

// NewClientWithRunner creates a Client with a custom command runner
// for testing.
func NewClientWithRunner(runner CommandRunner) *Client {
	return &Client{runner: runner}
}

// FetchIssue retrieves issue details via gh issue view --json.
func (c *Client) FetchIssue(ctx context.Context, repo string, number int) (*Issue, error) {
	_ = ctx
	_ = repo
	_ = number
	return nil, fmt.Errorf("not implemented")
}

// ReadyItems lists items in the "Ready" column of a project board.
func (c *Client) ReadyItems(ctx context.Context, projectID string) ([]BoardItem, error) {
	_ = ctx
	_ = projectID
	return nil, fmt.Errorf("not implemented")
}

// MoveToInProgress moves a board item to the "In Progress" status.
func (c *Client) MoveToInProgress(ctx context.Context, projectID string, itemID string) error {
	_ = ctx
	_ = projectID
	_ = itemID
	return fmt.Errorf("not implemented")
}

// MoveToInReview moves a board item to the "In Review" status.
func (c *Client) MoveToInReview(ctx context.Context, projectID string, itemID string) error {
	_ = ctx
	_ = projectID
	_ = itemID
	return fmt.Errorf("not implemented")
}

// PushBranch pushes a branch to the origin remote.
func (c *Client) PushBranch(ctx context.Context, repoPath string, branch string) error {
	_ = ctx
	_ = repoPath
	_ = branch
	return fmt.Errorf("not implemented")
}

// CreatePR opens a pull request via gh pr create.
func (c *Client) CreatePR(ctx context.Context, repo string, branch string, title string, body string) (*PR, error) {
	_ = ctx
	_ = repo
	_ = branch
	_ = title
	_ = body
	return nil, fmt.Errorf("not implemented")
}
