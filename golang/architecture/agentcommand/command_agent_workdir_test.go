// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandAgentRefusesNonEmptyWorkDirectory(t *testing.T) {
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "ambient.txt"), []byte("must not be visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = NewCodexAgent(CommandAgentConfig{
		Command:              command,
		WorkDir:              workDir,
		MaxStdoutBytes:       1024,
		MaxStderrBytes:       1024,
		MaxMutationPlanBytes: 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("error = %v", err)
	}
}

func TestCommandAgentRechecksWorkDirectoryBeforeExecution(t *testing.T) {
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	agentValue, err := NewCodexAgent(CommandAgentConfig{
		Command:              command,
		WorkDir:              workDir,
		MaxStdoutBytes:       1024,
		MaxStderrBytes:       1024,
		MaxMutationPlanBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "late-ambient.txt"), []byte("must not be visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = agentValue.Generate(t.Context(), GenerationPrompt{SchemaVersion: GenerationPromptSchemaVersion}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("error = %v", err)
	}
}
