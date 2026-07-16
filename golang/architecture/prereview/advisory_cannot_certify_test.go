// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"context"
	"errors"
	"testing"
)

// The PR-1 advisory assembler has no authority to establish certification or
// terminal closure. Regardless of graph context — rich, empty, or uncollectable
// — GenerateAdvisory must never emit a certified or terminally_closed
// disposition, never display a certification verdict or completion receipt, and
// never claim coverage above advisory. Those states come only from receipts,
// which advisory sources do not carry.
func TestGenerateAdvisoryCannotClaimCertification(t *testing.T) {
	cases := []struct {
		name  string
		graph fakeGraphSource
	}{
		{"rich_graph", fakeGraphSource{graph: sampleGraph()}},
		{"empty_graph", fakeGraphSource{graph: GraphContext{}}},
		{"uncollectable_graph", fakeGraphSource{err: errors.New("graph offline")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, tc.graph, GenerateRequest{Purpose: "Add a route."})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if r.Disposition == DispositionCertified || r.Disposition == DispositionTerminallyClosed {
				t.Fatalf("advisory emitted a terminal-positive disposition: %q", r.Disposition)
			}
			if r.Coverage.Level != CoverageAdvisory {
				t.Fatalf("advisory coverage level = %q, want advisory", r.Coverage.Level)
			}
			if r.Proof.Certification.IsCertified() {
				t.Fatal("advisory report displayed a certified verdict without a receipt")
			}
			if r.Result.Completion.HasReceipt() {
				t.Fatal("advisory report displayed a completion receipt")
			}
			// certification and terminal closure must be reported unavailable.
			if !containsSubstring(r.Coverage.Unavailable, "certification") {
				t.Fatalf("certification not reported unavailable: %v", r.Coverage.Unavailable)
			}
			if err := Validate(r); err != nil {
				t.Fatalf("advisory report failed validation: %v", err)
			}
		})
	}
}

// Missing graph context stays visibly unavailable/degraded in non-strict mode
// and fails in strict mode — it is never silently converted into a clean or
// certified result.
func TestGenerateAdvisoryMissingContextStaysUnavailable(t *testing.T) {
	r, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, nil, GenerateRequest{})
	if err != nil {
		t.Fatalf("nil graph source should degrade, not fail: %v", err)
	}
	if r.Disposition == DispositionCertified || r.Disposition == DispositionTerminallyClosed {
		t.Fatalf("degraded report claimed a terminal-positive disposition: %q", r.Disposition)
	}
	if len(r.Limitations) == 0 {
		t.Fatal("degraded report named no limitation")
	}
	if _, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{err: errors.New("offline")}, GenerateRequest{Strict: true}); err == nil {
		t.Fatal("strict mode accepted uncollectable graph context")
	}
}
