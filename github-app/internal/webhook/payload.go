package webhook

// PullRequestEvent contains only the webhook fields required by the MVP.
type PullRequestEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Number int `json:"number"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

// Supported reports whether this action should trigger a fresh briefing.
func (e PullRequestEvent) Supported() bool {
	switch e.Action {
	case "opened", "reopened", "synchronize":
		return true
	default:
		return false
	}
}

// Valid verifies the minimum immutable identity needed for analysis.
func (e PullRequestEvent) Valid() bool {
	return e.Installation.ID > 0 &&
		e.Repository.FullName != "" &&
		e.Repository.Owner.Login != "" &&
		e.Repository.Name != "" &&
		e.PullRequest.Number > 0 &&
		e.PullRequest.Base.SHA != "" &&
		e.PullRequest.Head.SHA != ""
}
