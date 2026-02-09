// Package authz provides authorization checks for hive operations.
package authz

import "strings"

// IsAllowed checks whether login is in the allowedUsers list.
// Comparison is case-insensitive (GitHub usernames are case-insensitive).
// Returns false when allowedUsers is empty or nil (fail-closed).
func IsAllowed(login string, allowedUsers []string) bool {
	if len(allowedUsers) == 0 {
		return false
	}
	lower := strings.ToLower(login)
	for _, u := range allowedUsers {
		if strings.ToLower(u) == lower {
			return true
		}
	}
	return false
}
