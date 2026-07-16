// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

func ledgerEventTypes(t *testing.T, dir string) []closureprotocol.LedgerEventType {
	t.Helper()
	chain, err := ledger.NewStore(dir, ledger.WithPayloadValidator(cliValidator)).VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	var out []closureprotocol.LedgerEventType
	for _, ve := range chain.Entries {
		out = append(out, ve.Entry.EventType)
	}
	return out
}

func countEvent(types []closureprotocol.LedgerEventType, want closureprotocol.LedgerEventType) int {
	n := 0
	for _, et := range types {
		if et == want {
			n++
		}
	}
	return n
}

func indexOfEvent(types []closureprotocol.LedgerEventType, want closureprotocol.LedgerEventType) int {
	for i, et := range types {
		if et == want {
			return i
		}
	}
	return -1
}

// The exact observed change is recorded as a change_observed event between
// admission_consumed and scope_verified.
func TestVerifyAdmissionRecordsChangeObservedBeforeScopeVerified(t *testing.T) {
	repo, baseRev, baseTree, resultRev := verifyRepo(t, false)
	dir := seedLedger(t, baseRev, baseTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatal("admit failed")
	}
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 0 {
		t.Fatal("consume failed")
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 0 {
		t.Fatalf("verify-admission exit = %d, want 0", code)
	}
	types := ledgerEventTypes(t, dir)
	co := indexOfEvent(types, closureprotocol.LedgerEventChangeObserved)
	sv := indexOfEvent(types, closureprotocol.LedgerEventScopeVerified)
	cons := indexOfEvent(types, closureprotocol.LedgerEventAdmissionConsumed)
	if co < 0 || sv < 0 || !(cons < co && co < sv) {
		t.Fatalf("expected admission_consumed < change_observed < scope_verified, got %v", types)
	}
	if countEvent(types, closureprotocol.LedgerEventChangeObserved) != 1 {
		t.Fatalf("expected exactly one change_observed, got %v", types)
	}
}

// Crash after change_observed but before scope_verified: a re-run with an
// identical observation resumes from the recorded change without appending a
// second change_observed.
func TestVerifyAdmissionResumesIdenticalObservedChange(t *testing.T) {
	repo, baseRev, baseTree, resultRev := verifyRepo(t, false)
	dir := seedLedger(t, baseRev, baseTree, true)
	runAdmitChangeV2(dir, head(t, dir), "yaml")
	runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"})

	rec, err := admission.LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	actorDigest := closureprotocol.MustSemanticDigest(rec.Actor)
	observed, err := observeChange(repo, baseRev, resultRev, actorDigest, rec.Resolution.AuthorityResolutionDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the crash: change_observed recorded, scope_verified not yet.
	if _, err := admission.RecordChangeObserved(newAdmissionStore(dir), head(t, dir), rec.Base.Task, observed, nowUTC()); err != nil {
		t.Fatal(err)
	}
	if countEvent(ledgerEventTypes(t, dir), closureprotocol.LedgerEventScopeVerified) != 0 {
		t.Fatal("precondition: scope_verified should not exist yet")
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 0 {
		t.Fatalf("resume verify exit = %d, want 0", code)
	}
	types := ledgerEventTypes(t, dir)
	if countEvent(types, closureprotocol.LedgerEventChangeObserved) != 1 {
		t.Fatalf("resume must not append a second change_observed, got %v", types)
	}
	if countEvent(types, closureprotocol.LedgerEventScopeVerified) != 1 {
		t.Fatalf("resume must verify scope exactly once, got %v", types)
	}
}

// Crash then a DIFFERENT observation: the re-run fails closed (stale/divergent)
// and neither replaces the recorded change_observed nor records scope_verified.
func TestVerifyAdmissionRefusesDivergentObservedChange(t *testing.T) {
	repo, baseRev, baseTree, resultRev := verifyRepo(t, false)
	dir := seedLedger(t, baseRev, baseTree, true)
	runAdmitChangeV2(dir, head(t, dir), "yaml")
	runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"})

	rec, err := admission.LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	actorDigest := closureprotocol.MustSemanticDigest(rec.Actor)
	observed, err := observeChange(repo, baseRev, resultRev, actorDigest, rec.Resolution.AuthorityResolutionDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	// Record a divergent observation (an injected extra file) as change_observed.
	observed.Files = append(observed.Files, admission.ObservedFile{Path: "injected.go", ChangeType: "modify"})
	if _, err := admission.RecordChangeObserved(newAdmissionStore(dir), head(t, dir), rec.Base.Task, observed, nowUTC()); err != nil {
		t.Fatal(err)
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 3 {
		t.Fatalf("divergent verify exit = %d, want 3 (stale/divergent)", code)
	}
	types := ledgerEventTypes(t, dir)
	if countEvent(types, closureprotocol.LedgerEventChangeObserved) != 1 {
		t.Fatalf("divergent run must not replace change_observed, got %v", types)
	}
	if countEvent(types, closureprotocol.LedgerEventScopeVerified) != 0 {
		t.Fatalf("divergent run must not record scope_verified, got %v", types)
	}
}
