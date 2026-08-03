// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredVendorProfilesReuseConfinedCommandConfiguration(t *testing.T) {
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	base := CommandAgentConfig{
		Command:                   command,
		EnvironmentAllowlist:      []string{"EXPLICIT_ONLY"},
		MaxStdoutBytes:            4096,
		MaxStderrBytes:            4096,
		MaxStructuredPayloadBytes: 2048,
	}
	codexConfig := base
	codexConfig.WorkDir = t.TempDir()
	codexAgent, err := NewCodexStructuredAgent(codexConfig)
	if err != nil {
		t.Fatal(err)
	}
	codex := codexAgent.(*commandAgent)
	wantCodex := []string{"exec", "--sandbox", "read-only", "--skip-git-repo-check", "-"}
	if !reflect.DeepEqual(codex.config.Args, wantCodex) {
		t.Fatalf("Codex structured argv=%#v, want %#v", codex.config.Args, wantCodex)
	}

	claudeConfig := base
	claudeConfig.WorkDir = t.TempDir()
	claudeAgent, err := NewClaudeStructuredAgent(claudeConfig)
	if err != nil {
		t.Fatal(err)
	}
	claude := claudeAgent.(*commandAgent)
	joined := strings.Join(claude.config.Args, " ")
	for _, required := range []string{"-p", "--output-format json", "--max-turns 1", "--permission-mode plan", "--disallowedTools"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Claude structured argv %q lacks %q", joined, required)
		}
	}
	if codex.config.WorkDir == claude.config.WorkDir {
		t.Fatal("structured profiles unexpectedly share a work directory")
	}
	if !reflect.DeepEqual(codex.config.EnvironmentAllowlist, []string{"EXPLICIT_ONLY"}) ||
		!reflect.DeepEqual(claude.config.EnvironmentAllowlist, []string{"EXPLICIT_ONLY"}) {
		t.Fatal("structured profiles changed the explicit environment allowlist")
	}
}

func TestClaudeEnvelopeRejectsDuplicateResultKeys(t *testing.T) {
	agent := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	_, err := agent.extractFinalPayload([]byte(`{"result":"first","result":"second"}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate object key") || !strings.Contains(err.Error(), "result") {
		t.Fatalf("err=%v", err)
	}
}

// TestClaudeEnvelopeStripsWholeMessageCodeFence covers a real failure
// observed driving an actual `claude` CLI subprocess: it fenced its JSON
// answer in Markdown (```json ... ```) despite the O8 planning prompt asking
// for exactly one JSON object and nothing else, which broke the downstream
// JSON parse on the leading backtick.
func TestClaudeEnvelopeStripsWholeMessageCodeFence(t *testing.T) {
	agent := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	payload, err := agent.extractFinalPayload(marshalClaudeEnvelope(t, "```json\n{\"a\":1}\n```"))
	if err != nil {
		t.Fatalf("extractFinalPayload: %v", err)
	}
	if string(payload) != `{"a":1}` {
		t.Fatalf("payload = %q, want %q", payload, `{"a":1}`)
	}
}

func TestClaudeEnvelopeStripsPlainCodeFenceWithoutLanguageTag(t *testing.T) {
	agent := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	payload, err := agent.extractFinalPayload(marshalClaudeEnvelope(t, "```\n{\"a\":1}\n```"))
	if err != nil {
		t.Fatalf("extractFinalPayload: %v", err)
	}
	if string(payload) != `{"a":1}` {
		t.Fatalf("payload = %q, want %q", payload, `{"a":1}`)
	}
}

func TestClaudeEnvelopeLeavesUnfencedResultUnchanged(t *testing.T) {
	agent := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	payload, err := agent.extractFinalPayload(marshalClaudeEnvelope(t, `{"a":1}`))
	if err != nil {
		t.Fatalf("extractFinalPayload: %v", err)
	}
	if string(payload) != `{"a":1}` {
		t.Fatalf("payload = %q, want %q", payload, `{"a":1}`)
	}
}

func marshalClaudeEnvelope(t *testing.T, result string) []byte {
	t.Helper()
	envelope, err := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: result})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

// TestClaudeEnvelopeDoesNotStripFenceAlongsideOtherText guards the
// conservative half of stripWholeMessageCodeFence: a fence that does not
// span the entire message is left alone -- passed through verbatim so the
// caller's own JSON parse fails on it, rather than guessed at here.
func TestClaudeEnvelopeDoesNotStripFenceAlongsideOtherText(t *testing.T) {
	agent := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	const resultText = "Here is the plan:\n```json\n{\"a\":1}\n```"
	payload, err := agent.extractFinalPayload(marshalClaudeEnvelope(t, resultText))
	if err != nil {
		t.Fatalf("extractFinalPayload: %v", err)
	}
	if string(payload) != resultText {
		t.Fatalf("payload = %q, want unchanged %q", payload, resultText)
	}
}
