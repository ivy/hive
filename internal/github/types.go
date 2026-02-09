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

// graphQLReadyItemsResponse is the GraphQL response for querying ready items.
type graphQLReadyItemsResponse struct {
	Data struct {
		Viewer struct {
			ProjectV2 struct {
				Items struct {
					Nodes []graphQLProjectItem `json:"nodes"`
				} `json:"items"`
			} `json:"projectV2"`
		} `json:"viewer"`
	} `json:"data"`
}

// graphQLProjectItem is a single item node in the GraphQL response.
type graphQLProjectItem struct {
	ID      string `json:"id"`
	Content struct {
		Typename   string `json:"__typename"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
	} `json:"content"`
}
