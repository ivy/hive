package main

import (
	"fmt"
	"sort"

	"github.com/ivy/hive/internal/claim"
	"github.com/ivy/hive/internal/session"
)

// resolveSession resolves a ref or UUID to a session.
// Resolution order:
// 1. If it looks like a UUID, look up sessions/{uuid}.json directly
// 2. Otherwise, treat as source ref — scan claims for matching claim to find active session
// 3. If no active claim, find most recent session matching that ref
func resolveSession(dataDir, arg string) (*session.Session, error) {
	if isUUID(arg) {
		return session.Load(dataDir, arg)
	}

	// Normalize ref: bare "owner/repo#123" → "github:owner/repo#123"
	ref := arg
	if !isUUID(ref) && !hasSourcePrefix(ref) {
		ref = "github:" + ref
	}

	// Try claim lookup first (fastest path for active work)
	if claim.Exists(dataDir, ref) {
		sessionID, err := claim.SessionForRef(dataDir, ref)
		if err == nil {
			return session.Load(dataDir, sessionID)
		}
	}

	// Fall back to scanning all sessions for matching ref
	sessions, err := session.ListAll(dataDir)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	// Find sessions matching the ref, sorted by CreatedAt descending
	var matches []*session.Session
	for _, s := range sessions {
		if s.Ref == ref {
			matches = append(matches, s)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no session found for %q", arg)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})

	return matches[0], nil
}

// isUUID returns true if s matches the UUID format.
// This uses the same pattern as uuidRe in run.go.
func isUUID(s string) bool {
	return uuidRe.MatchString(s)
}

// hasSourcePrefix returns true if ref has a source type prefix (e.g. "github:").
func hasSourcePrefix(ref string) bool {
	for i, c := range ref {
		if c == ':' && i > 0 {
			return true
		}
		if c == '/' || c == '#' {
			return false
		}
	}
	return false
}
