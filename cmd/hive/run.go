package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <owner/repo#issue>",
	Short: "Orchestrate prepare → exec → publish",
	Long:  "Full pipeline: creates a workspace, runs the agent, pushes a branch, and opens a PR.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRun,
}

func init() {
	runCmd.Flags().Bool("no-publish", false, "skip the publish step")
	runCmd.Flags().String("model", "sonnet", "Claude model to use")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	noPublish, _ := cmd.Flags().GetBool("no-publish")
	model, _ := cmd.Flags().GetString("model")

	slog.Info("running full pipeline", "ref", args[0])

	// Phase 1: Prepare
	if err := runPrepare(cmd, args); err != nil {
		return fmt.Errorf("prepare: %w", err)
	}

	// Find the workspace that was just created
	ref := args[0]
	repo, issueNumber, err := parseIssueRef(ref)
	if err != nil {
		return err
	}

	// Load the most recently created workspace for this issue
	ws, err := findLatestWorkspace(cmd.Context(), repo, issueNumber)
	if err != nil {
		return fmt.Errorf("find workspace: %w", err)
	}

	// Phase 2: Exec
	execArgs := []string{ws.Path}
	cmd.Flags().Set("model", model)
	if err := runExec(cmd, execArgs); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	// Phase 3: Publish (unless --no-publish)
	if noPublish {
		slog.Info("skipping publish (--no-publish)")
		fmt.Printf("Workspace ready for review: %s\n", ws.Path)
		return nil
	}

	if err := runPublish(cmd, []string{ws.Path}); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	return nil
}

// findLatestWorkspace scans for the most recent workspace matching the given repo and issue.
func findLatestWorkspace(ctx context.Context, repo string, issueNumber int) (*workspace.Workspace, error) {
	all, err := workspace.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	var latest *workspace.Workspace
	for _, ws := range all {
		if ws.Repo == repo && ws.IssueNumber == issueNumber {
			if latest == nil || ws.Path > latest.Path {
				latest = ws
			}
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no workspace found for %s#%d", repo, issueNumber)
	}
	return latest, nil
}
