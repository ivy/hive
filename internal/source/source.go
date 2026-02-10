package source

import "context"

// Source is the boundary between hive's core and wherever work items come from.
type Source interface {
	Ready(ctx context.Context) ([]WorkItem, error)
	Take(ctx context.Context, ref string) error
	Complete(ctx context.Context, ref string) error
	Release(ctx context.Context, ref string) error
}

// WorkItem represents a unit of work from a source.
type WorkItem struct {
	Ref      string            // opaque identifier (source-specific)
	Repo     string            // repository location (e.g., "ivy/hive")
	Title    string            // human-readable summary
	Prompt   string            // agent instructions (e.g., issue body)
	Metadata map[string]string // source-specific data
}
