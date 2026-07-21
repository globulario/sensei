// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/certification"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// certifyChangeGreenBundle builds a minimal, fully digest-linked green bundle
// (one admitted modify op, consumed capability, compliant scope verification,
// valid authority resolution, discharged obligation, satisfied test profile).
// It mirrors the certification package's internal fixture at the CLI seam.
func certifyChangeGreenBundle(t *testing.T) (certification.Request, []any) {
	t.Helper()
	mustDigest := func(record any) string {
		digest, err := closureprotocol.SemanticDigest(record)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	result := closureprotocol.ResultBinding{
		BaseRevision: "baserev123", PatchDigestSHA256: "patch123",
		ResultTreeDigestSHA256: "tree123", GraphDigestSHA256: "resultgraph123",
	}
	actor := closureprotocol.ActorBinding{PrincipalID: "actor.dave", ActorKind: closureprotocol.ActorHuman}
	admissionRequest := closureprotocol.AdmissionRequest{
		ActorBinding: actor,
		BaseBinding: closureprotocol.BaseBinding{
			Repository: closureprotocol.RepositorySnapshot{Domain: "github.com/globulario/sensei", Revision: "baserev123", RevisionStatus: "resolved", TreeDigestSHA256: "basetree"},
			Graph:      closureprotocol.GraphSnapshot{DigestSHA256: "basegraph", DigestStatus: "resolved", SchemaVersion: "awareness-ontology/0.2"},
			Task:       closureprotocol.TaskBinding{ID: "task.cli", SessionID: "session.cli"},
			Policies: closureprotocol.PolicyBinding{
				Admission: "admission.strict.v2", Certification: certification.PolicyDefaultID,
				Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
				Ledger: "ledger.task.v1", Canonicalization: "canonicalization.architectural_closure.v1",
			},
		},
		ChangePlan: closureprotocol.ChangePlan{PlanID: "plan.cli", Operations: []closureprotocol.ChangeOperation{{
			OperationID: "op.modify.cli", Kind: closureprotocol.OperationModify, TargetKind: "file",
			Target: "golang/core/model.go", SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
		}}},
		PolicyID: "admission.strict.v2",
	}
	decision := closureprotocol.AdmissionDecision{
		RequestDigestSHA256: mustDigest(admissionRequest), PolicyID: "admission.strict.v2",
		OperationVerdicts:        []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.modify.cli", Verdict: "admitted"}},
		CapabilityID:             "cap.cli",
		RequiredProofSlots:       []string{"proof.cli"},
		RequiredEvidenceProfiles: []string{"profile.test.cli"},
		CompletionPolicyID:       "completion.architectural_closure.v1",
	}
	consumption := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.cli", Task: closureprotocol.TaskBinding{ID: "task.cli", SessionID: "session.cli"},
		ConsumerActor: actor, ConsumedOperationIDs: []string{"op.modify.cli"},
		ConsumedAt: "2026-07-15T11:30:00Z", DecisionDigestSHA256: mustDigest(decision),
		OneUseStatus: closureprotocol.ReceiptValid,
	}
	verification := certification.ScopeVerification{
		DecisionDigestSHA256: mustDigest(decision), ResultBinding: result,
		ObservedPaths: []string{"golang/core/model.go"}, ObservedOperationIDs: []string{"op.modify.cli"},
		Status: certification.ScopeCompliant,
	}
	resolution := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:         "actorbinding-cli",
		BaseBindingDigestSHA256:          "basebinding-cli",
		ClosureAssessmentDigestSHA256:    "closure-cli",
		OperationSetDigestSHA256:         "operationset-cli",
		AuthorityPolicyGraphDigestSHA256: "authoritypolicygraph-cli",
		PolicyID:                         "admission.strict.v2",
		EvaluatedAt:                      "2026-07-15T11:45:00Z",
		Status:                           closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{{
			OperationID:       "op.modify.cli",
			Status:            closureprotocol.ReceiptValid,
			GrantIDs:          []string{"grant.cli"},
			LegalMechanisms:   []string{string(closureprotocol.MechanismRepositoryEdit)},
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
		}},
	}
	obligation := proofdischarge.ProofObligation{
		ID: "proof.cli", Status: "approved",
		RequiredSlots: []proofdischarge.ProofSlotSpec{{ID: "slot.tests", Kind: proofdischarge.SlotKindTestOrRuntime, Required: true}},
	}
	profile := closureprotocol.EvidenceProfile{
		ProfileID: "profile.test.cli", Owner: "component.cli", LegalObservationPath: "test_runner.go_test",
		EvidenceKind: closureprotocol.EvidenceTest, Freshness: "per-result", Trust: "high",
		Status: closureprotocol.ReceiptValid,
	}
	receipt := closureprotocol.EvidenceReceipt{
		ReceiptID: "receipt.test.cli", EvidenceKind: closureprotocol.EvidenceTest, ProfileID: "profile.test.cli",
		ResultBinding: result, Producer: "ci.local", ObservationPath: "go_test",
		ObservedAt: "2026-07-15T11:45:00Z", ExpiresAt: "2026-07-16T11:45:00Z",
		Status: closureprotocol.ReceiptValid, Trust: "high", PayloadDigestSHA256: "payload123",
	}
	discharge := closureprotocol.ProofDischarge{
		ObligationID: "proof.cli", Status: closureprotocol.ReceiptValid,
		SlotResults:    []closureprotocol.ProofSlotResult{{SlotID: "slot.tests", Status: closureprotocol.DimensionPass, ReceiptIDs: []string{"receipt.test.cli"}}},
		MappedEvidence: []string{"receipt.test.cli"},
	}
	dischargeDigest, err := closureprotocol.ProofDischargeDigest(discharge)
	if err != nil {
		t.Fatal(err)
	}
	resolutionDigest, err := closureprotocol.AuthorityResolutionDigest(resolution)
	if err != nil {
		t.Fatal(err)
	}
	req := certification.Request{
		TaskID: "task.cli", PolicyID: certification.PolicyDefaultID,
		EvaluatedAt: "2026-07-15T12:00:00Z", ResultBinding: result,
		AdmissionRequestDigestSHA256:      mustDigest(admissionRequest),
		AdmissionDecisionDigestSHA256:     mustDigest(decision),
		CapabilityConsumptionDigestSHA256: mustDigest(consumption),
		ScopeVerificationDigestSHA256:     mustDigest(verification),
		AuthorityResolutionDigests:        []string{resolutionDigest},
		ProofDischargeDigests:             []string{dischargeDigest},
		ProofObligationDigests:            []string{mustDigest(obligation)},
		EvidenceProfileDigests:            []string{mustDigest(profile)},
		EvidenceReceiptDigests:            []string{mustDigest(receipt)},
	}
	records := []any{admissionRequest, decision, consumption, verification, resolution, obligation, profile, receipt, discharge}
	return req, records
}

