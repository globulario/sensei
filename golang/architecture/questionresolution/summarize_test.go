// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

func resolutionFor(s Summary, id string) (QuestionResolution, bool) {
	for _, q := range s.Questions {
		if q.QuestionID == id {
			return q, true
		}
	}
	return QuestionResolution{}, false
}

// Item 9: a mixed question set yields exact, typed per-question statuses with
// preserved identifiers/provenance — never collapsed into prose.
func TestSummaryExactPerQuestionStates(t *testing.T) {
	w := seedWorld(t)
	qA, qB := w.Questions[0].QuestionID, w.Questions[1].QuestionID
	dA := w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	lineage := w.promote(t, dA, proposedInvariant())
	w.dispose2(t, qB, qd.DispositionDeferred, qd.ReusabilityNone, "B")

	s := w.summarize(t)
	rA, okA := resolutionFor(s, qA)
	rB, okB := resolutionFor(s, qB)
	if !okA || !okB {
		t.Fatal("both questions must appear in the summary")
	}
	if rA.State != StateReusablePromoted {
		t.Fatalf("qA state = %s, want reusable_promoted", rA.State)
	}
	if rA.DispositionReceiptDigestSHA256 != dA {
		t.Fatal("qA must preserve its exact disposition receipt digest")
	}
	if rA.PromotionLineageID != lineage || rA.GovernedNodeIRI == "" || rA.PromotionReceiptDigestSHA256 == "" {
		t.Fatalf("qA must preserve exact promotion provenance, got %+v", rA)
	}
	if rB.State != StateDeferred {
		t.Fatalf("qB state = %s, want deferred", rB.State)
	}
	// Determinism: an unchanged world yields a byte-identical summary.
	again := w.summarize(t)
	d1 := closureprotocol.MustSemanticDigest(s)
	d2 := closureprotocol.MustSemanticDigest(again)
	if d1 != d2 {
		t.Fatal("summary is not deterministic on an unchanged world")
	}
}

// Item 12: building the summary produces zero repository side effects.
func TestSummaryHasNoSideEffects(t *testing.T) {
	w := seedWorld(t)
	dA := w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")

	before := treeDigest(t, w.Repo)
	_ = w.summarize(t)
	_ = w.summarize(t)
	if after := treeDigest(t, w.Repo); after != before {
		t.Fatal("summarize mutated the repository")
	}
}

// Item 12 (refused gate): a refused certification writes nothing.
func TestRefusedGateWritesNothing(t *testing.T) {
	w := seedWorld(t)
	// qA reusable but unpromoted, qB unresolved → the gate refuses.
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")

	before := treeDigest(t, w.Repo)
	res := w.certify(t)
	if res.Outcome == OutcomeSatisfied || res.Outcome == OutcomeReplay {
		t.Fatalf("expected a refusal, got %s", res.Outcome)
	}
	if after := treeDigest(t, w.Repo); after != before {
		t.Fatal("a refused gate mutated the repository")
	}
	if res.Certificate != nil {
		t.Fatal("a refused gate must not produce a certificate")
	}
}
