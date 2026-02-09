// Package workspace manages git worktrees and .hive/ metadata directories.
// It handles creation, loading, listing, and removal of workspaces — the
// contract between pipeline stages.
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BaseDir is the root directory where workspaces are created.
const BaseDir = "/tmp/hive"

// MetaDir is the name of the metadata directory inside each workspace.
const MetaDir = ".hive"

// Status represents the lifecycle state of a workspace.
type Status string

const (
	StatusPrepared  Status = "prepared"
	StatusRunning   Status = "running"
	StatusStopped   Status = "stopped"
	StatusPublished Status = "published"
	StatusFailed    Status = "failed"
)

// Workspace represents a git worktree with .hive/ metadata.
type Workspace struct {
	// Path is the absolute path to the worktree directory.
	Path string

	// RepoPath is the absolute path to the main repository.
	RepoPath string

	// Repo is the GitHub owner/name (e.g. "ivy/dotfiles").
	Repo string

	// IssueNumber is the GitHub issue number this workspace targets.
	IssueNumber int

	// Branch is the git branch name for this worktree.
	Branch string

	// SessionID is the Claude session UUID for resume support.
	SessionID string

	// Status is the current lifecycle state.
	Status Status
}

// project extracts the repo name from an owner/name string (e.g. "dotfiles" from "ivy/dotfiles").
func project(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return repo
}

// Create creates a new workspace from a repository and issue number.
// It creates a git worktree, .hive/ metadata directory, and writes initial
// metadata files (repo, issue-number, session-id, status=prepared).
func Create(ctx context.Context, repoPath string, repo string, issueNumber int) (*Workspace, error) {
	proj := project(repo)
	ts := time.Now().Unix()
	dirName := fmt.Sprintf("%s-%d-%d", proj, issueNumber, ts)
	branch := fmt.Sprintf("hive/%s", dirName)
	wsPath := filepath.Join(BaseDir, dirName)

	if err := os.MkdirAll(BaseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating base dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, wsPath, "HEAD")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	metaPath := filepath.Join(wsPath, MetaDir)
	if err := os.MkdirAll(metaPath, 0o755); err != nil {
		return nil, fmt.Errorf("creating metadata dir: %w", err)
	}

	sessionID := uuid.New().String()

	files := map[string]string{
		"repo":         repo,
		"issue-number": strconv.Itoa(issueNumber),
		"session-id":   sessionID,
		"status":       string(StatusPrepared),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(metaPath, name), []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
	}

	return &Workspace{
		Path:        wsPath,
		RepoPath:    repoPath,
		Repo:        repo,
		IssueNumber: issueNumber,
		Branch:      branch,
		SessionID:   sessionID,
		Status:      StatusPrepared,
	}, nil
}

// Load reads .hive/ metadata from an existing workspace directory and
// reconstructs the Workspace struct.
func Load(ctx context.Context, wsPath string) (*Workspace, error) {
	metaPath := filepath.Join(wsPath, MetaDir)

	info, err := os.Stat(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading metadata dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", metaPath)
	}

	readMeta := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(metaPath, name))
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", name, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	repo, err := readMeta("repo")
	if err != nil {
		return nil, err
	}

	issueStr, err := readMeta("issue-number")
	if err != nil {
		return nil, err
	}
	issueNumber, err := strconv.Atoi(issueStr)
	if err != nil {
		return nil, fmt.Errorf("parsing issue-number %q: %w", issueStr, err)
	}

	sessionID, err := readMeta("session-id")
	if err != nil {
		return nil, err
	}

	statusStr, err := readMeta("status")
	if err != nil {
		return nil, err
	}

	// Resolve repoPath from worktree's git config.
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-common-dir")
	cmd.Dir = wsPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resolving repo path: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(out))
	// git-common-dir returns the .git dir of the main repo (possibly relative).
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(wsPath, gitCommonDir)
	}
	repoPath := filepath.Dir(gitCommonDir)

	// Resolve branch from HEAD.
	cmd = exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = wsPath
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resolving branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))

	return &Workspace{
		Path:        wsPath,
		RepoPath:    repoPath,
		Repo:        repo,
		IssueNumber: issueNumber,
		Branch:      branch,
		SessionID:   sessionID,
		Status:      Status(statusStr),
	}, nil
}

