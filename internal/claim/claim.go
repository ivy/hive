// Package claim provides atomic claim files for work-item dedup.
// Claims live at {dataDir}/claims/. One file per active work item.
// The filename is a truncated SHA-256 hash of the source ref.
// The content is the session UUID.
package claim

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claim represents an active claim on a work item.
type Claim struct {
	Key       string // truncated hash — the filename
	SessionID string // session UUID — the file content
}

// claimKey computes a truncated SHA-256 hash of ref (first 12 hex chars).
func claimKey(ref string) string {
	h := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(h[:])[:12]
}

// TryClaim atomically creates a claim file for ref.
// Returns (true, nil) on success, (false, nil) if already claimed.
func TryClaim(dataDir, ref, sessionID string) (bool, error) {
	dir := filepath.Join(dataDir, "claims")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating claims dir: %w", err)
	}

	path := filepath.Join(dir, claimKey(ref))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("creating claim file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(sessionID); err != nil {
		return false, fmt.Errorf("writing claim file: %w", err)
	}
	return true, nil
}

// Release removes the claim file for ref. Idempotent — returns nil if
// the file doesn't exist.
func Release(dataDir, ref string) error {
	path := filepath.Join(dataDir, "claims", claimKey(ref))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing claim file: %w", err)
	}
	return nil
}

// Exists returns true if a claim file exists for ref.
func Exists(dataDir, ref string) bool {
	path := filepath.Join(dataDir, "claims", claimKey(ref))
	_, err := os.Stat(path)
	return err == nil
}

// SessionForRef reads the session UUID from the claim file for ref.
func SessionForRef(dataDir, ref string) (string, error) {
	path := filepath.Join(dataDir, "claims", claimKey(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading claim file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ListAll returns all active claims in the data directory.
// Returns an empty slice (not an error) if the claims directory doesn't exist.
func ListAll(dataDir string) ([]Claim, error) {
	dir := filepath.Join(dataDir, "claims")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Claim{}, nil
		}
		return nil, fmt.Errorf("reading claims dir: %w", err)
	}

	claims := make([]Claim, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading claim %s: %w", e.Name(), err)
		}
		claims = append(claims, Claim{
			Key:       e.Name(),
			SessionID: strings.TrimSpace(string(data)),
		})
	}
	return claims, nil
}
