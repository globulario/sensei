// SPDX-License-Identifier: Apache-2.0

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
	if _, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, nil, ledgerProducedAt()); err != nil {
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

func delegationRoundTripReceipt() closureprotocol.DelegationReceipt {
	return closureprotocol.DelegationReceipt{
		DelegationID:         "delegation.repository_repair.actor-2",
		ParentGrantID:        "grant.sensei.closure_repository_edit",
		DelegatorPrincipalID: "actor.dave",
		DelegatedPrincipalID: "actor.codex.session-2",
		RoleIDs:              []string{"role.repository_repair_agent"},
		AuthorityDomainIDs:   []string{"authority.sensei_closure"},
		Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify},
		MechanismKinds:       []closureprotocol.MechanismKind{closureprotocol.MechanismRepositoryEdit},
		TargetKinds:          []string{"source_file"},
		TargetSelectors:      []string{"golang/architecture/closure/model.go"},
		MaximumRiskClass:     "architecture_sensitive",
		PolicyID:             "delegation_policy.repository_repair",
		Issuer:               "sensei.local",
		IssuedAt:             "2026-07-15T12:00:00Z",
		ValidFrom:            "2026-07-15T12:00:00Z",
		ValidUntil:           "2026-07-15T18:00:00Z",
		Status:               closureprotocol.ReceiptValid,
	}
}

// TestLoadRecordedAuthorityRoundTripsDelegationReceipts proves the concrete
// delegation records a delegated resolution consumed survive the authority_resolved
// event and reload identically (by digest), so certification can resolve the
// resolution's delegation_chain to real records rather than reconstructing them.
func TestLoadRecordedAuthorityRoundTripsDelegationReceipts(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.deleg", SessionID: "session.deleg"}
	store, dir, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	receipts := []closureprotocol.DelegationReceipt{delegationRoundTripReceipt()}
	if _, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, receipts, ledgerProducedAt()); err != nil {
		t.Fatalf("RecordAuthorityResolved: %v", err)
	}

	loaded, err := LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatalf("LoadRecordedAuthority: %v", err)
	}
	if len(loaded.DelegationReceipts) != 1 {
		t.Fatalf("delegation_receipts = %d, want 1", len(loaded.DelegationReceipts))
	}
	got, _ := closureprotocol.DelegationReceiptDigest(loaded.DelegationReceipts[0])
	want, _ := closureprotocol.DelegationReceiptDigest(receipts[0])
	if got != want {
		t.Fatalf("loaded delegation receipt digest %s != original %s", got, want)
	}
}

// TestRecordAuthorityResolvedOmitsDelegationArtifactWhenAbsent proves a
// non-delegated resolution records no delegation_receipts artifact at all, so
// existing direct-grant events stay byte-identical and DelegationReceipts loads
// as nil.
func TestRecordAuthorityResolvedOmitsDelegationArtifactWhenAbsent(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.direct", SessionID: "session.direct"}
	store, dir, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	if _, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, nil, ledgerProducedAt()); err != nil {
		t.Fatalf("RecordAuthorityResolved: %v", err)
	}

	var sink []closureprotocol.DelegationReceipt
	found, err := LoadLatestArtifactOptional(dir, closureprotocol.LedgerEventAuthorityResolved, "delegation_receipts", &sink)
	if err != nil {
		t.Fatalf("LoadLatestArtifactOptional: %v", err)
	}
	if found {
		t.Fatal("non-delegated resolution must not record a delegation_receipts artifact")
	}
	loaded, err := LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatalf("LoadRecordedAuthority: %v", err)
	}
	if loaded.DelegationReceipts != nil {
		t.Fatalf("DelegationReceipts = %v, want nil", loaded.DelegationReceipts)
	}
}
