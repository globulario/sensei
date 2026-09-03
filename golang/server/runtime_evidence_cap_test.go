// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

func evProfiles(n int) []loadedRuntimeEvidence {
	out := make([]loadedRuntimeEvidence, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, loadedRuntimeEvidence{
			ID:                  string(rune('a'+i)) + "-profile",
			Label:               string(rune('A'+i)) + " evidence",
			AuthorityDomainIDs:  []string{"dom"},
			ObservedFromService: "svc",
		})
	}
	return out
}

// A REQUIREMENT THAT IS NOT SHOWN MUST NOT BE SILENTLY ABSENT.
//
// maxEvidenceSurfaced capped the MATCHED set and returned the prefix, so
// Preflight's RequiredActions carried two requirements out of three with
// nothing saying so. A caller satisfied what it was shown and had no way to
// learn a third applied. Bounding the list is fine; bounding it in silence
// turns "these are the requirements" into "these are two of the requirements"
// without changing a single word of the output.
func TestATruncatedRequiredEvidenceListSaysItIsIncomplete(t *testing.T) {
	matched := matchRuntimeEvidence(
		[]loadedAuthorityDomain{{ID: "dom"}}, evProfiles(maxEvidenceSurfaced+2))

	// Matching returns the COMPLETE set: the presentation bound does not belong
	// in the step that decides what applies.
	if len(matched) != maxEvidenceSurfaced+2 {
		t.Fatalf("matchRuntimeEvidence returned %d of %d profiles: a presentation cap is "+
			"still inside the matching step", len(matched), maxEvidenceSurfaced+2)
	}

	actions := evidenceRequirementActions(matched)
	joined := strings.Join(actions, "\n")
	if !strings.Contains(joined, "INCOMPLETE") {
		t.Fatalf("a bounded requirement list did not report its own incompleteness:\n%s", joined)
	}
	if !strings.Contains(joined, "2 further requirement(s)") {
		t.Errorf("the omission is not quantified, so a reader cannot tell how much is missing:\n%s", joined)
	}
	// The rendering stays bounded — the repair must not trade silence for an
	// unbounded action list.
	shown := 0
	for _, a := range actions {
		if strings.HasPrefix(a, "Evidence required [") {
			shown++
		}
	}
	if shown != maxEvidenceSurfaced {
		t.Errorf("rendered %d requirement lines, want the bound %d", shown, maxEvidenceSurfaced)
	}
}

// POSITIVE CONTROL: an un-truncated list must NOT claim incompleteness, or the
// warning becomes noise attached to every preflight and stops being read.
func TestACompleteRequiredEvidenceListDoesNotClaimIncompleteness(t *testing.T) {
	matched := matchRuntimeEvidence([]loadedAuthorityDomain{{ID: "dom"}}, evProfiles(maxEvidenceSurfaced))
	joined := strings.Join(evidenceRequirementActions(matched), "\n")
	if strings.Contains(joined, "INCOMPLETE") {
		t.Fatalf("a complete list reported itself incomplete:\n%s", joined)
	}
	if joined == "" {
		t.Fatal("no actions rendered at all: the fixture no longer reaches the case under test")
	}
}

// The decision the consumer makes on this list is len(matched) > 0. It was
// cap-invariant before and must stay so — this pins that the repair did not
// change which changes surface evidence requirements at all.
func TestMatchingStillDecidesTheSameChangesNeedEvidence(t *testing.T) {
	if got := matchRuntimeEvidence(nil, evProfiles(3)); len(got) != 0 {
		t.Errorf("no matched domains must match no evidence, got %d", len(got))
	}
	if got := matchRuntimeEvidence([]loadedAuthorityDomain{{ID: "other"}}, evProfiles(3)); len(got) != 0 {
		t.Errorf("a non-matching domain must match no evidence, got %d", len(got))
	}
	if got := matchRuntimeEvidence([]loadedAuthorityDomain{{ID: "dom"}}, evProfiles(1)); len(got) != 1 {
		t.Errorf("a matching domain must match its evidence, got %d", len(got))
	}
}
