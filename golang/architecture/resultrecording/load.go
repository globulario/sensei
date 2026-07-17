// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
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

	build, err := reconstructBuildResult(taskDir, receipt, stageArtifacts, rt.ImpactReport)
	if err != nil {
		return RecordedTransition{}, err
	}
	rt.ReconstructedBuildResult = build
	return rt, nil
}

// reconstructBuildResult assembles a BuildResult from the receipt, the stored
// stage artifacts, the impact report, and the earlier verified upstream ledger
// records — with no repository read and no graph rebuild.
func reconstructBuildResult(taskDir string, receipt closureprotocol.ResultTransitionReceipt, stages []resultpipeline.PipelineArtifact, impact governedimpact.Report) (resultpipeline.BuildResult, error) {
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

	recAuth, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "load authority: %v", err)
	}
	decision, err := admission.LoadRecordedDecision(taskDir)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "load decision: %v", err)
	}
	observed, err := admission.LoadRecordedObservedChange(taskDir)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "load observed change: %v", err)
	}
	evaluatedAt, err := admission.LoadEventProducedAt(taskDir, closureprotocol.LedgerEventScopeVerified)
	if err != nil {
		return resultpipeline.BuildResult{}, recErr(CodeReloadFailed, "load evaluated at: %v", err)
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
		BaseBindingDigestSHA256:           receipt.BaseBindingDigestSHA256,
		ActorBindingDigestSHA256:          receipt.ActorBindingDigestSHA256,
		AuthorityResolutionDigestSHA256:   receipt.AuthorityResolutionDigestSHA256,
		AdmissionDecisionDigestSHA256:     receipt.AdmissionDecisionDigestSHA256,
		CapabilityConsumptionDigestSHA256: receipt.CapabilityConsumptionDigestSHA256,
		ObservedChangeSetDigestSHA256:     receipt.ObservedChangeSetDigestSHA256,
		ScopeVerificationDigestSHA256:     receipt.ScopeVerificationDigestSHA256,
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
	return nil
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
