// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"errors"
	"testing"
)

func TestSelectLLMClient_AutoPrefersClaudeCLIOverDirectAPIEnv(t *testing.T) {
	oldFind := findClaudeCLI
	oldFindCodex := findCodexCLI
	defer func() {
		findClaudeCLI = oldFind
		findCodexCLI = oldFindCodex
	}()
	findClaudeCLI = func() string { return "/tmp/fake-claude" }
	findCodexCLI = func() string { return "/tmp/fake-codex" }

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	client, receipt, err := SelectLLMClient(DrafterAuto, "")
	if err != nil {
		t.Fatalf("SelectLLMClient(auto): %v", err)
	}
	if _, ok := client.(*ClaudeCLIClient); !ok {
		t.Fatalf("auto selected %T, want ClaudeCLIClient", client)
	}
	if receipt.Drafter != DrafterClaudeCLI || receipt.CredentialSource != "claude_cli_login" {
		t.Fatalf("receipt = %+v, want claude-cli via CLI login", receipt)
	}
	if !receipt.DirectAPIEnvironmentIgnored {
		t.Fatalf("receipt should record ignored direct API environment: %+v", receipt)
	}
}

func TestSelectLLMClient_AutoUsesCodexCLIOverDirectAPIEnv(t *testing.T) {
	oldFind := findClaudeCLI
	oldFindCodex := findCodexCLI
	defer func() {
		findClaudeCLI = oldFind
		findCodexCLI = oldFindCodex
	}()
	findClaudeCLI = func() string { return "" }
	findCodexCLI = func() string { return "/tmp/fake-codex" }

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	client, receipt, err := SelectLLMClient(DrafterAuto, "")
	if err != nil {
		t.Fatalf("SelectLLMClient(auto): %v", err)
	}
	if _, ok := client.(*CodexCLIClient); !ok {
		t.Fatalf("auto selected %T, want CodexCLIClient", client)
	}
	if receipt.Drafter != DrafterCodexCLI || receipt.CredentialSource != "codex_cli_login" {
		t.Fatalf("receipt = %+v, want codex-cli via CLI login", receipt)
	}
	if !receipt.DirectAPIEnvironmentIgnored {
		t.Fatalf("receipt should record ignored direct API environment: %+v", receipt)
	}
}

func TestSelectLLMClient_ExplicitBackendsDoNotFallback(t *testing.T) {
	oldFind := findClaudeCLI
	oldFindCodex := findCodexCLI
	defer func() {
		findClaudeCLI = oldFind
		findCodexCLI = oldFindCodex
	}()
	findClaudeCLI = func() string { return "/tmp/fake-claude" }
	findCodexCLI = func() string { return "/tmp/fake-codex" }

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, _, err := SelectLLMClient(DrafterLLM, ""); !errors.Is(err, ErrNoLLMConfig) {
		t.Fatalf("explicit llm err = %v, want ErrNoLLMConfig", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-present")
	findClaudeCLI = func() string { return "" }
	if _, _, err := SelectLLMClient(DrafterClaudeCLI, ""); !errors.Is(err, ErrNoClaudeCLI) {
		t.Fatalf("explicit claude-cli err = %v, want ErrNoClaudeCLI", err)
	}

	findCodexCLI = func() string { return "" }
	if _, _, err := SelectLLMClient(DrafterCodexCLI, ""); !errors.Is(err, ErrNoCodexCLI) {
		t.Fatalf("explicit codex-cli err = %v, want ErrNoCodexCLI", err)
	}
}

func TestSelectLLMClient_AutoUsesAuthTokenAndReportsSource(t *testing.T) {
	oldFind := findClaudeCLI
	oldFindCodex := findCodexCLI
	defer func() {
		findClaudeCLI = oldFind
		findCodexCLI = oldFindCodex
	}()
	findClaudeCLI = func() string { return "" }
	findCodexCLI = func() string { return "" }

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-present")

	client, receipt, err := SelectLLMClient(DrafterAuto, "test-model")
	if err != nil {
		t.Fatalf("SelectLLMClient(auto): %v", err)
	}
	if _, ok := client.(*AnthropicClient); !ok {
		t.Fatalf("auto selected %T, want AnthropicClient", client)
	}
	if receipt.Drafter != DrafterLLM || receipt.CredentialSource != "anthropic_auth_token" {
		t.Fatalf("receipt = %+v, want direct API auth-token source", receipt)
	}
}

func TestSelectLLMClient_AutoNoBackend(t *testing.T) {
	oldFind := findClaudeCLI
	oldFindCodex := findCodexCLI
	defer func() {
		findClaudeCLI = oldFind
		findCodexCLI = oldFindCodex
	}()
	findClaudeCLI = func() string { return "" }
	findCodexCLI = func() string { return "" }

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, _, err := SelectLLMClient(DrafterAuto, ""); !errors.Is(err, ErrNoLLMConfig) {
		t.Fatalf("auto err = %v, want ErrNoLLMConfig", err)
	}
}
