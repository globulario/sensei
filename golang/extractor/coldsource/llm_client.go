// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultModel is the model the LLM drafter uses unless overridden. Per the
// Claude API guidance, default to the most capable current model.
const DefaultModel = "claude-opus-4-8"

// ErrNoLLMConfig is returned when an LLM drafter is requested but no credential
// is present. The CLI fails clearly on this — it never silently falls back.
var ErrNoLLMConfig = errors.New("no LLM configured: set ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) to use --drafter llm")

// LLMRequest is one structured completion request. Schema, when set, is a JSON
// Schema string passed via output_config.format so the model returns strict
// parseable JSON.
type LLMRequest struct {
	System    string
	User      string
	Schema    string // json_schema; empty means free-form text
	MaxTokens int
}

// LLMClient is the seam the LLM drafter calls. Behind an interface so the
// experiment can swap models/providers and so tests use a deterministic fake.
type LLMClient interface {
	Complete(ctx context.Context, req LLMRequest) (string, error)
}

// AnthropicClient is a minimal net/http client for the Messages API. It has no
// SDK dependency. It is only constructed when --drafter llm is requested.
//
// Auth is one of two mutually exclusive schemes, matching the official SDK /
// Claude Code env conventions:
//   - APIKey    → sent as the "x-api-key" header (Console API keys).
//   - AuthToken → sent as "Authorization: Bearer …" (gateways / proxies that
//     front the Messages API, e.g. LiteLLM, Bedrock/Vertex proxies, or a
//     subscription-translating proxy). Takes precedence when both are set.
//
// BaseURL defaults to the public API but is overridable via ANTHROPIC_BASE_URL
// so the client can be pointed at any Anthropic-compatible endpoint.
type AnthropicClient struct {
	APIKey    string
	AuthToken string
	Model     string
	BaseURL   string
	HTTP      *http.Client
}

// NewAnthropicClientFromEnv builds a client from the environment. It requires a
// credential — ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN — and returns
// ErrNoLLMConfig if neither is present; the caller must fail clearly, not fall
// back to a non-LLM drafter silently. ANTHROPIC_BASE_URL, when set, overrides
// the endpoint (useful for gateways/proxies).
func NewAnthropicClientFromEnv(model string) (*AnthropicClient, error) {
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	authToken := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if key == "" && authToken == "" {
		return nil, ErrNoLLMConfig
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &AnthropicClient{
		APIKey:    key,
		AuthToken: authToken,
		Model:     model,
		BaseURL:   baseURL,
		HTTP:      &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// ── wire types ───────────────────────────────────────────────────────────────

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicJSONFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type anthropicOutputConfig struct {
	Format *anthropicJSONFormat `json:"format,omitempty"`
}

type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       string                 `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete implements LLMClient against POST /v1/messages.
func (c *AnthropicClient) Complete(ctx context.Context, req LLMRequest) (string, error) {
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 4096
	}
	body := anthropicRequest{
		Model:     c.Model,
		MaxTokens: maxTok,
		System:    req.System,
		Messages:  []anthropicMessage{{Role: "user", Content: req.User}},
	}
	if strings.TrimSpace(req.Schema) != "" {
		body.OutputConfig = &anthropicOutputConfig{
			Format: &anthropicJSONFormat{Type: "json_schema", Schema: json.RawMessage(req.Schema)},
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("content-type", "application/json")
	if c.AuthToken != "" {
		httpReq.Header.Set("authorization", "Bearer "+c.AuthToken)
	} else {
		httpReq.Header.Set("x-api-key", c.APIKey)
	}
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("messages request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("messages API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var ar anthropicResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("messages API error %s: %s", ar.Error.Type, ar.Error.Message)
	}
	if ar.StopReason == "refusal" {
		return "", fmt.Errorf("model refused the request (stop_reason=refusal)")
	}
	// First text block is the answer (skips any thinking blocks).
	for _, b := range ar.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}
