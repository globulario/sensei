// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

import "testing"

// The declaration must cover the closed set exactly. A type added to
// LedgerEventTypes without a declared cardinality would fall back to whatever the
// reader's iteration order happens to do -- which is the defect B-R1 exists to end.
func TestEventOccurrenceCoversTheClosedSet(t *testing.T) {
	for _, et := range LedgerEventTypes {
		if _, ok := EventOccurrence[et]; !ok {
			t.Errorf("event type %q has no declared occurrence semantics", et)
		}
	}
	for et := range EventOccurrence {
		found := false
		for _, k := range LedgerEventTypes {
			if k == et {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("occurrence declared for %q, which is not in the closed set", et)
		}
	}
	if len(EventOccurrence) != len(LedgerEventTypes) {
		t.Errorf("declared %d, closed set has %d", len(EventOccurrence), len(LedgerEventTypes))
	}
}

// Every type with a required artifact must be a declared member of the closed set.
func TestRequiredArtifactsNameRealEventTypes(t *testing.T) {
	for et := range RequiredEventArtifacts {
		if _, ok := EventOccurrence[et]; !ok {
			t.Errorf("required artifacts declared for undeclared event type %q", et)
		}
	}
}
