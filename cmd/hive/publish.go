package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var publishCmd = &cobra.Command{
	Use:   "publish <workspace-path>",
	Short: "Push branch, open PR, update board",
	Long:  "Loads a workspace, pushes the branch, creates a pull request, and moves the board item to In Review.",
	Args:  cobra.ExactArgs(1),
	RunE:  runPublish,
}

func init() {
	rootCmd.AddCommand(publishCmd)
}

func runPublish(cmd *cobra.Command, args []string) error {
	wsPath := args[0]

	ws, err := workspace.Load(cmd.Context(), wsPath)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	slog.Info("publishing workspace", "path", ws.Path, "branch", ws.Branch)

	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	gh.StatusFieldID = viper.GetString("github.status-field-id")
	gh.InProgressOptionID = viper.GetString("github.in-progress-option-id")
	gh.InReviewOptionID = viper.GetString("github.in-review-option-id")

	// Push branch
	if err := gh.PushBranch(cmd.Context(), ws.RepoPath, ws.Branch); err != nil {
		return fmt.Errorf("push branch: %w", err)
	}
	slog.Info("pushed branch", "branch", ws.Branch)

	// Create PR
	title := fmt.Sprintf("hive: implement #%d", ws.IssueNumber)
	body := fmt.Sprintf("Automated by [hive](https://github.com/ivy/hive).\n\nCloses #%d", ws.IssueNumber)
	pr, err := gh.CreatePR(cmd.Context(), ws.Repo, ws.Branch, title, body)
	if err != nil {
		return fmt.Errorf("create PR: %w", err)
	}
	slog.Info("created PR", "url", pr.URL)

	// Move board item to In Review
	projectID := viper.GetString("github.project-id")
	if projectID != "" {
		itemIDBytes, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "board-item-id"))
		if err != nil {
			slog.Warn("no board item ID found, skipping board update")
		} else {
			itemID := strings.TrimSpace(string(itemIDBytes))
			if err := gh.MoveToInReview(cmd.Context(), projectID, itemID); err != nil {
				slog.Warn("could not move board item to In Review", "error", err)
			}
		}
	}

	// Set status to published
	if err := workspace.SetStatus(ws, workspace.StatusPublished); err != nil {
		return fmt.Errorf("set status: %w", err)
	}

	fmt.Printf("Published: %s\n", pr.URL)
	return nil
}
