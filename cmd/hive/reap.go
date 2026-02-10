package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ivy/hive/internal/claim"
	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/source"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var reapCmd = &cobra.Command{
	Use:   "reap",
	Short: "Clean up finished sessions and recover stuck items",
	Long:  "Scans sessions for completed or stale work. Removes expired workspaces and sessions. Recovers stuck items by marking them failed and releasing their claims.",
	Args:  cobra.NoArgs,
	RunE:  runReap,
}

func init() {
	reapCmd.Flags().Duration("published-retention", 24*time.Hour, "retention for published sessions")
	reapCmd.Flags().Duration("failed-retention", 72*time.Hour, "retention for failed sessions")
	viper.BindPFlag("reap.published-retention", reapCmd.Flags().Lookup("published-retention"))
	viper.BindPFlag("reap.failed-retention", reapCmd.Flags().Lookup("failed-retention"))
	rootCmd.AddCommand(reapCmd)
}

func runReap(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	dataDir := session.DataDir()

	sessions, err := session.ListAll(dataDir)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		slog.Info("no sessions found")
		return nil
	}

	// Build source for releasing stale items back to the board.
	src, srcErr := buildSource()
	if srcErr != nil {
		slog.Warn("could not build source — stale items won't be released on the board", "error", srcErr)
	}

	publishedRetention := viper.GetDuration("reap.published-retention")
	failedRetention := viper.GetDuration("reap.failed-retention")
	now := time.Now().UTC()

	for _, sess := range sessions {
		switch sess.Status {
		case session.StatusPublished:
			if now.Sub(sess.CreatedAt) > publishedRetention {
				reapSession(ctx, dataDir, sess)
			}
		case session.StatusFailed:
			if now.Sub(sess.CreatedAt) > failedRetention {
				reapSession(ctx, dataDir, sess)
			}
		default:
			// Non-terminal — check if the systemd unit is still active.
			if !isUnitActive(ctx, sess.ID) {
				slog.Info("stale session detected", "id", sess.ID, "ref", sess.Ref, "status", sess.Status)
				if err := session.SetStatus(dataDir, sess.ID, session.StatusFailed); err != nil {
					slog.Error("failed to mark session as failed", "id", sess.ID, "error", err)
				}
				releaseOnSource(ctx, src, sess)
				if err := claim.Release(dataDir, sess.Ref); err != nil {
					slog.Error("failed to release claim", "ref", sess.Ref, "error", err)
				}
			}
		}
	}

	return nil
}

// reapSession removes the workspace, session file, and claim for a terminal session.
func reapSession(ctx context.Context, dataDir string, sess *session.Session) {
	slog.Info("reaping session", "id", sess.ID, "ref", sess.Ref, "status", sess.Status)

	// Remove workspace if it exists.
	wsPath := filepath.Join(workspace.BaseDir(), sess.ID)
	ws, err := workspace.Load(ctx, wsPath)
	if err == nil {
		if err := workspace.Remove(ctx, ws); err != nil {
			slog.Error("failed to remove workspace", "id", sess.ID, "error", err)
		}
	}

	// Remove session file.
	if err := session.Remove(dataDir, sess.ID); err != nil {
		slog.Error("failed to remove session", "id", sess.ID, "error", err)
	}

	// Release claim (idempotent).
	if err := claim.Release(dataDir, sess.Ref); err != nil {
		slog.Error("failed to release claim", "ref", sess.Ref, "error", err)
	}
}

// releaseOnSource attempts to release the item on the source (best effort).
func releaseOnSource(ctx context.Context, src source.Source, sess *session.Session) {
	if src == nil {
		return
	}

	// Register source metadata so the adapter can look up the board item.
	type itemRegistrar interface {
		RegisterItem(ref, boardItemID string)
	}
	if reg, ok := src.(itemRegistrar); ok {
		if boardItemID, exists := sess.SourceMetadata["board_item_id"]; exists {
			reg.RegisterItem(sess.Ref, boardItemID)
		}
	}

	if err := src.Release(ctx, sess.Ref); err != nil {
		slog.Warn("failed to release item on source", "ref", sess.Ref, "error", err)
	}
}

// isUnitActive checks if a hive-run@{uuid} systemd unit is active.
func isUnitActive(ctx context.Context, id string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-active",
		fmt.Sprintf("hive-run@%s.service", id))
	err := cmd.Run()
	return err == nil // exit 0 = active
}
