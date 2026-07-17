// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedimpact"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// recordedRef is the lightweight identity of an already-recorded transition.
type recordedRef struct {
	entry         ledger.Entry
	receiptDigest string
}

// RecordedStage binds a canonical stage to its ref and reconstructed artifact.
type RecordedStage struct {
	Stage    closureprotocol.ResultPipelineStage
	Ref      closureprotocol.LedgerPayloadRef
	Artifact resultpipeline.PipelineArtifact
}

// RecordedTransition is a transition reloaded and reconstructed from the ledger.
type RecordedTransition struct {
	Entry        ledger.Entry
	EventPayload ledger.TaskEventPayload

	ReceiptRef   closureprotocol.LedgerPayloadRef
	Receipt      closureprotocol.ResultTransitionReceipt
	ReceiptBytes []byte

	ImpactReportRef closureprotocol.LedgerPayloadRef
	ImpactReport    governedimpact.Report

	Stages []RecordedStage

	SessionRef     closureprotocol.LedgerPayloadRef
	TaskControlRef closureprotocol.LedgerPayloadRef
	StatusRef      closureprotocol.LedgerPayloadRef

	ReconstructedBuildResult resultpipeline.BuildResult

	// projectionBytes holds the reloaded canonical bytes of the three projections,
	// validated for exact correspondence to the derived next state.
	projectionBytes map[string][]byte
}

// findRecordedTransition scans the verified chain for a result_transition_recorded
// event whose receipt carries transitionID.
func findRecordedTransition(taskDir string, chain ledger.VerifiedChain, transitionID string) (recordedRef, bool, error) {
	for _, ve := range chain.Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventResultTransitionRecorded {
			continue
		}
		payload, err := loadEventPayload(ve)
		if err != nil {
			return recordedRef{}, false, err
		}
		ref, ok := payload.Artifacts[KeyReceipt]
		if !ok {
			continue
		}
		data, err := readArtifact(taskDir, ref)
		if err != nil {
			return recordedRef{}, false, err
		}
		var receipt closureprotocol.ResultTransitionReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return recordedRef{}, false, recErr(CodeReloadFailed, "parse recorded receipt: %v", err)
		}
		if receipt.TransitionID == transitionID {
			return recordedRef{entry: ve.Entry, receiptDigest: receipt.ReceiptDigestSHA256}, true, nil
		}
	}
	return recordedRef{}, false, nil
}

func loadEventPayload(ve ledger.VerifiedEntry) (ledger.TaskEventPayload, error) {
	data, err := readFileAbs(ve.PayloadPath)
	if err != nil {
		return ledger.TaskEventPayload{}, recErr(CodeReloadFailed, "read payload: %v", err)
	}
	return ledger.ParseTaskEventPayload(data)
}

