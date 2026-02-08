package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [workspace-id]",
	Short: "Tear down workspace(s)",
	Long:  "Removes a workspace by ID, or all workspaces with --all.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCleanup,
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "remove all workspaces")
	rootCmd.AddCommand(cleanupCmd)
}

func runCleanup(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")

	if all {
		return cleanupAll(cmd)
	}

	if len(args) == 0 {
		return fmt.Errorf("specify a workspace ID or use --all")
	}

	return cleanupOne(cmd, args[0])
}

func cleanupAll(cmd *cobra.Command) error {
	workspaces, err := workspace.ListAll(cmd.Context())
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No workspaces to clean up.")
		return nil
	}

	var errs []error
	for _, ws := range workspaces {
		slog.Info("removing workspace", "path", ws.Path)
		if err := workspace.Remove(cmd.Context(), ws); err != nil {
			slog.Error("failed to remove workspace", "path", ws.Path, "error", err)
			errs = append(errs, err)
			continue
		}
		fmt.Printf("Removed: %s\n", ws.Path)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d workspace(s) failed to remove", len(errs))
	}
	return nil
}

func cleanupOne(cmd *cobra.Command, wsID string) error {
	// Resolve workspace path
	wsPath := wsID
	if !filepath.IsAbs(wsPath) {
		workspaces, err := workspace.ListAll(cmd.Context())
		if err != nil {
			return fmt.Errorf("list workspaces: %w", err)
		}
		for _, ws := range workspaces {
			if strings.Contains(filepath.Base(ws.Path), wsID) {
				wsPath = ws.Path
				break
			}
		}
		if !filepath.IsAbs(wsPath) {
			return fmt.Errorf("workspace not found: %s", wsID)
		}
	}

	ws, err := workspace.Load(cmd.Context(), wsPath)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	slog.Info("removing workspace", "path", ws.Path)
	if err := workspace.Remove(cmd.Context(), ws); err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}

	fmt.Printf("Removed: %s\n", ws.Path)
	return nil
}
