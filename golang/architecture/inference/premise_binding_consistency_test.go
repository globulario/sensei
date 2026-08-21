// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
)

// TestReadersAgreeOnWhetherAPremiseIsPinned is the cross-projection form of the
// property behind this repair.
//
// Three readers decide something from the same provenance and each has its own
// vocabulary for it — maintenance produces a lane state, inference produces an
// epistemic status, and questiongen decides whether to suppress a question.
// Their outputs are not comparable and are not meant to be. What must hold is
// that none of them treats a premise as pinned when the authority — the digest
// that pins the bytes — does not pin it.
//
// The defect this replaces: architecture.ValidateFact accepts
// source_digest_status: resolved carrying an empty or malformed digest, and
// inference read the status alone, so one reader called a fact bound while
// another called the same fact unknown.
func TestReadersAgreeOnWhetherAPremiseIsPinned(t *testing.T) {
	cases := []struct {
		name   string
		digest string
		pinned bool
	}{
		{"absent digest", "", false},
		{"malformed digest", "not-a-digest", false},
		{"short digest", strings.Repeat("a", 63), false},
		{"uppercase digest", strings.Repeat("A", 64), false},
		{"non-hex digest", strings.Repeat("z", 64), false},
		{"well-formed digest", strings.Repeat("a", 64), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fact := premiseWithDigest(tc.digest)

			// 1. The owner's own answer.
			owner := fact.Provenance.SourceDigestPinsContent()
			if owner != tc.pinned {
				t.Fatalf("owner says pinned=%v, want %v", owner, tc.pinned)
			}

			// 2. inference: an unpinned premise on an uncommitted tree cannot
			// support a claim.
			status, _ := statusForPremises(boundContext(), []architecture.Fact{fact})
			if tc.pinned && status != architecture.StatusSupported {
				t.Fatalf("inference refused a pinned premise: %s", status)
			}
			if !tc.pinned && status == architecture.StatusSupported {
				t.Fatalf("inference supported a claim whose premise pins nothing")
			}

			// 3. maintenance: reads the digest and must not call an unpinned
			// premise anything better than unknown.
			lane := maintenance.VerifySourceReceipt(t.TempDir(), architecture.ClaimFactReceipt{
				Fact: fact, Provenance: *fact.Provenance,
			})
			// Asserted as "not current" rather than "exactly unknown": an
			// absent digest reads as unknown and a malformed one reads as
			// stale, because maintenance gets far enough to compare it against
			// the file and finds a mismatch. Both are negative; demanding one
			// specific negative would pin an implementation detail instead of
			// the property. What must never happen is that maintenance calls a
			// premise CURRENT while inference cannot support it, or the reverse.
			if !tc.pinned && lane.State == maintenance.LaneCurrent {
				t.Fatalf("maintenance called an unpinned premise current")
			}
		})
	}
}
