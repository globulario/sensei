// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
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
	wantCodex := []string{"exec", "--sandbox", "read-only", "--ask-for-approval", "never", "--skip-git-repo-check", "-"}
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
