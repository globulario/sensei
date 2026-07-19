// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

const sharedReceiptYAML = `
    status: machine_adopted
    promotion_status: machine_adopted
    assertion_origin: history_inferred
    epistemic_status: supported
    architectural_plane: historical
    decision_actor: sensei.knowledge_adoption
    decision_context: project_reconstruction
    decision_policy: adoption.test.v1
    decision_timestamp: "2026-07-14T09:00:00Z"
    valid_for_revision: 34dac209ffb6ef85cc78c5d217bbb7ad001d68fd
    valid_for_graph_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    review_status: not_human_reviewed
    adoption_basis: [independent evidence]
    source_receipts: [commit:abc, file:router.go]
    corroboration_kinds: [commit, source_file]
    revocation_conditions: [repository revision changes]
`

func assertFixtureEmitsAdoptionReceipt(t *testing.T, fixture string) {
	t.Helper()
	out, report := intentDir(t, map[string]string{"knowledge.yaml": fixture})
	assertValidNT(t, out)
	if len(report.Imported()) != 1 {
		t.Fatalf("expected one imported document, got %+v", report.Files)
	}
	for _, want := range []string{
		rdf.IRI(rdf.PropStatus) + ` "machine_adopted"`,
		rdf.IRI(rdf.PropDecisionActor) + ` "sensei.knowledge_adoption"`,
		rdf.IRI(rdf.PropDecisionContext) + ` "project_reconstruction"`,
		rdf.IRI(rdf.PropDecisionPolicy) + ` "adoption.test.v1"`,
		rdf.IRI(rdf.PropReviewStatus) + ` "not_human_reviewed"`,
		rdf.IRI(rdf.PropValidForGraphDigest) + ` "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
		rdf.IRI(rdf.PropSourcePath) + ` "commit:abc"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing adoption triple %s in:\n%s", want, out)
		}
	}
}

func TestInvariantEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "invariants:\n  - id: invariant.writer_monotonic\n    title: Writer state is monotonic\n"+sharedReceiptYAML)
}

func TestFailureModeEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "failure_modes:\n  - id: failure_mode.writer_reentry\n    title: Writer state regresses\n"+sharedReceiptYAML)
}

func TestForbiddenFixEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "forbidden_fixes:\n  - id: forbidden_fix.reset_writer_state\n    title: Reset writer state\n"+sharedReceiptYAML)
}

func TestBoundaryEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "boundaries:\n  - id: boundary.internal_bytesconv\n    title: Internal bytes conversion\n"+sharedReceiptYAML)
}

func TestDecisionEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "decisions:\n  - id: decision.writer_path\n    title: Keep one writer path\n"+sharedReceiptYAML)
}

func TestContractEmitsAdoptionReceipt(t *testing.T) {
	assertFixtureEmitsAdoptionReceipt(t, "contracts:\n  - id: contract.writer_state\n    name: Writer state contract\n"+sharedReceiptYAML)
}

func TestIncidentEmitsAdoptionReceipt(t *testing.T) {
	fixture := `
incident_id: incident.writer_regression
title: Writer state regression
status: machine_adopted
promotion_status: machine_adopted
assertion_origin: history_inferred
epistemic_status: supported
architectural_plane: historical
decision_actor: sensei.knowledge_adoption
decision_context: project_reconstruction
decision_policy: adoption.test.v1
decision_timestamp: "2026-07-14T09:00:00Z"
valid_for_revision: 34dac209ffb6ef85cc78c5d217bbb7ad001d68fd
valid_for_graph_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
review_status: not_human_reviewed
source_receipts: [commit:abc]
`
	assertFixtureEmitsAdoptionReceipt(t, fixture)
}
