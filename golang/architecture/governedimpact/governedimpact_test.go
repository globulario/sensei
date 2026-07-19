// SPDX-License-Identifier: Apache-2.0

package governedimpact

import (
	"errors"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
)

func iri(s, p, o string) graphsnapshot.Triple {
	return graphsnapshot.Triple{Subject: s, Predicate: p, Object: o, ObjectIsIRI: true}
}
func lit(s, p, o string) graphsnapshot.Triple {
	return graphsnapshot.Triple{Subject: s, Predicate: p, Object: o, ObjectIsIRI: false}
}

const oblig1 = `proof_obligations:
  - id: ob.1
    label: first
    evidence_lane: static_test
    template_kind: contract_test
    required_slots:
      - id: slot.1
        kind: static_test
`

// snap builds a snapshot; the graph digest is a real digest of the triples so it
// is a valid 64-hex and changes when the triples change.
func snap(triples []graphsnapshot.Triple, obligYAML, cert, comp string) Snapshot {
	s := Snapshot{
		GraphSemanticDigestSHA256: closureprotocol.MustSemanticDigest(triples),
		Triples:                   triples,
		CertificationPolicyID:     cert,
		CompletionPolicyID:        comp,
	}
	if obligYAML != "" {
		s.ProofObligationsBytes = []byte(obligYAML)
		s.ProofObligationsSemanticDigestSHA256 = closureprotocol.MustSemanticDigest(obligYAML)
	}
	return s
}

func impactByCategory(t *testing.T, rep Report, name string) closureprotocol.GovernedKnowledgeImpact {
	t.Helper()
	for _, im := range rep.Impacts {
		if im.Category == name {
			return im
		}
	}
	t.Fatalf("category %q not in report", name)
	return closureprotocol.GovernedKnowledgeImpact{}
}

func mustCompare(t *testing.T, base, result Snapshot) Report {
	t.Helper()
	rep, err := Compare(base, result)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	return rep
}

// A base invariant graph, reused as the unchanged baseline.
func baseTriples() []graphsnapshot.Triple {
	return []graphsnapshot.Triple{
		iri("aw:inv.1", rdf.PropType, rdf.ClassInvariant),
		lit("aw:inv.1", rdf.PropLabel, "no stale seed"),
		iri("aw:inv.1", rdf.PropRequiresTest, "aw:test.1"),
		iri("aw:test.1", rdf.PropType, rdf.ClassTest),
		iri("aw:auth.1", rdf.PropType, rdf.ClassAuthoritySurface),
		lit("aw:auth.1", rdf.PropLabel, "repo mutation"),
	}
}

func TestCompareIdentical(t *testing.T) {
	tr := baseTriples()
	rep := mustCompare(t, snap(tr, oblig1, "cert.v1", "comp.v1"), snap(tr, oblig1, "cert.v1", "comp.v1"))
	if len(rep.Impacts) != 10 || len(rep.BaseManifests) != 10 || len(rep.ResultManifests) != 10 {
		t.Fatalf("report not ten-wide: %d/%d/%d", len(rep.Impacts), len(rep.BaseManifests), len(rep.ResultManifests))
	}
	for _, im := range rep.Impacts {
		if closureprotocol.GovernedKnowledgeImpactChanged(im) {
			t.Fatalf("category %q changed on identical inputs", im.Category)
		}
		if len(im.ChangedRecordIDs) != 0 {
			t.Fatalf("category %q has changed ids on identical inputs", im.Category)
		}
	}
}

func TestReorderedTriplesUnchanged(t *testing.T) {
	a := baseTriples()
	b := []graphsnapshot.Triple{a[5], a[0], a[3], a[1], a[4], a[2]} // shuffled
	rep := mustCompare(t, snap(a, oblig1, "c", "d"), snap(b, oblig1, "c", "d"))
	inv := impactByCategory(t, rep, "invariants")
	if closureprotocol.GovernedKnowledgeImpactChanged(inv) {
		t.Fatal("reordered triples changed invariants")
	}
}

