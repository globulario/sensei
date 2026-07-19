// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"errors"
	"fmt"
	"testing"
)

// The typed verification failure exposes a closed class + reason via AsVerificationError,
// preserves the human-readable message, and survives wrapping — so a consumer classifies by
// type, never by text.
func TestVerificationError_TypedAndTextPreserved(t *testing.T) {
	inner := errors.New("boom")
	err := vfail(VerifyIntegrityFailure, "receipt_invalid", "receipt invalid", inner)
	if err.Error() != "receipt invalid: boom" {
		t.Fatalf("message not preserved: %q", err.Error())
	}
	ve, ok := AsVerificationError(fmt.Errorf("wrapped: %w", err))
	if !ok || ve.Class != VerifyIntegrityFailure || ve.ReasonCode != "receipt_invalid" {
		t.Fatalf("typed class lost through wrapping: %+v ok=%v", ve, ok)
	}
	if !errors.Is(ve.Cause, inner) {
		t.Fatal("cause must unwrap to the original error")
	}
	// A plain error is not a verification error (fails closed for the consumer).
	if _, ok := AsVerificationError(errors.New("plain")); ok {
		t.Fatal("a plain error must not classify as a verification error")
	}
	// msg-less form uses the cause text.
	if got := vfail(VerifyUnverifiable, "x", "", inner).Error(); got != "boom" {
		t.Fatalf("msg-less form = %q, want cause text", got)
	}
}

// An unreadable candidate descriptor is Readable=false with no claimed identity — treated as
// unrelated debris, never a scoped defect; it confers no authority.
func TestLoadCandidateDescriptor_UnreadableIsUnrelated(t *testing.T) {
	d := LoadCandidateDescriptor(t.TempDir(), "no-such-lineage")
	if d.Readable || d.ClaimedDomain != "" || len(d.ClaimedFiles) != 0 || d.ClaimedTaskID != "" {
		t.Fatalf("unreadable descriptor must carry no claimed identity: %+v", d)
	}
	if d.LineageID != "no-such-lineage" {
		t.Fatalf("lineage id must be retained: %+v", d)
	}
}
