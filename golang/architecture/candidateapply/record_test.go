// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"context"
	"reflect"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// applied returns a real application and its receipt, since every property
// below is about linking to an application that actually happened.
func applied(t *testing.T) (applyFixture, Receipt) {
	t.Helper()
	fixture := newApplyFixture(t)
	_, receipt, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return fixture, receipt
}

// THE property Decision A exists for: recording a verification later must not
// touch the application receipt. The receipt is the consumption record for one
// application, and rewriting it when verification arrives would make the
// document that says WHAT WAS APPLIED depend on something observed afterwards.
func TestRecordingLeavesTheApplicationReceiptByteIdentical(t *testing.T) {
	fixture, receipt := applied(t)
	before := receipt

	verification := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)
	record, err := RecordVerification(receipt, fixture.input.Decision, verification, "2026-08-02T00:05:00Z")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !reflect.DeepEqual(receipt, before) {
		t.Fatal("recording a verification mutated the application receipt")
	}
	// Byte-identity, not just field equality: the receipt's own digest must
	// still be the digest of the receipt.
	afterDigest, err := ReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if afterDigest != before.ReceiptDigestSHA256 {
		t.Fatal("the application receipt no longer digests to its recorded identity")
	}
	if record.ApplicationReceiptDigestSHA256 != before.ReceiptDigestSHA256 {
		t.Fatalf("the record names application %s, want %s", record.ApplicationReceiptDigestSHA256, before.ReceiptDigestSHA256)
	}
	if record.PatchDigestSHA256 != before.PatchDigestSHA256 || record.CandidateArtifactDigestSHA256 != before.CandidateArtifactDigestSHA256 {
		t.Fatal("the record does not bind the exact applied candidate")
	}
}

// A scope-violated verification is a fact about the applied result, and
// recording it is a success of the RECORDING even though it reports a failure
// of the application. Refusing to record it would leave the strongest evidence
// against a change as the only evidence nobody kept.
func TestAScopeViolationIsRecordable(t *testing.T) {
	fixture, receipt := applied(t)

	violated := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeViolated)
	record, err := RecordVerification(receipt, fixture.input.Decision, violated, "2026-08-02T00:05:00Z")
	if err != nil {
		t.Fatalf("a scope violation must be recordable: %v", err)
	}
	if record.AdmissionVerificationStatus != admission.VerificationScopeViolated {
		t.Fatalf("the record carries status %q, want the admission owner's verbatim", record.AdmissionVerificationStatus)
	}
	if record.ApplicationReceiptDigestSHA256 != receipt.ReceiptDigestSHA256 {
		t.Fatal("a violated verification did not bind to the exact application")
	}
}

// The lineage refusals, each constructed independently: a record that bound
// the wrong application would be worse than no record, because it would read
// as evidence about something it never described.
func TestRecordRefusesBrokenLineage(t *testing.T) {
	fixture, receipt := applied(t)
	good := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)

	t.Run("wrong patch digest", func(t *testing.T) {
		wrong := canonicalVerificationFixture(t, fixture.input.Decision, hex64("another-patch"), admission.VerificationScopeCompliant)
		if _, err := RecordVerification(receipt, fixture.input.Decision, wrong, "2026-08-02T00:05:00Z"); err == nil {
			t.Fatal("a verification of a different patch was recorded against this application")
		}
	})

	t.Run("wrong admission id", func(t *testing.T) {
		wrong := good
		wrong.AdmissionID = "admission.someone-elses"
		if _, err := RecordVerification(receipt, fixture.input.Decision, wrong, "2026-08-02T00:05:00Z"); err == nil {
			t.Fatal("a verification from another admission was recorded")
		}
	})

	t.Run("wrong decision digest", func(t *testing.T) {
		wrong := good
		wrong.DecisionDigestSHA256 = hex64("another-decision")
		if _, err := RecordVerification(receipt, fixture.input.Decision, wrong, "2026-08-02T00:05:00Z"); err == nil {
			t.Fatal("a verification of another decision was recorded")
		}
	})

	t.Run("wrong binding", func(t *testing.T) {
		wrong := good
		wrong.Binding.RepositoryDomain = "github.com/example/elsewhere"
		if _, err := RecordVerification(receipt, fixture.input.Decision, wrong, "2026-08-02T00:05:00Z"); err == nil {
			t.Fatal("a verification bound to another repository was recorded")
		}
	})

	t.Run("decision changed after application", func(t *testing.T) {
		moved := fixture.input.Decision
		moved.DecisionDigestSHA256 = hex64("decision-moved")
		if _, err := RecordVerification(receipt, moved, good, "2026-08-02T00:05:00Z"); err == nil {
			t.Fatal("a decision that changed after the application was accepted")
		}
	})
}

// Idempotence, pinned: re-recording the SAME verification against the SAME
// application yields the same record identity, so a retried command cannot
// manufacture a second logically distinct record of one event. A DIFFERENT
// verification of the same application is a different record, not a rewrite.
func TestRecordIdentityIsTheApplicationAndTheVerification(t *testing.T) {
	fixture, receipt := applied(t)
	verification := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)

	first, err := RecordVerification(receipt, fixture.input.Decision, verification, "2026-08-02T00:05:00Z")
	if err != nil {
		t.Fatal(err)
	}
	// A later clock is the same fact, so it must be the same record.
	again, err := RecordVerification(receipt, fixture.input.Decision, verification, "2026-08-09T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.RecordDigestSHA256 != again.RecordDigestSHA256 {
		t.Fatal("re-recording the same verification produced a second, different record")
	}

	other := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeViolated)
	different, err := RecordVerification(receipt, fixture.input.Decision, other, "2026-08-02T00:06:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if different.RecordDigestSHA256 == first.RecordDigestSHA256 {
		t.Fatal("two different verifications collapsed into one record identity")
	}
}

// The record must not be constructible for an application that never happened.
func TestRecordRequiresAnApplication(t *testing.T) {
	fixture, receipt := applied(t)
	verification := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)

	refused := receipt
	refused.Disposition = "refused"
	if _, err := RecordVerification(refused, fixture.input.Decision, verification, "2026-08-02T00:05:00Z"); err == nil {
		t.Fatal("a verification was recorded against an application that did not happen")
	}
}

func TestVerificationRecordValidationFailsClosed(t *testing.T) {
	fixture, receipt := applied(t)
	verification := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)
	record, err := RecordVerification(receipt, fixture.input.Decision, verification, "2026-08-02T00:05:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateVerificationRecord(record); err != nil {
		t.Fatalf("a freshly composed record failed its own validation: %v", err)
	}

	tampered := record
	tampered.AdmissionVerificationStatus = admission.VerificationScopeCompliant + "-ish"
	if err := ValidateVerificationRecord(tampered); err == nil {
		t.Fatal("a record whose content no longer matches its digest validated")
	}
}
