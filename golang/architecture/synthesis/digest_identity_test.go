// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import "testing"

// A terminal receipt's identity is WHAT the session concluded, not when the
// conclusion was stamped.
//
// This was the last open row of #149's proof matrix, and it was a real
// divergence rather than a property of replay. runnercomposition.RunnerReceipt
// and evaluatorcomposition.EvaluationReceipt both zero their completion
// timestamp before hashing; synthesis.Receipt did not, so the O1 terminal
// receipt was the only member of the chain whose identity moved with the clock.
// Because the run receipt carries it, two replays that reached an identical
// conclusion from identical inputs could not produce the same run receipt.
//
// Worse than the divergence: evaluatorcomposition's own comment asserted that
// synthesis.Receipt followed the convention. A reader checking was told it held.
func TestReceiptIdentityIsWhatConcludedNotWhenItWasStamped(t *testing.T) {
	receipt := Receipt{
		SchemaVersion:  ReceiptSchemaVersion,
		ReceiptID:      "receipt.identity.test",
		TerminalReason: ReasonCandidateReadyForAdmission,
		CompletedAt:    "2026-08-02T00:00:01Z",
	}
	first, err := ReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}

	later := receipt
	later.CompletedAt = "2026-09-14T11:32:57Z"
	second, err := ReceiptDigest(later)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the same conclusion stamped at two times produced two identities:\n1: %s\n2: %s", first, second)
	}

	// The timestamp is still recorded -- excluded from identity is not the same
	// as discarded, and an auditor still needs to know when this happened.
	if later.CompletedAt == "" {
		t.Fatal("zeroing for the digest must not clear the field itself")
	}

	// And identity must still move when the CONCLUSION moves, or the exclusion
	// above would have made the digest indifferent to everything.
	different := receipt
	different.Summary = "the session ended for a different reason"
	third, err := ReceiptDigest(different)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("two different conclusions share one receipt identity")
	}
}
