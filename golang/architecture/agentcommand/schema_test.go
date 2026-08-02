// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedMutationPlanSchemaMatchesCanonicalSource(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	canonical := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "schemas", "agentcommand", "v1", MutationPlanSchemaFilename)
	canonicalBytes, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	embeddedBytes, err := embeddedSchemas.ReadFile("schemas/" + MutationPlanSchemaFilename)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalBytes) != string(embeddedBytes) {
		t.Fatal("embedded mutation-plan schema drifted from docs/schemas/agentcommand/v1")
	}
}

func TestMutationPlanSchemaAcceptsCanonicalPlanAndRejectsAuthorityField(t *testing.T) {
	plan := finalizedPlan(t, []MutationOperation{{
		OperationID: "op-1",
		Kind:        MutationWrite,
		Path:        "a.txt",
		Content:     []byte("hello"),
	}})
	if err := ValidateMutationPlanSchema(plan); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["merge_pull_request"] = true
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MutationPlan
	if err := json.Unmarshal(mutated, &decoded); err != nil {
		t.Fatal(err)
	}
	// The typed value cannot retain the invented field. Prove the raw schema
	// rejects it directly as well.
	if err := compileMutationPlanSchema(); err != nil {
		t.Fatal(err)
	}
	var instance any
	if err := json.Unmarshal(mutated, &instance); err != nil {
		t.Fatal(err)
	}
	if err := mutationPlanSchema.Validate(instance); err == nil {
		t.Fatal("schema accepted invented merge authority")
	}
}
