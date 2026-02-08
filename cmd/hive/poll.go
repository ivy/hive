package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/ivy/hive/internal/github"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var pollCmd = &cobra.Command{
	Use:   "poll",
	Short: "Find Ready items on the board and dispatch runs",
	Long:  "Polls a GitHub Projects board for items in the Ready column, moves them to In Progress, and dispatches hive run for each.",
	Args:  cobra.NoArgs,
	RunE:  runPoll,
}

func init() {
	rootCmd.AddCommand(pollCmd)
}

func runPoll(cmd *cobra.Command, args []string) error {
	projectID := viper.GetString("github.project-id")
	if projectID == "" {
		return fmt.Errorf("github.project-id not configured (set in .hive.toml)")
	}

	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}
	gh.StatusFieldID = viper.GetString("github.status-field-id")
	gh.InProgressOptionID = viper.GetString("github.in-progress-option-id")

	slog.Info("polling for ready items", "project", projectID)

	items, err := gh.ReadyItems(cmd.Context(), projectID)
	if err != nil {
		return fmt.Errorf("fetch ready items: %w", err)
	}

	if len(items) == 0 {
		slog.Info("no ready items found")
		return nil
	}

	projectNodeID := viper.GetString("github.project-node-id")
	if projectNodeID == "" {
		return fmt.Errorf("github.project-node-id not configured (set in .hive.toml)")
	}

	slog.Info("found ready items", "count", len(items))

	for _, item := range items {
		if item.IsDraft {
			slog.Warn("skipping draft", "title", item.Title)
			continue
		}

		slog.Info("dispatching run", "title", item.Title, "repo", item.Repo, "issue", item.Number)

		// Move to In Progress
		if err := gh.MoveToInProgress(cmd.Context(), projectNodeID, item.ID); err != nil {
			slog.Error("failed to move to In Progress", "error", err, "item", item.Title)
			continue
		}

		// Dispatch hive run in background
		ref := fmt.Sprintf("%s#%d", item.Repo, item.Number)
		hiveCmd := exec.CommandContext(cmd.Context(), os.Args[0], "run", "--board-item-id", item.ID, ref)
		hiveCmd.Stdout = os.Stdout
		hiveCmd.Stderr = os.Stderr
		if err := hiveCmd.Start(); err != nil {
			slog.Error("failed to dispatch run", "error", err, "ref", ref)
			continue
		}

		go func() {
			if err := hiveCmd.Wait(); err != nil {
				slog.Error("run failed", "ref", ref, "error", err)
			}
		}()

		slog.Info("dispatched", "ref", ref, "pid", hiveCmd.Process.Pid)
	}

	return nil
}
