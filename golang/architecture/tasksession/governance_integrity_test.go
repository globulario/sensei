// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// corruptGovernedArtifact overwrites the referenced artifact of the latest event of
// et with bytes that no longer match its recorded digest, simulating a
// missing/malformed/drifted record while the hash-bound event payload is untouched.
func corruptGovernedArtifact(t *testing.T, taskDir string, et closureprotocol.LedgerEventType, key string) {
	t.Helper()
	chain, err := taskLedgerStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != et {
			continue
		}
		data, err := os.ReadFile(ve.PayloadPath)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			t.Fatal(err)
		}
		ref, ok := payload.Artifacts[key]
		if !ok {
			t.Fatalf("%s event has no artifact %q", et, key)
		}
		if err := os.WriteFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)), []byte(`{"tampered":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no %s event found", et)
}

// TestGovernanceCorruptConsumptionCannotRestoreGrant proves the fail-closed
// reducer: an admission_consumed event whose artifact is drifted is a hard error,
// never mistaken for "unconsumed" — it can never resurrect a spent mutation grant.
func TestGovernanceCorruptConsumptionCannotRestoreGrant(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)

	corruptGovernedArtifact(t, taskDir, closureprotocol.LedgerEventAdmissionConsumed, "capability_consumption")

	disp, err := governanceDisposition(taskDir, now, nil)
	var gerr *GovernanceError
	if !errors.As(err, &gerr) {
		t.Fatalf("want GovernanceError, got %v (disp %s grant=%v)", err, disp.Status, disp.GrantModify)
	}
	if disp.GrantModify {
		t.Fatal("a corrupt consumption must NEVER restore a mutation grant")
	}
}

// TestGovernanceCorruptScopeVerificationCannotReopen proves a drifted
// scope_verified artifact is a hard error, never "no terminal": it cannot reopen
// the task to a mutation grant nor be reported as a clean terminal.
func TestGovernanceCorruptScopeVerificationCannotReopen(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)
	recordScopeVerification(t, taskDir, true)

	corruptGovernedArtifact(t, taskDir, closureprotocol.LedgerEventScopeVerified, "scope_verification")

	disp, err := governanceDisposition(taskDir, now, nil)
	var gerr *GovernanceError
	if !errors.As(err, &gerr) {
		t.Fatalf("want GovernanceError, got %v (disp %s)", err, disp.Status)
	}
	if disp.GrantModify || disp.Terminal {
		t.Fatal("a corrupt scope verification must neither reopen a grant nor be terminal")
	}
}

// TestGovernanceIgnoresAppendDuringReduction proves single-snapshot folding: an
// append that lands after the reducer takes its verified-chain snapshot cannot
// change the reduced disposition (no mixed-ledger world).
func TestGovernanceIgnoresAppendDuringReduction(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now) // → admitted

	fired := false
	afterSnapshot := func(td string) {
		if fired {
			return
		}
		fired = true
		recordScopeVerification(t, td, true) // append a terminal AFTER the snapshot
	}

	disp, err := governanceDisposition(taskDir, now, afterSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !fired {
		t.Fatal("seam did not fire")
	}
	if disp.Terminal || disp.Status == StatusScopeVerified {
		t.Fatal("an append after the snapshot must not change the reduced disposition")
	}
	if disp.Status != StatusAdmitted {
		t.Fatalf("disposition = %s, want admitted (the pre-append snapshot)", disp.Status)
	}
}