// LoadRecordedTransition reloads a recorded transition entirely from disk and
// reconstructs a result-bound build view. It reads no repository, rebuilds no
// graph, and regenerates no artifact.
func LoadRecordedTransition(taskDir, transitionID string) (RecordedTransition, error) {
	taskDir = strings.TrimSpace(taskDir)
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return RecordedTransition{}, recErr(CodeReloadFailed, "verify chain: %v", err)
	}
	// Test seam: a concurrent append here must not leak into the reconstruction,
	// which reads only from this frozen chain snapshot.
	if afterChainSnapshot != nil {
		afterChainSnapshot(taskDir)
	}
	var target ledger.VerifiedEntry
	found := false
	var receipt closureprotocol.ResultTransitionReceipt
	var receiptBytes []byte
	var payload ledger.TaskEventPayload
	for _, ve := range chain.Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventResultTransitionRecorded {
			continue
		}
		p, err := loadEventPayload(ve)
		if err != nil {
			return RecordedTransition{}, err
		}
		ref := p.Artifacts[KeyReceipt]
		data, err := readArtifact(taskDir, ref)
		if err != nil {
			return RecordedTransition{}, err
		}
		var r closureprotocol.ResultTransitionReceipt
		if err := json.Unmarshal(data, &r); err != nil {
			return RecordedTransition{}, recErr(CodeReloadFailed, "parse receipt: %v", err)
		}
		if r.TransitionID == transitionID {
			target, found, receipt, receiptBytes, payload = ve, true, r, data, p
			break
		}
	}
	if !found {
		return RecordedTransition{}, recErr(CodeReloadFailed, "transition %q not found in ledger", transitionID)
	}

	// Canonical receipt bytes + self-digest recompute.
	wantBytes, err := closureprotocol.MarshalCanonicalResultTransitionReceipt(receipt)
	if err != nil {
		return RecordedTransition{}, err
	}
	if string(wantBytes) != string(receiptBytes) {
		return RecordedTransition{}, recErr(CodeReceiptMismatch, "recorded receipt bytes are not canonical")
	}
	wantDigest, err := closureprotocol.ResultTransitionReceiptDigest(receipt)
	if err != nil {
		return RecordedTransition{}, err
	}
	if receipt.ReceiptDigestSHA256 == "" || receipt.ReceiptDigestSHA256 != wantDigest {
		return RecordedTransition{}, recErr(CodeReceiptMismatch, "recorded receipt self-digest does not recompute")
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(receipt); err != nil {
		return RecordedTransition{}, recErr(CodeRecordedTransitionInvalid, "receipt: %v", err)
	}

	// The stored event payload must itself be contract-valid AND bind exactly to the
	// receipt: same task, session, and result binding. A payload naming another
	// task/session/result or a swapped ref fails even if the bytes are individually
	// valid.
	if err := ValidateResultTransitionEventPayload(payload); err != nil {
		return RecordedTransition{}, recErr(CodeRecordedTransitionInvalid, "event payload: %v", err)
	}
	if payload.TaskID != receipt.Task.ID || payload.SessionID != receipt.Task.SessionID {
		return RecordedTransition{}, recErr(CodeRecordedTransitionInvalid, "event task/session differs from the receipt")
	}
	if target.Entry.Task.ID != receipt.Task.ID || target.Entry.Task.SessionID != receipt.Task.SessionID {
		return RecordedTransition{}, recErr(CodeSessionMismatch, "ledger entry task/session differs from the receipt")
	}
	if payload.ResultBinding == nil || closureprotocol.MustSemanticDigest(*payload.ResultBinding) != closureprotocol.MustSemanticDigest(receipt.ResultBinding) {
		return RecordedTransition{}, recErr(CodeRecordedTransitionInvalid, "event result binding differs from the receipt")
	}

	rt := RecordedTransition{
		Entry: target.Entry, EventPayload: payload,
		ReceiptRef: payload.Artifacts[KeyReceipt], Receipt: receipt, ReceiptBytes: append([]byte(nil), receiptBytes...),
		ImpactReportRef: payload.Artifacts[KeyImpactReport],
		SessionRef:      payload.Artifacts[KeySession],
		TaskControlRef:  payload.Artifacts[KeyTaskControl],
		StatusRef:       payload.Artifacts[KeyStatus],
	}

	// Impact report.
	impactBytes, err := readArtifact(taskDir, rt.ImpactReportRef)
	if err != nil {
		return RecordedTransition{}, err
	}
	rt.ImpactReport, err = governedimpact.ParseReport(impactBytes)
	if err != nil {
		return RecordedTransition{}, recErr(CodeImpactMismatch, "parse impact report: %v", err)
	}

	// Ten stage artifacts, reconstructed from stored bytes + the receipt's own
	// receipts and derivations. The receipt's arrays are canonicalized as SETS, so
	// they are matched by identity (receipt id / derivation stage), never position.
	if len(receipt.OperationalArtifactReceipts) != len(closureprotocol.ResultPipelineStages) ||
		len(receipt.Derivations) != len(closureprotocol.ResultPipelineStages) {
		return RecordedTransition{}, recErr(CodeStageMismatch, "receipt does not carry ten operational receipts and derivations")
	}
	receiptByID := map[string]closureprotocol.ArtifactReceipt{}
	for _, r := range receipt.OperationalArtifactReceipts {
		receiptByID[r.ID] = r
	}
	derivByStage := map[closureprotocol.ResultPipelineStage]closureprotocol.ArtifactDerivation{}
	for _, d := range receipt.Derivations {
		derivByStage[d.Stage] = d
	}
	stageArtifacts := make([]resultpipeline.PipelineArtifact, 0, len(closureprotocol.ResultPipelineStages))
	for _, stage := range closureprotocol.ResultPipelineStages {
		ref, ok := payload.Artifacts[stageKey(stage)]
		if !ok {
			return RecordedTransition{}, recErr(CodeStageMismatch, "missing stage artifact %s", stage)
		}
		rec, ok := receiptByID["artifact."+string(stage)]
		if !ok {
			return RecordedTransition{}, recErr(CodeStageMismatch, "receipt has no artifact for stage %s", stage)
		}
		deriv, ok := derivByStage[stage]
		if !ok {
			return RecordedTransition{}, recErr(CodeStageMismatch, "receipt has no derivation for stage %s", stage)
		}
		bytes, err := readArtifact(taskDir, ref)
		if err != nil {
			return RecordedTransition{}, err
		}
		if sha256Hex(bytes) != rec.ByteDigestSHA256 {
			return RecordedTransition{}, recErr(CodeStageMismatch, "stage %s bytes do not match its receipt", stage)
		}
		art := resultpipeline.PipelineArtifact{
			Stage: stage, LogicalPath: rec.Path, MediaType: rec.MediaType, Bytes: bytes,
			Receipt: rec, Derivation: deriv,
		}
		stageArtifacts = append(stageArtifacts, art)
		rt.Stages = append(rt.Stages, RecordedStage{Stage: stage, Ref: ref, Artifact: art})
	}

	// Reload the three projection artifacts (bytes verified against their refs).
	rt.projectionBytes = map[string][]byte{}
	for key, ref := range map[string]closureprotocol.LedgerPayloadRef{
		KeySession: rt.SessionRef, KeyTaskControl: rt.TaskControlRef, KeyStatus: rt.StatusRef,
	} {
		data, err := readArtifact(taskDir, ref)
		if err != nil {
			return RecordedTransition{}, err
		}
		rt.projectionBytes[key] = data
	}

	build, err := reconstructBuildResult(chain, taskDir, target.Entry.Sequence, receipt, stageArtifacts, rt.ImpactReport)
	if err != nil {
		return RecordedTransition{}, err
	}
	rt.ReconstructedBuildResult = build
	return rt, nil
}

