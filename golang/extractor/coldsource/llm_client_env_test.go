// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAnthropicClient_EnvWiring verifies the ANTHROPIC_BASE_URL override and the
// two mutually exclusive auth schemes (x-api-key vs Authorization: Bearer). It
// runs entirely against an httptest server, so it needs no real credential.
func TestAnthropicClient_EnvWiring(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		authToken string
		wantXKey  string
		wantBear  string
	}{
		{name: "api key -> x-api-key", apiKey: "sk-ant-api-test", wantXKey: "sk-ant-api-test"},
		{name: "auth token -> bearer", authToken: "oauth-abc", wantBear: "Bearer oauth-abc"},
		{name: "auth token wins over api key", apiKey: "sk-ant-api-test", authToken: "oauth-abc", wantBear: "Bearer oauth-abc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotXKey, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotXKey = r.Header.Get("x-api-key")
				gotAuth = r.Header.Get("authorization")
				_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
			}))
			defer srv.Close()

			// Point the env-built client at the fake server.
			t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
			t.Setenv("ANTHROPIC_API_KEY", tc.apiKey)
			t.Setenv("ANTHROPIC_AUTH_TOKEN", tc.authToken)

			c, err := NewAnthropicClientFromEnv("")
			if err != nil {
				t.Fatalf("NewAnthropicClientFromEnv: %v", err)
			}
			if c.BaseURL != srv.URL {
				t.Fatalf("BaseURL = %q, want override %q", c.BaseURL, srv.URL)
			}

			out, err := c.Complete(context.Background(), LLMRequest{User: "hi"})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if out != "ok" {
				t.Fatalf("Complete text = %q, want %q", out, "ok")
			}
			if gotPath != "/v1/messages" {
				t.Fatalf("request path = %q, want /v1/messages", gotPath)
			}
			if gotXKey != tc.wantXKey {
				t.Fatalf("x-api-key = %q, want %q", gotXKey, tc.wantXKey)
			}
			if gotAuth != tc.wantBear {
				t.Fatalf("authorization = %q, want %q", gotAuth, tc.wantBear)
			}
		})
	}
}

// TestNewAnthropicClientFromEnv_RequiresCredential verifies the no-silent-fallback
// contract: with neither credential set, construction fails with ErrNoLLMConfig.
func TestNewAnthropicClientFromEnv_RequiresCredential(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := NewAnthropicClientFromEnv(""); err != ErrNoLLMConfig {
		t.Fatalf("err = %v, want ErrNoLLMConfig", err)
	}
}
