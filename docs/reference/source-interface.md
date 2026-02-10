# Source Interface Reference

Complete reference for the `Source` interface, `WorkItem` struct, and the GitHub Projects adapter.

## Source Interface

Defined in `internal/source/source.go`. The boundary between hive's core pipeline and wherever work items come from.

```go
type Source interface {
    Ready(ctx context.Context) ([]WorkItem, error)
    Take(ctx context.Context, ref string) error
    Complete(ctx context.Context, ref string) error
    Release(ctx context.Context, ref string) error
}
```

### Methods

| Method | Called By | Description |
|--------|----------|-------------|
| `Ready` | `poll` | Returns all work items currently available for dispatch. Items are filtered by source-specific criteria (e.g. board column, author authorization). |
| `Take` | `poll` | Marks a work item as claimed/in-progress on the source. Called after local claim and systemd unit start. |
| `Complete` | — | Marks a work item as done on the source (e.g. moves to In Review). Currently unused: `publish` calls `gh.MoveToInReview()` directly instead. |
| `Release` | `reap` | Returns a work item to the ready state on the source. Used when a session fails or is detected as stale. |

### Ref Format

The `ref` parameter is an opaque string that uniquely identifies a work item within a source. The format is source-specific:

- **GitHub Projects:** `github:{owner}/{repo}#{number}` (e.g. `github:ivy/hive#42`)

Refs are constructed by the adapter's `Ready` method and passed through to `Take`, `Complete`, and `Release`.

## WorkItem Struct

Defined in `internal/source/source.go`. Represents a unit of work returned by `Ready`.

```go
type WorkItem struct {
    Ref      string            // opaque identifier (source-specific)
    Repo     string            // repository location (e.g., "ivy/hive")
    Title    string            // human-readable summary
    Prompt   string            // agent instructions (e.g., issue body)
    Metadata map[string]string // source-specific data
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Ref` | string | Source-specific unique identifier. Used as the key for claims and all subsequent Source operations. |
| `Repo` | string | Repository in `owner/name` format. Used to resolve the local repo path. |
| `Title` | string | Issue or item title. Written to session JSON. |
| `Prompt` | string | Agent instructions. For GitHub issues, this is the issue body. Written to `.hive/prompt.md`. |
| `Metadata` | map[string]string | Source-specific key-value data. Carried through to `session.SourceMetadata`. |

### Metadata Keys (GitHub Projects)

| Key | Description |
|-----|-------------|
| `board_item_id` | GraphQL item ID on the project board. Required for column transitions. |
| `project_node_id` | GraphQL node ID of the project. Passed through from adapter config. |

## GitHub Projects Adapter

Defined in `internal/source/ghprojects/ghprojects.go`. Implements `Source` backed by a GitHub Projects (v2) board.

### Config

```go
type Config struct {
    Client        *github.Client
    ProjectNumber string
    ProjectNodeID string
    AllowedUsers  []string
}
```

| Field | Description |
|-------|-------------|
| `Client` | GitHub client (wraps `gh` CLI). Configured with status field IDs and option IDs. |
| `ProjectNumber` | Board number (e.g. `"29"`). Passed to GraphQL query. |
| `ProjectNodeID` | GraphQL node ID (e.g. `"PVT_kwHOADgWUM4BOsG3"`). Used for `item-edit` mutations. |
| `AllowedUsers` | Authorized GitHub usernames. Issues by other authors are silently skipped. |

### Behavior

#### Ready

1. Queries the GraphQL API for items matching the configured `ReadyStatus` (server-side filtering)
2. Skips draft issues (`DraftIssue` typename)
3. Fetches full issue data for each item via `gh issue view`
4. Checks issue author against `AllowedUsers` (case-insensitive)
5. Builds `WorkItem` with ref format `github:{owner}/{repo}#{number}`
6. Caches `ref → board_item_id` mapping for subsequent operations

#### Take

Moves the board item to In Progress using `gh project item-edit` with the configured `InProgressOptionID`.

#### Complete

Moves the board item to In Review using the configured `InReviewOptionID`.

#### Release

Moves the board item back to Ready using the configured `ReadyOptionID`.

### RegisterItem

```go
func (a *Adapter) RegisterItem(ref, boardItemID string)
```

Populates the internal `ref → board_item_id` cache for `Complete` and `Release` calls outside the `Ready → Take` flow. Used by `reap` to release items that were fetched in a previous poll cycle.

### Item Cache

The adapter maintains an in-memory `map[string]itemInfo` (mutex-protected) that maps refs to board item IDs. This cache is populated by:

- `Ready` — for each item returned from the board
- `RegisterItem` — for items loaded from session metadata

The cache is required because `Take`, `Complete`, and `Release` need the board item ID but only receive the ref string.