// afterChainSnapshot is a test seam fired once, immediately after the reload's
// single chain snapshot is taken and before any upstream record is decoded from
// it. A test uses it to append to the on-disk ledger and prove the reconstruction
// still reads only the frozen snapshot (no mixed ledger world).
var afterChainSnapshot func(taskDir string)

// latestEventFromChain returns the latest entry of eventType that PRECEDES the
// transition entry (beforeSeq) in the frozen chain — the upstream world the
// transition was recorded against — its parsed payload, and requires the
// payload/entry task-session to equal want. Bounding to entries before the
// transition makes reconstruction stable across later ledger appends.
func latestEventFromChain(chain ledger.VerifiedChain, taskDir string, eventType closureprotocol.LedgerEventType, want closureprotocol.TaskBinding, beforeSeq int) (ledger.VerifiedEntry, ledger.TaskEventPayload, error) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.Sequence >= beforeSeq {
			continue
		}
		if ve.Entry.EventType != eventType {
			continue
		}
		payload, err := loadEventPayload(ve)
		if err != nil {
			return ledger.VerifiedEntry{}, ledger.TaskEventPayload{}, err
		}
		if ve.Entry.Task.ID != want.ID || ve.Entry.Task.SessionID != want.SessionID ||
			payload.TaskID != want.ID || payload.SessionID != want.SessionID {
			return ledger.VerifiedEntry{}, ledger.TaskEventPayload{}, recErr(CodeSessionMismatch, "%s event task/session differs from the transition", eventType)
		}
		return ve, payload, nil
	}
	return ledger.VerifiedEntry{}, ledger.TaskEventPayload{}, recErr(CodeReloadFailed, "no %s event in chain", eventType)
}

// chainArtifactJSON reads and JSON-decodes an artifact named key from one event's
// payload, verifying its byte digest against the chain-recorded ref.
func chainArtifactJSON(taskDir string, payload ledger.TaskEventPayload, key string, out any) error {
	ref, ok := payload.Artifacts[key]
	if !ok {
		return recErr(CodeReloadFailed, "event has no artifact %q", key)
	}
	data, err := readArtifact(taskDir, ref)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return recErr(CodeReloadFailed, "decode %q: %v", key, err)
	}
	return nil
}

