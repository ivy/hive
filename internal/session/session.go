// Package session manages session metadata JSON files stored under
// {dataDir}/sessions/{uuid}.json. The dataDir is resolved via XDG —
// $XDG_DATA_HOME/hive falling back to ~/.local/share/hive.
package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status represents the lifecycle state of a session.
type Status string

const (
	StatusDispatching Status = "dispatching"
	StatusPrepared    Status = "prepared"
	StatusRunning     Status = "running"
	StatusStopped     Status = "stopped"
	StatusPublished   Status = "published"
	StatusFailed      Status = "failed"
)

// Session represents a dispatched work item tracked as a JSON file.
type Session struct {
	ID             string            `json:"id"`
	Ref            string            `json:"ref"`
	Repo           string            `json:"repo"`
	Title          string            `json:"title"`
	Prompt         string            `json:"prompt"`
	SourceMetadata map[string]string `json:"source_metadata"`
	Status         Status            `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	PollInstance   string            `json:"poll_instance"`
}

// DataDir resolves $XDG_DATA_HOME/hive. If XDG_DATA_HOME is not set,
// falls back to ~/.local/share/hive.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "hive")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "hive")
}

// sessionsDir returns the path to the sessions subdirectory.
func sessionsDir(dataDir string) string {
	return filepath.Join(dataDir, "sessions")
}

// sessionPath returns the path to a session's JSON file.
func sessionPath(dataDir, id string) string {
	return filepath.Join(sessionsDir(dataDir), id+".json")
}

// Create JSON-marshals the session and writes it to
// {dataDir}/sessions/{s.ID}.json. Creates the sessions directory if needed.
func Create(dataDir string, s *Session) error {
	dir := sessionsDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating sessions dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}

	if err := os.WriteFile(sessionPath(dataDir, s.ID), data, 0o644); err != nil {
		return fmt.Errorf("writing session file: %w", err)
	}

	return nil
}

// Load reads and unmarshals {dataDir}/sessions/{id}.json.
func Load(dataDir string, id string) (*Session, error) {
	data, err := os.ReadFile(sessionPath(dataDir, id))
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshaling session: %w", err)
	}

	return &s, nil
}

// ListAll reads all *.json files in {dataDir}/sessions/ and returns the
// parsed sessions. Returns an empty slice (not error) if the sessions
// directory doesn't exist. Skips files that fail to parse.
func ListAll(dataDir string) ([]*Session, error) {
	dir := sessionsDir(dataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Session{}, nil
		}
		return nil, fmt.Errorf("reading sessions dir: %w", err)
	}

	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			slog.Warn("skipping session file", "file", entry.Name(), "error", err)
			continue
		}

		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			slog.Warn("skipping malformed session file", "file", entry.Name(), "error", err)
			continue
		}

		sessions = append(sessions, &s)
	}

	return sessions, nil
}

// SetStatus loads the session, updates its status, and writes it back.
func SetStatus(dataDir string, id string, status Status) error {
	s, err := Load(dataDir, id)
	if err != nil {
		return fmt.Errorf("loading session for status update: %w", err)
	}

	s.Status = status

	if err := Create(dataDir, s); err != nil {
		return fmt.Errorf("writing updated session: %w", err)
	}

	return nil
}

// Remove deletes {dataDir}/sessions/{id}.json. Returns nil if the file
// doesn't exist (idempotent).
func Remove(dataDir string, id string) error {
	err := os.Remove(sessionPath(dataDir, id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing session file: %w", err)
	}
	return nil
}