func TestReorderedObligationYAMLUnchanged(t *testing.T) {
	y2 := `proof_obligations:
  - id: ob.1
    template_kind: contract_test
    evidence_lane: static_test
    label: first
    required_slots:
      - kind: static_test
        id: slot.1
`
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(baseTriples(), y2, "c", "d"))
	po := impactByCategory(t, rep, "proof_obligations")
	if closureprotocol.GovernedKnowledgeImpactChanged(po) {
		t.Fatal("reordered obligation YAML changed proof_obligations")
	}
}

func TestInvariantChangeAffectsInvariants(t *testing.T) {
	result := append(baseTriples()[:0:0], baseTriples()...)
	result[1] = lit("aw:inv.1", rdf.PropLabel, "CHANGED") // modify invariant label
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	inv := impactByCategory(t, rep, "invariants")
	if !closureprotocol.GovernedKnowledgeImpactChanged(inv) || len(inv.ChangedRecordIDs) != 1 || inv.ChangedRecordIDs[0] != "aw:inv.1" {
		t.Fatalf("invariant change not reported exactly: %+v", inv)
	}
	if closureprotocol.GovernedKnowledgeImpactChanged(impactByCategory(t, rep, "authority")) {
		t.Fatal("authority spuriously changed")
	}
}

func TestRecordAdditionAndRemoval(t *testing.T) {
	result := append(baseTriples(), iri("aw:inv.2", rdf.PropType, rdf.ClassInvariant))
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	inv := impactByCategory(t, rep, "invariants")
	if len(inv.ChangedRecordIDs) != 1 || inv.ChangedRecordIDs[0] != "aw:inv.2" {
		t.Fatalf("addition not reported: %+v", inv)
	}
	// Reverse direction: removal.
	rep2 := mustCompare(t, snap(result, oblig1, "c", "d"), snap(baseTriples(), oblig1, "c", "d"))
	if ids := impactByCategory(t, rep2, "invariants").ChangedRecordIDs; len(ids) != 1 || ids[0] != "aw:inv.2" {
		t.Fatalf("removal not reported: %v", ids)
	}
}

