// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestMCPAdmissionToolsAreRegistered(t *testing.T) {
	b := &bridge{}
	seen := map[string]bool{}
	for _, tool := range b.tools() {
		seen[tool.Name] = true
	}
	if !seen["admit_change"] {
		t.Fatal("admit_change MCP tool is not registered")
	}
	if !seen["verify_admission"] {
		t.Fatal("verify_admission MCP tool is not registered")
	}
}

func TestMCPAdmissionToolsDoNotRequireProtoChanges(t *testing.T) {
	b := &bridge{}
	for _, tool := range b.tools() {
		if tool.Name != "admit_change" && tool.Name != "verify_admission" {
			continue
		}
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["sparql"]; ok {
			t.Fatalf("%s exposed a raw graph query field", tool.Name)
		}
		if _, ok := props["source_patch"]; ok {
			t.Fatalf("%s exposed a source-edit field", tool.Name)
		}
	}
}
