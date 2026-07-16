// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// TestLoadRecordedAuthorityRoundTrips proves the read side reconstructs exactly
// what the writer substrate recorded: the loaded resolution's digest still
// matches, and DecideAdmission binds to it — so a downstream command can
// operate on verified records, not caller flags.
func TestLoadRecordedAuthorityRoundTrips(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.load", SessionID: "session.load"}
	store, dir, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	if _, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, ledgerProducedAt()); err != nil {
		t.Fatalf("RecordAuthorityResolved: %v", err)
	}

	loaded, err := LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatalf("LoadRecordedAuthority: %v", err)
	}
	// The reconstructed resolution must digest identically to the original.
	origDigest, _ := closureprotocol.AuthorityResolutionDigest(resolution)
	loadDigest, _ := closureprotocol.AuthorityResolutionDigest(loaded.Resolution)
	if origDigest != loadDigest {
		t.Fatalf("loaded resolution digest %s != original %s", loadDigest, origDigest)
	}

	// And admission binds to the loaded records end-to-end.
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    loaded.Actor,
		BaseBinding:                     loaded.Base,
		ChangePlan:                      loaded.ChangePlan,
		AuthorityResolutionDigestSHA256: loaded.Resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        "admission.strict.v2",
	}
	policy := AdmissionV2Policy{PolicyID: "admission.strict.v2", CompletionPolicyID: "completion.architectural_closure.v1"}
	decision, err := DecideAdmission(req, loaded.Resolution, policy, v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission against loaded records: %v", err)
	}
	if !AllAdmitted(decision) {
		t.Fatalf("expected admission from loaded records, got %+v", decision.OperationVerdicts)
	}
}

func TestLoadRecordedAuthorityAbsentFailsClosed(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.empty", SessionID: "session.empty"}
	_, dir, _ := admissionLedgerStore(t, task)
	if _, err := LoadRecordedAuthority(dir); err == nil {
		t.Fatal("expected failure when no authority_resolved event exists")
	}
}