// reconstructBuildResult assembles a BuildResult from the receipt, the stored
// stage artifacts, the impact report, and the earlier verified upstream ledger
// records — with no repository read and no graph rebuild.
func reconstructBuildResult(chain ledger.VerifiedChain, taskDir string, transitionSeq int, receipt closureprotocol.ResultTransitionReceipt, stages []resultpipeline.PipelineArtifact, impact governedimpact.Report) (resultpipeline.BuildResult, error) {
	byStage := map[closureprotocol.ResultPipelineStage]resultpipeline.PipelineArtifact{}
	for _, a := range stages {
		byStage[a.Stage] = a
	}
	var closureReport closure.Report
	if err := json.Unmarshal(byStage[closureprotocol.StageClosureAssessment].Bytes, &closureReport); err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "decode closure: %v", err)
	}
	var questions resultpipeline.ArchitectQuestionsBundle
	if err := json.Unmarshal(byStage[closureprotocol.StageArchitectQuestions].Bytes, &questions); err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "decode questions: %v", err)
	}
	var proof proofrequirements.Document
	if err := json.Unmarshal(byStage[closureprotocol.StageProofRequirements].Bytes, &proof); err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "decode proof: %v", err)
	}

	// Reload EVERY upstream record from the SINGLE verified chain snapshot (never the
	// multi-read admission helpers, which each re-verify the ledger and could mix
	// different ledger worlds during a concurrent append), RECOMPUTE its digest, and
	// require equality with the receipt — the same laws resulttransition.BindRepositoryResult
	// enforced. Nothing is trusted merely because the receipt carries it.
	want := receipt.Task

	prepEntry, prepPayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventTaskPrepared, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	_ = prepEntry
	if prepPayload.BaseBinding == nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "task_prepared carries no base binding")
	}
	base := *prepPayload.BaseBinding

	_, authPayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventAuthorityResolved, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	var resolution closureprotocol.AuthorityResolution
	var actor closureprotocol.ActorBinding
	if err := chainArtifactJSON(taskDir, authPayload, "authority_resolution", &resolution); err != nil {
		return resultpipeline.BuildResult{}, err
	}
	if err := chainArtifactJSON(taskDir, authPayload, "actor_binding", &actor); err != nil {
		return resultpipeline.BuildResult{}, err
	}

	_, decPayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventAdmissionDecided, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	var decision closureprotocol.AdmissionDecision
	if err := chainArtifactJSON(taskDir, decPayload, "admission_decision", &decision); err != nil {
		return resultpipeline.BuildResult{}, err
	}

	_, consPayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventAdmissionConsumed, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	var consumption closureprotocol.CapabilityConsumption
	if err := chainArtifactJSON(taskDir, consPayload, "capability_consumption", &consumption); err != nil {
		return resultpipeline.BuildResult{}, err
	}

	_, obsPayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventChangeObserved, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	var observed admission.ObservedChangeSet
	if err := chainArtifactJSON(taskDir, obsPayload, "observed_change_set", &observed); err != nil {
		return resultpipeline.BuildResult{}, err
	}

	scopeEntry, scopePayload, err := latestEventFromChain(chain, taskDir, closureprotocol.LedgerEventScopeVerified, want, transitionSeq)
	if err != nil {
		return resultpipeline.BuildResult{}, err
	}
	var scope admission.ScopeVerification
	if err := chainArtifactJSON(taskDir, scopePayload, "scope_verification", &scope); err != nil {
		return resultpipeline.BuildResult{}, err
	}
	evaluatedAt := scopeEntry.Entry.ProducedAt
	recAuth := admission.RecordedAuthority{Resolution: resolution, Actor: actor, Base: base}

	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "base digest: %v", err)
	}
	authDigest, err := closureprotocol.AuthorityResolutionDigest(recAuth.Resolution)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "authority digest: %v", err)
	}
	observedDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "observed digest: %v", err)
	}
	scopeDigest, err := admission.ScopeVerificationDigest(scope)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "scope digest: %v", err)
	}
	actorDigest := closureprotocol.MustSemanticDigest(recAuth.Actor)
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	consumptionDigest := closureprotocol.MustSemanticDigest(consumption)

	for _, c := range []struct{ name, got, want string }{
		{"base_binding", baseDigest, receipt.BaseBindingDigestSHA256},
		{"actor_binding", actorDigest, receipt.ActorBindingDigestSHA256},
		{"authority_resolution", authDigest, receipt.AuthorityResolutionDigestSHA256},
		{"admission_decision", decisionDigest, receipt.AdmissionDecisionDigestSHA256},
		{"capability_consumption", consumptionDigest, receipt.CapabilityConsumptionDigestSHA256},
		{"observed_change", observedDigest, receipt.ObservedChangeSetDigestSHA256},
		{"scope_verification", scopeDigest, receipt.ScopeVerificationDigestSHA256},
	} {
		if strings.TrimSpace(c.got) != strings.TrimSpace(c.want) {
			return resultpipeline.BuildResult{}, recErr(CodeReceiptMismatch, "recomputed %s digest does not match the receipt", c.name)
		}
	}

	rb := receipt.ResultBinding
	bound := resulttransition.BoundRepositoryResult{
		Mode: resulttransition.ResultModeRevision,
		Task: receipt.Task,
		RepositoryResult: resulttransition.RepositoryResultBinding{
			BaseRevision:           rb.BaseRevision,
			PatchDigestSHA256:      rb.PatchDigestSHA256,
			ResultTreeDigestSHA256: rb.ResultTreeDigestSHA256,
			ResultRevision:         rb.ResultRevision,
		},
		ObservedChange:                    observed,
		AuthorityResolution:               recAuth.Resolution,
		AdmissionDecision:                 decision,
		BaseBindingDigestSHA256:           baseDigest,
		ActorBindingDigestSHA256:          actorDigest,
		AuthorityResolutionDigestSHA256:   authDigest,
		AdmissionDecisionDigestSHA256:     decisionDigest,
		CapabilityConsumptionDigestSHA256: consumptionDigest,
		ObservedChangeSetDigestSHA256:     observedDigest,
		ScopeVerificationDigestSHA256:     scopeDigest,
	}

	return resultpipeline.BuildResult{
		BoundRepositoryResult:         bound,
		ResultBinding:                 rb,
		ResultBindingDigestSHA256:     receipt.ResultBindingDigestSHA256,
		StageArtifacts:                stages,
		ClosureReport:                 closureReport,
		Dialogue:                      questions.Dialogue,
		ProofRequirements:             proof,
		GovernedKnowledgeImpactReport: impact,
		PipelinePolicyID:              receipt.PipelinePolicyID,
		EvaluatedAt:                   evaluatedAt,
		Limitations:                   receipt.Limitations,
	}, nil
}

