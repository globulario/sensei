// SPDX-License-Identifier: AGPL-3.0-only

package proofrequirements

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const rebuildPath = "docs/awareness/generated/proof_obligations.yaml"

const repoObligationsYAML = `proof_obligations:
  - id: ob.repo.1
    label: repository obligation
    evidence_lane: static_test
    template_kind: contract_test
    applies_to_authority_surfaces: [authority.x]
    required_slots:
      - id: slot.repo.1
        kind: static_test
        required: true
`

// baseline builds a fully-consulted, normal ComposeInput that yields
// extraction_completeness=complete, proving_disposition=ready.
func baseline(t *testing.T) ComposeInput {
	t.Helper()
	res := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:      "a",
		BaseBindingDigestSHA256:       "b",
		ClosureAssessmentDigestSHA256: "c",
		OperationSetDigestSHA256:      "d",
		PolicyID:                      "authority.v2",
		EvaluatedAt:                   "2026-07-15T00:00:00Z",
		Status:                        closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{{
			OperationID: "op.1", Status: closureprotocol.ReceiptValid,
			AuthorityDomainIDs:          []string{"authority.x"},
			SelectedMechanism:           closureprotocol.MechanismRepositoryEdit,
			RequiredRuntimeMechanismIDs: []string{"mech.repository_edit"},
		}},
	}
	authDigest, err := closureprotocol.AuthorityResolutionDigest(res)
	if err != nil {
		t.Fatal(err)
	}
	dec := closureprotocol.AdmissionDecision{
		DecisionID: "dec.1", RequestDigestSHA256: "r", PolicyID: "admission.strict.v2",
		CapabilityID: "cap.1", CompletionPolicyID: "completion.v1",
		RequiredProofSlots:       []string{"slot.a"},
		RequiredEvidenceProfiles: []string{"ev.profile"},
		RequiredResultRebuilds:   []string{rebuildPath},
	}
	// slot.a and ev.profile are defined by the scoped graph.
	graph := closure.GraphIndex{Nodes: map[string]closure.Node{}, NodesByID: map[string]string{}}
	var rep closure.Report
	rep.Verdict = closure.VerdictConditionallyClosed
	for _, n := range []closure.Node{
		{ID: "slot.a", IRI: "iri:slot.a", Classes: []string{ClassProofSlot}, Kind: "static_test"},
		{ID: "ev.profile", IRI: "iri:ev.profile", Classes: []string{ClassRuntimeEvidence}, Kind: "runtime"},
		{ID: "test.1", IRI: "iri:test.1", Classes: []string{ClassTest}},
	} {
		graph.Nodes[n.IRI] = n
		graph.NodesByID[n.ID] = n.IRI
		rep.RelevantNodes = append(rep.RelevantNodes, closure.NodeReceipt{ID: n.ID, IRI: n.IRI, Classes: n.Classes})
	}
	return ComposeInput{
		ResultBindingDigestSHA256:         "resultbinding-digest",
		AuthorityResolution:               res,
		ExpectedAuthorityResolutionDigest: authDigest,
		AdmissionDecision:                 dec,
		ExpectedAdmissionDecisionDigest:   closureprotocol.MustSemanticDigest(dec),
		GeneratedArtifacts: GeneratedArtifactSummary{
			ManifestDigestSHA256: "manifest-digest", AllRequiredMatched: true,
			VerifiedPaths: []string{rebuildPath},
		},
		RepositoryProofOutput: RepositoryProofOutput{
			Path: rebuildPath, Bytes: []byte(repoObligationsYAML), SemanticDigestSHA256: "repo-sem",
		},
		Graph:         graph,
		ClosureReport: rep,
		Questions:     QuestionInput{Actionable: true},
	}
}

func mustCompose(t *testing.T, in ComposeInput) Document {
	t.Helper()
	doc, err := Compose(context.Background(), in)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	return doc
}

