// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DrafterBackend is the configured LLM transport for model-backed drafting.
type DrafterBackend string

const (
	DrafterAuto      DrafterBackend = "auto"
	DrafterEcho      DrafterBackend = "echo"
	DrafterLLM       DrafterBackend = "llm"
	DrafterClaudeCLI DrafterBackend = "claude-cli"
	DrafterCodexCLI  DrafterBackend = "codex-cli"
)

// BackendReceipt records how an LLM client was selected. It deliberately never
// carries credential values.
type BackendReceipt struct {
	Drafter                     DrafterBackend
	CredentialSource            string
	Model                       string
	DirectAPIEnvironmentIgnored bool
}

// DirectAPICredentialConfigured reports whether the direct Messages API path has
// any configured credential. It is capability detection only; it does not prove
// the credential is valid.
func DirectAPICredentialConfigured() bool {
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" ||
		strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")) != ""
}

// SelectLLMClient returns an LLM client for the requested backend. Explicit
// backends fail clearly and never fall back. Auto prefers CLI auth brokers when
// installed, then direct API credentials, then returns ErrNoLLMConfig.
func SelectLLMClient(backend DrafterBackend, model string) (LLMClient, BackendReceipt, error) {
	switch backend {
	case "", DrafterAuto:
		if client, err := NewClaudeCLIClient(model); err == nil {
			return client, BackendReceipt{
				Drafter:                     DrafterClaudeCLI,
				CredentialSource:            "claude_cli_login",
				Model:                       client.Model,
				DirectAPIEnvironmentIgnored: DirectAPICredentialConfigured(),
			}, nil
		} else if !errors.Is(err, ErrNoClaudeCLI) {
			return nil, BackendReceipt{}, err
		}
		if client, err := NewCodexCLIClient(model); err == nil {
			return client, BackendReceipt{
				Drafter:                     DrafterCodexCLI,
				CredentialSource:            "codex_cli_login",
				Model:                       client.Model,
				DirectAPIEnvironmentIgnored: DirectAPICredentialConfigured(),
			}, nil
		} else if !errors.Is(err, ErrNoCodexCLI) {
			return nil, BackendReceipt{}, err
		}
		if DirectAPICredentialConfigured() {
			client, err := NewAnthropicClientFromEnv(model)
			if err != nil {
				return nil, BackendReceipt{}, err
			}
			return client, BackendReceipt{
				Drafter:          DrafterLLM,
				CredentialSource: directCredentialSource(),
				Model:            client.Model,
			}, nil
		}
		return nil, BackendReceipt{}, ErrNoLLMConfig
	case DrafterLLM:
		client, err := NewAnthropicClientFromEnv(model)
		if err != nil {
			return nil, BackendReceipt{}, err
		}
		return client, BackendReceipt{
			Drafter:          DrafterLLM,
			CredentialSource: directCredentialSource(),
			Model:            client.Model,
		}, nil
	case DrafterClaudeCLI:
		client, err := NewClaudeCLIClient(model)
		if err != nil {
			return nil, BackendReceipt{}, err
		}
		return client, BackendReceipt{
			Drafter:                     DrafterClaudeCLI,
			CredentialSource:            "claude_cli_login",
			Model:                       client.Model,
			DirectAPIEnvironmentIgnored: DirectAPICredentialConfigured(),
		}, nil
	case DrafterCodexCLI:
		client, err := NewCodexCLIClient(model)
		if err != nil {
			return nil, BackendReceipt{}, err
		}
		return client, BackendReceipt{
			Drafter:                     DrafterCodexCLI,
			CredentialSource:            "codex_cli_login",
			Model:                       client.Model,
			DirectAPIEnvironmentIgnored: DirectAPICredentialConfigured(),
		}, nil
	default:
		return nil, BackendReceipt{}, fmt.Errorf("unknown LLM backend %q", backend)
	}
}

func directCredentialSource() string {
	if strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")) != "" {
		return "anthropic_auth_token"
	}
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
		return "anthropic_api_key"
	}
	return ""
}

func (r BackendReceipt) Label() string {
	if r.Model == "" {
		return string(r.Drafter)
	}
	return string(r.Drafter) + ":" + r.Model
}
