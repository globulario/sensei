package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetPullRequestIdentity(t *testing.T) {
	client, server, requests := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/9/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"token","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/globulario/example/pulls/4":
			_, _ = w.Write([]byte(`{"base":{"sha":"base-sha"},"head":{"sha":"head-sha"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	})
	defer server.Close()

	identity, err := client.GetPullRequestIdentity(context.Background(), 9, "globulario", "example", 4)
	if err != nil {
		t.Fatal(err)
	}
	if identity.BaseSHA != "base-sha" || identity.HeadSHA != "head-sha" {
		t.Fatalf("identity = %#v", identity)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestUpsertIssueCommentUpdatesExistingOwnedMarker(t *testing.T) {
	client, server, requests := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/9/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"token","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/globulario/example/issues/4/comments":
			_, _ = w.Write([]byte(`[{"id":88,"body":"<!-- sensei-architectural-briefing -->\nold","performed_via_github_app":{"id":123}}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/globulario/example/issues/comments/88":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["body"] != "<!-- sensei-architectural-briefing -->\nnew" {
				t.Fatalf("body = %q", payload["body"])
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	})
	defer server.Close()

	err := client.UpsertIssueComment(context.Background(), 9, "globulario", "example", 4, "<!-- sensei-architectural-briefing -->", "<!-- sensei-architectural-briefing -->\nnew")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
}

func TestUpsertIssueCommentDoesNotOverwriteSpoofedMarker(t *testing.T) {
	client, server, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/9/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"token","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/globulario/example/issues/4/comments":
			_, _ = w.Write([]byte(`[{"id":88,"body":"<!-- sensei-architectural-briefing -->\nuser text","performed_via_github_app":null}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/globulario/example/issues/4/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	})
	defer server.Close()

	if err := client.UpsertIssueComment(context.Background(), 9, "globulario", "example", 4, "<!-- sensei-architectural-briefing -->", "<!-- sensei-architectural-briefing -->\nnew"); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertCheckRunUpdatesMatchingOwnedExternalID(t *testing.T) {
	client, server, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/9/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"token","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/globulario/example/commits/abc/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"id":55,"external_id":"sensei-pr-4-abc","app":{"id":123}}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/globulario/example/check-runs/55":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["external_id"] != "sensei-pr-4-abc" {
				t.Fatalf("external_id = %#v", payload["external_id"])
			}
			if _, exists := payload["head_sha"]; exists {
				t.Fatal("update payload unexpectedly contains head_sha")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	})
	defer server.Close()

	if err := client.UpsertCheckRun(context.Background(), 9, "globulario", "example", "abc", "sensei-pr-4-abc", "summary", "text"); err != nil {
		t.Fatal(err)
	}
}

func testClient(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*Client, *httptest.Server, *atomic.Int32) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/app/installations/9/access_tokens" {
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			if !strings.Contains(r.Header.Get("Accept"), "github") {
				t.Fatalf("accept = %q", r.Header.Get("Accept"))
			}
		}
		handler(w, r)
	}))
	auth, err := NewAuthenticator("123", privateKeyPEM, server.URL, server.Client())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	client, err := NewClient(auth, server.URL, server.Client())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server, &requests
}
