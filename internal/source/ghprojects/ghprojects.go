package ghprojects

import (
	"context"
	"fmt"
	"sync"

	"github.com/ivy/hive/internal/authz"
	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/source"
)

// Config holds the settings for a GitHub Projects source adapter.
type Config struct {
	Client        *github.Client
	ProjectNumber string
	ProjectNodeID string
	AllowedUsers  []string
}

// Adapter implements source.Source backed by a GitHub Projects board.
type Adapter struct {
	cfg   Config
	mu    sync.Mutex
	items map[string]itemInfo // ref → cached board info
}

type itemInfo struct {
	boardItemID string
}

// NewAdapter creates an Adapter with the given configuration.
func NewAdapter(cfg Config) *Adapter {
	return &Adapter{
		cfg:   cfg,
		items: make(map[string]itemInfo),
	}
}

// Ready returns work items in the "Ready" column of the project board.
func (a *Adapter) Ready(ctx context.Context) ([]source.WorkItem, error) {
	boardItems, err := a.cfg.Client.ReadyItems(ctx, a.cfg.ProjectNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching ready items: %w", err)
	}

	var items []source.WorkItem
	for _, bi := range boardItems {
		if bi.IsDraft {
			continue
		}

		issue, err := a.cfg.Client.FetchIssue(ctx, bi.Repo, bi.Number)
		if err != nil {
			return nil, fmt.Errorf("fetching issue %s#%d: %w", bi.Repo, bi.Number, err)
		}

		if !authz.IsAllowed(issue.Author.Login, a.cfg.AllowedUsers) {
			continue
		}

		ref := buildRef(bi.Repo, bi.Number)
		items = append(items, source.WorkItem{
			Ref:    ref,
			Repo:   bi.Repo,
			Title:  bi.Title,
			Prompt: issue.Body,
			Metadata: map[string]string{
				"board_item_id":   bi.ID,
				"project_node_id": a.cfg.ProjectNodeID,
			},
		})

		a.mu.Lock()
		a.items[ref] = itemInfo{boardItemID: bi.ID}
		a.mu.Unlock()
	}

	return items, nil
}

// Take moves the referenced item to "In Progress".
func (a *Adapter) Take(ctx context.Context, ref string) error {
	info, err := a.lookup(ref)
	if err != nil {
		return err
	}
	return a.cfg.Client.MoveToInProgress(ctx, a.cfg.ProjectNodeID, info.boardItemID)
}

// Complete moves the referenced item to "In Review".
func (a *Adapter) Complete(ctx context.Context, ref string) error {
	info, err := a.lookup(ref)
	if err != nil {
		return err
	}
	return a.cfg.Client.MoveToInReview(ctx, a.cfg.ProjectNodeID, info.boardItemID)
}

// Release moves the referenced item back to "Ready".
func (a *Adapter) Release(ctx context.Context, ref string) error {
	info, err := a.lookup(ref)
	if err != nil {
		return err
	}
	return a.cfg.Client.MoveToReady(ctx, a.cfg.ProjectNodeID, info.boardItemID)
}

// RegisterItem adds a ref → boardItemID mapping to the cache.
// This allows callers to populate the cache for Complete/Release calls
// outside the Ready→Take flow.
func (a *Adapter) RegisterItem(ref, boardItemID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.items[ref] = itemInfo{boardItemID: boardItemID}
}

func (a *Adapter) lookup(ref string) (itemInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info, ok := a.items[ref]
	if !ok {
		return itemInfo{}, fmt.Errorf("unknown ref: %s", ref)
	}
	return info, nil
}

func buildRef(repo string, number int) string {
	return fmt.Sprintf("github:%s#%d", repo, number)
}

