package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivy/hive/internal/claim"
	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/source"
	"github.com/ivy/hive/internal/source/ghprojects"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var pollCmd = &cobra.Command{
	Use:   "poll",
	Short: "Find Ready items on the board and dispatch runs",
	Long:  "Polls a configured source for ready work items, claims them locally, and dispatches systemd units. With --interval, runs as a long-lived daemon.",
	Args:  cobra.NoArgs,
	RunE:  runPoll,
}

func init() {
	pollCmd.Flags().Duration("interval", 0, "poll interval (e.g. 5m); if unset, run once and exit")
	viper.BindPFlag("poll.interval", pollCmd.Flags().Lookup("interval"))
	rootCmd.AddCommand(pollCmd)
}

func runPoll(cmd *cobra.Command, args []string) error {
	interval := viper.GetDuration("poll.interval")
	ctx := cmd.Context()

	// Single-shot mode: run once and exit.
	if interval <= 0 {
		return pollOnce(ctx)
	}

	// Daemon mode: poll immediately, then on each tick.
	slog.Info("starting poll loop", "interval", interval)

	if err := pollOnce(ctx); err != nil {
		slog.Error("poll cycle failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down poll loop")
			return nil
		case <-ticker.C:
			if err := pollOnce(ctx); err != nil {
				slog.Error("poll cycle failed", "error", err)
			}
		}
	}
}

// pollOnce runs a single poll cycle: query the source for ready items,
// claim each locally, create a session, start a systemd unit, and mark
// the item as taken on the source.
func pollOnce(ctx context.Context) error {
	src, err := buildSource()
	if err != nil {
		return fmt.Errorf("building source: %w", err)
	}

	slog.Info("polling for ready items")

	items, err := src.Ready(ctx)
	if err != nil {
		return fmt.Errorf("fetching ready items: %w", err)
	}

	if len(items) == 0 {
		slog.Info("no ready items found")
		return nil
	}

	slog.Info("found ready items", "count", len(items))

	maxConcurrent := viper.GetInt("poll.max-concurrent")
	dataDir := session.DataDir()
	instance := viper.GetString("poll.instance")
	if instance == "" {
		instance = "default"
	}

	for _, item := range items {
		if err := dispatchItem(ctx, src, item, dataDir, instance, maxConcurrent); err != nil {
			slog.Error("dispatch failed", "error", err, "ref", item.Ref, "title", item.Title)
		}
	}

	return nil
}

// dispatchItem handles claiming, session creation, unit start, and source
// notification for a single work item. On failure, it cleans up the claim
// and session.
func dispatchItem(ctx context.Context, src source.Source, item source.WorkItem, dataDir, instance string, maxConcurrent int) error {
	// Check max-concurrent limit before dispatching.
	if maxConcurrent > 0 {
		active, err := countActiveRuns(ctx)
		if err != nil {
			return fmt.Errorf("checking concurrency: %w", err)
		}
		if active >= maxConcurrent {
			slog.Info("at max concurrency, skipping remaining items",
				"active", active, "max", maxConcurrent)
			return nil
		}
	}

	id := uuid.New().String()

	// Double gate: claim locally to prevent duplicate dispatch.
	claimed, err := claim.TryClaim(dataDir, item.Ref, id)
	if err != nil {
		return fmt.Errorf("claiming %s: %w", item.Ref, err)
	}
	if !claimed {
		slog.Info("already claimed, skipping", "ref", item.Ref)
		return nil
	}

	// Write session metadata.
	sess := &session.Session{
		ID:             id,
		Ref:            item.Ref,
		Repo:           item.Repo,
		Title:          item.Title,
		Prompt:         item.Prompt,
		SourceMetadata: item.Metadata,
		Status:         session.StatusDispatching,
		CreatedAt:      time.Now().UTC(),
		PollInstance:   instance,
	}
	if err := session.Create(dataDir, sess); err != nil {
		_ = claim.Release(dataDir, item.Ref)
		return fmt.Errorf("creating session for %s: %w", item.Ref, err)
	}

	slog.Info("dispatching run", "ref", item.Ref, "title", item.Title, "uuid", id)

	// Start the systemd unit.
	if err := startRunUnit(ctx, id); err != nil {
		_ = claim.Release(dataDir, item.Ref)
		_ = session.Remove(dataDir, id)
		return fmt.Errorf("starting unit for %s: %w", item.Ref, err)
	}

	// Mark as taken on the source (e.g., move to In Progress).
	if err := src.Take(ctx, item.Ref); err != nil {
		slog.Error("failed to mark item as taken on source (unit already started)",
			"error", err, "ref", item.Ref)
	}

	slog.Info("dispatched", "ref", item.Ref, "uuid", id)
	return nil
}

// buildSource creates a Source from viper configuration.
func buildSource() (source.Source, error) {
	projectID := viper.GetString("github.project-id")
	if projectID == "" {
		return nil, fmt.Errorf("github.project-id not configured")
	}

	projectNodeID := viper.GetString("github.project-node-id")
	if projectNodeID == "" {
		return nil, fmt.Errorf("github.project-node-id not configured")
	}

	allowedUsers := viper.GetStringSlice("security.allowed-users")
	if len(allowedUsers) == 0 {
		return nil, fmt.Errorf("security.allowed-users not configured — refusing to run (fail-closed)")
	}

	gh, err := github.NewClient()
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}
	gh.ReadyStatus = viper.GetString("github.ready-status")
	gh.StatusFieldID = viper.GetString("github.status-field-id")
	gh.InProgressOptionID = viper.GetString("github.in-progress-option-id")
	gh.InReviewOptionID = viper.GetString("github.in-review-option-id")
	gh.ReadyOptionID = viper.GetString("github.ready-option-id")

	adapter := ghprojects.NewAdapter(ghprojects.Config{
		Client:        gh,
		ProjectNumber: projectID,
		ProjectNodeID: projectNodeID,
		AllowedUsers:  allowedUsers,
	})

	return adapter, nil
}

// countActiveRuns counts active hive-run@* systemd user units.
func countActiveRuns(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "list-units",
		"hive-run@*", "--state=activating,active", "--no-legend")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("counting active runs: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0, nil
	}
	return len(lines), nil
}

// startRunUnit starts a hive-run@{uuid} systemd user unit.
func startRunUnit(ctx context.Context, uuid string) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "start",
		fmt.Sprintf("hive-run@%s.service", uuid))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start hive-run@%s: %s: %w", uuid, string(out), err)
	}
	return nil
}
