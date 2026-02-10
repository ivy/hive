package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/ivy/hive/internal/authz"
	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare <owner/repo#issue>",
	Short: "Create workspace from issue",
	Long:  "Creates a git worktree, fetches issue data, writes metadata, and sets status to prepared.",
	Args:  cobra.ExactArgs(1),
	RunE:  runPrepare,
}

func init() {
	rootCmd.AddCommand(prepareCmd)
}

// parseIssueRef parses "owner/repo#123" into repo and issue number.
func parseIssueRef(ref string) (repo string, issueNumber int, err error) {
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid issue reference %q: expected owner/repo#number", ref)
	}
	repo = parts[0]
	if !strings.Contains(repo, "/") {
		return "", 0, fmt.Errorf("invalid repo %q: expected owner/repo", repo)
	}
	issueNumber, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid issue number %q: %w", parts[1], err)
	}
	return repo, issueNumber, nil
}

// repoPath resolves a GitHub repo to a local path by convention:
// ~/src/github.com/<owner>/<repo>/
func repoPath(repo string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "src", "github.com", repo)
}

func runPrepare(cmd *cobra.Command, args []string) error {
	repo, issueNumber, err := parseIssueRef(args[0])
	if err != nil {
		return err
	}

	localPath := repoPath(repo)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		return fmt.Errorf("repo not found on disk: %s (expected at %s)", repo, localPath)
	}

	slog.Info("preparing workspace", "repo", repo, "issue", issueNumber)

	// Fetch issue data from GitHub
	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	issue, err := gh.FetchIssue(cmd.Context(), repo, issueNumber)
	if err != nil {
		return fmt.Errorf("fetch issue: %w", err)
	}

	slog.Info("fetched issue", "title", issue.Title)

	// Authz: check issue author against allowed-users (defense in depth)
	allowedUsers := viper.GetStringSlice("security.allowed-users")
	if len(allowedUsers) == 0 {
		return fmt.Errorf("security.allowed-users not configured (set in .hive.toml) — refusing to run (fail-closed)")
	}
	if !authz.IsAllowed(issue.Author.Login, allowedUsers) {
		return fmt.Errorf("author %q not in allowed-users — refusing to prepare workspace", issue.Author.Login)
	}

	// Create workspace (worktree + .hive/ metadata)
	id := uuid.New().String()
	ws, err := workspace.Create(cmd.Context(), localPath, repo, issueNumber, id)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	slog.Info("created workspace", "path", ws.Path, "branch", ws.Branch)

	// Write issue data
	issueJSON, err := json.Marshal(issue)
	if err != nil {
		return fmt.Errorf("marshal issue: %w", err)
	}
	if err := workspace.WriteIssueData(ws, issueJSON); err != nil {
		return fmt.Errorf("write issue data: %w", err)
	}

	// Write prompt from issue body
	if err := workspace.WritePrompt(ws, issue.Body); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}

	fmt.Printf("Workspace ready: %s\n", ws.Path)
	fmt.Printf("Branch: %s\n", ws.Branch)
	fmt.Printf("Issue: %s\n", issue.Title)
	return nil
}

// prepareFromSession creates a workspace from session data without fetching
// from GitHub. Used by the UUID path in runFromSession — the session already
// has the prompt and title from poll's earlier fetch.
func prepareFromSession(cmd *cobra.Command, sess *session.Session, repo string, issueNumber int) (*workspace.Workspace, error) {
	localPath := repoPath(repo)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("repo not found on disk: %s (expected at %s)", repo, localPath)
	}

	slog.Info("preparing workspace from session", "repo", repo, "issue", issueNumber, "uuid", sess.ID)

	ws, err := workspace.Create(cmd.Context(), localPath, repo, issueNumber, sess.ID)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	slog.Info("created workspace", "path", ws.Path, "branch", ws.Branch)

	// Write prompt from session data
	if err := workspace.WritePrompt(ws, sess.Prompt); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	fmt.Printf("Workspace ready: %s\n", ws.Path)
	fmt.Printf("Branch: %s\n", ws.Branch)
	fmt.Printf("Session: %s\n", sess.ID)
	return ws, nil
}
