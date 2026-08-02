// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestDispatchExposesPhase10Investigation(t *testing.T) {
	if got := dispatch("investigate", []string{"--help"}); got != 0 {
		t.Fatalf("dispatch investigate --help exit=%d, want 0", got)
	}
}

func TestDispatchExposesPhase10CandidateReview(t *testing.T) {
	if got := dispatch("candidates", []string{"--help"}); got != 0 {
		t.Fatalf("dispatch candidates --help exit=%d, want 0", got)
	}
}
