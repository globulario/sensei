// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

type RepositorySnapshot struct {
	Domain           string `json:"domain" yaml:"domain"`
	Revision         string `json:"revision,omitempty" yaml:"revision,omitempty"`
	RevisionStatus   string `json:"revision_status" yaml:"revision_status"`
	TreeDigestSHA256 string `json:"tree_digest_sha256,omitempty" yaml:"tree_digest_sha256,omitempty"`
}

type GraphSnapshot struct {
	DigestSHA256  string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	DigestStatus  string `json:"digest_status" yaml:"digest_status"`
	SchemaVersion string `json:"schema_version,omitempty" yaml:"schema_version,omitempty"`
	Path          string `json:"path,omitempty" yaml:"path,omitempty"`
}

type TaskBinding struct {
	ID                    string `json:"id" yaml:"id"`
	SessionID             string `json:"session_id" yaml:"session_id"`
	IterationDigestSHA256 string `json:"iteration_digest_sha256,omitempty" yaml:"iteration_digest_sha256,omitempty"`
}

type PolicyBinding struct {
	Admission        string `json:"admission" yaml:"admission"`
	Certification    string `json:"certification" yaml:"certification"`
	Completion       string `json:"completion" yaml:"completion"`
	Revocation       string `json:"revocation" yaml:"revocation"`
	Ledger           string `json:"ledger" yaml:"ledger"`
	Canonicalization string `json:"canonicalization" yaml:"canonicalization"`
}

type BaseBinding struct {
	Repository RepositorySnapshot `json:"repository" yaml:"repository"`
	Graph      GraphSnapshot      `json:"graph" yaml:"graph"`
	Task       TaskBinding        `json:"task" yaml:"task"`
	Policies   PolicyBinding      `json:"policies" yaml:"policies"`
}

type ResultArtifact struct {
	Path         string `json:"path" yaml:"path"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

type ResultBinding struct {
	BaseRevision           string           `json:"base_revision" yaml:"base_revision"`
	PatchDigestSHA256      string           `json:"patch_digest_sha256" yaml:"patch_digest_sha256"`
	ResultTreeDigestSHA256 string           `json:"result_tree_digest_sha256" yaml:"result_tree_digest_sha256"`
	ResultRevision         string           `json:"result_revision,omitempty" yaml:"result_revision,omitempty"`
	GraphDigestSHA256      string           `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	GeneratedArtifacts     []ResultArtifact `json:"generated_artifacts,omitempty" yaml:"generated_artifacts,omitempty"`
}

type RuntimeTarget struct {
	Platform                string   `json:"platform" yaml:"platform"`
	EnvironmentID           string   `json:"environment_id,omitempty" yaml:"environment_id,omitempty"`
	DeploymentID            string   `json:"deployment_id,omitempty" yaml:"deployment_id,omitempty"`
	NodeIDs                 []string `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	ServiceInstances        []string `json:"service_instances,omitempty" yaml:"service_instances,omitempty"`
	ReleaseRevision         string   `json:"release_revision,omitempty" yaml:"release_revision,omitempty"`
	ConfigurationGeneration string   `json:"configuration_generation,omitempty" yaml:"configuration_generation,omitempty"`
}

type ActorBinding struct {
	PrincipalID                       string    `json:"principal_id" yaml:"principal_id"`
	ActorKind                         ActorKind `json:"actor_kind" yaml:"actor_kind"`
	Roles                             []string  `json:"roles,omitempty" yaml:"roles,omitempty"`
	Issuer                            string    `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	AuthenticationReceiptDigestSHA256 string    `json:"authentication_receipt_digest_sha256,omitempty" yaml:"authentication_receipt_digest_sha256,omitempty"`
	RoleAttestationReceiptDigests     []string  `json:"role_attestation_receipt_digests_sha256,omitempty" yaml:"role_attestation_receipt_digests_sha256,omitempty"`
	DelegationReceiptDigests          []string  `json:"delegation_receipt_digests_sha256,omitempty" yaml:"delegation_receipt_digests_sha256,omitempty"`
}

