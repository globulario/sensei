// SPDX-License-Identifier: AGPL-3.0-only

package changebindingproducer

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// GitHubEventIdentities is the authoritative subset extracted from a GitHub event. It is
// the ONLY channel for repository/change/base/head identity — never a branch name, commit
// message, PR title/body/labels, or Git-derived value.
type GitHubEventIdentities struct {
	EventSource        EventSource
	RepositoryProvider string
	RepositoryIdentity string
	ChangeProvider     string
	ChangeID           string
	BaseSHA            string
	HeadSHA            string
}

// ExtractGitHubEvent reads the authoritative event identities from the GitHub Actions
// environment: GITHUB_EVENT_NAME (only pull_request is authoritative), GITHUB_REPOSITORY
// (owner/repo), and the event payload at GITHUB_EVENT_PATH (base/head SHA + PR number). env
// and readFile are injected for testability. An unsupported event or an absent
// pull-request payload is a typed failure — it NEVER falls back to branch- or Git-derived
// identity.
func ExtractGitHubEvent(env func(string) string, readFile func(string) ([]byte, error)) (GitHubEventIdentities, ProducerFailure) {
	if env("GITHUB_EVENT_NAME") != "pull_request" {
		return GitHubEventIdentities{}, FailUnsupportedEvent
	}
	repo := strings.TrimSpace(env("GITHUB_REPOSITORY"))
	if repo == "" {
		return GitHubEventIdentities{}, FailMissingEventIdentity
	}
	data, err := readFile(env("GITHUB_EVENT_PATH"))
	if err != nil {
		return GitHubEventIdentities{}, FailMissingEventIdentity
	}
	var ev struct {
		Number      int `json:"number"`
		PullRequest struct {
			Base struct {
				SHA string `json:"sha"`
			} `json:"base"`
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return GitHubEventIdentities{}, FailMissingEventIdentity
	}
	if ev.Number == 0 || ev.PullRequest.Base.SHA == "" || ev.PullRequest.Head.SHA == "" {
		return GitHubEventIdentities{}, FailMissingEventIdentity
	}
	return GitHubEventIdentities{
		EventSource:        EventPullRequest,
		RepositoryProvider: "github",
		RepositoryIdentity: "github.com/" + repo,
		ChangeProvider:     "github",
		ChangeID:           strconv.Itoa(ev.Number),
		BaseSHA:            ev.PullRequest.Base.SHA,
		HeadSHA:            ev.PullRequest.Head.SHA,
	}, FailNone
}

// CheckoutState is the READ (never mutated) state of the checked-out repository.
type CheckoutState struct {
	RepositoryIdentity string
	HeadSHA            string
}

// ReadCheckout reads the checked-out repository identity (from origin's URL) and head SHA
// (git rev-parse HEAD) WITHOUT mutating repository state — no fetch, switch, reset, or
// checkout. Used by the CLI to populate the checkout-verification inputs; the pure producer
// never calls it.
func ReadCheckout(repoRoot string) (CheckoutState, error) {
	head, err := gitOut(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return CheckoutState{}, err
	}
	origin, err := gitOut(repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return CheckoutState{}, err
	}
	return CheckoutState{RepositoryIdentity: originToDomain(origin), HeadSHA: strings.TrimSpace(head)}, nil
}

func gitOut(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// originToDomain maps a git origin URL to the canonical github.com/owner/repo domain. It is
// a pure lexical transform (no network); a non-GitHub or unparseable origin yields the raw
// trimmed value, which will then simply MISMATCH the event identity and fail closed.
func originToDomain(origin string) string {
	o := strings.TrimSpace(origin)
	o = strings.TrimSuffix(o, ".git")
	switch {
	case strings.HasPrefix(o, "git@github.com:"):
		return "github.com/" + strings.TrimPrefix(o, "git@github.com:")
	case strings.HasPrefix(o, "https://github.com/"):
		return "github.com/" + strings.TrimPrefix(o, "https://github.com/")
	case strings.HasPrefix(o, "http://github.com/"):
		return "github.com/" + strings.TrimPrefix(o, "http://github.com/")
	default:
		return o
	}
}
