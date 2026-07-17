// SPDX-License-Identifier: Apache-2.0

package closureprotocol

type ReasoningClosureVerdict string
type TaskTerminalStatus string
type ActorKind string
type OperationKind string
type MechanismKind string
type EvidenceKind string
type ReceiptStatus string
type CertificationVerdict string
type Dimension string
type DimensionStatus string
type TaskPhase string
type LedgerEventType string

const (
	ProtocolVersion = "architectural-closure/v1"

	ReasoningOpen          ReasoningClosureVerdict = "open"
	ReasoningConditional   ReasoningClosureVerdict = "conditional"
	ReasoningClosed        ReasoningClosureVerdict = "closed"
	ReasoningUncertifiable ReasoningClosureVerdict = "uncertifiable"
	ReasoningStale         ReasoningClosureVerdict = "stale"

	TerminalCompleted              TaskTerminalStatus = "completed"
	TerminalCompletedWithException TaskTerminalStatus = "completed_with_exception"
	TerminalRefused                TaskTerminalStatus = "refused"
	TerminalAbandoned              TaskTerminalStatus = "abandoned"
	TerminalRevoked                TaskTerminalStatus = "revoked"

	ActorHuman   ActorKind = "human"
	ActorAgent   ActorKind = "agent"
	ActorService ActorKind = "service"
	ActorCI      ActorKind = "ci"
	ActorSystem  ActorKind = "system"

	OperationRead    OperationKind = "read"
	OperationCreate  OperationKind = "create"
	OperationModify  OperationKind = "modify"
	OperationDelete  OperationKind = "delete"
	OperationRename  OperationKind = "rename"
	OperationExecute OperationKind = "execute"
	OperationMigrate OperationKind = "migrate"
	OperationRebuild OperationKind = "rebuild"
	OperationObserve OperationKind = "observe"

	MechanismRepositoryEdit           MechanismKind = "repository_edit"
	MechanismOwnerRPC                 MechanismKind = "owner_rpc"
	MechanismGovernedWorkflow         MechanismKind = "governed_workflow"
	MechanismMigrationRunner          MechanismKind = "migration_runner"
	MechanismGeneratedArtifactRebuild MechanismKind = "generated_artifact_rebuild"
	MechanismTestRunner               MechanismKind = "test_runner"
	MechanismRuntimeAdapter           MechanismKind = "runtime_adapter"
	MechanismManualAuthorized         MechanismKind = "manual_authorized"

	EvidenceStatic    EvidenceKind = "static"
	EvidenceTest      EvidenceKind = "test"
	EvidenceRuntime   EvidenceKind = "runtime"
	EvidenceArtifact  EvidenceKind = "artifact"
	EvidenceReview    EvidenceKind = "review"
	EvidenceAuthority EvidenceKind = "authority"
	EvidenceHybrid    EvidenceKind = "hybrid"

	ReceiptValid      ReceiptStatus = "valid"
	ReceiptInvalid    ReceiptStatus = "invalid"
	ReceiptStale      ReceiptStatus = "stale"
	ReceiptConflicted ReceiptStatus = "conflicted"
	ReceiptSuperseded ReceiptStatus = "superseded"
	ReceiptRevoked    ReceiptStatus = "revoked"
	ReceiptUnknown    ReceiptStatus = "unknown"

	Certified                   CertificationVerdict = "certified"
	CertifiedWithConditions     CertificationVerdict = "certified_with_conditions"
	CertificationReviewRequired CertificationVerdict = "review_required"
	CertificationBlocked        CertificationVerdict = "blocked"
	CertificationUncertifiable  CertificationVerdict = "uncertifiable"
	CertificationStale          CertificationVerdict = "stale"
	CertificationRevoked        CertificationVerdict = "revoked"

	DimensionIdentity   Dimension = "identity"
	DimensionScope      Dimension = "scope"
	DimensionDirection  Dimension = "direction"
	DimensionAuthority  Dimension = "authority"
	DimensionMutation   Dimension = "mutation"
	DimensionProtection Dimension = "protection"
	DimensionEpistemic  Dimension = "epistemic"
	DimensionProof      Dimension = "proof"
	DimensionFreshness  Dimension = "freshness"
	DimensionCompletion Dimension = "completion"

	DimensionPass              DimensionStatus = "pass"
	DimensionPassWithException DimensionStatus = "pass_with_exception"
	DimensionBlocked           DimensionStatus = "blocked"
	DimensionUnknown           DimensionStatus = "unknown"
	DimensionStale             DimensionStatus = "stale"
	DimensionConflicted        DimensionStatus = "conflicted"
	DimensionNotApplicable     DimensionStatus = "not_applicable"

	PhasePrepared                TaskPhase = "prepared"
	PhaseConverging              TaskPhase = "converging"
	PhaseReadyForAdmission       TaskPhase = "ready_for_admission"
	PhaseAdmitted                TaskPhase = "admitted"
	PhaseMutationObserved        TaskPhase = "mutation_observed"
	PhaseScopeVerified           TaskPhase = "scope_verified"
	PhaseProving                 TaskPhase = "proving"
	PhaseCertified               TaskPhase = "certified"
	PhaseCompleted               TaskPhase = "completed"
	PhaseWaitingArchitect        TaskPhase = "waiting_architect"
	PhaseWaitingEvidence         TaskPhase = "waiting_evidence"
	PhaseWaitingGovernance       TaskPhase = "waiting_governance"
	PhaseWaitingMechanicalRepair TaskPhase = "waiting_mechanical_repair"
	PhaseRefused                 TaskPhase = "refused"
	PhaseStale                   TaskPhase = "stale"
	PhaseUncertifiable           TaskPhase = "uncertifiable"
	PhaseAbandoned               TaskPhase = "abandoned"
	PhaseRevoked                 TaskPhase = "revoked"

	LedgerEventLegacyImport                LedgerEventType = "legacy_import"
	LedgerEventTaskPrepared                LedgerEventType = "task_prepared"
	LedgerEventConvergenceAdvanced         LedgerEventType = "convergence_advanced"
	LedgerEventClosureAssessed             LedgerEventType = "closure_assessed"
	LedgerEventAdmissionDecided            LedgerEventType = "admission_decided"
	LedgerEventAuthorityResolved           LedgerEventType = "authority_resolved"
	LedgerEventAdmissionConsumed           LedgerEventType = "admission_consumed"
	LedgerEventChangeObserved              LedgerEventType = "change_observed"
	LedgerEventScopeVerified               LedgerEventType = "scope_verified"
	LedgerEventResultTransitionRecorded    LedgerEventType = "result_transition_recorded"
	LedgerEventQuestionDispositionRecorded LedgerEventType = "question_disposition_recorded"
	LedgerEventEvidenceRecorded            LedgerEventType = "evidence_recorded"
	LedgerEventProofDischarged             LedgerEventType = "proof_discharged"
	LedgerEventCertified                   LedgerEventType = "certified"
	LedgerEventCompleted                   LedgerEventType = "completed"
	LedgerEventRevoked                     LedgerEventType = "revoked"
	LedgerEventMigrationExecuted           LedgerEventType = "migration_executed"
	LedgerEventTaskControlProjected        LedgerEventType = "task_control_projected"
	LedgerEventTaskMarkedStale             LedgerEventType = "task_marked_stale"
)

