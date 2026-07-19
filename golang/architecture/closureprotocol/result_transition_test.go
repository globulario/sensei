// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func hexOfTest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// §6.1-6.3, 6.5: a receipt is applicable to the current result only when its
// result-binding digest matches; a receipt bound to a previous result — evidence,
// proof, or certification — never applies, and no projection can make it current.
func TestReceiptAppliesOnlyToMatchingResult(t *testing.T) {
	current := hexOfTest("result.new")
	previous := hexOfTest("result.old")
	if !ReceiptAppliesToCurrentResult(current, current) {
		t.Fatal("a receipt bound to the current result must apply")
	}
	for _, boundTo := range []string{previous, ""} {
		if ReceiptAppliesToCurrentResult(boundTo, current) {
			t.Fatalf("a receipt bound to %q must not apply to the current result", boundTo)
		}
	}
}

// §6.3-6.4: a certification bound to the previous result cannot certify the new
// result, and the historical certification remains byte-identical.
func TestCertificationBoundToPreviousResultInapplicableAndImmutable(t *testing.T) {
	prev := CertificationReceipt{
		ResultBinding:        ResultBinding{BaseRevision: "r0", PatchDigestSHA256: hexOfTest("p0"), ResultTreeDigestSHA256: hexOfTest("t0"), GraphDigestSHA256: hexOfTest("g0")},
		CertificationPolicy:  "certification.strict.v1",
		CertificationVerdict: Certified,
	}
	prevResult, err := ResultBindingDigest(prev.ResultBinding)
	if err != nil {
		t.Fatal(err)
	}
	newResult := hexOfTest("t-new")
	if ReceiptAppliesToCurrentResult(prevResult, newResult) {
		t.Fatal("a certification bound to the previous result must not apply to the new result")
	}
	// The historical certification is never rewritten.
	d1, _ := CertificationReceiptDigest(prev)
	d2, _ := CertificationReceiptDigest(prev)
	if d1 != d2 {
		t.Fatal("historical certification digest must be stable (never rewritten)")
	}
}

// §5.9 / safety: the result-transition contract claims no certification and no
// completion — there is no field a caller could set to assert either, and the
// receipt status vocabulary contains no terminal "completed".
func TestResultTransitionClaimsNoCompletion(t *testing.T) {
	rt := reflect.TypeOf(ResultTransitionReceipt{})
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.ToLower(rt.Field(i).Tag.Get("json"))
		for _, forbidden := range []string{"complet", "terminal", "certif"} {
			if strings.Contains(tag, forbidden) {
				t.Fatalf("result transition receipt exposes a %q field (%q)", forbidden, tag)
			}
		}
	}
	for _, s := range ReceiptStatuses {
		if string(s) == "completed" || string(s) == "certified" {
			t.Fatalf("receipt status vocabulary must not contain %q", s)
		}
	}
}

// §5: governed-knowledge "changed" is derived from a manifest-digest difference,
// never a stored boolean — the impact type carries no forgeable boolean.
func TestGovernedKnowledgeImpactChangedIsDigestDerived(t *testing.T) {
	unchanged := GovernedKnowledgeImpact{Category: "authority", BaseManifestDigestSHA256: hexOfTest("m"), ResultManifestDigestSHA256: hexOfTest("m")}
	changed := GovernedKnowledgeImpact{Category: "authority", BaseManifestDigestSHA256: hexOfTest("a"), ResultManifestDigestSHA256: hexOfTest("b")}
	if GovernedKnowledgeImpactChanged(unchanged) {
		t.Fatal("equal manifest digests must derive as unchanged")
	}
	if !GovernedKnowledgeImpactChanged(changed) {
		t.Fatal("unequal manifest digests must derive as changed")
	}
	it := reflect.TypeOf(GovernedKnowledgeImpact{})
	for i := 0; i < it.NumField(); i++ {
		if it.Field(i).Type.Kind() == reflect.Bool {
			t.Fatalf("governed knowledge impact must carry no forgeable boolean (%s)", it.Field(i).Name)
		}
	}
}

// §10: reordering the artifact, derivation, governed-impact, and producer
// collections does not change the receipt's semantic identity.
func TestResultTransitionCanonicalizationIgnoresCollectionOrder(t *testing.T) {
	a := ResultTransitionReceipt{
		OperationalArtifactReceipts: []ArtifactReceipt{{ID: "a", Kind: "architecture_graph"}, {ID: "b", Kind: "inferred_claims"}},
		Derivations: []ArtifactDerivation{
			{Stage: StageArchitectureGraph, OutputArtifactReceiptDigestSHA256: hexOfTest("1"), InputArtifactReceiptDigestsSHA256: []string{hexOfTest("x"), hexOfTest("y")}},
			{Stage: StageInferredClaims, OutputArtifactReceiptDigestSHA256: hexOfTest("2")},
		},
		GovernedKnowledgeImpacts: []GovernedKnowledgeImpact{{Category: "authority"}, {Category: "invariants"}},
		PipelineProducerVersions: []ProducerVersion{{Producer: "p1", Version: "v1"}, {Producer: "p2", Version: "v2"}},
	}
	b := ResultTransitionReceipt{
		OperationalArtifactReceipts: []ArtifactReceipt{{ID: "b", Kind: "inferred_claims"}, {ID: "a", Kind: "architecture_graph"}},
		Derivations: []ArtifactDerivation{
			{Stage: StageInferredClaims, OutputArtifactReceiptDigestSHA256: hexOfTest("2")},
			{Stage: StageArchitectureGraph, OutputArtifactReceiptDigestSHA256: hexOfTest("1"), InputArtifactReceiptDigestsSHA256: []string{hexOfTest("y"), hexOfTest("x")}},
		},
		GovernedKnowledgeImpacts: []GovernedKnowledgeImpact{{Category: "invariants"}, {Category: "authority"}},
		PipelineProducerVersions: []ProducerVersion{{Producer: "p2", Version: "v2"}, {Producer: "p1", Version: "v1"}},
	}
	if MustSemanticDigest(a) != MustSemanticDigest(b) {
		t.Fatal("result transition identity must be invariant to collection ordering")
	}
}

func TestResultTransitionReceiptDigestOmitsSelfDigest(t *testing.T) {
	base := ResultTransitionReceipt{
		Task:             TaskBinding{ID: "task.x", SessionID: "session.x"},
		PipelinePolicyID: "pipeline.result_transition.v1",
		RecordedAt:       "2026-07-16T00:15:00Z",
		Status:           ReceiptValid,
	}
	d1, err := ResultTransitionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.ReceiptDigestSHA256 = "different"
	d2, err := ResultTransitionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("self digest field must not change semantic identity")
	}
}

func TestArtifactReceiptDigestOmitsSelfDigest(t *testing.T) {
	a := ArtifactReceipt{Kind: "architecture_graph", SemanticDigestSHA256: hexOfTest("s"), Producer: ArtifactProducer{ID: "p", Version: "v"}, ResultBindingDigestSHA256: hexOfTest("rb")}
	d1, err := ArtifactReceiptDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.ReceiptDigestSHA256 = "different"
	d2, err := ArtifactReceiptDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("artifact receipt self digest must not change semantic identity")
	}
}
