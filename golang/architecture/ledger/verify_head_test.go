// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// appendTruncationChain builds a valid two-entry chain and returns the store
// together with the tail entry, so a test can delete exactly the highest-sequence
// entry file and observe what verification then says.
func appendTruncationChain(t *testing.T, taskDir string) (*Store, Entry) {
	t.Helper()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
		EventType:        closureprotocol.LedgerEventClosureAssessed,
		Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 2 {
		t.Fatalf("chain was not valid before truncation: %+v", report)
	}
	return store, second.Entry
}

// deleteTailEntry removes the highest-sequence entry file from the chain.
func deleteTailEntry(t *testing.T, taskDir string, tail Entry) {
	t.Helper()
	path := filepath.Join(taskDir, "ledger", ledgerEntryFilename(tail.Sequence, tail.EventType, tail.EntryDigestSHA256))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func reportHasError(report VerificationReport, code string) bool {
	for _, e := range report.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// TestVerifyRejectsTruncatedTailWithStaleHead is the truncation attack of issue
// #352: deleting the highest-sequence entry leaves HEAD pointing at an entry that
// is no longer on the chain. A ledger whose HEAD disagrees with its entries is
// damaged history, not an advisory, so it must not verify as valid.
func TestVerifyRejectsTruncatedTailWithStaleHead(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	deleteTailEntry(t, taskDir, tail)

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("truncated chain verified as valid: %+v", report)
	}
	if !reportHasError(report, "ledger.head_stale") {
		t.Fatalf("expected ledger.head_stale error, got %+v", report.Errors)
	}
}

// TestVerifyRejectsTruncatedTailWithHeadRemoved is the same attack carried one
// deletion further. If only the stale case is promoted to an error, removing
// HEAD.yaml as well restores a clean verdict, because an absent HEAD matches
// neither branch of the HEAD check and reports nothing at all. A chain that has
// entries but no HEAD is damaged too.
func TestVerifyRejectsTruncatedTailWithHeadRemoved(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	deleteTailEntry(t, taskDir, tail)
	if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("truncated chain with HEAD removed verified as valid: %+v", report)
	}
	if reportHasError(report, "ledger.head_stale") || reportHasError(report, "ledger.head_unreadable") {
		t.Fatalf("an absent HEAD is its own fact, not stale or unreadable: %+v", report.Errors)
	}
	if !reportHasError(report, "ledger.head_missing") {
		t.Fatalf("expected ledger.head_missing error, got %+v", report.Errors)
	}
}

// TestVerifyAcceptsEmptyLedgerWithNoHead pins the case the repair must not
// damage: a task with no entries and no HEAD has no chain yet. It is fresh, not
// truncated, and must keep verifying exactly as it did before.
func TestVerifyAcceptsEmptyLedgerWithNoHead(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("empty ledger must verify as valid: %+v", report)
	}
	if report.EntryCount != 0 || len(report.Errors) != 0 {
		t.Fatalf("empty ledger must report no entries and no errors: %+v", report)
	}
}

// durableEvidenceFor is the ErrEntryDurable a real append produces for entry:
// the committed entry together with the HEAD that publication would have
// written. A test that truncates the chain holds exactly this and nothing
// better, which is the point -- it is the strongest proof an attacker who
// deleted the tail could present to the recovery path.
func durableEvidenceFor(entry Entry) ErrEntryDurable {
	return ErrEntryDurable{
		Entry: entry,
		Head: Head{
			SchemaVersion:     HeadSchemaVersion,
			TaskID:            entry.Task.ID,
			Sequence:          entry.Sequence,
			EntryDigestSHA256: entry.EntryDigestSHA256,
			EntryPath:         "ledger/" + ledgerEntryFilename(entry.Sequence, entry.EventType, entry.EntryDigestSHA256),
		},
	}
}

