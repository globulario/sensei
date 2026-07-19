// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// TestReloadUsesSingleChainSnapshot proves an append between the reload's chain
// snapshot and the upstream reconstruction cannot mix ledger worlds: even after a
// NEW, conflicting scope_verified event is appended on disk, the reconstruction
// (which reads only the frozen snapshot) still reloads and validates the recorded
// transition. A multi-read reconstruction would have picked up the new event and
// failed the digest cross-check.
func TestReloadUsesSingleChainSnapshot(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}

	// The seam appends a divergent scope_verified event once, after the reload's
	// chain snapshot is taken.
	fired := false
	afterChainSnapshot = func(td string) {
		if fired {
			return
		}
		fired = true
		store := ledger.NewStore(td, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
			return ledger.ValidateTaskEventPayload(et, data)
		}))
		head, err := admission.TaskLedgerHead(td)
		if err != nil {
			t.Fatal(err)
		}
		divergent := admission.ScopeVerification{
			CapabilityID: "cap.divergent", DecisionDigestSHA256: "d", ActorBindingDigestSHA256: "a",
			AuthorityResolutionDigestSHA256: "au", BaseTreeDigestSHA256: "b", ResultTreeDigestSHA256: "r",
			ObservedChangeSetDigestSHA256: "o", VerifiedOperationIDs: []string{"op.9"},
			Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-19T00:00:00Z",
		}
		if _, err := admission.RecordScopeVerified(store, head, c.Receipt.Task, divergent, time.Unix(0, 0).UTC()); err != nil {
			t.Fatalf("append divergent scope: %v", err)
		}
	}
	defer func() { afterChainSnapshot = nil }()

	rt, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		t.Fatalf("reload after concurrent append: %v", err)
	}
	if err := ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("validate after concurrent append: %v", err)
	}
	if !fired {
		t.Fatal("seam did not fire")
	}
}
