// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
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

// TestArtifactLessAdmissionConsumedCannotHideRecordedConsumption is the witness for
// issue #354: with only append rights and a complete valid chain, a second
// admission_consumed carrying no capability_consumption must not make the already
// recorded capability unreadable. The append itself must be refused as malformed
// history, and the recorded consumption must still load.
func TestArtifactLessAdmissionConsumedCannotHideRecordedConsumption(t *testing.T) {
	task := v2Task()
	store, dir, head := admissionLedgerStore(t, task)

	exp, _ := scopeFixture(t)
	consumed, err := RecordAdmissionConsumed(store, head, exp.Consumption, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAdmissionConsumed: %v", err)
	}
	if _, err := LoadRecordedConsumption(dir); err != nil {
		t.Fatalf("LoadRecordedConsumption before the masking append: %v", err)
	}

	_, appendErr := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: consumed.Entry.EntryDigestSHA256,
		EventType:                closureprotocol.LedgerEventAdmissionConsumed,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventAdmissionConsumed,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "test",
		ProducedAt:       ledgerProducedAt(),
	})

	loaded, err := LoadRecordedConsumption(dir)
	if err != nil {
		t.Fatalf("recorded capability became unreadable after an artifact-less admission_consumed append (append error: %v): %v", appendErr, err)
	}
	if appendErr == nil {
		t.Fatal("an admission_consumed payload without capability_consumption must be rejected at append")
	}
	if loaded.CapabilityID != exp.Consumption.CapabilityID || loaded.ConsumedAt != exp.Consumption.ConsumedAt {
		t.Fatalf("loaded consumption %+v is not the recorded one %+v", loaded, exp.Consumption)
	}
}

func TestLoadRecordedAuthorityAbsentFailsClosed(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.empty", SessionID: "session.empty"}
	_, dir, _ := admissionLedgerStore(t, task)
	if _, err := LoadRecordedAuthority(dir); err == nil {
		t.Fatal("expected failure when no authority_resolved event exists")
	}
}

// TestLoadRecordedAuthorityCtxSharesOneVerification proves the read side now verifies
// the chain once and reads all five artifacts from that single verified snapshot: the
// ctx variant returns the byte-identical bundle, and a SECOND load in the same
// evaluation scope adds zero digest computations (the whole chain was already digested
// once), rather than re-verifying the ledger per artifact as the old five-call path did.
func TestLoadRecordedAuthorityCtxSharesOneVerification(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.once", SessionID: "session.once"}
	store, dir, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	if _, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, nil, ledgerProducedAt()); err != nil {
		t.Fatalf("RecordAuthorityResolved: %v", err)
	}

	// Equivalence: the ctx variant reconstructs the same bundle as the plain loader.
	plain, err := LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatalf("plain load: %v", err)
	}
	ctx, scope := ledger.WithVerificationScope(context.Background())
	scoped, err := LoadRecordedAuthorityCtx(ctx, dir)
	if err != nil {
		t.Fatalf("ctx load: %v", err)
	}
	pd, _ := closureprotocol.AuthorityResolutionDigest(plain.Resolution)
	sd, _ := closureprotocol.AuthorityResolutionDigest(scoped.Resolution)
	if pd != sd {
		t.Fatalf("ctx-loaded resolution %s != plain %s", sd, pd)
	}

	// One load digested the whole chain once. A second load in the same scope reuses
	// every memoized digest — proving the five artifact reads share one verification.
	afterFirst := scope.DigestComputations()
	if afterFirst == 0 {
		t.Fatal("expected the load to verify (and digest) the chain within the scope")
	}
	if _, err := LoadRecordedAuthorityCtx(ctx, dir); err != nil {
		t.Fatalf("second ctx load: %v", err)
	}
	if afterSecond := scope.DigestComputations(); afterSecond != afterFirst {
		t.Fatalf("second load must add no digest computations, went %d -> %d", afterFirst, afterSecond)
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

// TestLoadRecordedAuthorityReadsOneBundleAfterDelegatedThenDirect proves the
// authority bundle is decoded from the single newest authority_resolved event: a
// direct resolution recorded after a delegated one loads with nil delegation
// receipts rather than inheriting the older event's receipts.
func TestLoadRecordedAuthorityReadsOneBundleAfterDelegatedThenDirect(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.deleg-then-direct", SessionID: "session.deleg-then-direct"}
	store, dir, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	receipts := []closureprotocol.DelegationReceipt{delegationRoundTripReceipt()}
	delegated, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, receipts, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAuthorityResolved (delegated): %v", err)
	}
	if _, err := RecordAuthorityResolved(store, delegated.Entry.EntryDigestSHA256, task, resolution, in.Actor, in.ChangePlan, in.Base, nil, ledgerProducedAt()); err != nil {
		t.Fatalf("RecordAuthorityResolved (direct): %v", err)
	}

	loaded, err := LoadRecordedAuthority(dir)
	if err != nil {
		t.Fatalf("LoadRecordedAuthority: %v", err)
	}
	if loaded.DelegationReceipts != nil {
		t.Fatalf("latest direct bundle loaded delegation receipts from an older event: %v", loaded.DelegationReceipts)
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