func TestComposeCompleteReady(t *testing.T) {
	doc := mustCompose(t, baseline(t))
	if doc.ExtractionCompleteness != ExtractionComplete {
		t.Fatalf("completeness = %q, want complete; coverage=%+v changes=%+v", doc.ExtractionCompleteness, doc.SourceCoverage, doc.RequirementChanges)
	}
	if doc.ProvingDisposition != ProvingReady {
		t.Fatalf("disposition = %q, want ready", doc.ProvingDisposition)
	}
	if len(doc.SourceCoverage) != len(mandatorySources) {
		t.Fatalf("coverage count = %d", len(doc.SourceCoverage))
	}
	for _, c := range doc.SourceCoverage {
		if c.Status != CoverageConsulted {
			t.Fatalf("source %q status = %q, want consulted", c.Source, c.Status)
		}
	}
	if doc.CompletionPolicyID != "completion.v1" {
		t.Fatalf("completion policy = %q", doc.CompletionPolicyID)
	}
	// admission slot.a is defined by the graph.
	var slot Requirement
	for _, s := range doc.RequiredSlots {
		if s.ID == "slot.a" {
			slot = s
		}
	}
	if slot.ID == "" || slot.DefinitionStatus != "defined" {
		t.Fatalf("slot.a = %+v, want defined", slot)
	}
	// the rebuild is satisfied by the verified result.
	if len(doc.RequiredResultRebuilds) != 1 || doc.RequiredResultRebuilds[0].Status != "satisfied_by_result" {
		t.Fatalf("rebuilds = %+v", doc.RequiredResultRebuilds)
	}
}

// A complete extraction can still be blocked by an unresolved human decision.
func TestComposeCompleteButBlocked(t *testing.T) {
	in := baseline(t)
	in.Questions.UnresolvedArchitectQuestionIDs = []string{"q.1"}
	doc := mustCompose(t, in)
	if doc.ExtractionCompleteness != ExtractionComplete {
		t.Fatalf("completeness = %q, want complete", doc.ExtractionCompleteness)
	}
	if doc.ProvingDisposition != ProvingBlocked {
		t.Fatalf("disposition = %q, want blocked", doc.ProvingDisposition)
	}
	if len(doc.ArchitectQuestions) != 1 || doc.ArchitectQuestions[0].ID != "q.1" {
		t.Fatalf("questions = %+v", doc.ArchitectQuestions)
	}
}

// The admission floor is never dropped: an admission slot nothing downstream
// represents is retained unresolved, flagged as a change, and the extraction is
// incomplete (never silently complete by omission).
func TestComposeAdmissionFloorRetained(t *testing.T) {
	in := baseline(t)
	in.AdmissionDecision.RequiredProofSlots = []string{"slot.a", "slot.orphan"}
	in.ExpectedAdmissionDecisionDigest = closureprotocol.MustSemanticDigest(in.AdmissionDecision)
	doc := mustCompose(t, in)
	var orphan Requirement
	for _, s := range doc.RequiredSlots {
		if s.ID == "slot.orphan" {
			orphan = s
		}
	}
	if orphan.ID == "" || orphan.DefinitionStatus != "unresolved" {
		t.Fatalf("orphan slot = %+v, want retained unresolved", orphan)
	}
	found := false
	for _, ch := range doc.RequirementChanges {
		if ch.ID == "slot.orphan" && ch.ResultGraphStatus == "no_longer_represented" && ch.Disposition == "governance_review_required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no governance_review_required change for orphan: %+v", doc.RequirementChanges)
	}
	if doc.ExtractionCompleteness != ExtractionIncomplete {
		t.Fatalf("completeness = %q, want incomplete", doc.ExtractionCompleteness)
	}
}

// A forged carried admission decision (digest mismatch) marks the source invalid
// and never certifies the floor.
func TestComposeForgedAdmissionDigest(t *testing.T) {
	in := baseline(t)
	in.ExpectedAdmissionDecisionDigest = "not-the-real-digest"
	doc := mustCompose(t, in)
	var cov SourceCoverage
	for _, c := range doc.SourceCoverage {
		if c.Source == OriginAdmission {
			cov = c
		}
	}
	if cov.Status != CoverageInvalid {
		t.Fatalf("admission coverage = %+v, want invalid", cov)
	}
	if doc.ExtractionCompleteness == ExtractionComplete {
		t.Fatal("must not be complete with an invalid admission source")
	}
}

// A required result rebuild the Stage 2 verification did not confirm is
// uncertifiable, and drags the whole document to uncertifiable.
func TestComposeRebuildUncertifiable(t *testing.T) {
	in := baseline(t)
	in.GeneratedArtifacts.VerifiedPaths = nil // rebuild path no longer verified
	doc := mustCompose(t, in)
	if len(doc.RequiredResultRebuilds) != 1 || doc.RequiredResultRebuilds[0].Status != "uncertifiable" {
		t.Fatalf("rebuild = %+v, want uncertifiable", doc.RequiredResultRebuilds)
	}
	if doc.ExtractionCompleteness != ExtractionUncertifiable || doc.ProvingDisposition != ProvingUncertifiable {
		t.Fatalf("verdict = %q/%q, want uncertifiable/uncertifiable", doc.ExtractionCompleteness, doc.ProvingDisposition)
	}
}

