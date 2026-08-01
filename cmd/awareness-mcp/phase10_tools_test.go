// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"
)

func TestPhase10ToolsAreRegisteredAndReadOnly(t *testing.T) {
	b := testBridge(fakeClient{})
	want := map[string]bool{"awareness_investigate": false, "awareness_evidence_coverage": false, "awareness_candidates": false, "awareness_challenge": false}
	for _, tool := range b.tools() {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing Phase 10.7 MCP tool %s", name)
		}
	}
}

func TestPhase10ToolArgumentsFailClosed(t *testing.T) {
	b := testBridge(fakeClient{})
	for _, name := range []string{"awareness_investigate", "awareness_evidence_coverage", "awareness_candidates", "awareness_challenge"} {
		if _, err := b.callTool(context.Background(), name, map[string]interface{}{}); err == nil {
			t.Fatalf("%s accepted missing required arguments", name)
		}
	}
}
