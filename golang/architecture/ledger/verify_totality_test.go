// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ONE CORRUPT ENTRY MUST PRODUCE A VERDICT, NOT A PANIC.
//
// verifyAndLoadChain iterates the FILES slice and, for an unreadable or invalid entry,
// records the error and continues WITHOUT appending to out.Entries. Every later iteration
// then read out.Entries[idx-1] -- a files index used against the entries slice. After any
// skipped entry the two diverge, so the read runs off the end.
//
// Every admission loader calls VerifyChain, and appendEntry verifies before every append,
// so this takes a task's whole governance path down by panic rather than producing the
// typed refusal the design calls for. A fail-closed system that panics has no verdict to
// fail closed WITH: the caller never reaches its refusal branch, and recovers nothing it
// could report.
func TestOneCorruptEntryReturnsATypedRefusalRatherThanPanicking(t *testing.T) {
	store, chain := buildScopeChain(t, 5)
	if len(chain.Entries) != 5 {
		t.Fatalf("fixture built %d entries, want 5", len(chain.Entries))
	}

	// Corrupt the FIRST entry, so the files slice and the entries slice diverge at the
	// earliest possible point and every subsequent entry indexes past the end.
	files, err := listLedgerEntryFiles(store.ledgerDir())
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if err := os.WriteFile(files[0], []byte("{not yaml at all"), 0o600); err != nil {
		t.Fatalf("corrupt first entry: %v", err)
	}

	report, err := store.Verify()
	if err != nil {
		// An error return is an acceptable verdict; a panic is not.
		return
	}
	if report.Valid {
		t.Fatal("a chain whose first entry is unreadable was reported VALID")
	}
	if len(report.Errors) == 0 {
		t.Fatal("no error names the unreadable entry, so nothing states why the chain is invalid")
	}
	var named bool
	for _, e := range report.Errors {
		if strings.Contains(e.Code, "entry_unreadable") || strings.Contains(e.Code, "entry_invalid") {
			named = true
			if filepath.Base(e.Path) != filepath.Base(files[0]) {
				t.Errorf("the refusal blames %q, not the entry that is actually corrupt (%q)",
					e.Path, filepath.Base(files[0]))
			}
		}
	}
	if !named {
		t.Errorf("no typed error identifies the corrupt entry; got %+v", report.Errors)
	}
}

// The divergence is not special to the first entry: a corrupt entry ANYWHERE makes the two
// slices disagree from that point on. Driven separately so a repair that only special-cases
// index 0 does not pass.
func TestACorruptEntryInTheMiddleAlsoReturnsAVerdict(t *testing.T) {
	store, _ := buildScopeChain(t, 5)
	files, err := listLedgerEntryFiles(store.ledgerDir())
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if err := os.WriteFile(files[2], []byte("{not yaml at all"), 0o600); err != nil {
		t.Fatalf("corrupt middle entry: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		return
	}
	if report.Valid {
		t.Fatal("a chain with an unreadable middle entry was reported VALID")
	}
}

// A REFUSAL MUST NAME THE ENTRY THAT IS ACTUALLY AT FAULT.
//
// The task id was read from files[0]. When files[0] is the corrupt one it is skipped, so
// out.TaskID stayed empty and every later entry compared its own id against "" -- reporting
// "task id changes within one chain" for a chain in which no id changed. One real defect
// then arrives as a cascade of invented ones, and an operator reading the report repairs the
// wrong entry.
//
// This is the half of the repair the panic witnesses cannot reach: they stop at "did it
// return a verdict", and a verdict full of fabricated errors is still a verdict.
func TestTheRefusalDoesNotFabricateFailuresForEntriesThatAreIntact(t *testing.T) {
	store, _ := buildScopeChain(t, 5)
	files, err := listLedgerEntryFiles(store.ledgerDir())
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if err := os.WriteFile(files[0], []byte("{not yaml at all"), 0o600); err != nil {
		t.Fatalf("corrupt first entry: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Skipf("verification returned an error rather than a report: %v", err)
	}
	for _, e := range report.Errors {
		if strings.Contains(e.Code, "task_id_changed") {
			t.Errorf("the chain is reported to change task id, but only one entry was corrupted "+
				"and no id changed: %+v", e)
		}
		if strings.Contains(e.Code, "previous_digest_mismatch") {
			t.Errorf("an intact entry is blamed for a broken link to an entry that simply could "+
				"not be read: %+v", e)
		}
	}
	// And the real fault is still reported: this must not pass by reporting nothing at all.
	if report.Valid || len(report.Errors) == 0 {
		t.Fatal("no fault reported for a chain whose first entry is unreadable")
	}
}
