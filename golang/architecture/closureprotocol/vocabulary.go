// SPDX-License-Identifier: AGPL-3.0-only

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

	OperationRead     OperationKind = "read"
	OperationCreate   OperationKind = "create"
	OperationModify   OperationKind = "modify"
	OperationDelete   OperationKind = "delete"
	OperationRename   OperationKind = "rename"
	OperationExecute  OperationKind = "execute"
	OperationMigrate  OperationKind = "migrate"
	OperationRebuild  OperationKind = "rebuild"
	OperationObserve  OperationKind = "observe"
	OperationDispose  OperationKind = "dispose"
	OperationPromote  OperationKind = "promote"
	OperationComplete OperationKind = "complete"

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
		OperationDispose,
		OperationPromote,
		OperationComplete,
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

// OCCURRENCE SEMANTICS — the declared cardinality of each ledger event type.
//
// Without this table the protocol semantics of all nineteen types are encoded,
// accidentally, in the direction of a `for` loop: a backward scan makes every
// type behave as "latest wins", which is correct for a projection, wrong for a
// single-use fact, and destructive for an accumulating record.
//
// "LATEST" IS A DECLARED PROPERTY OF AN EVENT CLASS, never an emergent property
// of iteration order. Ratified as B-R1.
type OccurrenceSemantics string

const (
	// OccurrenceSingleton: at most one occurrence within its cardinality key.
	// Zero, one-valid, more-than-one and one-malformed are four distinct
	// semantic states, and there is NO selection algorithm among occurrences.
	OccurrenceSingleton OccurrenceSemantics = "singleton"
	// OccurrenceSuperseding: may legitimately recur; the current one is current.
	OccurrenceSuperseding OccurrenceSemantics = "superseding"
	// OccurrenceAccumulating: may recur and EVERY occurrence matters. Selecting
	// "latest" discards evidence.
	OccurrenceAccumulating OccurrenceSemantics = "accumulating"
	// OccurrenceUnratified: the vocabulary recognises the token, but no producer
	// contract has been ratified. A writer must not emit it, and a reader must
	// refuse it rather than let it participate in an authority reduction.
	OccurrenceUnratified OccurrenceSemantics = "unratified"
)

// EventOccurrence declares the cardinality of every member of LedgerEventTypes.
// TestEventOccurrenceCoversTheClosedSet pins it to that closed set.
var EventOccurrence = map[LedgerEventType]OccurrenceSemantics{
	LedgerEventLegacyImport:      OccurrenceSingleton,
	LedgerEventTaskPrepared:      OccurrenceSingleton,
	LedgerEventAdmissionConsumed: OccurrenceSingleton,
	LedgerEventChangeObserved:    OccurrenceSingleton,
	LedgerEventCertified:         OccurrenceSingleton,
	LedgerEventCompleted:         OccurrenceSingleton,
	LedgerEventRevoked:           OccurrenceSingleton,

	LedgerEventAdmissionDecided:     OccurrenceSuperseding,
	LedgerEventAuthorityResolved:    OccurrenceSuperseding,
	LedgerEventTaskControlProjected: OccurrenceSuperseding,

	LedgerEventConvergenceAdvanced:         OccurrenceAccumulating,
	LedgerEventClosureAssessed:             OccurrenceAccumulating,
	LedgerEventScopeVerified:               OccurrenceAccumulating,
	LedgerEventResultTransitionRecorded:    OccurrenceAccumulating,
	LedgerEventQuestionDispositionRecorded: OccurrenceAccumulating,

	LedgerEventEvidenceRecorded:  OccurrenceUnratified,
	LedgerEventProofDischarged:   OccurrenceUnratified,
	LedgerEventMigrationExecuted: OccurrenceUnratified,
	LedgerEventTaskMarkedStale:   OccurrenceUnratified,
}

// RequiredEventArtifacts declares the artifacts an event type's SEMANTICS depend
// on. An event of that type without them is not an impoverished record; it is a
// record whose meaning cannot be reconstructed.
//
// Deliberately NOT the full B-R1 artifact column: only the rows enforced today
// are listed, so this table never claims an enforcement that does not exist.
// Adding a row is a behaviour change and belongs with its own falsifier.
var RequiredEventArtifacts = map[LedgerEventType][]string{
	LedgerEventAdmissionConsumed:           {"capability_consumption"},
	LedgerEventResultTransitionRecorded:    {"result_transition_receipt"},
	LedgerEventQuestionDispositionRecorded: {"question_disposition_receipt"},
}

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
