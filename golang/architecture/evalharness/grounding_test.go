// SPDX-License-Identifier: AGPL-3.0-only

package evalharness

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

func groundingFixture() (investigation.Document, investigation.Document) {
	how := investigation.Document{
		Observations: []architecture.Fact{{ID: "obs.1"}, {ID: "obs.2"}},
		RawEvidence:  []investigation.EvidenceReceipt{{ID: "ev.how.1"}},
	}
	why := investigation.Document{
		RawEvidence: []investigation.EvidenceReceipt{{ID: "ev.why.1"}},
	}
	return how, why
}

func TestCandidateCitingOnlyPresentIdentitiesIsGrounded(t *testing.T) {
	how, why := groundingFixture()
	grounded, dangling := groundingOfCandidates([]investigator.CandidateEnvelope{{
		CandidateID:              "cand.1",
		ObservationRefIDs:        []string{"obs.1", "obs.2"},
		SupportingEvidenceRefIDs: []string{"ev.how.1"},
		RefutingEvidenceRefIDs:   []string{"ev.why.1"},
	}}, how, why)
	if grounded != 1 || len(dangling) != 0 {
		t.Fatalf("grounded=%d dangling=%v, want 1 and none", grounded, dangling)
	}
}

// The metric has to be able to fail, or it is decoration. A candidate citing an
// identity that is not in the documents it was composed from is pointing at
// nothing, and that is checkable without any reference set.
func TestCandidateCitingAnAbsentIdentityIsNotGrounded(t *testing.T) {
	how, why := groundingFixture()
	grounded, dangling := groundingOfCandidates([]investigator.CandidateEnvelope{{
		CandidateID:              "cand.1",
		ObservationRefIDs:        []string{"obs.1"},
		SupportingEvidenceRefIDs: []string{"ev.does.not.exist"},
	}}, how, why)
	if grounded != 0 {
		t.Fatalf("a candidate citing an absent receipt counted as grounded")
	}
	if len(dangling) != 1 || !strings.Contains(dangling[0], "ev.does.not.exist") {
		t.Fatalf("the dangling reference is not named: %v", dangling)
	}
	if !strings.Contains(dangling[0], "cand.1") {
		t.Fatalf("the dangling reference does not say which candidate made it: %v", dangling)
	}
}

// Each side is checked, so a fabrication in the refuting lane cannot hide behind
// a well-grounded supporting lane.
func TestEveryCitationLaneIsChecked(t *testing.T) {
	how, why := groundingFixture()
	for name, envelope := range map[string]investigator.CandidateEnvelope{
		"observation": {CandidateID: "c", ObservationRefIDs: []string{"missing"}},
		"supporting":  {CandidateID: "c", SupportingEvidenceRefIDs: []string{"missing"}},
		"refuting":    {CandidateID: "c", RefutingEvidenceRefIDs: []string{"missing"}},
	} {
		grounded, dangling := groundingOfCandidates([]investigator.CandidateEnvelope{envelope}, how, why)
		if grounded != 0 || len(dangling) != 1 {
			t.Fatalf("%s lane unchecked: grounded=%d dangling=%v", name, grounded, dangling)
		}
	}
}

// A candidate that cites nothing at all is not "grounded" by having no
// citations to fail — but it is also not a dangling reference. It counts as
// grounded here because nothing it says is unresolvable, and whether a
// citation-free candidate should exist is a different question this metric does
// not pretend to answer.
func TestCandidateWithNoCitationsHasNoDanglingReferences(t *testing.T) {
	how, why := groundingFixture()
	grounded, dangling := groundingOfCandidates([]investigator.CandidateEnvelope{{CandidateID: "c"}}, how, why)
	if grounded != 1 || len(dangling) != 0 {
		t.Fatalf("grounded=%d dangling=%v", grounded, dangling)
	}
}
