// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// Shuffling every input-order axis must not change any projection digest.
func TestDeterminism_ShuffledInputs(t *testing.T) {
	reg := DefaultRegistry()

	n1, _ := BuildNavigationDescriptor(reg)
	n2, _ := BuildNavigationDescriptor(reg)
	if n1.DigestSHA256 != n2.DigestSHA256 {
		t.Fatal("navigation descriptor not deterministic")
	}

	// ArtifactState: shuffle contradiction findings order; identical digest.
	bundleA := satisfiedBundle(reg, rdf.ClassContract)
	bundleA.Contradiction.Findings = []ContradictionObservation{{Identity: "c.2", Relevant: true}, {Identity: "c.1", Relevant: true}}
	bundleB := satisfiedBundle(reg, rdf.ClassContract)
	bundleB.Contradiction.Findings = []ContradictionObservation{{Identity: "c.1", Relevant: true}, {Identity: "c.2", Relevant: true}}
	idA, resA := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	idB, resB := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	stA, err := BuildArtifactState(reg, idA, resA, bundleA)
	if err != nil {
		t.Fatal(err)
	}
	stB, _ := BuildArtifactState(reg, idB, resB, bundleB)
	if stA.DigestSHA256 != stB.DigestSHA256 {
		t.Fatal("artifact state digest depends on contradiction input order")
	}

	// ArtifactIndex: shuffle catalog order; identical page + digest.
	catFwd := catalog(reg, 120)
	catRev := catFwd
	catRev.Artifacts = nil
	for i := len(catFwd.Artifacts) - 1; i >= 0; i-- {
		catRev.Artifacts = append(catRev.Artifacts, catFwd.Artifacts[i])
	}
	iFwd, _ := BuildArtifactIndex(reg, indexReq(100), catFwd)
	iRev, _ := BuildArtifactIndex(reg, indexReq(100), catRev)
	if iFwd.DigestSHA256 != iRev.DigestSHA256 {
		t.Fatal("artifact index digest depends on catalog order")
	}

	// ControlSnapshot: shuffle attention input order; identical digest.
	att := []AttentionItem{}
	for _, class := range []string{AttnContradictionPresent, AttnEnforcementMissing, AttnEvidenceMissing} {
		sev, basis := severityForClass(class, false)
		a, _ := newAttention("o", "s", "id."+class, "", class, "r", sev, basis, []string{"aw:x"}, true, nil, "architect", false)
		att = append(att, a)
	}
	mkInput := func(items []AttentionItem) ControlSnapshotInput {
		return ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
			Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable, Items: items},
			Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}}
	}
	sFwd, _ := BuildControlSnapshot(reg, mkInput(att))
	sRev, _ := BuildControlSnapshot(reg, mkInput([]AttentionItem{att[2], att[0], att[1]}))
	if sFwd.DigestSHA256 != sRev.DigestSHA256 {
		t.Fatal("control snapshot digest depends on attention input order")
	}
}