// ValidateRecordedTransition independently revalidates a reloaded transition: the
// reconstructed build validates, the receipt is contract-valid, and the receipt's
// set-canonicalized arrays correspond to the reconstructed build by identity.
func ValidateRecordedTransition(rt RecordedTransition) error {
	if err := resultpipeline.ValidateBuildResult(rt.ReconstructedBuildResult); err != nil {
		return recErr(CodeRecordedTransitionInvalid, "reconstructed build: %v", err)
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(rt.Receipt); err != nil {
		return recErr(CodeRecordedTransitionInvalid, "receipt: %v", err)
	}
	if err := governedimpact.ValidateReport(rt.ImpactReport); err != nil {
		return recErr(CodeImpactMismatch, "impact report: %v", err)
	}
	build := rt.ReconstructedBuildResult

	// Task, upstream digests, result binding, and pipeline policy correspond.
	b := build.BoundRepositoryResult
	r := rt.Receipt
	if r.Task != b.Task ||
		r.BaseBindingDigestSHA256 != b.BaseBindingDigestSHA256 ||
		r.ActorBindingDigestSHA256 != b.ActorBindingDigestSHA256 ||
		r.AuthorityResolutionDigestSHA256 != b.AuthorityResolutionDigestSHA256 ||
		r.AdmissionDecisionDigestSHA256 != b.AdmissionDecisionDigestSHA256 ||
		r.CapabilityConsumptionDigestSHA256 != b.CapabilityConsumptionDigestSHA256 ||
		r.ObservedChangeSetDigestSHA256 != b.ObservedChangeSetDigestSHA256 ||
		r.ScopeVerificationDigestSHA256 != b.ScopeVerificationDigestSHA256 ||
		r.ResultBindingDigestSHA256 != build.ResultBindingDigestSHA256 ||
		r.PipelinePolicyID != build.PipelinePolicyID {
		return recErr(CodeReceiptMismatch, "receipt upstream/binding fields differ from the reconstructed build")
	}
	if closureprotocol.MustSemanticDigest(r.ResultBinding) != closureprotocol.MustSemanticDigest(build.ResultBinding) {
		return recErr(CodeReceiptMismatch, "receipt result binding differs from the build")
	}

	// Artifact receipts and derivations correspond by identity (set semantics).
	recByID := map[string]string{}
	for _, ar := range r.OperationalArtifactReceipts {
		recByID[ar.ID] = closureprotocol.MustSemanticDigest(ar)
	}
	derByStage := map[closureprotocol.ResultPipelineStage]string{}
	for _, d := range r.Derivations {
		derByStage[d.Stage] = closureprotocol.MustSemanticDigest(d)
	}
	for _, a := range build.StageArtifacts {
		if recByID["artifact."+string(a.Stage)] != closureprotocol.MustSemanticDigest(a.Receipt) {
			return recErr(CodeStageMismatch, "receipt artifact for %s differs from the build", a.Stage)
		}
		if derByStage[a.Stage] != closureprotocol.MustSemanticDigest(a.Derivation) {
			return recErr(CodeStageMismatch, "receipt derivation for %s differs from the build", a.Stage)
		}
	}

	// Producer summary and impacts correspond as sets.
	if closureprotocol.MustSemanticDigest(sortedProducers(r.PipelineProducerVersions)) != closureprotocol.MustSemanticDigest(sortedProducers(producerVersionsOf(build))) {
		return recErr(CodeProducerSummaryMismatch, "receipt producer versions differ from the build")
	}
	if closureprotocol.MustSemanticDigest(sortedImpacts(r.GovernedKnowledgeImpacts)) != closureprotocol.MustSemanticDigest(sortedImpacts(rt.ImpactReport.Impacts)) {
		return recErr(CodeImpactMismatch, "receipt impacts differ from the stored full impact report")
	}

	// Projections correspond exactly to the derived next state, the receipt, and the
	// result binding — and never claim certified or completed.
	next, err := ClassifyNextState(build.ProofRequirements)
	if err != nil {
		return recErr(CodeRecordedTransitionInvalid, "next state: %v", err)
	}
	for _, kind := range []string{KeySession, KeyTaskControl, KeyStatus} {
		if err := validateProjection(rt.projectionBytes[kind], kind, rt.Receipt, next, build.EvaluatedAt); err != nil {
			return err
		}
	}
	return nil
}

// validateProjection strictly parses one projection and requires it to be exactly
// the canonical projection derived from the receipt, the authoritative reconstructed
// evaluation time, and the derived next state — and to never claim certified/completed.
func validateProjection(data []byte, kind string, receipt closureprotocol.ResultTransitionReceipt, next NextState, evaluatedAt string) error {
	if data == nil {
		return recErr(CodeProjectionDrift, "projection %q is absent", kind)
	}
	doc, err := parseProjection(data)
	if err != nil {
		return recErr(CodeProjectionDrift, "projection %q: %v", kind, err)
	}
	if doc.TaskPhase == closureprotocol.PhaseCertified || doc.TaskPhase == closureprotocol.PhaseCompleted ||
		strings.Contains(doc.OperationalStatus, "certified") || strings.Contains(doc.OperationalStatus, "completed") {
		return recErr(CodeProjectionDrift, "projection %q claims certified/completed", kind)
	}
	want := newProjectionDoc(kind, resultpipeline.TransitionCandidate{
		BuildResult: resultpipeline.BuildResult{EvaluatedAt: evaluatedAt},
		Receipt:     receipt,
	}, next)
	wb, err := closureprotocol.CanonicalJSON(want)
	if err != nil {
		return err
	}
	if string(wb) != string(data) {
		return recErr(CodeProjectionDrift, "projection %q does not correspond to the derived state/receipt", kind)
	}
	return nil
}

func parseProjection(data []byte) (projectionDoc, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc projectionDoc
	if err := dec.Decode(&doc); err != nil {
		return projectionDoc{}, err
	}
	if _, err := dec.Token(); err == nil {
		return projectionDoc{}, recErr(CodeProjectionDrift, "trailing content in projection")
	}
	if doc.SchemaVersion != projectionSchemaVersion {
		return projectionDoc{}, recErr(CodeProjectionDrift, "unexpected projection schema version %q", doc.SchemaVersion)
	}
	return doc, nil
}

func readFileAbs(path string) ([]byte, error) { return os.ReadFile(path) }

func producerVersionsOf(build resultpipeline.BuildResult) []closureprotocol.ProducerVersion {
	seen := map[string]closureprotocol.ProducerVersion{}
	for _, a := range build.StageArtifacts {
		seen[a.Receipt.Producer.ID+"\x00"+a.Receipt.Producer.Version] = closureprotocol.ProducerVersion{Producer: a.Receipt.Producer.ID, Version: a.Receipt.Producer.Version}
	}
	out := make([]closureprotocol.ProducerVersion, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return sortedProducers(out)
}

func sortedProducers(in []closureprotocol.ProducerVersion) []closureprotocol.ProducerVersion {
	out := append([]closureprotocol.ProducerVersion(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Producer != out[j].Producer {
			return out[i].Producer < out[j].Producer
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func sortedImpacts(in []closureprotocol.GovernedKnowledgeImpact) []closureprotocol.GovernedKnowledgeImpact {
	out := append([]closureprotocol.GovernedKnowledgeImpact(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out
}
