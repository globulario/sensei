// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedProposalSchemasMatchCanonicalSources(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "schemas", "cognitivecommand", "v1")
	for _, filename := range []string{InterpretationProposalSchemaFilename, PlanProposalSchemaFilename} {
		canonical, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := embeddedSchemas.ReadFile("schemas/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		if string(canonical) != string(embedded) {
			t.Fatalf("embedded %s drifted from canonical source", filename)
		}
	}
}

func TestProposalSchemasRejectIdentityAndAuthorityFields(t *testing.T) {
	interpretation := InterpretationProposal{
		SchemaVersion:             InterpretationProposalSchemaVersion,
		ApplicableIntent:          []string{},
		BindingInvariants:         []string{},
		RelevantContracts:        []string{},
		AuthorityBoundaries:      []string{},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{},
		RequiredProofObligations: []string{},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{},
		Limitations:              []synthesis.Limitation{},
	}
	data, err := json.Marshal(interpretation)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInterpretationProposalSchema(data); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["admission_decision"] = "admitted"
	mutated, _ := json.Marshal(document)
	if err := ValidateInterpretationProposalSchema(mutated); err == nil {
		t.Fatal("interpretation proposal schema accepted admission authority")
	}

	plan := PlanProposal{
		SchemaVersion: PlanProposalSchemaVersion,
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
	if err := json.Unmarshal(planData, &document); err != nil {
		t.Fatal(err)
	}
	document["plan_generation"] = 999
	mutatedPlan, _ := json.Marshal(document)
	if err := ValidatePlanProposalSchema(mutatedPlan); err == nil {
		t.Fatal("plan proposal schema accepted Go-owned plan generation")
	}
}
