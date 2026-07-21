// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

func evidenceDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

const sampleRuntimeEvidence = `
runtime_evidence:
  - id: evidence.example
    label: Example evidence
    status: active
    observed_from_service: example-owner
    observed_via_paths:
      - example owner RPC
    trust_level: high
    freshness_window: current sweep only
    must_come_from_owner_path: true
    cannot_promote_to_pass_when_stale: true
    evidence_for_authority_domains:
      - authority_domain:authority.example
    evidence_for_invariants:
      - invariant:example.invariant
    evidence_for_repair_plans:
      - repair_plan:globular.repair.example
`

func TestRuntimeEvidenceImporter(t *testing.T) {
	out, report := evidenceDir(t, map[string]string{"e.yaml": sampleRuntimeEvidence})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "runtime_evidence" {
		t.Errorf("schema: want runtime_evidence, got %q", got)
	}
	subj := rdf.MintIRI(rdf.ClassRuntimeEvidence, "evidence.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassRuntimeEvidence)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropObservedFromService)+` "example-owner"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropObservedViaPath)+` "example owner RPC"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasTrustLevel)+` "high"`)
}

// The freshness/owner-path rule must be represented so consumers can enforce it.
func TestRuntimeEvidenceRequiresFreshness(t *testing.T) {
	out, _ := evidenceDir(t, map[string]string{"e.yaml": sampleRuntimeEvidence})
	subj := rdf.MintIRI(rdf.ClassRuntimeEvidence, "evidence.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasFreshnessWindow)+` "current sweep only"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustComeFromOwnerPath)+` "true"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCannotPromoteToPassWhenStale)+` "true"`)
}

// Evidence links to invariants / authority domains / repair plans but must NOT
// type those targets — it describes the contract, it is not the authority.
func TestRuntimeEvidenceDoesNotBecomeAuthority(t *testing.T) {
	out, _ := evidenceDir(t, map[string]string{"e.yaml": sampleRuntimeEvidence})
	subj := rdf.MintIRI(rdf.ClassRuntimeEvidence, "evidence.example")

	inv := rdf.MintIRI(rdf.ClassInvariant, "example.invariant")
	ad := rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.example")
	rp := rdf.MintIRI(rdf.ClassRepairPlan, "globular.repair.example")

	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropEvidenceForInvariant)+" "+inv+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropEvidenceForAuthorityDomain)+" "+ad+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropEvidenceForRepairPlan)+" "+rp+" .")

	// Targets are linked, never typed by this importer.
	mustNotContain(t, out, inv+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassInvariant))
	mustNotContain(t, out, ad+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassAuthorityDomain))
}
