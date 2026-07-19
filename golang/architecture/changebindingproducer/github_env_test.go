// SPDX-License-Identifier: Apache-2.0

package changebindingproducer

import "testing"

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func fileFrom(content string, err error) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), err }
}

const prEvent = `{"number": 81, "pull_request": {"base": {"sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, "head": {"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`

// 1: a supported pull_request event yields typed authoritative identities.
func TestExtract_PullRequestAuthoritative(t *testing.T) {
	env := envFrom(map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REPOSITORY": "globulario/sensei", "GITHUB_EVENT_PATH": "/e.json"})
	ids, f := ExtractGitHubEvent(env, fileFrom(prEvent, nil))
	if f != FailNone {
		t.Fatalf("supported event must extract, got %q", f)
	}
	if ids.RepositoryIdentity != "github.com/globulario/sensei" || ids.ChangeID != "81" ||
		ids.HeadSHA != hexn('a', 40) || ids.BaseSHA != hexn('b', 40) || ids.EventSource != EventPullRequest {
		t.Fatalf("wrong identities: %+v", ids)
	}
}

// 2-5 + 53: unsupported / missing event forms fail typed; a non-GitHub (local) context is
// unsupported, never silently promoted to authoritative.
func TestExtract_UnsupportedAndMissing(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		file string
		ferr error
		want ProducerFailure
	}{
		{"local_no_event", map[string]string{}, prEvent, nil, FailUnsupportedEvent},
		{"push_event", map[string]string{"GITHUB_EVENT_NAME": "push", "GITHUB_REPOSITORY": "o/r", "GITHUB_EVENT_PATH": "/e"}, prEvent, nil, FailUnsupportedEvent},
		{"missing_repo", map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": "/e"}, prEvent, nil, FailMissingEventIdentity},
		{"unreadable_payload", map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REPOSITORY": "o/r", "GITHUB_EVENT_PATH": "/e"}, "", errRead, FailMissingEventIdentity},
		{"missing_base_head", map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REPOSITORY": "o/r", "GITHUB_EVENT_PATH": "/e"}, `{"number": 5}`, nil, FailMissingEventIdentity},
		{"missing_number", map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REPOSITORY": "o/r", "GITHUB_EVENT_PATH": "/e"}, `{"pull_request":{"base":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`, nil, FailMissingEventIdentity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, f := ExtractGitHubEvent(envFrom(c.env), fileFrom(c.file, c.ferr))
			if f != c.want {
				t.Fatalf("got %q, want %q", f, c.want)
			}
		})
	}
}

// originToDomain is a pure lexical transform; a non-github origin stays raw and will simply
// mismatch (fail closed), never be coerced to look like the event repository.
func TestOriginToDomain(t *testing.T) {
	cases := map[string]string{
		"git@github.com:globulario/sensei.git":     "github.com/globulario/sensei",
		"https://github.com/globulario/sensei.git": "github.com/globulario/sensei",
		"https://gitlab.com/x/y.git":               "https://gitlab.com/x/y",
	}
	for in, want := range cases {
		if got := originToDomain(in); got != want {
			t.Fatalf("originToDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

var errRead = &readErr{}

type readErr struct{}

func (*readErr) Error() string { return "read failed" }
