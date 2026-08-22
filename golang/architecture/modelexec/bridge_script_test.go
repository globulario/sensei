// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// The shipped example bridge is a real deliverable, so its own logic is tested
// rather than assumed. A stub endpoint stands in for the model API: this proves
// the script's request construction, refusal handling and error handling are
// correct WITHOUT credentials, so the supplementary live smoke is the only
// thing left depending on an external service.
func bridgeScript(t *testing.T, apiURL string) *CommandProvider {
	t.Helper()
	for _, bin := range []string{"bash", "jq", "curl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available; the example bridge needs it", bin)
		}
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "model-bridge", "anthropic-bridge.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("example bridge missing: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key-not-a-real-credential")
	t.Setenv("ANTHROPIC_API_URL", apiURL)
	return &CommandProvider{ProviderID: "anthropic-bridge", ProviderVersion: "example-v1", Path: path}
}

func bridgeRequest() Request {
	return Request{
		SchemaVersion:    ArtifactSchemaVersion,
		RepositoryDomain: "github.com/example/repo",
		SuppliedEvidence: []SuppliedEvidence{{
			ID: "ev-1", DigestSHA256: sha, FilePath: "a.go", Excerpt: "func A() { B() }",
		}},
		Model: ModelIdentity{Name: "claude-opus-5", DigestAbsent: true},
	}
}

func TestExampleBridgeMapsAModelAnswerToAnArtifact(t *testing.T) {
	var seen map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seen)
		// The bridge must ask for structured output and send only bounded material.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"items\":[{\"kind\":\"candidate_claim\",\"text\":\"A calls B\",\"cited_evidence_ids\":[\"ev-1\"],\"file_paths\":[\"a.go\"]}]}"}]}`))
	}))
	defer srv.Close()

	p := bridgeScript(t, srv.URL)
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "anthropic-bridge", ModelName: "claude-opus-5"},
		Registry{"anthropic-bridge": p}, bridgeRequest())

	if out.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("status = %q (reason %q), want %q", out.Binding.Status, out.Binding.Reason, investigation.ModelStatusResolved)
	}
	if out.Artifact == nil || len(out.Artifact.Items) != 1 {
		t.Fatal("the bridge did not map the model answer into an artifact item")
	}

	// The request the bridge built must ask for a closed schema, and must not
	// smuggle material the envelope never carried.
	if seen["output_config"] == nil {
		t.Error("the bridge did not request structured output")
	}
	sys, _ := seen["system"].(string)
	if sys == "" {
		t.Error("the bridge sent no system contract")
	}
	blob, _ := json.Marshal(seen)
	for _, forbidden := range []string{"test-key-not-a-real-credential", "ANTHROPIC_API_KEY"} {
		if bytesContain(blob, forbidden) {
			t.Errorf("the bridge leaked %q into the request body", forbidden)
		}
	}
}

// A policy decline must arrive as a refusal, not an outage.
func TestExampleBridgePreservesRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},"content":[]}`))
	}))
	defer srv.Close()

	p := bridgeScript(t, srv.URL)
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "anthropic-bridge", ModelName: "claude-opus-5"},
		Registry{"anthropic-bridge": p}, bridgeRequest())
	if out.Binding.Status != investigation.ModelStatusRefused {
		t.Fatalf("status = %q, want %q", out.Binding.Status, investigation.ModelStatusRefused)
	}
}

// An API failure is errored, and the credential must not reach the diagnostic.
func TestExampleBridgeReportsApiFailureAsErrored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"credit balance too low"}}`))
	}))
	defer srv.Close()

	p := bridgeScript(t, srv.URL)
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "anthropic-bridge", ModelName: "claude-opus-5"},
		Registry{"anthropic-bridge": p}, bridgeRequest())
	if out.Binding.Status != investigation.ModelStatusErrored {
		t.Fatalf("status = %q, want %q", out.Binding.Status, investigation.ModelStatusErrored)
	}
	if out.Binding.ArtifactDigestSHA256 != "" {
		t.Error("a failed call carries an accepted artifact identity")
	}
}

func bytesContain(haystack []byte, needle string) bool {
	return len(needle) > 0 && len(haystack) > 0 && jsonContains(string(haystack), needle)
}

func jsonContains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
