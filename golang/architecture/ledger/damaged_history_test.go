// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHeadForTest(s *Store, h Head) error { return writeHead(s.headPath(), h) }

func hasErrorCode(errs []VerificationError, code string) bool {
	for _, e := range errs {
		if strings.Contains(e.Code, code) {
			return true
		}
	}
	return false
}

func hasWarningCode(warns []VerificationWarning, code string) bool {
	for _, w := range warns {
		if strings.Contains(w.Code, code) {
			return true
		}
	}
	return false
}

// TAIL TRUNCATION IS DESTRUCTION, NOT STALENESS.
//
// A truncated chain is a valid PREFIX: every per-entry check -- sequence links, previous
// digests -- is satisfied, so nothing else objects. HEAD.yaml is the only witness that the
// chain was ever longer, and its disagreement was recorded as a WARNING. report.Valid is
// len(report.Errors) == 0, so warnings do not affect validity and no caller in the governance
// path inspects them.
//
// The consequence (sensei#352) is that a single file deletion resurrects a spent mutation
// capability: the fold sees genuine ABSENCE of admission_consumed, and absence is
// indistinguishable from deletion.
//
// Integrity checks verify LINKS, not LENGTH. This is the length check.
func TestTailTruncationIsAnErrorNotAWarning(t *testing.T) {
	store, chain := buildScopeChain(t, 4)
	if len(chain.Entries) != 4 {
		t.Fatalf("fixture built %d entries, want 4", len(chain.Entries))
	}
	files, err := listLedgerEntryFiles(store.ledgerDir())
	if err != nil {
		t.Fatal(err)
	}
	// Delete the HIGHEST-sequence entry and leave HEAD naming it. No tampering, no forgery:
	// a single deletion.
	if err := os.Remove(files[len(files)-1]); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		return // an error return is a verdict; silent validity is not
	}
	if report.Valid {
		t.Errorf("a truncated chain was reported VALID (errors=%d warnings=%d); "+
			"a reader may now reconstruct authority that the deleted entry had already spent",
			len(report.Errors), len(report.Warnings))
	}
	if !hasErrorCode(report.Errors, "head") {
		t.Errorf("no ERROR names the head that the chain no longer contains; got errors=%+v warnings=%+v",
			report.Errors, report.Warnings)
	}
}

// THE NEGATIVE CONTROL, AND THE REASON THE REPAIR IS NOT "MAKE head_stale AN ERROR".
//
// Disagreement between HEAD and the chain has a DIRECTION. An entry written whose HEAD update
// had not yet landed -- an interrupted append -- leaves HEAD naming an entry the chain still
// CONTAINS, at a lower sequence. That is recoverable staleness and must stay a warning, or
// every crashed append becomes an unrecoverable task.
//
// Truncation is the other direction: HEAD names a digest the chain does not contain at all.
// One predicate cannot serve both, which is why the repair distinguishes them rather than
// promoting the existing warning.
func TestAHeadBehindAChainItStillContainsRemainsARecoverableWarning(t *testing.T) {
	store, _ := buildScopeChain(t, 2)
	// Rewind HEAD to the FIRST entry: it is still present in the chain.
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	first := chain.Entries[0]
	if err := writeHeadForTest(store, Head{
		SchemaVersion:     HeadSchemaVersion,
		TaskID:            chain.TaskID,
		Sequence:          first.Entry.Sequence,
		EntryDigestSHA256: first.Entry.EntryDigestSHA256,
		EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(first.EntryPath))),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("a recoverable stale head returned an error: %v", err)
	}
	if !report.Valid {
		t.Errorf("an interrupted append (HEAD behind a chain that still contains it) was reported "+
			"INVALID, which makes every crashed append an unrecoverable task: %+v", report.Errors)
	}
	if !hasWarningCode(report.Warnings, "head_stale") {
		t.Errorf("the recoverable case lost its warning; nothing reports the disagreement at all: %+v", report.Warnings)
	}
}

// A task that genuinely has no chain yet must still verify, or no task could ever be created.
// The destroyed-history case is separated in tasksession, which can see the task's own state;
// the verifier cannot, because appendEntry verifies before it writes a payload and a legacy
// import writes artifacts before its first entry -- both legitimately show artifacts with an
// empty chain.
func TestATaskWithNoLedgerYetIsStillValid(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("a fresh task refused verification: %v", err)
	}
	if !report.Valid {
		t.Errorf("a task that has never had a ledger was reported INVALID: %+v", report.Errors)
	}
}

// THE SECOND DIRECTION: A DAMAGED HEAD IS RECOVERABLE, A SHORT CHAIN IS NOT.
//
// A HEAD naming a digest the chain does not contain is NOT sufficient evidence of truncation:
// a corrupted HEAD file names nonsense too. What separates them is whether HEAD claims MORE
// entries than the chain holds. Here HEAD names an unknown digest at a LOW sequence -- it
// attests to less than the chain has -- so the chain is the authority and HEAD is rebuilt.
//
// Without this control the repair would have been "HEAD's digest must be in the chain", which
// turns every damaged HEAD into an unrecoverable task.
func TestAHeadNamingAnUnknownDigestBelowTheChainLengthStaysRecoverable(t *testing.T) {
	store, chain := buildScopeChain(t, 3)
	if err := writeHeadForTest(store, Head{
		SchemaVersion:     HeadSchemaVersion,
		TaskID:            chain.TaskID,
		Sequence:          0,
		EntryDigestSHA256: "deadbeef",
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("a damaged HEAD returned an error: %v", err)
	}
	if !report.Valid {
		t.Errorf("a damaged HEAD over an INTACT 3-entry chain was reported INVALID, making every "+
			"corrupted HEAD an unrecoverable task: %+v", report.Errors)
	}
}

// THE TORN READ. HEAD AHEAD OF THE ENTRIES A READER SAW IS NOT TRUNCATION.
//
// A reader that does not hold the append lock can list the ledger directory BEFORE an entry
// lands and read HEAD AFTER it is published, so head.Sequence briefly exceeds the entries that
// listing contains. The entry is on disk the whole time.
//
// This is why the discriminator is not a length comparison. An earlier form of this repair
// compared head.Sequence against the entries loaded, and eight concurrent writers turned that
// transient window into "invalid ledger chain" -- a legitimate concurrent append refused as
// history damage. CI caught it; it is witnessed here so a length comparison cannot come back.
func TestAHeadAheadOfTheEntriesReadButStillOnDiskIsNotTruncation(t *testing.T) {
	store, chain := buildScopeChain(t, 3)
	last := chain.Entries[len(chain.Entries)-1]

	// HEAD claims a sequence beyond the chain while naming an entry that EXISTS.
	if err := writeHeadForTest(store, Head{
		SchemaVersion:     HeadSchemaVersion,
		TaskID:            chain.TaskID,
		Sequence:          len(chain.Entries) + 5,
		EntryDigestSHA256: last.Entry.EntryDigestSHA256,
		EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(last.EntryPath))),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("a head ahead of the read entries returned an error: %v", err)
	}
	if !report.Valid {
		t.Errorf("a HEAD ahead in SEQUENCE but naming an entry still on disk was reported "+
			"INVALID; a concurrent append would be refused as history damage: %+v", report.Errors)
	}
}
