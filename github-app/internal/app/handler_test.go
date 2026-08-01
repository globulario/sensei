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

const (
	baseSHA      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headSHA      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	advancedHead = "cccccccccccccccccccccccccccccccccccccccc"
	deliveryID   = "11111111-2222-3333-4444-555555555555"
)

type fakeGitHubClient struct {
	identities    []githubapi.PullRequestIdentity
	files         []githubapi.PullRequestFile
	identityCalls int
	fileCalls     int
	commentCalls  int
	checkCalls    int
	commentBody   string
	checkHeadSHA  string
	checkExternal string
}

func (f *fakeGitHubClient) GetPullRequestIdentity(context.Context, int64, string, string, int) (githubapi.PullRequestIdentity, error) {
	index := f.identityCalls
	f.identityCalls++
	if len(f.identities) == 0 {
		return githubapi.PullRequestIdentity{}, nil
	}
	if index >= len(f.identities) {
		index = len(f.identities) - 1
	}
	return f.identities[index], nil
}

func (f *fakeGitHubClient) ListPullRequestFiles(context.Context, int64, string, string, int) ([]githubapi.PullRequestFile, error) {
	f.fileCalls++
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

func (f *fakeGitHubClient) UpsertCheckRun(_ context.Context, _ int64, _, _, checkHeadSHA, externalID, _, _ string) error {
	f.checkCalls++
	f.checkHeadSHA = checkHeadSHA
	f.checkExternal = externalID
	return nil
}

func TestHandlerHealthAndReadiness(t *testing.T) {
	handler, err := NewHandler([]byte("secret"), &fakeGitHubClient{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/healthz": "ok\n", "/readyz": "ready\n"} {
		response := httptest.NewRecorder()
		handler.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != want {
			t.Fatalf("%s: status = %d, body = %q", path, response.Code, response.Body.String())
		}
	}
}

func TestHandlerProcessesSupportedPullRequest(t *testing.T) {
	secret := []byte("webhook-secret")
	identity := githubapi.PullRequestIdentity{BaseSHA: baseSHA, HeadSHA: headSHA}
	client := &fakeGitHubClient{
		identities: []githubapi.PullRequestIdentity{identity, identity},
		files:      []githubapi.PullRequestFile{{Filename: "cmd/app/main.go", Status: "modified", Additions: 4, Deletions: 1}},
	}
	response := performPullRequestWebhook(t, secret, client, deliveryID)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.identityCalls != 2 || client.fileCalls != 1 {
		t.Fatalf("identity calls = %d, file calls = %d", client.identityCalls, client.fileCalls)
	}
	if client.commentCalls != 1 || client.checkCalls != 1 {
		t.Fatalf("comment calls = %d, check calls = %d", client.commentCalls, client.checkCalls)
	}
	if !strings.Contains(client.commentBody, briefing.CommentMarker) {
		t.Fatal("briefing marker missing from sticky comment")
	}
	if client.checkHeadSHA != headSHA {
		t.Fatalf("check head SHA = %q", client.checkHeadSHA)
	}
	if client.checkExternal != "sensei-pr-7-"+headSHA {
		t.Fatalf("check external ID = %q", client.checkExternal)
	}
}

func TestHandlerRequiresDeliveryIdentity(t *testing.T) {
	secret := []byte("webhook-secret")
	client := &fakeGitHubClient{}
	response := performPullRequestWebhook(t, secret, client, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.identityCalls != 0 || client.fileCalls != 0 || client.commentCalls != 0 || client.checkCalls != 0 {
		t.Fatal("GitHub API was called without a delivery identity")
	}
}

func TestHandlerDiscardsDeliveryAlreadyStale(t *testing.T) {
	secret := []byte("webhook-secret")
	client := &fakeGitHubClient{identities: []githubapi.PullRequestIdentity{{BaseSHA: baseSHA, HeadSHA: advancedHead}}}
	response := performPullRequestWebhook(t, secret, client, deliveryID)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.identityCalls != 1 || client.fileCalls != 0 {
		t.Fatalf("identity calls = %d, file calls = %d", client.identityCalls, client.fileCalls)
	}
	if client.commentCalls != 0 || client.checkCalls != 0 {
		t.Fatal("stale delivery published GitHub output")
	}
}

func TestHandlerDiscardsDeliveryThatRacesWithNewCommit(t *testing.T) {
	secret := []byte("webhook-secret")
	client := &fakeGitHubClient{
		identities: []githubapi.PullRequestIdentity{
			{BaseSHA: baseSHA, HeadSHA: headSHA},
			{BaseSHA: baseSHA, HeadSHA: advancedHead},
		},
		files: []githubapi.PullRequestFile{{Filename: "cmd/app/main.go", Status: "modified", Additions: 4, Deletions: 1}},
	}
	response := performPullRequestWebhook(t, secret, client, deliveryID)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.identityCalls != 2 || client.fileCalls != 1 {
		t.Fatalf("identity calls = %d, file calls = %d", client.identityCalls, client.fileCalls)
	}
	if client.commentCalls != 0 || client.checkCalls != 0 {
		t.Fatal("racing delivery published mixed-generation output")
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
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	response := httptest.NewRecorder()

	handler.Routes().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if client.identityCalls != 0 || client.fileCalls != 0 || client.commentCalls != 0 || client.checkCalls != 0 {
		t.Fatal("GitHub API was called for an unauthenticated webhook")
	}
}

func performPullRequestWebhook(t *testing.T, secret []byte, client *fakeGitHubClient, delivery string) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := NewHandler(secret, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{
		"action":"opened",
		"installation":{"id":123},
		"repository":{"full_name":"globulario/example","name":"example","owner":{"login":"globulario"}},
		"pull_request":{"number":7,"base":{"sha":"` + baseSHA + `"},"head":{"sha":"` + headSHA + `","repo":{"full_name":"globulario/example"}}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, req)
	return response
}

func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
