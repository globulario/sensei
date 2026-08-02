// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func TestEmbeddedPlanProposalSchemaMatchesCanonicalSource(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "schemas", "cognitivecommand", "v1")
	canonical, err := os.ReadFile(filepath.Join(root, PlanProposalSchemaFilename))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := PlanProposalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("embedded %s drifted from canonical source", PlanProposalSchemaFilename)
	}
}

func TestPlanProposalSchemaRejectsIdentityAndAuthorityFields(t *testing.T) {
	plan := PlanProposal{
		SchemaVersion:  PlanProposalSchemaVersion,
		Steps:          []synthesis.PlanStep{},
		Assumptions:    []string{},
		Risks:          []string{},
		StopConditions: []string{},
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanProposalSchema(planData); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(planData, &document); err != nil {
		t.Fatal(err)
	}
	document["plan_generation"] = 999
	mutatedPlan, _ := json.Marshal(document)
	if err := ValidatePlanProposalSchema(mutatedPlan); err == nil {
		t.Fatal("plan proposal schema accepted Go-owned plan generation")
	}
}