// TestReconciliationCannotLaunderATruncatedChain is the companion to the two
// verification witnesses above, and it is the half of the repair that is easy to
// miss. Classifying a stale or absent HEAD as an error only holds if no repair
// undoes the classification: a derived-state reconciliation that recomputes HEAD
// from "the entries that are there" turns a truncated prefix into a matching
// HEAD, and the very next verification is clean. Deleting the tail would then
// cost one extra call rather than being impossible.
//
// So every public path that writes derived state is run against both truncation
// variants, and each must refuse and leave the ledger invalid.
func TestReconciliationCannotLaunderATruncatedChain(t *testing.T) {
	for _, variant := range []struct {
		name      string
		removeHea bool
		wantCode  string
	}{
		{name: "stale HEAD", removeHea: false, wantCode: codeHeadStale},
		{name: "absent HEAD", removeHea: true, wantCode: codeHeadMissing},
	} {
		t.Run(variant.name, func(t *testing.T) {
			taskDir := t.TempDir()
			store, tail := appendTruncationChain(t, taskDir)
			deleteTailEntry(t, taskDir, tail)
			if variant.removeHea {
				if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
					t.Fatal(err)
				}
			}

			stillInvalid := func(t *testing.T, after string) {
				t.Helper()
				report, err := store.Verify()
				if err != nil {
					t.Fatal(err)
				}
				if report.Valid {
					t.Fatalf("the truncated ledger verified as valid after %s: the repair laundered the deletion", after)
				}
				if !reportHasError(report, variant.wantCode) {
					t.Fatalf("after %s the ledger no longer reports %s: %+v", after, variant.wantCode, report.Errors)
				}
			}

			if _, err := store.ReconcileDerivedState(); err == nil {
				t.Fatal("ReconcileDerivedState repaired a truncated chain; it must require a chain that already verifies")
			}
			stillInvalid(t, "ReconcileDerivedState")

			if _, err := RebuildProjections(taskDir, testPayloadValidator); err == nil {
				t.Fatal("RebuildProjections accepted a truncated chain")
			}
			stillInvalid(t, "RebuildProjections")

			// The proof-bound path, handed the best evidence the deletion leaves
			// available: the tail entry that really was committed. The chain no
			// longer ends in it, so HEAD is not republished.
			_, err := store.RecoverDurableAppend(context.Background(), durableEvidenceFor(tail))
			var rec *ReconcileError
			if err == nil || !errorAs(err, &rec) || rec.Code != CodeDurableAppendUnproven {
				t.Fatalf("RecoverDurableAppend must refuse a chain that does not end in the committed entry, got %v", err)
			}
			stillInvalid(t, "RecoverDurableAppend")
		})
	}
}

// TestRecoverDurableAppendRepublishesTheProvenHead is the positive control. The
// refusals above are only meaningful if the genuine post-commit condition -- the
// entry is durable and HEAD was never written -- still recovers; otherwise the
// repair would have closed the attack by breaking the recovery.
func TestRecoverDurableAppendRepublishesTheProvenHead(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	// Exactly what a failed HEAD publication leaves behind: every entry durable,
	// HEAD absent.
	if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Fatal(err)
	}

	rec, err := store.RecoverDurableAppend(context.Background(), durableEvidenceFor(tail))
	if err != nil {
		t.Fatalf("the genuine durable-append recovery must still work: %v", err)
	}
	if !rec.HeadRewritten {
		t.Fatal("expected HEAD to be republished from the verified chain")
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.HeadDigestSHA256 != tail.EntryDigestSHA256 {
		t.Fatalf("ledger did not recover: %+v", report)
	}
}

// TestRecoverDurableAppendRefusesDamageBeyondHead keeps the proof-bound path from
// becoming a general-purpose repair. Its licence covers the HEAD pointer only; a
// chain whose entries are damaged is not something a HEAD write fixes.
func TestRecoverDurableAppendRefusesDamageBeyondHead(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	// Corrupt the tail's payload: the chain tip still has the right digest, so the
	// identity proof passes and only the extra damage can refuse.
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chain.Entries[len(chain.Entries)-1].PayloadPath, []byte("message: tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.RecoverDurableAppend(context.Background(), durableEvidenceFor(tail))
	var rec *ReconcileError
	if err == nil || !errorAs(err, &rec) || rec.Code != CodeDurableAppendUnproven {
		t.Fatalf("recovery must refuse a chain damaged beyond HEAD, got %v", err)
	}
}
