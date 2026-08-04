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

func TestCodexAndClaudeProfilesUseBoundedDirectArgv(t *testing.T) {
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	base := CommandAgentConfig{
		Command:              command,
		WorkDir:              t.TempDir(),
		EnvironmentAllowlist: []string{"EXPLICIT_ONLY"},
		MaxStdoutBytes:       4096,
		MaxStderrBytes:       4096,
		MaxMutationPlanBytes: 2048,
	}
	codexAgent, err := NewCodexAgent(base)
	if err != nil {
		t.Fatal(err)
	}
	codex := codexAgent.(*commandAgent)
	wantCodex := []string{"exec", "--sandbox", "read-only", "--skip-git-repo-check", "-"}
	if !reflect.DeepEqual(codex.config.Args, wantCodex) {
		t.Fatalf("Codex argv = %#v, want %#v", codex.config.Args, wantCodex)
	}
	if strings.Join(codex.config.Args, " ") == "" || strings.Contains(strings.Join(codex.config.Args, " "), "bash -c") {
		t.Fatal("Codex profile introduced a shell command")
	}

	claudeAgent, err := NewClaudeAgent(base)
	if err != nil {
		t.Fatal(err)
	}
	claude := claudeAgent.(*commandAgent)
	joined := strings.Join(claude.config.Args, " ")
	for _, required := range []string{"-p", "--output-format json", "--max-turns 1", "--permission-mode plan", "--disallowedTools"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Claude argv %q does not contain %q", joined, required)
		}
	}
	for _, forbiddenTool := range []string{"Bash", "Edit", "Write", "Read", "Glob", "Grep", "WebFetch", "WebSearch"} {
		if !strings.Contains(joined, forbiddenTool) {
			t.Fatalf("Claude argv %q does not deny %q", joined, forbiddenTool)
		}
	}
}

func TestVendorEnvelopeExtractionAndClosedMutationPlan(t *testing.T) {
	proposal := mutationPlanProposal{
		SchemaVersion: MutationPlanSchemaVersion,
		Summary:       "write one file",
		Operations: []MutationOperation{{
			OperationID: "op-1",
			Kind:        MutationWrite,
			Path:        "a.txt",
			Content:     []byte("hello\n"),
		}},
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}

	codex := &commandAgent{config: CommandAgentConfig{Profile: ProfileCodex}}
	codexPayload, err := codex.extractFinalPayload(append(payload, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	codexPlan, err := decodeMutationPlanProposal(codexPayload)
	if err != nil {
		t.Fatal(err)
	}
	if codexPlan.MutationPlanDigestSHA256 == "" {
		t.Fatal("Codex plan was not assigned a Go-owned digest")
	}

	claudeEnvelope, err := json.Marshal(map[string]any{
		"result":     string(payload),
		"session_id": "untrusted-vendor-metadata",
		"total_cost": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	claude := &commandAgent{config: CommandAgentConfig{Profile: ProfileClaude}}
	claudePayload, err := claude.extractFinalPayload(claudeEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	claudePlan, err := decodeMutationPlanProposal(claudePayload)
	if err != nil {
		t.Fatal(err)
	}
	if claudePlan.MutationPlanDigestSHA256 != codexPlan.MutationPlanDigestSHA256 {
		t.Fatal("vendor envelopes changed the semantic mutation-plan identity")
	}

	unknown := strings.TrimSuffix(string(payload), "}") + `,"shell_command":"rm -rf /"}`
	if _, err := decodeMutationPlanProposal([]byte(unknown)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func TestAgentPromptContainsNoFilesystemAuthority(t *testing.T) {
	prompt := GenerationPrompt{
		SchemaVersion:       GenerationPromptSchemaVersion,
		RequestDigestSHA256: strings.Repeat("a", 64),
		RepositoryDomain:    "github.com/globulario/sensei",
		BaseRevision:        strings.Repeat("b", 40),
		SnapshotFiles: []SnapshotFile{{
			Path:    "a.txt",
			Content: []byte("hello"),
		}},
	}
	encoded, err := encodeAgentPrompt(prompt)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"candidate-buffer", "repository_root", "worktree_path", t.TempDir()} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("prompt leaked filesystem authority %q", forbidden)
		}
	}
	if !strings.Contains(text, "a.txt") || !strings.Contains(text, "GENERATION_PROMPT_JSON") {
		t.Fatal("prompt omitted governed input")
	}
}

// TestAgentPromptStatesModeConstEmptyRule guards against a real failure
// observed driving an actual `claude` CLI subprocess for O3 generation: asked
// only for "Modes: regular or executable" with no further qualification, it
// produced kind="write" with mode="regular", which
// agentcommand-mutation-plan-v1.schema.json correctly rejects (mode must be
// the empty string for every kind except set-mode). The prompt must state
// that qualification explicitly so a real agent isn't invited into
// schema-invalid output for the common (non-set-mode) case.
func TestAgentPromptStatesModeConstEmptyRule(t *testing.T) {
	encoded, err := encodeAgentPrompt(GenerationPrompt{
		SchemaVersion:       GenerationPromptSchemaVersion,
		RequestDigestSHA256: strings.Repeat("a", 64),
		RepositoryDomain:    "github.com/globulario/sensei",
		BaseRevision:        strings.Repeat("b", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, "set-mode") || !strings.Contains(strings.ToLower(text), `mode must be the empty string`) {
		t.Fatalf("prompt does not state the mode-const-empty rule for non-set-mode kinds: %s", text)
	}
}
