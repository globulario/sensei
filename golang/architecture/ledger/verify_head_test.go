// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"bytes"
	"context"
	"errors"
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

// headBytes returns HEAD.yaml verbatim, or nil when it is absent. Tests compare
// it across a would-be repair: "no HEAD is written" has to be checked on the
// bytes, because a repair that rewrote HEAD to the same value it already held
// would still be a repair that ran.
func headBytes(t *testing.T, taskDir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(taskDir, "ledger", "HEAD.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return data
}

// survivingTipIdentity is the strongest evidence a tail truncation leaves
// available: the committed-entry identity of the entry that is STILL on disk,
// shaped exactly as a real append would report it.
//
// This is the forgery at the centre of the round-two review finding. A recovery
// that took this identity as an argument and checked it against the chain would
// pass every check -- the digest matches, the sequence matches, the path matches
// -- because the caller simply names whatever survived the deletion. The value
// proves nothing about who committed the entry, so no public API may accept it.
func survivingTipIdentity(t *testing.T, store *Store) ErrEntryDurable {
	t.Helper()
	// Read straight off the entry files, the way a caller who just truncated the
	// chain would: the chain no longer verifies, so no verified accessor is
	// available -- and none is needed to construct this.
	entries, err := listLedgerEntryFiles(filepath.Join(store.taskDir, "ledger"))
	if err != nil || len(entries) == 0 {
		t.Fatal("no entries survived the truncation")
	}
	tip, err := readEntry(entries[len(entries)-1])
	if err != nil {
		t.Fatal(err)
	}
	return ErrEntryDurable{
		Entry: tip,
		Head: Head{
			SchemaVersion:     HeadSchemaVersion,
			TaskID:            tip.Task.ID,
			Sequence:          tip.Sequence,
			EntryDigestSHA256: tip.EntryDigestSHA256,
			EntryPath:         "ledger/" + ledgerEntryFilename(tip.Sequence, tip.EventType, tip.EntryDigestSHA256),
		},
		Detail: "injected: HEAD publication failed",
	}
}

// TestNoPublicPathRepublishesHeadForATruncatedChain is the regression for the
// round-two review finding, and it is the half of issue #352 that a passing
// truncation test can still hide.
//
// Classifying a stale or absent HEAD as an error only holds while no repair
// undoes the classification. The first attempt at this repair kept a recovery
// that republished HEAD when the caller presented the committed-entry identity
// and the chain tip matched it. That looked proof-bound and was not: the identity
// is an ordinary exported value and the method was public, so after deleting the
// tail a caller constructs the identity of the entry that SURVIVED, every
// equality check passes, HEAD is republished over the truncated prefix, and the
// deletion has been laundered into a valid ledger -- the spent mutation
// capability comes back for the cost of one extra call.
//
// So this drives every public derived-state path against both truncation
// variants WHILE HOLDING THAT FORGED IDENTITY, and requires that HEAD.yaml is
// byte-identical afterwards and the ledger still refuses.
func TestNoPublicPathRepublishesHeadForATruncatedChain(t *testing.T) {
	for _, variant := range []struct {
		name       string
		removeHead bool
		wantCode   string
	}{
		{name: "stale HEAD", removeHead: false, wantCode: codeHeadStale},
		{name: "absent HEAD", removeHead: true, wantCode: codeHeadMissing},
	} {
		t.Run(variant.name, func(t *testing.T) {
			taskDir := t.TempDir()
			store, tail := appendTruncationChain(t, taskDir)
			deleteTailEntry(t, taskDir, tail)
			if variant.removeHead {
				if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
					t.Fatal(err)
				}
			}
			headBefore := headBytes(t, taskDir)

			// Constructed, not observed: this is exactly what a caller who deleted
			// the tail can hand to any API that will take it.
			forged := survivingTipIdentity(t, store)
			if forged.Entry.EntryDigestSHA256 == tail.EntryDigestSHA256 {
				t.Fatal("the forged identity names the deleted entry; it must name the SURVIVING tip to be the real attack")
			}

			stillRefused := func(after string) {
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
				if got := headBytes(t, taskDir); !bytes.Equal(got, headBefore) {
					t.Fatalf("%s rewrote HEAD for a chain it cannot prove is whole (before=%q after=%q)", after, headBefore, got)
				}
			}

			if _, err := store.ReconcileDerivedState(); err == nil {
				t.Fatal("ReconcileDerivedState repaired a truncated chain; it must require a chain that already verifies")
			}
			stillRefused("ReconcileDerivedState")

			if _, err := RebuildProjections(taskDir, testPayloadValidator); err == nil {
				t.Fatal("RebuildProjections accepted a truncated chain")
			}
			stillRefused("RebuildProjections")

			// An append is the one operation that may publish HEAD, and it refuses
			// to run at all on a ledger that does not verify -- so the forged
			// identity has nowhere left to go.
			_, err := store.Append(context.Background(), AppendRequest{
				TaskID: "task.example", SessionID: "session.example",
				ExpectedHeadDigestSHA256: forged.Entry.EntryDigestSHA256,
				EventType:                closureprotocol.LedgerEventClosureAssessed,
				Payload:                  testPayload{SchemaVersion: "1", Message: "resurrect"},
				PayloadMediaType:         "application/yaml", ProducerID: "sensei.test",
				ProducedAt: time.Date(2026, 7, 15, 12, 10, 0, 0, time.UTC),
			})
			if err == nil {
				t.Fatal("Append extended a ledger that does not verify")
			}
			stillRefused("Append")
		})
	}
}