var (
	ReasoningVerdicts = []ReasoningClosureVerdict{
		ReasoningOpen, ReasoningConditional, ReasoningClosed, ReasoningUncertifiable, ReasoningStale,
	}
	TerminalStatuses = []TaskTerminalStatus{
		TerminalCompleted, TerminalCompletedWithException, TerminalRefused, TerminalAbandoned, TerminalRevoked,
	}
	ActorKinds     = []ActorKind{ActorHuman, ActorAgent, ActorService, ActorCI, ActorSystem}
	OperationKinds = []OperationKind{
		OperationRead, OperationCreate, OperationModify, OperationDelete, OperationRename, OperationExecute,
		OperationMigrate, OperationRebuild, OperationObserve,
	}
	MechanismKinds = []MechanismKind{
		MechanismRepositoryEdit, MechanismOwnerRPC, MechanismGovernedWorkflow, MechanismMigrationRunner,
		MechanismGeneratedArtifactRebuild, MechanismTestRunner, MechanismRuntimeAdapter, MechanismManualAuthorized,
	}
	EvidenceKinds = []EvidenceKind{
		EvidenceStatic, EvidenceTest, EvidenceRuntime, EvidenceArtifact, EvidenceReview, EvidenceAuthority, EvidenceHybrid,
	}
	ReceiptStatuses = []ReceiptStatus{
		ReceiptValid, ReceiptInvalid, ReceiptStale, ReceiptConflicted, ReceiptSuperseded, ReceiptRevoked, ReceiptUnknown,
	}
	CertificationVerdicts = []CertificationVerdict{
		Certified, CertifiedWithConditions, CertificationReviewRequired, CertificationBlocked,
		CertificationUncertifiable, CertificationStale, CertificationRevoked,
	}
	Dimensions = []Dimension{
		DimensionIdentity, DimensionScope, DimensionDirection, DimensionAuthority, DimensionMutation,
		DimensionProtection, DimensionEpistemic, DimensionProof, DimensionFreshness, DimensionCompletion,
	}
	DimensionStatuses = []DimensionStatus{
		DimensionPass, DimensionPassWithException, DimensionBlocked, DimensionUnknown,
		DimensionStale, DimensionConflicted, DimensionNotApplicable,
	}
	TaskPhases = []TaskPhase{
		PhasePrepared, PhaseConverging, PhaseReadyForAdmission, PhaseAdmitted, PhaseMutationObserved,
		PhaseScopeVerified, PhaseProving, PhaseCertified, PhaseCompleted, PhaseWaitingArchitect,
		PhaseWaitingEvidence, PhaseWaitingGovernance, PhaseWaitingMechanicalRepair, PhaseRefused,
		PhaseStale, PhaseUncertifiable, PhaseAbandoned, PhaseRevoked,
	}
	LedgerEventTypes = []LedgerEventType{
		LedgerEventLegacyImport,
		LedgerEventTaskPrepared,
		LedgerEventConvergenceAdvanced,
		LedgerEventClosureAssessed,
		LedgerEventAdmissionDecided,
		LedgerEventAuthorityResolved,
		LedgerEventAdmissionConsumed,
		LedgerEventChangeObserved,
		LedgerEventScopeVerified,
		LedgerEventResultTransitionRecorded,
		LedgerEventQuestionDispositionRecorded,
		LedgerEventEvidenceRecorded,
		LedgerEventProofDischarged,
		LedgerEventCertified,
		LedgerEventCompleted,
		LedgerEventRevoked,
		LedgerEventMigrationExecuted,
		LedgerEventTaskControlProjected,
		LedgerEventTaskMarkedStale,
	}
)

var AllowedTaskTransitions = map[TaskPhase][]TaskPhase{
	PhasePrepared:                {PhaseConverging, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseConverging:              {PhaseReadyForAdmission, PhaseWaitingArchitect, PhaseWaitingEvidence, PhaseWaitingGovernance, PhaseWaitingMechanicalRepair, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseWaitingArchitect:        {PhaseConverging, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseWaitingEvidence:         {PhaseConverging, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseWaitingGovernance:       {PhaseConverging, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseWaitingMechanicalRepair: {PhaseConverging, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseReadyForAdmission:       {PhaseAdmitted, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseAdmitted:                {PhaseMutationObserved, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseMutationObserved:        {PhaseScopeVerified, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseScopeVerified:           {PhaseProving, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseProving:                 {PhaseCertified, PhaseStale, PhaseUncertifiable, PhaseAbandoned},
	PhaseCertified:               {PhaseCompleted, PhaseRevoked},
	PhaseCompleted:               {PhaseRevoked},
	PhaseRefused:                 {},
	PhaseStale:                   {},
	PhaseUncertifiable:           {},
	PhaseAbandoned:               {},
	PhaseRevoked:                 {},
}
