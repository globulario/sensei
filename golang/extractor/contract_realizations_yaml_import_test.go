// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// TestRealizesContract_AuthoritativeEmitsForwardAndReverse: an authoritative
// realization emits BOTH impl --realizesContract--> arch and the reverse
// arch --realizedByContract--> impl, so the spine traverses in both directions.
func TestRealizesContract_AuthoritativeEmitsForwardAndReverse(t *testing.T) {
	root := makeDir(t, map[string]string{
		"contract_realizations.yaml": `
contract_realizations:
  realizations:
    - implementation: contract.grpc.config.set
      realizes: contract.config_mutation_requires_valid_token
      source: manual
      confidence: high
      evidence:
        - handler validates a token before mutating configuration
`,
	})
	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	impl := rdf.MintIRI(rdf.ClassContract, "contract.grpc.config.set")
	arch := rdf.MintIRI(rdf.ClassContract, "contract.config_mutation_requires_valid_token")

	forward := impl + " " + rdf.IRI(rdf.PropRealizesContract) + " " + arch + " ."
	if !strings.Contains(out, forward) {
		t.Errorf("missing authoritative forward edge:\n  want: %s", forward)
	}
	reverse := arch + " " + rdf.IRI(rdf.PropRealizedByContract) + " " + impl + " ."
	if !strings.Contains(out, reverse) {
		t.Errorf("missing reverse (arch->impl) edge:\n  want: %s", reverse)
	}
	if strings.Contains(out, impl+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassContract)+" .") {
		t.Error("authoritative realization import must not auto-type the implementation contract")
	}
	if strings.Contains(out, arch+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassContract)+" .") {
		t.Error("authoritative realization import must not auto-type the architectural contract")
	}

	evidenceIRI := rdf.MintIRI(rdf.ClassEvidence, "contract_realization.accepted.contract.grpc.config.set__contract.config_mutation_requires_valid_token")
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassEvidence)+" .") {
		t.Errorf("missing realization evidence node:\n  want subject: %s", evidenceIRI)
	}
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropSourceKind)+` "manual"`) {
		t.Error("missing realization evidence source kind")
	}
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropConfidence)+` "high"`) {
		t.Error("missing realization evidence confidence")
	}
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropPromotionStatus)+` "accepted"`) {
		t.Error("missing realization evidence promotion status")
	}
	if !strings.Contains(out, impl+" "+rdf.IRI(rdf.PropSupportedByEvidence)+" "+evidenceIRI+" .") {
		t.Error("implementation contract missing supportedByEvidence link")
	}
	if !strings.Contains(out, arch+" "+rdf.IRI(rdf.PropSupportedByEvidence)+" "+evidenceIRI+" .") {
		t.Error("architectural contract missing supportedByEvidence link")
	}
}

// TestCandidateRealizes_IsNotAuthoritative: a candidate (the shape a path/name
// overlap generator produces) emits ONLY candidateRealizesContract — never
// realizesContract and never the authoritative reverse. Path overlap alone must
// not become a realized promise.
func TestCandidateRealizes_IsNotAuthoritative(t *testing.T) {
	root := makeDir(t, map[string]string{
		"contract_realizations.yaml": `
contract_realizations:
  candidates:
    - implementation: contract.grpc.workflow.reconcile
      realizes: contract.workflow.foreach_guard_order
      source: path_overlap
      confidence: low
      evidence:
        - same component
        - matching service name
`,
	})
	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	impl := rdf.MintIRI(rdf.ClassContract, "contract.grpc.workflow.reconcile")
	arch := rdf.MintIRI(rdf.ClassContract, "contract.workflow.foreach_guard_order")

	candidate := impl + " " + rdf.IRI(rdf.PropCandidateRealizesContract) + " " + arch + " ."
	if !strings.Contains(out, candidate) {
		t.Errorf("missing candidate edge:\n  want: %s", candidate)
	}
	// The guardrail: a candidate is NOT authoritative.
	if strings.Contains(out, rdf.IRI(rdf.PropRealizesContract)) {
		t.Error("candidate must NOT emit authoritative aw:realizesContract")
	}
	if strings.Contains(out, rdf.IRI(rdf.PropRealizedByContract)) {
		t.Error("candidate must NOT emit the authoritative reverse aw:realizedByContract")
	}

	evidenceIRI := rdf.MintIRI(rdf.ClassEvidence, "contract_realization.candidate.contract.grpc.workflow.reconcile__contract.workflow.foreach_guard_order")
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropPromotionStatus)+` "candidate"`) {
		t.Error("candidate realization evidence missing candidate promotion status")
	}
	if !strings.Contains(out, evidenceIRI+" "+rdf.IRI(rdf.PropConfidence)+` "low"`) {
		t.Error("candidate realization evidence missing confidence")
	}
}
