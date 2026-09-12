// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyCorruptEntryRefusesWithoutPanic is the required test for
// invariant.ledger_verification_returns_a_typed_refusal_never_panics.
//
// verifyAndLoadChain records an unreadable or invalid entry and skips it WITHOUT
// appending, so the enumeration index of the files slice and the length of the
// accumulated slice diverge after the first skip. Indexing the accumulated
// entries by the file index then reads past the end: one corrupt FIRST entry
// produced "panic: runtime error: index out of range [0] with length 0"
// (issue #356).
//
// A panic is not a refusal. Every governed read calls VerifyChain, so a verifier
// that dies leaves the governance path with no verdict to fail closed on -- the
// caller gets a crash where it needed an answer. The verdict must be typed, and
// it must be "invalid".
//
// The corruption is placed at each position in turn: the first entry is the case
// that panicked, but a skip anywhere makes the two indices diverge for every
// entry after it, so a fix that special-cased position zero would still be wrong.
func TestVerifyCorruptEntryRefusesWithoutPanic(t *testing.T) {
	for _, corrupt := range []struct {
		name  string
		index int
	}{
		{name: "first entry", index: 0},
		{name: "last entry", index: 1},
	} {
		t.Run(corrupt.name, func(t *testing.T) {
			taskDir := t.TempDir()
			store, _ := appendTruncationChain(t, taskDir)
			files, err := listLedgerEntryFiles(filepath.Join(taskDir, "ledger"))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 2 {
				t.Fatalf("expected a two-entry chain, got %d", len(files))
			}
			if err := os.WriteFile(files[corrupt.index], []byte("{{{ not yaml at all"), 0o644); err != nil {
				t.Fatal(err)
			}

			// The call itself is the assertion: an index-out-of-range here fails the
			// test as a panic rather than returning anything.
			report, err := store.Verify()
			if err != nil {
				t.Fatalf("verification must return a report, not a hard error: %v", err)
			}
			if report.Valid {
				t.Fatalf("a chain with a corrupt entry must not verify: %+v", report)
			}
			if len(report.Errors) == 0 {
				t.Fatal("an invalid chain must say what is wrong; a bare Valid=false is not a refusal")
			}
			if !reportHasError(report, "ledger.entry_unreadable") {
				t.Fatalf("expected ledger.entry_unreadable, got %+v", report.Errors)
			}
		})
	}
}

// TestVerifyCorruptFirstEntryDoesNotRenumberTheSurvivors guards the fix's other
// half. Deriving the sequence check from the accumulated slice is only correct
// if a skipped entry still breaks the chain: if expectedSeq simply followed the
// survivors, a corrupt first entry would leave entry 2 looking like a valid
// entry 1 and the remaining suffix would verify as a whole chain -- the same
// truncation laundering issue #352 is about, arriving through a corrupt file
// instead of a deleted one.
func TestVerifyCorruptFirstEntryDoesNotRenumberTheSurvivors(t *testing.T) {
	taskDir := t.TempDir()
	store, _ := appendTruncationChain(t, taskDir)
	files, err := listLedgerEntryFiles(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], []byte("{{{ not yaml at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("the surviving suffix verified as a whole chain: %+v", report)
	}
	// The survivor is entry 2 and must still be judged against sequence 1, which
	// it is not -- so the gap is reported rather than silently closed.
	if !reportHasError(report, "ledger.sequence_gap") {
		t.Fatalf("expected ledger.sequence_gap so the skipped entry still breaks the chain, got %+v", report.Errors)
	}
}
