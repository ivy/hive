package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/prdraft"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// publishSourceMeta, when non-nil, provides source metadata (e.g. board_item_id)
// from a session. Set by runFromSession before calling runPublish. Falls back to
// reading .hive/board-item-id when nil.
var publishSourceMeta map[string]string

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

	// Safety check: verify commits exist before attempting to push/PR
	hasCommits, err := workspace.HasNewCommits(cmd.Context(), ws)
	if err != nil {
		slog.Warn("failed to check for commits", "error", err)
	} else if !hasCommits {
		slog.Warn("no commits to publish, skipping")
		return nil
	}

	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	gh.StatusFieldID = viper.GetString("github.status-field-id")
	gh.InProgressOptionID = viper.GetString("github.in-progress-option-id")
	gh.InReviewOptionID = viper.GetString("github.in-review-option-id")

	// Commit uncommitted agent work before pushing
	hasChanges, err := workspace.HasUncommittedChanges(cmd.Context(), ws)
	if err != nil {
		return fmt.Errorf("check uncommitted changes: %w", err)
	}
	if hasChanges {
		slog.Warn("agent left uncommitted changes after retries, auto-committing")
		msg := fmt.Sprintf("feat: implement #%d\n\nAutomated commit by hive — agent exited without committing.", ws.IssueNumber)
		if err := workspace.CommitAll(cmd.Context(), ws, msg); err != nil {
			return fmt.Errorf("auto-commit: %w", err)
		}
	}

	// Push branch
	if err := gh.PushBranch(cmd.Context(), ws.RepoPath, ws.Branch); err != nil {
		return fmt.Errorf("push branch: %w", err)
	}
	slog.Info("pushed branch", "branch", ws.Branch)

	// Draft PR content using Claude
	backend := viper.GetString("jail.backend")
	if backend == "" {
		backend = "systemd-run"
	}
	j, err := jail.New(backend)
	if err != nil {
		return fmt.Errorf("create jail: %w", err)
	}

	drafter := prdraft.New(j)
	prContent, draftErr := drafter.Draft(cmd.Context(), prdraft.DraftParams{
		Workspace: ws,
		Model:     "sonnet",
		Resume:    true,
	})
	if draftErr != nil {
		slog.Warn("PR draft failed, using fallback", "error", draftErr)
		prContent = prdraft.Fallback(ws.IssueNumber)
	}

	pr, err := gh.CreatePR(cmd.Context(), ws.Repo, ws.Branch, prContent.Title, prContent.Body)
	if err != nil {
		return fmt.Errorf("create PR: %w", err)
	}
	slog.Info("created PR", "url", pr.URL)

	// Move board item to In Review
	projectNodeID := viper.GetString("github.project-node-id")
	if projectNodeID != "" {
		itemID := boardItemIDFromMeta()
		if itemID == "" {
			// Fall back to .hive/board-item-id file
			itemIDBytes, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "board-item-id"))
			if err != nil {
				slog.Warn("no board item ID found, skipping board update")
			} else {
				itemID = strings.TrimSpace(string(itemIDBytes))
			}
		}
		if itemID != "" {
			if err := gh.MoveToInReview(cmd.Context(), projectNodeID, itemID); err != nil {
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

// boardItemIDFromMeta returns the board_item_id from publishSourceMeta if set.
func boardItemIDFromMeta() string {
	if publishSourceMeta == nil {
		return ""
	}
	return publishSourceMeta["board_item_id"]
}