// A closure blocker is a verification requirement, never a forbidden move.
func TestComposeClosureBlockerNotForbidden(t *testing.T) {
	in := baseline(t)
	in.ClosureReport.Blockers = []closure.Blocker{{ID: "blk.1", Dimension: "proof", Severity: "high", Code: "closure.x", RequiredNextAction: "prove"}}
	// add a real forbidden fix to the scoped graph
	ff := closure.Node{ID: "ff.1", IRI: "iri:ff.1", Classes: []string{ClassForbiddenFix}, Forbids: []string{"cache_it"}}
	in.Graph.Nodes[ff.IRI] = ff
	in.Graph.NodesByID[ff.ID] = ff.IRI
	in.ClosureReport.RelevantNodes = append(in.ClosureReport.RelevantNodes, closure.NodeReceipt{ID: ff.ID, IRI: ff.IRI, Classes: ff.Classes})
	doc := mustCompose(t, in)
	if len(doc.ClosureBlockers) != 1 || doc.ClosureBlockers[0].ID != "blk.1" {
		t.Fatalf("closure blockers = %+v", doc.ClosureBlockers)
	}
	for _, f := range doc.ForbiddenMoves {
		if f.ID == "blk.1" {
			t.Fatal("closure blocker leaked into forbidden moves")
		}
	}
	if len(doc.ForbiddenMoves) != 1 || doc.ForbiddenMoves[0].ID != "ff.1" {
		t.Fatalf("forbidden moves = %+v", doc.ForbiddenMoves)
	}
}

// Compose is deterministic: the identical input always yields a byte-identical
// document (no map-iteration leakage).
func TestComposeStable(t *testing.T) {
	in := baseline(t)
	in.AdmissionDecision.RequiredProofSlots = []string{"slot.a", "slot.orphan"}
	in.AdmissionDecision.RequiredEvidenceProfiles = []string{"ev.profile", "ev.orphan"}
	in.ExpectedAdmissionDecisionDigest = closureprotocol.MustSemanticDigest(in.AdmissionDecision)
	d1 := closureprotocol.MustSemanticDigest(mustCompose(t, in))
	d2 := closureprotocol.MustSemanticDigest(mustCompose(t, in))
	if d1 != d2 {
		t.Fatalf("Compose is nondeterministic: %s != %s", d1, d2)
	}
}

// The derived requirement lists do not depend on relevant-node order — only the
// closure provenance digest, which faithfully reflects input bytes, may differ.
func TestComposeProjectionOrderIndependent(t *testing.T) {
	in := baseline(t)
	in.AdmissionDecision.RequiredProofSlots = []string{"slot.a", "slot.orphan"}
	in.ExpectedAdmissionDecisionDigest = closureprotocol.MustSemanticDigest(in.AdmissionDecision)
	a := mustCompose(t, in)

	rn := in.ClosureReport.RelevantNodes
	for i, j := 0, len(rn)-1; i < j; i, j = i+1, j-1 {
		rn[i], rn[j] = rn[j], rn[i]
	}
	b := mustCompose(t, in)

	// Blank the input-provenance digests that legitimately track byte order (the
	// closure digest, echoed in its coverage entry); compare the rest.
	blankClosureProvenance := func(d *Document) {
		d.SourceClosureDigestSHA256 = ""
		for i := range d.SourceCoverage {
			if d.SourceCoverage[i].Source == OriginClosure {
				d.SourceCoverage[i].DigestSHA256 = ""
			}
		}
	}
	blankClosureProvenance(&a)
	blankClosureProvenance(&b)
	if closureprotocol.MustSemanticDigest(a) != closureprotocol.MustSemanticDigest(b) {
		t.Fatal("derived requirements depend on relevant-node order")
	}
}

func TestValidateDocumentRejectsMissingSource(t *testing.T) {
	doc := mustCompose(t, baseline(t))
	doc.SourceCoverage = doc.SourceCoverage[:len(doc.SourceCoverage)-1] // drop one mandatory source
	if err := ValidateDocument(doc); err == nil {
		t.Fatal("expected ValidateDocument to reject a document missing a mandatory source")
	}
}

func TestComposeClosureUncertifiableVerdict(t *testing.T) {
	in := baseline(t)
	in.ClosureReport.Verdict = closure.VerdictUncertifiable
	doc := mustCompose(t, in)
	if doc.ExtractionCompleteness != ExtractionUncertifiable {
		t.Fatalf("completeness = %q, want uncertifiable", doc.ExtractionCompleteness)
	}
}
