// Package github provides GitHub API interactions via the gh CLI.
// It handles board status changes, issue fetching, PR creation,
// and branch pushing.
package github

// Author represents a GitHub user (issue author, PR author, etc.).
type Author struct {
	Login string `json:"login"`
}

// Issue represents a GitHub issue fetched via gh.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Author Author `json:"author"`
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

// projectItemListResponse is the top-level JSON from gh project item-list --format json.
type projectItemListResponse struct {
	Items []projectItem `json:"items"`
}

// projectItem is a single item in the gh project item-list response.
type projectItem struct {
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Status  string             `json:"status"`
	Content projectItemContent `json:"content"`
}

// projectItemContent holds nested content fields (number, repository, type).
type projectItemContent struct {
	Number     int    `json:"number"`
	Repository string `json:"repository"`
	Type       string `json:"type"`
}