func seedCertifyChangeTask(t *testing.T, req certification.Request, records []any) (string, string) {
	t.Helper()
	taskDir := t.TempDir()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}))
	seed, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: req.TaskID, SessionID: "session.cli", EventType: closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventTaskPrepared,
			TaskID:        req.TaskID, SessionID: "session.cli",
			TaskPhase: closureprotocol.PhasePrepared,
		},
		PayloadMediaType: "application/yaml", ProducerID: "cli tests",
		ProducedAt: time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	for _, record := range records {
		if _, err := certification.WriteRecordArtifact(taskDir, record); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}
	requestBytes, err := certification.CanonicalRequestYAML(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, certification.RequestFileName), requestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return taskDir, seed.Head.EntryDigestSHA256
}

func TestRunCertifyChange_RequiresFlags(t *testing.T) {
	if code := runCertifyChange(nil); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if code := runCertifyChange([]string{"--task-dir", "x"}); code != 2 {
		t.Fatalf("exit = %d, want 2 (missing --expected-head)", code)
	}
	if code := runCertifyChange([]string{"--task-dir", "x", "--expected-head", "y", "--format", "xml"}); code != 2 {
		t.Fatalf("exit = %d, want 2 (bad format)", code)
	}
}

func TestRunCertifyChange_CertifiedExitsZeroAndAppends(t *testing.T) {
	req, records := certifyChangeGreenBundle(t)
	taskDir, head := seedCertifyChangeTask(t, req, records)
	if code := runCertifyChange([]string{"--task-dir", taskDir, "--expected-head", head, "--format", "json"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	store := ledger.NewStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	last := chain.Entries[len(chain.Entries)-1].Entry
	if last.EventType != closureprotocol.LedgerEventCertified {
		t.Fatalf("last event = %s, want certified", last.EventType)
	}
	for _, entry := range chain.Entries {
		if entry.Entry.EventType == closureprotocol.LedgerEventCompleted {
			t.Fatal("CLI created a completed event")
		}
	}
}

func TestRunCertifyChange_BlockedExitsOneWithoutAppend(t *testing.T) {
	req, records := certifyChangeGreenBundle(t)
	// Drop the proof discharge reference and record: proof lane blocks.
	req.ProofDischargeDigests = nil
	filtered := records[:0]
	for _, record := range records {
		if _, ok := record.(closureprotocol.ProofDischarge); ok {
			continue
		}
		filtered = append(filtered, record)
	}
	taskDir, head := seedCertifyChangeTask(t, req, filtered)
	if code := runCertifyChange([]string{"--task-dir", taskDir, "--expected-head", head}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	store := ledger.NewStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.Entries) != 1 {
		t.Fatalf("ledger grew on blocked evaluation: %d entries", len(chain.Entries))
	}
}

func TestRunCertifyChange_StaleHeadExitsOne(t *testing.T) {
	req, records := certifyChangeGreenBundle(t)
	taskDir, _ := seedCertifyChangeTask(t, req, records)
	stale := strings.Repeat("0", 64)
	if code := runCertifyChange([]string{"--task-dir", taskDir, "--expected-head", stale}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// --- Legacy adapter seam ------------------------------------------------------

func TestCompatAdapter_LegacyOutputNeverClaimsClosureProtocol(t *testing.T) {
	event := map[string]any{
		"id":                   "event.legacy",
		"certification_status": "certified_clean_repair",
		"promotion_allowed":    true,
		"certification": map[string]any{
			"scope_valid": true, "evidence_sufficient": true,
			"required_paths_satisfied": true, "frozen_contract_present": true,
			"contract_block_valid": true, "contract_block_maps_to_frozen_contract": true,
			"governing_contract_id": "contract.core",
		},
	}
	res := buildCertifyResult(event, proofObligationsDoc{})
	if res.Protocol != legacyBenchmarkProtocol {
		t.Fatalf("protocol = %q, want %q", res.Protocol, legacyBenchmarkProtocol)
	}
	if res.Protocol == closureprotocol.ProtocolVersion {
		t.Fatal("legacy output claims the closure protocol version")
	}
	// Even a promotion-allowed clean legacy repair is not, and can never be, a
	// closure certification: the result type has no lane statuses, no result
	// binding, and no receipt digest.
	if res.GovernanceCertification.Verdict == string(closureprotocol.Certified) {
		t.Fatalf("legacy verdict collided with the frozen vocabulary: %q", res.GovernanceCertification.Verdict)
	}
}

func TestLegacyCertifyCannotAppendCertification(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	legacyPath := filepath.Join(filepath.Dir(file), "cmd_certify.go")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	// The structural guarantee: without these imports the legacy adapter
	// cannot construct a ledger append or a frozen closure record, no matter
	// what its event input claims.
	for _, forbidden := range []string{
		`"github.com/globulario/sensei/golang/architecture/ledger"`,
		`"github.com/globulario/sensei/golang/architecture/closureprotocol"`,
		`"github.com/globulario/sensei/golang/architecture/certification"`,
		"LedgerEventCertified",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("legacy adapter references %q — it must stay structurally unable to certify", forbidden)
		}
	}
}
