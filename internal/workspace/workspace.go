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

// Create creates a new workspace from a repository and issue number.
// It creates a git worktree, .hive/ metadata directory, and writes initial
// metadata files (repo, issue-number, session-id, status=prepared).
func Create(ctx context.Context, repoPath string, repo string, issueNumber int) (*Workspace, error) {
	_ = ctx
	_ = repoPath
	_ = repo
	_ = issueNumber
	return nil, fmt.Errorf("not implemented")
}

// Load reads .hive/ metadata from an existing workspace directory and
// reconstructs the Workspace struct.
func Load(ctx context.Context, wsPath string) (*Workspace, error) {
	_ = ctx
	_ = wsPath
	return nil, fmt.Errorf("not implemented")
}

// Remove tears down a workspace: removes the git worktree, deletes the
// branch, and prunes worktree metadata.
func Remove(ctx context.Context, ws *Workspace) error {
	_ = ctx
	_ = ws
	return fmt.Errorf("not implemented")
}

// ListAll scans BaseDir for directories containing .hive/ and returns
// loaded Workspace structs for each.
func ListAll(ctx context.Context) ([]*Workspace, error) {
	_ = ctx
	return nil, fmt.Errorf("not implemented")
}

// SetStatus writes the status file in .hive/.
func SetStatus(ws *Workspace, status Status) error {
	_ = ws
	_ = status
	return fmt.Errorf("not implemented")
}

// WriteIssueData writes the issue JSON payload to .hive/issue.json.
func WriteIssueData(ws *Workspace, data []byte) error {
	_ = ws
	_ = data
	return fmt.Errorf("not implemented")
}

// WritePrompt writes the prompt content to .hive/prompt.md.
func WritePrompt(ws *Workspace, content string) error {
	_ = ws
	_ = content
	return fmt.Errorf("not implemented")
}

// ReadSessionID reads the session-id from .hive/session-id.
func ReadSessionID(ws *Workspace) (string, error) {
	_ = ws
	return "", fmt.Errorf("not implemented")
}

// unexported helpers used by the stubs above — teammates will use these.
var _ = os.MkdirAll
var _ = os.WriteFile
var _ = os.ReadFile
var _ = filepath.Join
var _ = strconv.Itoa
var _ = strings.TrimSpace
var _ = time.Now
var _ = uuid.New
var _ = exec.Command