// TestAppendRecoversItsOwnUnpublishedHead is the positive control: the refusals
// above are only meaningful if the genuine post-commit condition still recovers.
//
// It recovers in the one place the evidence actually exists -- inside the Append
// that minted the entry, under the lock it still holds, on a HEAD value it
// computed itself. Nothing is taken from a caller, so there is nothing to forge.
func TestAppendRecoversItsOwnUnpublishedHead(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir,
		WithPayloadValidator(testPayloadValidator),
		WithHeadPublicationFault(errors.New("injected: HEAD publication failed")),
	)
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("the bounded in-append retry must absorb a single failed HEAD publication: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.HeadDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Fatalf("the append did not publish HEAD after its retry: %+v", report)
	}
}

// TestAppendLeavesTheLedgerInvalidWhenHeadIsNeverPublished is the fail-closed
// end of the same contract. When the retry also fails, HEAD was never published
// and NOTHING may repair it afterwards: this is byte-for-byte the state a
// deleted tail entry produces, so the ledger stays invalid and the durable
// identity is reported rather than acted on.
func TestAppendLeavesTheLedgerInvalidWhenHeadIsNeverPublished(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir,
		WithPayloadValidator(testPayloadValidator),
		WithHeadPublicationFaults(2, errors.New("injected: HEAD publication failed")),
	)
	_, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	var durable ErrEntryDurable
	if err == nil || !errorAs(err, &durable) {
		t.Fatalf("want ErrEntryDurable when both publications fail, got %v", err)
	}
	if durable.Entry.EntryDigestSHA256 == "" {
		t.Fatal("the post-commit error must carry the committed entry identity")
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("a chain whose HEAD was never published must not verify: %+v", report)
	}
	if !reportHasError(report, codeHeadMissing) {
		t.Fatalf("expected %s, got %+v", codeHeadMissing, report.Errors)
	}
	// Handing the durable identity straight back to the public surface repairs
	// nothing: there is no method that accepts it, and the generic path refuses.
	if _, rerr := store.ReconcileDerivedState(); rerr == nil {
		t.Fatal("ReconcileDerivedState published a HEAD for a chain it cannot prove is whole")
	}
	if headBytes(t, taskDir) != nil {
		t.Fatal("HEAD was written by a path that holds no proof of the append")
	}
}
