package main

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivy/hive/internal/authz"
	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/session"
	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var runCmd = &cobra.Command{
	Use:   "run <uuid | owner/repo#issue>",
	Short: "Orchestrate prepare → exec → publish",
	Long: `Full pipeline: creates a workspace, runs the agent, pushes a branch, and opens a PR.

Accepts a session UUID (from systemd dispatch) or an issue reference (manual workflow).
Both paths produce identical workspace layouts and outcomes.`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

func init() {
	runCmd.Flags().Bool("no-publish", false, "skip the publish step")
	runCmd.Flags().String("model", "sonnet", "Claude model to use")
	runCmd.Flags().String("board-item-id", "", "GitHub Projects board item ID")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	arg := args[0]

	if uuidRe.MatchString(arg) {
		return runFromSession(cmd, arg)
	}
	if strings.Contains(arg, "#") {
		return runFromIssueRef(cmd, arg)
	}
	return fmt.Errorf("argument %q is neither a UUID nor an issue ref (owner/repo#number)", arg)
}

// runFromSession runs the pipeline from a pre-existing session JSON (systemd dispatch path).
func runFromSession(cmd *cobra.Command, id string) error {
	noPublish, _ := cmd.Flags().GetBool("no-publish")
	model, _ := cmd.Flags().GetString("model")
	dataDir := session.DataDir()

	slog.Info("running from session", "uuid", id)

	sess, err := session.Load(dataDir, id)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}

	repo, issueNumber, err := parseSessionRef(sess.Ref)
	if err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("parse session ref: %w", err)
	}

	// Prepare workspace from session data
	ws, err := prepareFromSession(cmd, sess, repo, issueNumber)
	if err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("prepare: %w", err)
	}

	if err := session.SetStatus(dataDir, id, session.StatusPrepared); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	// Write board-item-id from session source_metadata if present
	if itemID, ok := sess.SourceMetadata["board_item_id"]; ok && itemID != "" {
		if err := workspace.WriteBoardItemID(ws, itemID); err != nil {
			_ = session.SetStatus(dataDir, id, session.StatusFailed)
			return fmt.Errorf("write board item ID: %w", err)
		}
	}

	// Exec
	if err := session.SetStatus(dataDir, id, session.StatusRunning); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	cmd.Flags().Set("model", model)
	if err := runExec(cmd, []string{ws.Path}); err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("exec: %w", err)
	}

	// Stopped
	if err := session.SetStatus(dataDir, id, session.StatusStopped); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	// Publish
	if noPublish {
		slog.Info("skipping publish (--no-publish)")
		fmt.Printf("Workspace ready for review: %s\n", ws.Path)
		return nil
	}

	publishSourceMeta = sess.SourceMetadata
	defer func() { publishSourceMeta = nil }()

	if err := runPublish(cmd, []string{ws.Path}); err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("publish: %w", err)
	}

	if err := session.SetStatus(dataDir, id, session.StatusPublished); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	return nil
}

// runFromIssueRef runs the pipeline from an issue reference (manual workflow).
func runFromIssueRef(cmd *cobra.Command, ref string) error {
	noPublish, _ := cmd.Flags().GetBool("no-publish")
	model, _ := cmd.Flags().GetString("model")
	boardItemID, _ := cmd.Flags().GetString("board-item-id")
	dataDir := session.DataDir()

	slog.Info("running full pipeline", "ref", ref)

	repo, issueNumber, err := parseIssueRef(ref)
	if err != nil {
		return err
	}

	// Create a session for tracking
	id := uuid.New().String()
	sess := &session.Session{
		ID:        id,
		Ref:       fmt.Sprintf("github:%s#%d", repo, issueNumber),
		Repo:      repo,
		Status:    session.StatusDispatching,
		CreatedAt: time.Now().UTC(),
	}
	if err := session.Create(dataDir, sess); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	// Fetch issue from GitHub (for authz + prompt + title)
	gh, err := github.NewClient()
	if err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("github client: %w", err)
	}

	issue, err := gh.FetchIssue(cmd.Context(), repo, issueNumber)
	if err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("fetch issue: %w", err)
	}

	// Authz: check issue author against allowed-users (defense in depth)
	allowedUsers := viper.GetStringSlice("security.allowed-users")
	if len(allowedUsers) == 0 {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("security.allowed-users not configured — refusing to run (fail-closed)")
	}
	if !authz.IsAllowed(issue.Author.Login, allowedUsers) {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("author %q not in allowed-users — refusing to prepare workspace", issue.Author.Login)
	}

	// Populate session with issue data
	sess.Title = issue.Title
	sess.Prompt = issue.Body
	if err := session.Create(dataDir, sess); err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("update session: %w", err)
	}

	// Prepare workspace using shared path (session UUID = workspace UUID)
	ws, err := prepareFromSession(cmd, sess, repo, issueNumber)
	if err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("prepare: %w", err)
	}

	if err := session.SetStatus(dataDir, id, session.StatusPrepared); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	if boardItemID != "" {
		if err := workspace.WriteBoardItemID(ws, boardItemID); err != nil {
			_ = session.SetStatus(dataDir, id, session.StatusFailed)
			return fmt.Errorf("write board item ID: %w", err)
		}
	}

	// Exec
	if err := session.SetStatus(dataDir, id, session.StatusRunning); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	cmd.Flags().Set("model", model)
	if err := runExec(cmd, []string{ws.Path}); err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("exec: %w", err)
	}

	if err := session.SetStatus(dataDir, id, session.StatusStopped); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	// Publish
	if noPublish {
		slog.Info("skipping publish (--no-publish)")
		fmt.Printf("Workspace ready for review: %s\n", ws.Path)
		return nil
	}

	if err := runPublish(cmd, []string{ws.Path}); err != nil {
		_ = session.SetStatus(dataDir, id, session.StatusFailed)
		return fmt.Errorf("publish: %w", err)
	}

	if err := session.SetStatus(dataDir, id, session.StatusPublished); err != nil {
		return fmt.Errorf("set session status: %w", err)
	}

	return nil
}

// parseSessionRef parses "github:{owner}/{repo}#{number}" into repo and issue number.
func parseSessionRef(ref string) (repo string, issueNumber int, err error) {
	if !strings.HasPrefix(ref, "github:") {
		return "", 0, fmt.Errorf("unsupported session ref format %q: expected github: prefix", ref)
	}
	return parseIssueRef(strings.TrimPrefix(ref, "github:"))
}