type AuthenticationReceipt struct {
	ReceiptID              string           `json:"receipt_id" yaml:"receipt_id"`
	PrincipalID            string           `json:"principal_id" yaml:"principal_id"`
	Issuer                 string           `json:"issuer" yaml:"issuer"`
	AuthenticationArtifact LedgerPayloadRef `json:"authentication_artifact" yaml:"authentication_artifact"`
	AuthenticatedAt        string           `json:"authenticated_at" yaml:"authenticated_at"`
	ExpiresAt              string           `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	Status                 ReceiptStatus    `json:"status" yaml:"status"`
	ReceiptDigestSHA256    string           `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

type RoleAttestationReceipt struct {
	ReceiptID                         string        `json:"receipt_id" yaml:"receipt_id"`
	PrincipalID                       string        `json:"principal_id" yaml:"principal_id"`
	ActorKind                         ActorKind     `json:"actor_kind" yaml:"actor_kind"`
	Issuer                            string        `json:"issuer" yaml:"issuer"`
	RoleIDs                           []string      `json:"role_ids" yaml:"role_ids"`
	AuthenticationReceiptDigestSHA256 string        `json:"authentication_receipt_digest_sha256,omitempty" yaml:"authentication_receipt_digest_sha256,omitempty"`
	IssuedAt                          string        `json:"issued_at" yaml:"issued_at"`
	ValidUntil                        string        `json:"valid_until,omitempty" yaml:"valid_until,omitempty"`
	Status                            ReceiptStatus `json:"status" yaml:"status"`
	ReceiptDigestSHA256               string        `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

type DelegationReceipt struct {
	DelegationID         string          `json:"delegation_id" yaml:"delegation_id"`
	ParentGrantID        string          `json:"parent_grant_id,omitempty" yaml:"parent_grant_id,omitempty"`
	ParentDelegationID   string          `json:"parent_delegation_id,omitempty" yaml:"parent_delegation_id,omitempty"`
	DelegatorPrincipalID string          `json:"delegator_principal_id" yaml:"delegator_principal_id"`
	DelegatedPrincipalID string          `json:"delegated_principal_id" yaml:"delegated_principal_id"`
	RoleIDs              []string        `json:"role_ids,omitempty" yaml:"role_ids,omitempty"`
	AuthorityDomainIDs   []string        `json:"authority_domain_ids,omitempty" yaml:"authority_domain_ids,omitempty"`
	Actions              []OperationKind `json:"actions,omitempty" yaml:"actions,omitempty"`
	MechanismKinds       []MechanismKind `json:"mechanism_kinds,omitempty" yaml:"mechanism_kinds,omitempty"`
	TargetKinds          []string        `json:"target_kinds,omitempty" yaml:"target_kinds,omitempty"`
	TargetSelectors      []string        `json:"target_selectors,omitempty" yaml:"target_selectors,omitempty"`
	MaximumRiskClass     string          `json:"maximum_risk_class,omitempty" yaml:"maximum_risk_class,omitempty"`
	PolicyID             string          `json:"policy_id" yaml:"policy_id"`
	Issuer               string          `json:"issuer" yaml:"issuer"`
	IssuedAt             string          `json:"issued_at" yaml:"issued_at"`
	ValidFrom            string          `json:"valid_from" yaml:"valid_from"`
	ValidUntil           string          `json:"valid_until,omitempty" yaml:"valid_until,omitempty"`
	AllowSubdelegation   bool            `json:"allow_subdelegation,omitempty" yaml:"allow_subdelegation,omitempty"`
	Status               ReceiptStatus   `json:"status" yaml:"status"`
	ReceiptDigestSHA256  string          `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

type ChangeOperation struct {
	OperationID        string        `json:"operation_id" yaml:"operation_id"`
	Kind               OperationKind `json:"kind" yaml:"kind"`
	TargetKind         string        `json:"target_kind" yaml:"target_kind"`
	Target             string        `json:"target" yaml:"target"`
	AuthorityDomainIDs []string      `json:"authority_domain_ids,omitempty" yaml:"authority_domain_ids,omitempty"`
	SelectedMechanism  MechanismKind `json:"selected_mechanism" yaml:"selected_mechanism"`
	IntendedEffect     string        `json:"intended_effect,omitempty" yaml:"intended_effect,omitempty"`
	RiskClass          string        `json:"risk_class,omitempty" yaml:"risk_class,omitempty"`
}

type ChangePlan struct {
	PlanID     string            `json:"plan_id" yaml:"plan_id"`
	Operations []ChangeOperation `json:"operations" yaml:"operations"`
}

type AuthorityResolutionOperation struct {
	OperationID                 string        `json:"operation_id" yaml:"operation_id"`
	Status                      ReceiptStatus `json:"status" yaml:"status"`
	AuthorityDomainIDs          []string      `json:"authority_domain_ids,omitempty" yaml:"authority_domain_ids,omitempty"`
	GrantIDs                    []string      `json:"grant_ids,omitempty" yaml:"grant_ids,omitempty"`
	DelegationChain             []string      `json:"delegation_chain,omitempty" yaml:"delegation_chain,omitempty"`
	LegalMechanisms             []string      `json:"legal_mechanisms,omitempty" yaml:"legal_mechanisms,omitempty"`
	SelectedMechanism           MechanismKind `json:"selected_mechanism" yaml:"selected_mechanism"`
	RequiredRuntimeMechanismIDs []string      `json:"required_runtime_mechanism_ids,omitempty" yaml:"required_runtime_mechanism_ids,omitempty"`
	Limitations                 []string      `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type AuthorityResolution struct {
	ResolutionID                      string                         `json:"resolution_id,omitempty" yaml:"resolution_id,omitempty"`
	ActorBindingDigestSHA256          string                         `json:"actor_binding_digest_sha256" yaml:"actor_binding_digest_sha256"`
	AuthenticationReceiptDigestSHA256 string                         `json:"authentication_receipt_digest_sha256,omitempty" yaml:"authentication_receipt_digest_sha256,omitempty"`
	BaseBindingDigestSHA256           string                         `json:"base_binding_digest_sha256" yaml:"base_binding_digest_sha256"`
	ClosureAssessmentDigestSHA256     string                         `json:"closure_assessment_digest_sha256" yaml:"closure_assessment_digest_sha256"`
	OperationSetDigestSHA256          string                         `json:"operation_set_digest_sha256" yaml:"operation_set_digest_sha256"`
	AuthorityPolicyGraphDigestSHA256  string                         `json:"authority_policy_graph_digest_sha256" yaml:"authority_policy_graph_digest_sha256"`
	PolicyID                          string                         `json:"policy_id" yaml:"policy_id"`
	EvaluatedAt                       string                         `json:"evaluated_at" yaml:"evaluated_at"`
	Status                            ReceiptStatus                  `json:"status" yaml:"status"`
	OperationResults                  []AuthorityResolutionOperation `json:"operation_results" yaml:"operation_results"`
	Limitations                       []string                       `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	AuthorityResolutionDigestSHA256   string                         `json:"authority_resolution_digest_sha256,omitempty" yaml:"authority_resolution_digest_sha256,omitempty"`
}

type AdmissionRequest struct {
	ActorBinding                    ActorBinding `json:"actor_binding" yaml:"actor_binding"`
	BaseBinding                     BaseBinding  `json:"base_binding" yaml:"base_binding"`
	ChangePlan                      ChangePlan   `json:"change_plan" yaml:"change_plan"`
	AuthorityResolutionDigestSHA256 string       `json:"authority_resolution_digest_sha256" yaml:"authority_resolution_digest_sha256"`
	AcceptedConditions              []string     `json:"accepted_conditions,omitempty" yaml:"accepted_conditions,omitempty"`
	RequestedValidity               string       `json:"requested_validity,omitempty" yaml:"requested_validity,omitempty"`
	PolicyID                        string       `json:"policy_id" yaml:"policy_id"`
}

type OperationAdmissionVerdict struct {
	OperationID string   `json:"operation_id" yaml:"operation_id"`
	Verdict     string   `json:"verdict" yaml:"verdict"`
	ReasonCodes []string `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
}

type AdmissionDecision struct {
	DecisionID               string                      `json:"decision_id,omitempty" yaml:"decision_id,omitempty"`
	RequestDigestSHA256      string                      `json:"request_digest_sha256" yaml:"request_digest_sha256"`
	PolicyID                 string                      `json:"policy_id" yaml:"policy_id"`
	OperationVerdicts        []OperationAdmissionVerdict `json:"operation_verdicts" yaml:"operation_verdicts"`
	CapabilityID             string                      `json:"capability_id" yaml:"capability_id"`
	CapabilityExpiry         string                      `json:"capability_expiry,omitempty" yaml:"capability_expiry,omitempty"`
	RiskBudget               int                         `json:"risk_budget,omitempty" yaml:"risk_budget,omitempty"`
	OperationBudget          int                         `json:"operation_budget,omitempty" yaml:"operation_budget,omitempty"`
	RequiredProofSlots       []string                    `json:"required_proof_slots,omitempty" yaml:"required_proof_slots,omitempty"`
	RequiredEvidenceProfiles []string                    `json:"required_evidence_profiles,omitempty" yaml:"required_evidence_profiles,omitempty"`
	RequiredResultRebuilds   []string                    `json:"required_result_rebuilds,omitempty" yaml:"required_result_rebuilds,omitempty"`
	CompletionPolicyID       string                      `json:"completion_policy_id" yaml:"completion_policy_id"`
}

type CapabilityConsumption struct {
	CapabilityID         string        `json:"capability_id" yaml:"capability_id"`
	Task                 TaskBinding   `json:"task" yaml:"task"`
	ConsumerActor        ActorBinding  `json:"consumer_actor" yaml:"consumer_actor"`
	ConsumedOperationIDs []string      `json:"consumed_operation_ids" yaml:"consumed_operation_ids"`
	ConsumedAt           string        `json:"consumed_at" yaml:"consumed_at"`
	DecisionDigestSHA256 string        `json:"decision_digest_sha256" yaml:"decision_digest_sha256"`
	OneUseStatus         ReceiptStatus `json:"one_use_status" yaml:"one_use_status"`
}

type EvidenceProfile struct {
	ProfileID            string        `json:"profile_id" yaml:"profile_id"`
	Owner                string        `json:"owner" yaml:"owner"`
	LegalObservationPath string        `json:"legal_observation_path" yaml:"legal_observation_path"`
	EvidenceKind         EvidenceKind  `json:"evidence_kind" yaml:"evidence_kind"`
	Freshness            string        `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	Trust                string        `json:"trust,omitempty" yaml:"trust,omitempty"`
	RuntimeTargetKind    string        `json:"runtime_target_kind,omitempty" yaml:"runtime_target_kind,omitempty"`
	GovernedTarget       string        `json:"governed_target,omitempty" yaml:"governed_target,omitempty"`
	Status               ReceiptStatus `json:"status" yaml:"status"`
}

type EvidenceReceipt struct {
	ReceiptID           string         `json:"receipt_id" yaml:"receipt_id"`
	EvidenceKind        EvidenceKind   `json:"evidence_kind" yaml:"evidence_kind"`
	ProfileID           string         `json:"profile_id" yaml:"profile_id"`
	ResultBinding       ResultBinding  `json:"result_binding" yaml:"result_binding"`
	RuntimeTarget       *RuntimeTarget `json:"runtime_target,omitempty" yaml:"runtime_target,omitempty"`
	Producer            string         `json:"producer" yaml:"producer"`
	ObservationPath     string         `json:"observation_path" yaml:"observation_path"`
	ObservedAt          string         `json:"observed_at" yaml:"observed_at"`
	ExpiresAt           string         `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	Status              ReceiptStatus  `json:"status" yaml:"status"`
	Trust               string         `json:"trust,omitempty" yaml:"trust,omitempty"`
	PayloadDigestSHA256 string         `json:"payload_digest_sha256" yaml:"payload_digest_sha256"`
	Conflicts           []string       `json:"conflicts,omitempty" yaml:"conflicts,omitempty"`
}

type ProofSlotResult struct {
	SlotID     string          `json:"slot_id" yaml:"slot_id"`
	Status     DimensionStatus `json:"status" yaml:"status"`
	ReceiptIDs []string        `json:"receipt_ids,omitempty" yaml:"receipt_ids,omitempty"`
}

type ProofDischarge struct {
	ObligationID          string            `json:"obligation_id" yaml:"obligation_id"`
	Status                ReceiptStatus     `json:"status" yaml:"status"`
	SlotResults           []ProofSlotResult `json:"slot_results" yaml:"slot_results"`
	MappedEvidence        []string          `json:"mapped_evidence,omitempty" yaml:"mapped_evidence,omitempty"`
	MissingSlots          []string          `json:"missing_slots,omitempty" yaml:"missing_slots,omitempty"`
	IncompatibleReceipts  []string          `json:"incompatible_receipts,omitempty" yaml:"incompatible_receipts,omitempty"`
	DischargeDigestSHA256 string            `json:"discharge_digest_sha256,omitempty" yaml:"discharge_digest_sha256,omitempty"`
}

type CertificationReceipt struct {
	ResultBinding            ResultBinding        `json:"result_binding" yaml:"result_binding"`
	CertificationPolicy      string               `json:"certification_policy" yaml:"certification_policy"`
	ScopeLane                DimensionStatus      `json:"scope_lane" yaml:"scope_lane"`
	AuthorityLane            DimensionStatus      `json:"authority_lane" yaml:"authority_lane"`
	ProofLane                DimensionStatus      `json:"proof_lane" yaml:"proof_lane"`
	EvidenceLane             DimensionStatus      `json:"evidence_lane" yaml:"evidence_lane"`
	ForbiddenMoves           []string             `json:"forbidden_moves,omitempty" yaml:"forbidden_moves,omitempty"`
	UnresolvedContradictions []string             `json:"unresolved_contradictions,omitempty" yaml:"unresolved_contradictions,omitempty"`
	Limitations              []string             `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	CertificationVerdict     CertificationVerdict `json:"certification_verdict" yaml:"certification_verdict"`
	DigestSHA256             string               `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

type WaiverReceipt struct {
	WaiverID      string        `json:"waiver_id" yaml:"waiver_id"`
	Dimension     Dimension     `json:"dimension" yaml:"dimension"`
	PolicyID      string        `json:"policy_id" yaml:"policy_id"`
	Justification string        `json:"justification" yaml:"justification"`
	ExpiresAt     string        `json:"expires_at" yaml:"expires_at"`
	AppliesTo     []string      `json:"applies_to,omitempty" yaml:"applies_to,omitempty"`
	Status        ReceiptStatus `json:"status" yaml:"status"`
	DigestSHA256  string        `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

type CompletionReceipt struct {
	Task                              TaskBinding        `json:"task" yaml:"task"`
	TerminalStatus                    TaskTerminalStatus `json:"terminal_status" yaml:"terminal_status"`
	BaseBinding                       BaseBinding        `json:"base_binding" yaml:"base_binding"`
	ResultBinding                     ResultBinding      `json:"result_binding" yaml:"result_binding"`
	ClosureAssessmentDigestSHA256     string             `json:"closure_assessment_digest_sha256" yaml:"closure_assessment_digest_sha256"`
	AuthorityResolutionDigestSHA256   string             `json:"authority_resolution_digest_sha256" yaml:"authority_resolution_digest_sha256"`
	AdmissionDecisionDigestSHA256     string             `json:"admission_decision_digest_sha256" yaml:"admission_decision_digest_sha256"`
	AdmissionVerificationDigestSHA256 string             `json:"admission_verification_digest_sha256" yaml:"admission_verification_digest_sha256"`
	CertificationDigestSHA256         string             `json:"certification_digest_sha256" yaml:"certification_digest_sha256"`
	ProofDischargeDigests             []string           `json:"proof_discharge_digests,omitempty" yaml:"proof_discharge_digests,omitempty"`
	EvidenceReceiptDigests            []string           `json:"evidence_receipt_digests,omitempty" yaml:"evidence_receipt_digests,omitempty"`
	ArtifactDigests                   []string           `json:"artifact_digests,omitempty" yaml:"artifact_digests,omitempty"`
	WaiverDigests                     []string           `json:"waiver_digests,omitempty" yaml:"waiver_digests,omitempty"`
	CompletionPolicy                  string             `json:"completion_policy" yaml:"completion_policy"`
	CompletedAt                       string             `json:"completed_at" yaml:"completed_at"`
	CompletingActor                   string             `json:"completing_actor" yaml:"completing_actor"`
	RevocationConditions              []string           `json:"revocation_conditions,omitempty" yaml:"revocation_conditions,omitempty"`
	ReceiptDigestSHA256               string             `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

type LedgerPayloadRef struct {
	Path         string `json:"path" yaml:"path"`
	MediaType    string `json:"media_type" yaml:"media_type"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

// ArtifactReceipt is the first-class receipt for a single produced artifact
// (a generated repository artifact or an operational task artifact). Its
// identity IS its content digest, so it carries no self-excluding digest.
// ArtifactProducer pins the exact producer of an artifact so a later freshness
// assessment can detect producer drift. Both fields are identity-bearing.
type ArtifactProducer struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
}

// ArtifactReceipt is the first-class receipt for a single operational pipeline
// artifact (compiled result graph, inferred/maintained claims, plane/closure
// assessment, proof requirements, artifact manifest). Repository-tree artifacts
// are NOT recorded here — they live in ResultBinding.GeneratedArtifacts as part
// of the exact result tree.
//
// SemanticDigestSHA256 is the artifact's content identity. ByteDigestSHA256 is
// its serialized-bytes identity, mandatory whenever the artifact has a Path (a
// serialized file); the two may hold the same value when they coincide. Every
// artifact binds the exact current result via ResultBindingDigestSHA256, so an
// artifact produced against another result cannot be reused. ReceiptDigestSHA256
// is the receipt's self-excluding identity; derivations reference an artifact by
// this digest.
type ArtifactReceipt struct {
	ID                        string           `json:"id,omitempty" yaml:"id,omitempty"`
	Kind                      string           `json:"kind" yaml:"kind"`
	Path                      string           `json:"path,omitempty" yaml:"path,omitempty"`
	MediaType                 string           `json:"media_type,omitempty" yaml:"media_type,omitempty"`
	SemanticDigestSHA256      string           `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
	ByteDigestSHA256          string           `json:"byte_digest_sha256,omitempty" yaml:"byte_digest_sha256,omitempty"`
	Producer                  ArtifactProducer `json:"producer" yaml:"producer"`
	ResultBindingDigestSHA256 string           `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	ReceiptDigestSHA256       string           `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

// ProducerVersion is the receipt-level summary of pipeline producer versions.
// It is a convenience summary and does NOT replace the mandatory per-artifact
// ArtifactReceipt.Producer.
type ProducerVersion struct {
	Producer string `json:"producer" yaml:"producer"`
	Version  string `json:"version" yaml:"version"`
}

// ResultPipelineStage names a stage in the result-derivation graph. The
// vocabulary is closed: an unknown stage fails validation.
type ResultPipelineStage string

const (
	StageGovernedSourceManifest       ResultPipelineStage = "governed_source_manifest"
	StageGeneratedRepositoryArtifacts ResultPipelineStage = "generated_repository_artifacts"
	StageArchitectureGraph            ResultPipelineStage = "architecture_graph"
	StageInferredClaims               ResultPipelineStage = "inferred_claims"
	StageMaintainedClaims             ResultPipelineStage = "maintained_claims"
	StagePlaneAssessment              ResultPipelineStage = "plane_assessment"
	StageClosureAssessment            ResultPipelineStage = "closure_assessment"
	StageProofRequirements            ResultPipelineStage = "proof_requirements"
	StageArtifactManifest             ResultPipelineStage = "artifact_manifest"
)

// ResultPipelineStages is the closed, ordered set of derivation stages. Every
// stage is mandatory in a complete result transition.
var ResultPipelineStages = []ResultPipelineStage{
	StageGovernedSourceManifest, StageGeneratedRepositoryArtifacts, StageArchitectureGraph,
	StageInferredClaims, StageMaintainedClaims, StagePlaneAssessment, StageClosureAssessment,
	StageProofRequirements, StageArtifactManifest,
}

// ArtifactDerivation is one edge of the freshness derivation graph: it records
// that the stage's output artifact was derived from the named input artifacts
// and bindings. Freshness is proven structurally from these edges, not inferred
// from artifact naming conventions. Ledger-only; never projected into RDF.
type ArtifactDerivation struct {
	Stage                             ResultPipelineStage `json:"stage" yaml:"stage"`
	OutputArtifactReceiptDigestSHA256 string              `json:"output_artifact_receipt_digest_sha256" yaml:"output_artifact_receipt_digest_sha256"`
	InputArtifactReceiptDigestsSHA256 []string            `json:"input_artifact_receipt_digests_sha256,omitempty" yaml:"input_artifact_receipt_digests_sha256,omitempty"`
	InputBindingDigestsSHA256         []string            `json:"input_binding_digests_sha256,omitempty" yaml:"input_binding_digests_sha256,omitempty"`
}

// GovernedKnowledgeImpact is a typed, digest-derived change fact for one
// governed-knowledge category. Whether the category changed is DERIVED as
// BaseManifestDigestSHA256 != ResultManifestDigestSHA256 — never a separately
// trusted boolean. When the exact changed record ids cannot be determined,
// ChangedRecordIDs is empty with unequal manifest digests; uncertainty is never
// converted into "unchanged".
type GovernedKnowledgeImpact struct {
	Category                   string   `json:"category" yaml:"category"`
	BaseManifestDigestSHA256   string   `json:"base_manifest_digest_sha256" yaml:"base_manifest_digest_sha256"`
	ResultManifestDigestSHA256 string   `json:"result_manifest_digest_sha256" yaml:"result_manifest_digest_sha256"`
	ChangedRecordIDs           []string `json:"changed_record_ids,omitempty" yaml:"changed_record_ids,omitempty"`
}

// ResultTransitionReceipt records the frozen, digest-bound transition of a task
// result into the current base of record: the point at which the result tree,
// the compiled result graph, and every generated artifact become the ground
// truth subsequent proving and certification are recomputed against. It is
// recorded on the ledger after scope_verified and before the proving phase; it
// establishes NO certification and NO completion.
//
// The result is one canonical representation — the embedded frozen ResultBinding
// (with ResultBindingDigestSHA256 recomputed from it). Operational pipeline
// artifacts and their derivation graph make freshness structurally verifiable;
// governed-knowledge impacts are derived from manifest-digest comparison, so a
// downstream freshness engine invalidates prior proof, evidence, and
// certification bound to a different result.
type ResultTransitionReceipt struct {
	TransitionID                      string                    `json:"transition_id,omitempty" yaml:"transition_id,omitempty"`
	Task                              TaskBinding               `json:"task" yaml:"task"`
	BaseBindingDigestSHA256           string                    `json:"base_binding_digest_sha256" yaml:"base_binding_digest_sha256"`
	ActorBindingDigestSHA256          string                    `json:"actor_binding_digest_sha256" yaml:"actor_binding_digest_sha256"`
	AuthorityResolutionDigestSHA256   string                    `json:"authority_resolution_digest_sha256" yaml:"authority_resolution_digest_sha256"`
	AdmissionDecisionDigestSHA256     string                    `json:"admission_decision_digest_sha256" yaml:"admission_decision_digest_sha256"`
	CapabilityConsumptionDigestSHA256 string                    `json:"capability_consumption_digest_sha256" yaml:"capability_consumption_digest_sha256"`
	ObservedChangeSetDigestSHA256     string                    `json:"observed_change_set_digest_sha256" yaml:"observed_change_set_digest_sha256"`
	ScopeVerificationDigestSHA256     string                    `json:"scope_verification_digest_sha256" yaml:"scope_verification_digest_sha256"`
	ResultBinding                     ResultBinding             `json:"result_binding" yaml:"result_binding"`
	ResultBindingDigestSHA256         string                    `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	OperationalArtifactReceipts       []ArtifactReceipt         `json:"operational_artifact_receipts,omitempty" yaml:"operational_artifact_receipts,omitempty"`
	Derivations                       []ArtifactDerivation      `json:"derivations,omitempty" yaml:"derivations,omitempty"`
	GovernedKnowledgeImpacts          []GovernedKnowledgeImpact `json:"governed_knowledge_impacts,omitempty" yaml:"governed_knowledge_impacts,omitempty"`
	PipelineProducerVersions          []ProducerVersion         `json:"pipeline_producer_versions,omitempty" yaml:"pipeline_producer_versions,omitempty"`
	PipelinePolicyID                  string                    `json:"pipeline_policy_id" yaml:"pipeline_policy_id"`
	Limitations                       []string                  `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	RecordedAt                        string                    `json:"recorded_at" yaml:"recorded_at"`
	Status                            ReceiptStatus             `json:"status" yaml:"status"`
	ReceiptDigestSHA256               string                    `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

type RevocationReceipt struct {
	RevocationID       string   `json:"revocation_id" yaml:"revocation_id"`
	RevokedTargetID    string   `json:"revoked_target_id" yaml:"revoked_target_id"`
	PriorDigestSHA256  string   `json:"prior_digest_sha256" yaml:"prior_digest_sha256"`
	RevocationReason   string   `json:"revocation_reason" yaml:"revocation_reason"`
	TriggeringEvidence []string `json:"triggering_evidence,omitempty" yaml:"triggering_evidence,omitempty"`
	PolicyID           string   `json:"policy_id" yaml:"policy_id"`
	ActorID            string   `json:"actor_id" yaml:"actor_id"`
	RevokedAt          string   `json:"revoked_at" yaml:"revoked_at"`
	RemediationTaskID  string   `json:"remediation_task_id,omitempty" yaml:"remediation_task_id,omitempty"`
}

type MigrationExecutionReceipt struct {
	MigrationPlanID             string        `json:"migration_plan_id" yaml:"migration_plan_id"`
	StepID                      string        `json:"step_id" yaml:"step_id"`
	SourceState                 string        `json:"source_state" yaml:"source_state"`
	TargetState                 string        `json:"target_state" yaml:"target_state"`
	Mechanism                   MechanismKind `json:"mechanism" yaml:"mechanism"`
	ActorID                     string        `json:"actor_id" yaml:"actor_id"`
	MutationReceiptDigestSHA256 string        `json:"mutation_receipt_digest_sha256" yaml:"mutation_receipt_digest_sha256"`
	ProofDischargeDigestSHA256  string        `json:"proof_discharge_digest_sha256" yaml:"proof_discharge_digest_sha256"`
	RollbackAvailable           bool          `json:"rollback_available" yaml:"rollback_available"`
	Status                      ReceiptStatus `json:"status" yaml:"status"`
}

type LedgerEntry struct {
	Sequence                  int              `json:"sequence" yaml:"sequence"`
	PreviousEntryDigestSHA256 string           `json:"previous_entry_digest_sha256,omitempty" yaml:"previous_entry_digest_sha256,omitempty"`
	EventType                 LedgerEventType  `json:"event_type" yaml:"event_type"`
	Task                      TaskBinding      `json:"task" yaml:"task"`
	Payload                   LedgerPayloadRef `json:"payload" yaml:"payload"`
	Producer                  string           `json:"producer" yaml:"producer"`
	ProducedAt                string           `json:"produced_at" yaml:"produced_at"`
	EntryDigestSHA256         string           `json:"entry_digest_sha256,omitempty" yaml:"entry_digest_sha256,omitempty"`
}

type DimensionResult struct {
	Dimension   Dimension       `json:"dimension" yaml:"dimension"`
	Status      DimensionStatus `json:"status" yaml:"status"`
	ReasonCodes []string        `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
	ReceiptIDs  []string        `json:"receipt_ids,omitempty" yaml:"receipt_ids,omitempty"`
	ExceptionID string          `json:"exception_id,omitempty" yaml:"exception_id,omitempty"`
}

type ClosureState struct {
	Dimensions []DimensionResult `json:"dimensions" yaml:"dimensions"`
}

type CompletionPolicy struct {
	PolicyID                string      `json:"policy_id" yaml:"policy_id"`
	AllowedWaiverDimensions []Dimension `json:"allowed_waiver_dimensions,omitempty" yaml:"allowed_waiver_dimensions,omitempty"`
	PermittedNotApplicable  []Dimension `json:"permitted_not_applicable,omitempty" yaml:"permitted_not_applicable,omitempty"`
}

type ClosureEvaluation struct {
	TerminallyClosed   bool              `json:"terminally_closed" yaml:"terminally_closed"`
	BlockingDimensions []DimensionResult `json:"blocking_dimensions,omitempty" yaml:"blocking_dimensions,omitempty"`
	AppliedExceptions  []string          `json:"applied_exceptions,omitempty" yaml:"applied_exceptions,omitempty"`
	ReasonCodes        []string          `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
	Limitations        []string          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}