// Removing the requiresTest edge changes required_tests, attributed to the owning
// invariant subject.
func TestRequiredTestRelationChange(t *testing.T) {
	base := baseTriples()
	var result []graphsnapshot.Triple
	for _, tr := range base {
		if tr.Predicate == rdf.PropRequiresTest {
			continue // drop the requiresTest edge
		}
		result = append(result, tr)
	}
	rep := mustCompare(t, snap(base, oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	rt := impactByCategory(t, rep, "required_tests")
	if !closureprotocol.GovernedKnowledgeImpactChanged(rt) {
		t.Fatal("required_tests did not change")
	}
	found := false
	for _, id := range rt.ChangedRecordIDs {
		if id == "aw:inv.1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("required_tests change not attributed to owning invariant: %v", rt.ChangedRecordIDs)
	}
}

func TestAuthorityChange(t *testing.T) {
	result := append(baseTriples()[:0:0], baseTriples()...)
	result[5] = lit("aw:auth.1", rdf.PropLabel, "renamed surface")
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	if ids := impactByCategory(t, rep, "authority").ChangedRecordIDs; len(ids) != 1 || ids[0] != "aw:auth.1" {
		t.Fatalf("authority change not reported: %v", ids)
	}
}

func TestProofObligationOnlyChange(t *testing.T) {
	y2 := `proof_obligations:
  - id: ob.1
    label: RELABELLED
    evidence_lane: static_test
    template_kind: contract_test
    required_slots:
      - id: slot.1
        kind: static_test
`
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(baseTriples(), y2, "c", "d"))
	po := impactByCategory(t, rep, "proof_obligations")
	if len(po.ChangedRecordIDs) != 1 || po.ChangedRecordIDs[0] != "ob.1" {
		t.Fatalf("proof obligation change not reported: %+v", po)
	}
	if closureprotocol.GovernedKnowledgeImpactChanged(impactByCategory(t, rep, "invariants")) {
		t.Fatal("invariants spuriously changed")
	}
}

func TestPolicyChanges(t *testing.T) {
	rep := mustCompare(t, snap(baseTriples(), oblig1, "cert.v1", "comp.v1"), snap(baseTriples(), oblig1, "cert.v2", "comp.v1"))
	if ids := impactByCategory(t, rep, "certification_policy").ChangedRecordIDs; len(ids) != 2 {
		t.Fatalf("certification policy change ids = %v, want cert.v1 removed + cert.v2 added", ids)
	}
	if closureprotocol.GovernedKnowledgeImpactChanged(impactByCategory(t, rep, "completion_policy")) {
		t.Fatal("completion policy spuriously changed")
	}
}

// A source-code comment (a SourceFile literal) is not a governed category, so it
// changes nothing.
func TestUnrelatedCommentUnchanged(t *testing.T) {
	base := baseTriples()
	result := append(base, lit("aw:src/model.go", rdf.PropComment, "a new comment"),
		iri("aw:src/model.go", rdf.PropType, rdf.ClassSourceFile))
	rep := mustCompare(t, snap(base, oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	for _, im := range rep.Impacts {
		if closureprotocol.GovernedKnowledgeImpactChanged(im) {
			t.Fatalf("category %q changed for an unrelated source-file comment", im.Category)
		}
	}
}

func TestDuplicateObligationRejected(t *testing.T) {
	dup := `proof_obligations:
  - id: ob.1
    label: a
    evidence_lane: static_test
    template_kind: contract_test
  - id: ob.1
    label: b
    evidence_lane: static_test
    template_kind: contract_test
`
	_, err := Compare(snap(baseTriples(), oblig1, "c", "d"), snap(baseTriples(), dup, "c", "d"))
	var ge *Error
	if !errors.As(err, &ge) || ge.Code != CodeDuplicateRecord {
		t.Fatalf("want duplicate_record, got %v", err)
	}
}

func TestChangedIDsSortedAndExact(t *testing.T) {
	result := append(baseTriples(),
		iri("aw:inv.9", rdf.PropType, rdf.ClassInvariant),
		iri("aw:inv.3", rdf.PropType, rdf.ClassInvariant),
	)
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(result, oblig1, "c", "d"))
	ids := impactByCategory(t, rep, "invariants").ChangedRecordIDs
	if len(ids) != 2 || ids[0] != "aw:inv.3" || ids[1] != "aw:inv.9" {
		t.Fatalf("changed ids not sorted/exact: %v", ids)
	}
}

func TestAllTenCategoriesAlwaysPresent(t *testing.T) {
	rep := mustCompare(t, snap(nil, "", "", ""), snap(nil, "", "", ""))
	want := closureprotocol.GovernedKnowledgeCategories()
	if len(rep.BaseManifests) != len(want) {
		t.Fatalf("empty snapshots produced %d manifests", len(rep.BaseManifests))
	}
	for i, name := range want {
		if rep.BaseManifests[i].Category != name || rep.Impacts[i].Category != name {
			t.Fatalf("category %d not %q", i, name)
		}
		if !isHex64(rep.BaseManifests[i].DigestSHA256) {
			t.Fatalf("empty category %q lacks a stable digest", name)
		}
	}
}

func TestDeterministicRepeated(t *testing.T) {
	a, b := snap(baseTriples(), oblig1, "cert.v1", "comp.v1"), snap(baseTriples(), oblig1, "cert.v1", "comp.v1")
	d1 := closureprotocol.MustSemanticDigest(mustCompare(t, a, b))
	d2 := closureprotocol.MustSemanticDigest(mustCompare(t, a, b))
	if d1 != d2 {
		t.Fatal("Compare is nondeterministic")
	}
}

func TestValidateReportRejectsGraphDigestSubstitution(t *testing.T) {
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(baseTriples(), oblig1, "c", "d"))
	rep.BaseManifests[1].DigestSHA256 = rep.BaseGraphDigestSHA256 // invariants := whole graph
	if err := ValidateReport(rep); err == nil {
		t.Fatal("expected graph-digest substitution to be rejected")
	}
}
