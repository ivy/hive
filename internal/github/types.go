// Package github provides GitHub API interactions via the gh CLI.
// It handles board status changes, issue fetching, PR creation,
// and branch pushing.
package github

// Issue represents a GitHub issue fetched via gh.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// PR represents a GitHub pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Branch string `json:"headRefName"`
}

// BoardItem represents an item on a GitHub Projects board.
type BoardItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Number  int    `json:"number"`
	Repo    string `json:"repository"`
	Status  string `json:"status"`
	IsDraft bool   `json:"isDraft"`
	Type    string `json:"type"`
}
