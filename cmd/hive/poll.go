package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/ivy/hive/internal/authz"
	"github.com/ivy/hive/internal/github"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var pollCmd = &cobra.Command{
	Use:   "poll",
	Short: "Find Ready items on the board and dispatch runs",
	Long:  "Polls a GitHub Projects board for items in the Ready column, moves them to In Progress, and dispatches hive run for each. With --interval, runs as a long-lived daemon.",
	Args:  cobra.NoArgs,
	RunE:  runPoll,
}

// runningProcesses tracks PIDs of dispatched hive run processes.
// Access must be serialized via the mutex.
var (
	runningProcesses   = make(map[int]struct{})
	runningProcessesMu sync.Mutex
)

func init() {
	pollCmd.Flags().Duration("interval", 0, "poll interval (e.g. 5m); if unset, run once and exit")
	pollCmd.Flags().Int("max-concurrent", 2, "max concurrent hive run processes (0 = no limit)")
	viper.BindPFlag("poll.interval", pollCmd.Flags().Lookup("interval"))
	viper.BindPFlag("poll.max-concurrent", pollCmd.Flags().Lookup("max-concurrent"))
	rootCmd.AddCommand(pollCmd)
}

// countRunningProcesses returns the number of tracked processes still alive.
// It prunes dead processes from the tracker.
func countRunningProcesses() int {
	runningProcessesMu.Lock()
	defer runningProcessesMu.Unlock()

	// Prune dead processes
	for pid := range runningProcesses {
		// Check if process is still alive by sending signal 0
		process, err := os.FindProcess(pid)
		if err != nil || process.Signal(syscall.Signal(0)) != nil {
			delete(runningProcesses, pid)
		}
	}

	return len(runningProcesses)
}

// trackProcess adds a PID to the running tracker and spawns a goroutine
// to remove it when the process exits.
func trackProcess(cmd *exec.Cmd) {
	pid := cmd.Process.Pid

	runningProcessesMu.Lock()
	runningProcesses[pid] = struct{}{}
	runningProcessesMu.Unlock()

	go func() {
		cmd.Wait()
		runningProcessesMu.Lock()
		delete(runningProcesses, pid)
		runningProcessesMu.Unlock()
	}()
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

// pollOnce runs a single poll cycle: fetch ready items and dispatch hive run
// for each eligible item.
func pollOnce(ctx context.Context) error {
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

	items, err := gh.ReadyItems(ctx, projectID)
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

	allowedUsers := viper.GetStringSlice("security.allowed-users")
	if len(allowedUsers) == 0 {
		return fmt.Errorf("security.allowed-users not configured (set in .hive.toml) — refusing to run (fail-closed)")
	}

	slog.Info("found ready items", "count", len(items))

	maxConcurrent := viper.GetInt("poll.max-concurrent")

	for _, item := range items {
		// Check concurrency limit (0 = no limit)
		if maxConcurrent > 0 {
			running := countRunningProcesses()
			if running >= maxConcurrent {
				slog.Info("concurrency limit reached, skipping remaining items",
					"limit", maxConcurrent, "running", running, "skipped_item", item.Title)
				break
			}
		}


		if item.IsDraft {
			slog.Warn("skipping draft", "title", item.Title)
			continue
		}

		// Authz: fetch issue to check author against allowed-users
		issue, err := gh.FetchIssue(ctx, item.Repo, item.Number)
		if err != nil {
			slog.Error("failed to fetch issue for authz", "error", err, "item", item.Title)
			continue
		}
		if !authz.IsAllowed(issue.Author.Login, allowedUsers) {
			slog.Warn("skipping item — author not in allowed-users",
				"author", issue.Author.Login, "title", item.Title)
			continue
		}

		slog.Info("dispatching run", "title", item.Title, "repo", item.Repo, "issue", item.Number)

		// Move to In Progress
		if err := gh.MoveToInProgress(ctx, projectNodeID, item.ID); err != nil {
			slog.Error("failed to move to In Progress", "error", err, "item", item.Title)
			continue
		}

		// Dispatch hive run in background
		ref := fmt.Sprintf("%s#%d", item.Repo, item.Number)
		hiveCmd := exec.Command(os.Args[0], "run", "--board-item-id", item.ID, ref)
		hiveCmd.Stdout = os.Stdout
		hiveCmd.Stderr = os.Stderr
		if err := hiveCmd.Start(); err != nil {
			slog.Error("failed to dispatch run", "error", err, "ref", ref)
			continue
		}

		trackProcess(hiveCmd)
		slog.Info("dispatched", "ref", ref, "pid", hiveCmd.Process.Pid)
	}

	return nil
}
