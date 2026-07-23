package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/globulario/sensei-github-app/internal/briefing"
	githubapi "github.com/globulario/sensei-github-app/internal/github"
)

type fakeGitHubClient struct {
	files         []githubapi.PullRequestFile
	commentCalls  int
	checkCalls    int
	commentBody   string
	checkHeadSHA  string
	checkExternal string
}

func (f *fakeGitHubClient) ListPullRequestFiles(context.Context, int64, string, string, int) ([]githubapi.PullRequestFile, error) {
	return append([]githubapi.PullRequestFile(nil), f.files...), nil
}

func (f *fakeGitHubClient) UpsertIssueComment(_ context.Context, _ int64, _, _ string, _ int, marker, body string) error {
	f.commentCalls++
	f.commentBody = body
	if !strings.Contains(body, marker) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (f *fakeGitHubClient) UpsertCheckRun(_ context.Context, _ int64, _, _, headSHA, externalID, _, _ string) error {
	f.checkCalls++
	f.checkHeadSHA = headSHA
	f.checkExternal = externalID
	return nil
}

func TestHandlerProcessesSupportedPullRequest(t *testing.T) {
	secret := []byte("webhook-secret")
	client := &fakeGitHubClient{files: []githubapi.PullRequestFile{{Filename: "cmd/app/main.go", Status: "modified", Additions: 4, Deletions: 1}}}
	handler, err := NewHandler(secret, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{
		"action":"opened",
		"installation":{"id":123},
		"repository":{"full_name":"globulario/example","name":"example","owner":{"login":"globulario"}},
		"pull_request":{"number":7,"base":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"head":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","repo":{"full_name":"globulario/example"}}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	response := httptest.NewRecorder()

	handler.Routes().ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.commentCalls != 1 || client.checkCalls != 1 {
		t.Fatalf("comment calls = %d, check calls = %d", client.commentCalls, client.checkCalls)
	}
	if !strings.Contains(client.commentBody, briefing.CommentMarker) {
		t.Fatal("briefing marker missing from sticky comment")
	}
	if client.checkHeadSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("check head SHA = %q", client.checkHeadSHA)
	}
	if client.checkExternal != "sensei-pr-7-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("check external ID = %q", client.checkExternal)
	}
}

func TestHandlerRejectsInvalidSignature(t *testing.T) {
	client := &fakeGitHubClient{}
	handler, err := NewHandler([]byte("secret"), client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(`{"action":"opened"}`))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	response := httptest.NewRecorder()

	handler.Routes().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if client.commentCalls != 0 || client.checkCalls != 0 {
		t.Fatal("GitHub API was called for an unauthenticated webhook")
	}
}

func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