// Remove tears down a workspace: removes the git worktree, deletes the
// branch, and prunes worktree metadata.
func Remove(ctx context.Context, ws *Workspace) error {
	// Remove the worktree.
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", ws.Path)
	cmd.Dir = ws.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Delete the branch.
	cmd = exec.CommandContext(ctx, "git", "branch", "-D", ws.Branch)
	cmd.Dir = ws.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Prune worktree metadata.
	cmd = exec.CommandContext(ctx, "git", "worktree", "prune")
	cmd.Dir = ws.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

// ListAll scans BaseDir for directories containing .hive/ and returns
// loaded Workspace structs for each.
func ListAll(ctx context.Context) ([]*Workspace, error) {
	entries, err := os.ReadDir(BaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading base dir: %w", err)
	}

	var workspaces []*Workspace
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wsPath := filepath.Join(BaseDir, entry.Name())
		metaPath := filepath.Join(wsPath, MetaDir)
		if info, err := os.Stat(metaPath); err != nil || !info.IsDir() {
			continue
		}
		ws, err := Load(ctx, wsPath)
		if err != nil {
			continue
		}
		workspaces = append(workspaces, ws)
	}

	return workspaces, nil
}

// SetStatus writes the status file in .hive/.
func SetStatus(ws *Workspace, status Status) error {
	metaPath := filepath.Join(ws.Path, MetaDir, "status")
	if err := os.WriteFile(metaPath, []byte(string(status)), 0o644); err != nil {
		return fmt.Errorf("writing status: %w", err)
	}
	ws.Status = status
	return nil
}

// WriteIssueData writes the issue JSON payload to .hive/issue.json.
func WriteIssueData(ws *Workspace, data []byte) error {
	path := filepath.Join(ws.Path, MetaDir, "issue.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing issue.json: %w", err)
	}
	return nil
}

// WritePrompt writes the prompt content to .hive/prompt.md.
func WritePrompt(ws *Workspace, content string) error {
	path := filepath.Join(ws.Path, MetaDir, "prompt.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing prompt.md: %w", err)
	}
	return nil
}

// ReadSessionID reads the session-id from .hive/session-id.
func ReadSessionID(ws *Workspace) (string, error) {
	path := filepath.Join(ws.Path, MetaDir, "session-id")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading session-id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteTmuxSession writes the tmux session name to .hive/tmux-session.
func WriteTmuxSession(ws *Workspace, name string) error {
	return os.WriteFile(filepath.Join(ws.Path, MetaDir, "tmux-session"), []byte(name), 0o644)
}

// WriteBoardItemID writes the board item ID to .hive/board-item-id.
func WriteBoardItemID(ws *Workspace, itemID string) error {
	return os.WriteFile(filepath.Join(ws.Path, MetaDir, "board-item-id"), []byte(itemID), 0o644)
}

// ReadBoardItemID reads the board item ID from .hive/board-item-id.
func ReadBoardItemID(ws *Workspace) (string, error) {
	data, err := os.ReadFile(filepath.Join(ws.Path, MetaDir, "board-item-id"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// HasUncommittedChanges returns true if the workspace worktree has
// modified, deleted, or untracked files (excluding .hive/ metadata).
func HasUncommittedChanges(ctx context.Context, ws *Workspace) (bool, error) {
	// Check for staged + unstaged changes.
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = ws.Path
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip .hive/ metadata files — they aren't part of the deliverable.
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[len(fields)-1], MetaDir+"/") {
			continue
		}
		return true, nil
	}
	return false, nil
}

// CommitAll stages all changes (excluding .hive/) and creates a commit
// with the given message. Returns nil if there is nothing to commit.
func CommitAll(ctx context.Context, ws *Workspace, message string) error {
	// Stage everything, then unstage .hive/.
	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = ws.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add -A: %s: %w", strings.TrimSpace(string(out)), err)
	}

	cmd = exec.CommandContext(ctx, "git", "reset", "HEAD", "--", MetaDir)
	cmd.Dir = ws.Path
	// reset may fail if .hive/ was never tracked — that's fine.
	_ = cmd.Run()

	cmd = exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = ws.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		outStr := strings.TrimSpace(string(out))
		// "nothing to commit" / "nothing added to commit" is not an error.
		if strings.Contains(outStr, "nothing to commit") || strings.Contains(outStr, "nothing added to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %s: %w", outStr, err)
	}
	return nil
}
