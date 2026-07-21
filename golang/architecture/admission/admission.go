// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/convergence"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/questiongen"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei admission"

	PolicyStrictID      = "admission.strict.v1"
	PolicyStrictVersion = "v1"

	ModeInspect = "inspect"
	ModeModify  = "modify"

	OperationRead   = "read"
	OperationModify = "modify"

	DecisionAdmitted                 = "admitted"
	DecisionAdmittedWithConditions   = "admitted_with_conditions"
	DecisionWaiting                  = "waiting"
	DecisionRefused                  = "refused"
	DecisionUncertifiable            = "uncertifiable"
	CapabilityAdmitted               = DecisionAdmitted
	CapabilityAdmittedWithConditions = DecisionAdmittedWithConditions
	CapabilityWaiting                = DecisionWaiting
	CapabilityRefused                = DecisionRefused
	CapabilityUncertifiable          = DecisionUncertifiable

	VerificationScopeCompliant = "scope_compliant"
	VerificationScopeViolated  = "scope_violated"
	VerificationStale          = "stale"
	VerificationUncertifiable  = "uncertifiable"

	ReasonClosedScope                     = "admission.closed_scope"
	ReasonConditionalScope                = "admission.conditional_scope"
	ReasonWaitingArchitect                = "admission.waiting.architect"
	ReasonWaitingEvidence                 = "admission.waiting.evidence"
	ReasonWaitingGovernance               = "admission.waiting.governance"
	ReasonWaitingMechanicalRepair         = "admission.waiting.mechanical_repair"
	ReasonScopeOutsideClosedScope         = "admission.scope.outside_closed_scope"
	ReasonScopeUnrepresented              = "admission.scope.unrepresented"
	ReasonScopeMissingFile                = "admission.scope.missing_file"
	ReasonScopeNoExactModifyPath          = "admission.scope.no_exact_modify_path"
	ReasonAccessExceedsClosedScope        = "admission.access.exceeds_closed_scope"
	ReasonOperationUnsupported            = "admission.operation.unsupported"
	ReasonTaskClassMismatch               = "admission.task_class.mismatch"
	ReasonConditionAcknowledgementMissing = "admission.condition.acknowledgement_missing"
	ReasonConditionUnknownOrStale         = "admission.condition.unknown_or_stale"
	ReasonSessionStalled                  = "admission.session.stalled"
	ReasonSessionOscillating              = "admission.session.oscillating"
	ReasonSessionBudgetExhausted          = "admission.session.budget_exhausted"
	ReasonSessionUncertifiable            = "admission.session.uncertifiable"
	ReasonSessionStaleIteration           = "admission.session.stale_iteration"
	ReasonSessionInconsistentStatus       = "admission.session.inconsistent_status"
	ReasonBundleInvalid                   = "admission.bundle.invalid"
	ReasonBindingMismatch                 = "admission.binding.mismatch"
	ReasonGraphUnverified                 = "admission.graph.unverified"
	ReasonRepositoryRevisionUnverified    = "admission.repository_revision.unverified"
	ReasonBootstrapDirectionInvalid       = "admission.bootstrap.direction.invalid"
	ReasonTaskKnowledgeDigestMismatch     = "admission.task_knowledge.digest_mismatch"

	VerifyReadOnlyMutation              = "admission.verify.read_only_mutation"
	VerifyPathOutsideEnvelope           = "admission.verify.path_outside_envelope"
	VerifyOperationNotAdmitted          = "admission.verify.operation_not_admitted"
	VerifyUntrackedFile                 = "admission.verify.untracked_file"
	VerifyDeletedFile                   = "admission.verify.deleted_file"
	VerifyRenamedFile                   = "admission.verify.renamed_file"
	VerifyCopiedFile                    = "admission.verify.copied_file"
	VerifyTypeChanged                   = "admission.verify.type_changed"
	VerifyUnmergedFile                  = "admission.verify.unmerged_file"
	VerifyBaseRevisionChanged           = "admission.verify.base_revision_changed"
	VerifySessionAdvanced               = "admission.verify.session_advanced"
	VerifyDecisionDigestInvalid         = "admission.verify.decision_digest_invalid"
	VerifyBootstrapAuthorizationInvalid = "admission.verify.bootstrap_authorization_invalid"
	VerifyBootstrapAuthorizationReused  = "admission.verify.bootstrap_authorization_reused"
	VerifyBootstrapMutationMismatch     = "admission.verify.bootstrap_mutation_mismatch"
	VerifyAwarenessMutation             = "admission.verify.awareness_mutation_failed"

	ChangeModified    = "modified"
	ChangeAdded       = "added"
	ChangeDeleted     = "deleted"
	ChangeRenamed     = "renamed"
	ChangeCopied      = "copied"
	ChangeTypeChanged = "type_changed"
	ChangeUnmerged    = "unmerged"
	ChangeUntracked   = "untracked"
)

type Policy struct {
	ID                              string   `json:"id" yaml:"id"`
	Version                         string   `json:"version" yaml:"version"`
	AllowInspectionWhenClosureOpen  bool     `json:"allow_inspection_when_closure_open" yaml:"allow_inspection_when_closure_open"`
	RequireConditionAcknowledgement bool     `json:"require_condition_acknowledgement" yaml:"require_condition_acknowledgement"`
	AllowConditionalMutation        bool     `json:"allow_conditional_mutation" yaml:"allow_conditional_mutation"`
	SupportedOperations             []string `json:"supported_operations" yaml:"supported_operations"`
	KnownLimitations                []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
}

type ConvergenceBinding struct {
	SessionID                 string `json:"session_id" yaml:"session_id"`
	IterationDigestSHA256     string `json:"iteration_digest_sha256" yaml:"iteration_digest_sha256"`
	SemanticStateDigestSHA256 string `json:"semantic_state_digest_sha256" yaml:"semantic_state_digest_sha256"`
}

type FileOperation struct {
	Path      string `json:"path" yaml:"path"`
	Operation string `json:"operation" yaml:"operation"`
}

type ChangeScope struct {
	Files           []FileOperation `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols         []string        `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components      []string        `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs        []string        `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys []string        `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
}

type Request struct {
	SchemaVersion        string                            `json:"schema_version" yaml:"schema_version"`
	Binding              architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Convergence          ConvergenceBinding                `json:"convergence" yaml:"convergence"`
	Mode                 string                            `json:"mode" yaml:"mode"`
	TaskClass            string                            `json:"task_class" yaml:"task_class"`
	Scope                ChangeScope                       `json:"scope" yaml:"scope"`
	AcceptedConditionIDs []string                          `json:"accepted_condition_ids,omitempty" yaml:"accepted_condition_ids,omitempty"`
	AwarenessMutation    *closure.AwarenessMutationBinding `json:"awareness_mutation,omitempty" yaml:"awareness_mutation,omitempty"`
	RequestedBy          string                            `json:"requested_by,omitempty" yaml:"requested_by,omitempty"`
	Note                 string                            `json:"note,omitempty" yaml:"note,omitempty"`
}

type Bundle struct {
	Root             string
	Session          convergence.Session
	Status           convergence.StatusReport
	LatestIteration  convergence.Iteration
	MaintainedClaims architecture.ClaimDocument
	Maintenance      maintenance.Report
	PlaneAssessment  plane.Report
	ClosureBefore    closure.Report
	Dialogue         architecture.DialogueDocument
	QuestionReport   questiongen.Report
	ClosureAfter     closure.Report
	Probes           probe.ProbeDocument
	ProbeReport      probe.GenerationReport
	ArtifactDigests  map[string]string
	StageBytes       map[string][]byte
}

type Reason struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type RequestReceipt struct {
	DigestSHA256 string      `json:"digest_sha256" yaml:"digest_sha256"`
	Scope        ChangeScope `json:"scope" yaml:"scope"`
	Mode         string      `json:"mode" yaml:"mode"`
	TaskClass    string      `json:"task_class" yaml:"task_class"`
}

type SessionReceipt struct {
	SessionID                 string `json:"session_id" yaml:"session_id"`
	LatestIteration           int    `json:"latest_iteration" yaml:"latest_iteration"`
	IterationDigestSHA256     string `json:"iteration_digest_sha256" yaml:"iteration_digest_sha256"`
	SemanticStateDigestSHA256 string `json:"semantic_state_digest_sha256" yaml:"semantic_state_digest_sha256"`
	Status                    string `json:"status" yaml:"status"`
	ClosureVerdict            string `json:"closure_verdict" yaml:"closure_verdict"`
}

type ChangeEnvelope struct {
	ReadPaths             []string `json:"read_paths,omitempty" yaml:"read_paths,omitempty"`
	ModifyPaths           []string `json:"modify_paths,omitempty" yaml:"modify_paths,omitempty"`
	Symbols               []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components            []string `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs              []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys       []string `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
	UnsupportedOperations []string `json:"unsupported_operations,omitempty" yaml:"unsupported_operations,omitempty"`
}

type GuidanceItem struct {
	ID        string   `json:"id" yaml:"id"`
	Class     string   `json:"class,omitempty" yaml:"class,omitempty"`
	Label     string   `json:"label,omitempty" yaml:"label,omitempty"`
	Status    string   `json:"status,omitempty" yaml:"status,omitempty"`
	Plane     string   `json:"plane,omitempty" yaml:"plane,omitempty"`
	SourceIDs []string `json:"source_ids,omitempty" yaml:"source_ids,omitempty"`
	Paths     []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	Details   []string `json:"details,omitempty" yaml:"details,omitempty"`
}

type ProofReceipt struct {
	ID               string         `json:"id" yaml:"id"`
	EvidenceLane     string         `json:"evidence_lane,omitempty" yaml:"evidence_lane,omitempty"`
	RequiredSlotIDs  []string       `json:"required_slot_ids,omitempty" yaml:"required_slot_ids,omitempty"`
	SlotKinds        []string       `json:"slot_kinds,omitempty" yaml:"slot_kinds,omitempty"`
	AvailableSources []GuidanceItem `json:"available_sources,omitempty" yaml:"available_sources,omitempty"`
}

type Decision struct {
	SchemaVersion            string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy              string                            `json:"generated_by" yaml:"generated_by"`
	AdmissionID              string                            `json:"admission_id" yaml:"admission_id"`
	PolicyID                 string                            `json:"policy_id" yaml:"policy_id"`
	PolicyVersion            string                            `json:"policy_version" yaml:"policy_version"`
	Decision                 string                            `json:"decision" yaml:"decision"`
	RequestedMode            string                            `json:"requested_mode" yaml:"requested_mode"`
	Binding                  architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SessionReceipt           SessionReceipt                    `json:"session_receipt" yaml:"session_receipt"`
	RequestReceipt           RequestReceipt                    `json:"request_receipt" yaml:"request_receipt"`
	InspectionCapability     string                            `json:"inspection_capability" yaml:"inspection_capability"`
	MutationCapability       string                            `json:"mutation_capability" yaml:"mutation_capability"`
	Envelope                 ChangeEnvelope                    `json:"envelope" yaml:"envelope"`
	Authority                []GuidanceItem                    `json:"authority,omitempty" yaml:"authority,omitempty"`
	MustPreserve             []GuidanceItem                    `json:"must_preserve,omitempty" yaml:"must_preserve,omitempty"`
	ForbiddenMoves           []GuidanceItem                    `json:"forbidden_moves,omitempty" yaml:"forbidden_moves,omitempty"`
	RequiredTests            []GuidanceItem                    `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	ProofObligations         []ProofReceipt                    `json:"proof_obligations,omitempty" yaml:"proof_obligations,omitempty"`
	RequiredRuntimeEvidence  []GuidanceItem                    `json:"required_runtime_evidence,omitempty" yaml:"required_runtime_evidence,omitempty"`
	Conditions               []closure.Condition               `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	AwarenessMutationBinding *closure.AwarenessMutationBinding `json:"awareness_mutation_binding,omitempty" yaml:"awareness_mutation_binding,omitempty"`
	AwarenessMutation        *closure.AwarenessMutationReceipt `json:"awareness_mutation,omitempty" yaml:"awareness_mutation,omitempty"`
	NextActions              []convergence.NextAction          `json:"next_actions,omitempty" yaml:"next_actions,omitempty"`
	FilesToRead              []string                          `json:"files_to_read,omitempty" yaml:"files_to_read,omitempty"`
	Reasons                  []Reason                          `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	Limitations              []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	ScopeOnly                bool                              `json:"scope_only" yaml:"scope_only"`
	CorrectnessCertified     bool                              `json:"correctness_certified" yaml:"correctness_certified"`
	DecisionDigestSHA256     string                            `json:"decision_digest_sha256" yaml:"decision_digest_sha256"`
}

type ChangeReceipt struct {
	Path                string `json:"path" yaml:"path"`
	OldPath             string `json:"old_path,omitempty" yaml:"old_path,omitempty"`
	ChangeType          string `json:"change_type" yaml:"change_type"`
	CurrentDigestSHA256 string `json:"current_digest_sha256,omitempty" yaml:"current_digest_sha256,omitempty"`
	CurrentSize         int64  `json:"current_size,omitempty" yaml:"current_size,omitempty"`
}

type Violation struct {
	Code              string `json:"code" yaml:"code"`
	Path              string `json:"path,omitempty" yaml:"path,omitempty"`
	ObservedOperation string `json:"observed_operation,omitempty" yaml:"observed_operation,omitempty"`
	ExpectedOperation string `json:"expected_operation,omitempty" yaml:"expected_operation,omitempty"`
	Detail            string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type AwarenessMutationVerification struct {
	SchemaVersion            string   `json:"schema_version" yaml:"schema_version"`
	PolicyID                 string   `json:"policy_id" yaml:"policy_id"`
	PlanDigestSHA256         string   `json:"plan_digest_sha256" yaml:"plan_digest_sha256"`
	RepositoryRevisionBefore string   `json:"repository_revision_before" yaml:"repository_revision_before"`
	GraphDigestBefore        string   `json:"graph_digest_before" yaml:"graph_digest_before"`
	SourcePaths              []string `json:"source_paths,omitempty" yaml:"source_paths,omitempty"`
	SenseiCheck              string   `json:"sensei_check,omitempty" yaml:"sensei_check,omitempty"`
	SenseiValidate           string   `json:"sensei_validate,omitempty" yaml:"sensei_validate,omitempty"`
	StrictBuild              string   `json:"strict_build,omitempty" yaml:"strict_build,omitempty"`
	CanonicalGraphPurity     string   `json:"canonical_graph_purity,omitempty" yaml:"canonical_graph_purity,omitempty"`
	OwnerResolution          string   `json:"owner_resolution,omitempty" yaml:"owner_resolution,omitempty"`
	AuthorityScopeValidation string   `json:"authority_scope_validation,omitempty" yaml:"authority_scope_validation,omitempty"`
	GeneratedNodeIDs         []string `json:"generated_node_ids,omitempty" yaml:"generated_node_ids,omitempty"`
	RejectedNodeIDs          []string `json:"rejected_node_ids,omitempty" yaml:"rejected_node_ids,omitempty"`
	Limitations              []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type Verification struct {
	SchemaVersion                 string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                   string                            `json:"generated_by" yaml:"generated_by"`
	AdmissionID                   string                            `json:"admission_id" yaml:"admission_id"`
	DecisionDigestSHA256          string                            `json:"decision_digest_sha256" yaml:"decision_digest_sha256"`
	Status                        string                            `json:"status" yaml:"status"`
	Binding                       architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SessionID                     string                            `json:"session_id" yaml:"session_id"`
	IterationDigestSHA256         string                            `json:"iteration_digest_sha256" yaml:"iteration_digest_sha256"`
	PatchDigestSHA256             string                            `json:"patch_digest_sha256" yaml:"patch_digest_sha256"`
	Changes                       []ChangeReceipt                   `json:"changes,omitempty" yaml:"changes,omitempty"`
	Violations                    []Violation                       `json:"violations,omitempty" yaml:"violations,omitempty"`
	PendingConditions             []closure.Condition               `json:"pending_conditions,omitempty" yaml:"pending_conditions,omitempty"`
	PendingTests                  []GuidanceItem                    `json:"pending_tests,omitempty" yaml:"pending_tests,omitempty"`
	PendingProofObligations       []ProofReceipt                    `json:"pending_proof_obligations,omitempty" yaml:"pending_proof_obligations,omitempty"`
	PendingRuntimeEvidence        []GuidanceItem                    `json:"pending_runtime_evidence,omitempty" yaml:"pending_runtime_evidence,omitempty"`
	AwarenessMutation             *closure.AwarenessMutationReceipt `json:"awareness_mutation,omitempty" yaml:"awareness_mutation,omitempty"`
	AwarenessMutationVerification *AwarenessMutationVerification    `json:"awareness_mutation_verification,omitempty" yaml:"awareness_mutation_verification,omitempty"`
	Reasons                       []Reason                          `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	Limitations                   []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	ScopeOnly                     bool                              `json:"scope_only" yaml:"scope_only"`
	CorrectnessCertified          bool                              `json:"correctness_certified" yaml:"correctness_certified"`
	VerificationDigestSHA256      string                            `json:"verification_digest_sha256" yaml:"verification_digest_sha256"`
}

type EvaluateOptions struct {
	BundleDir   string
	RequestPath string
	GraphNT     string
	Repo        string
	PolicyID    string
}

type VerifyOptions struct {
	DecisionPath string
	BundleDir    string
	Repo         string
}

type DirectionBootstrapMutation struct {
	SchemaVersion           string   `json:"schema_version" yaml:"schema_version"`
	TaskID                  string   `json:"task_id" yaml:"task_id"`
	BaseRevision            string   `json:"base_revision" yaml:"base_revision"`
	File                    string   `json:"file" yaml:"file"`
	Operation               string   `json:"operation" yaml:"operation"`
	GovernedRecordIDs       []string `json:"governed_record_ids" yaml:"governed_record_ids"`
	BaseContentDigestSHA256 string   `json:"base_content_digest_sha256" yaml:"base_content_digest_sha256"`
	PostContentDigestSHA256 string   `json:"post_content_digest_sha256" yaml:"post_content_digest_sha256"`
}

type BootstrapDirectionConsumption struct {
	SchemaVersion              string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                string `json:"generated_by" yaml:"generated_by"`
	TaskID                     string `json:"task_id" yaml:"task_id"`
	AdmissionID                string `json:"admission_id" yaml:"admission_id"`
	VerificationDigestSHA256   string `json:"verification_digest_sha256" yaml:"verification_digest_sha256"`
	AuthorizationDigestSHA256  string `json:"authorization_digest_sha256" yaml:"authorization_digest_sha256"`
	ApprovalSourcePath         string `json:"approval_source_path" yaml:"approval_source_path"`
	ApprovalSourceDigestSHA256 string `json:"approval_source_digest_sha256" yaml:"approval_source_digest_sha256"`
	ConsumedAt                 string `json:"consumed_at" yaml:"consumed_at"`
	ReceiptDigestSHA256        string `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
}

type requestEnvelope struct {
	ArchitectureChangeRequest Request `json:"architecture_change_request" yaml:"architecture_change_request"`
}

type decisionEnvelope struct {
	ArchitectureAdmissionDecision Decision `json:"architecture_admission_decision" yaml:"architecture_admission_decision"`
}

type verificationEnvelope struct {
	ArchitectureAdmissionVerification Verification `json:"architecture_admission_verification" yaml:"architecture_admission_verification"`
}

type bootstrapDirectionConsumptionEnvelope struct {
	ArchitectureBootstrapDirectionConsumption BootstrapDirectionConsumption `json:"architecture_bootstrap_direction_consumption" yaml:"architecture_bootstrap_direction_consumption"`
}

func DefaultPolicies() ([]Policy, error) {
	return []Policy{{
		ID:                              PolicyStrictID,
		Version:                         PolicyStrictVersion,
		AllowInspectionWhenClosureOpen:  true,
		RequireConditionAcknowledgement: true,
		AllowConditionalMutation:        true,
		SupportedOperations:             []string{OperationRead, OperationModify},
		KnownLimitations: []string{
			"admission verifies bounded scope only; correctness requires external proof",
			"admission does not support create, delete, rename, copy, chmod, submodule, or generated-tree rewrite operations",
		},
	}}, nil
}

func PolicyByID(id string) (Policy, bool) {
	for _, p := range mustPolicies() {
		if p.ID == strings.TrimSpace(id) {
			return p, true
		}
	}
	return Policy{}, false
}

func mustPolicies() []Policy {
	p, _ := DefaultPolicies()
	return p
}

func LoadRequest(path string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, err
	}
	return UnmarshalRequestYAML(data)
}

func UnmarshalRequestYAML(data []byte) (Request, error) {
	var env requestEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Request{}, err
	}
	if env.ArchitectureChangeRequest.SchemaVersion == "" {
		return Request{}, errors.New("missing architecture_change_request document")
	}
	return NormalizeRequest(env.ArchitectureChangeRequest)
}

func MarshalCanonicalRequestYAML(req Request) ([]byte, error) {
	req, err := NormalizeRequest(req)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(requestEnvelope{ArchitectureChangeRequest: req})
}

func NormalizeRequest(in Request) (Request, error) {
	req := in
	req.SchemaVersion = strings.TrimSpace(req.SchemaVersion)
	req.Binding = normalizeBinding(req.Binding)
	req.Convergence.SessionID = strings.TrimSpace(req.Convergence.SessionID)
	req.Convergence.IterationDigestSHA256 = strings.TrimSpace(req.Convergence.IterationDigestSHA256)
	req.Convergence.SemanticStateDigestSHA256 = strings.TrimSpace(req.Convergence.SemanticStateDigestSHA256)
	req.Mode = strings.TrimSpace(req.Mode)
	req.TaskClass = strings.TrimSpace(req.TaskClass)
	req.RequestedBy = strings.TrimSpace(req.RequestedBy)
	req.Note = strings.TrimSpace(req.Note)
	ops := make([]FileOperation, 0, len(req.Scope.Files))
	seen := map[string]string{}
	for _, f := range req.Scope.Files {
		path := normalizePath(f.Path)
		op := strings.TrimSpace(f.Operation)
		if prev, ok := seen[path]; ok && prev != op {
			return Request{}, fmt.Errorf("path %s has conflicting operations", path)
		}
		seen[path] = op
		ops = append(ops, FileOperation{Path: path, Operation: op})
	}
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Operation < ops[j].Operation
	})
	req.Scope.Files = dedupeFileOps(ops)
	req.Scope.Symbols = clean(req.Scope.Symbols)
	req.Scope.Components = clean(req.Scope.Components)
	req.Scope.ClaimIDs = clean(req.Scope.ClaimIDs)
	req.Scope.PropositionKeys = clean(req.Scope.PropositionKeys)
	req.AcceptedConditionIDs = clean(req.AcceptedConditionIDs)
	if req.AwarenessMutation != nil {
		req.AwarenessMutation.TaskID = strings.TrimSpace(req.AwarenessMutation.TaskID)
		req.AwarenessMutation.Path = filepath.ToSlash(strings.TrimSpace(req.AwarenessMutation.Path))
		req.AwarenessMutation.PlanDigestSHA256 = strings.TrimSpace(req.AwarenessMutation.PlanDigestSHA256)
		req.AwarenessMutation.PolicyID = strings.TrimSpace(req.AwarenessMutation.PolicyID)
	}
	if err := ValidateRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

func ValidateRequest(req Request) error {
	var errs []string
	if req.SchemaVersion != SchemaVersion {
		errs = append(errs, "unsupported schema_version")
	}
	if err := requireResolvedBinding(req.Binding); err != nil {
		errs = append(errs, "binding: "+err.Error())
	}
	if req.Convergence.SessionID == "" {
		errs = append(errs, "convergence session_id is required")
	}
	if !isSHA256(req.Convergence.IterationDigestSHA256) {
		errs = append(errs, "convergence iteration_digest_sha256 must be lowercase SHA-256")
	}
	if !isSHA256(req.Convergence.SemanticStateDigestSHA256) {
		errs = append(errs, "convergence semantic_state_digest_sha256 must be lowercase SHA-256")
	}
	if req.Mode != ModeInspect && req.Mode != ModeModify {
		errs = append(errs, "mode must be inspect or modify")
	}
	if req.TaskClass == "" {
		errs = append(errs, "task_class is required")
	}
	scopeCount := len(req.Scope.Files) + len(req.Scope.Symbols) + len(req.Scope.Components) + len(req.Scope.ClaimIDs) + len(req.Scope.PropositionKeys)
	if scopeCount == 0 {
		errs = append(errs, "scope is required")
	}
	modifies := 0
	for _, f := range req.Scope.Files {
		if f.Path == "" {
			errs = append(errs, "file path is required")
			continue
		}
		if !safeRelPath(f.Path) {
			errs = append(errs, "file path must be repository-relative and non-escaping")
		}
		if f.Operation != OperationRead && f.Operation != OperationModify {
			errs = append(errs, "file operation is unsupported")
		}
		if req.Mode == ModeInspect && f.Operation == OperationModify {
			errs = append(errs, "inspect mode cannot contain modify operations")
		}
		if f.Operation == OperationModify {
			modifies++
		}
	}
	if req.Mode == ModeModify && modifies == 0 {
		errs = append(errs, "modify mode requires at least one modify operation")
	}
	for _, v := range append(append(append(append([]string{}, req.Scope.Symbols...), req.Scope.Components...), req.Scope.ClaimIDs...), req.Scope.PropositionKeys...) {
		if strings.TrimSpace(v) == "" {
			errs = append(errs, "scope entries must not be empty")
			break
		}
	}
	if hasDuplicates(req.AcceptedConditionIDs) {
		errs = append(errs, "accepted_condition_ids must not duplicate")
	}
	if req.AwarenessMutation != nil {
		expected := ".sensei/tasks/" + req.AwarenessMutation.TaskID + "/source/awareness-mutation-enforcement.yaml"
		if req.AwarenessMutation.TaskID == "" {
			errs = append(errs, "awareness_mutation task_id is required")
		}
		if req.AwarenessMutation.Path != expected {
			errs = append(errs, "awareness_mutation path must match .sensei/tasks/<task-id>/source/awareness-mutation-enforcement.yaml exactly")
		}
		if req.AwarenessMutation.PlanDigestSHA256 == "" {
			errs = append(errs, "awareness_mutation plan_digest_sha256 is required")
		}
		if req.AwarenessMutation.PolicyID != closure.AwarenessMutationEnforcementPolicyV1 {
			errs = append(errs, "awareness_mutation policy_id is unknown")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func Evaluate(opts EvaluateOptions) (Decision, error) {
	policyID := strings.TrimSpace(opts.PolicyID)
	if policyID == "" {
		policyID = PolicyStrictID
	}
	policy, ok := PolicyByID(policyID)
	if !ok {
		return Decision{}, fmt.Errorf("unknown admission policy %s", policyID)
	}
	req, err := LoadRequest(opts.RequestPath)
	if err != nil {
		return Decision{}, err
	}
	bundle, err := LoadBundle(opts.BundleDir)
	if err != nil {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonBundleInvalid, Detail: err.Error()}), nil
	}
	graphReceipt, err := graphsnapshot.Verify(opts.GraphNT, req.Binding.GraphDigestSHA256, req.Binding.GraphDigestStatus)
	if err != nil {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonGraphUnverified, Detail: err.Error()}), nil
	}
	if !graphReceipt.Verified {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonGraphUnverified, Detail: joinGraphReasons(graphReceipt.Reasons)}), nil
	}
	repoRev, err := gitHead(opts.Repo)
	if err != nil {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonRepositoryRevisionUnverified, Detail: err.Error()}), nil
	}
	graphIndex, err := closure.LoadGraphIndex(opts.GraphNT)
	if err != nil {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonGraphUnverified, Detail: err.Error()}), nil
	}
	awarenessMutation, err := loadAwarenessMutationReceipt(opts.Repo, req)
	if err != nil {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonBundleInvalid, Detail: err.Error()}), nil
	}
	if bundle.ClosureAfter.AwarenessMutation != nil && awarenessMutation != nil && bundle.ClosureAfter.AwarenessMutation.PlanDigestSHA256 != awarenessMutation.PlanDigestSHA256 {
		return uncertifiableDecision(policy, req, Reason{Code: ReasonTaskKnowledgeDigestMismatch, Detail: "closure and admission awareness mutation plan digests differ"}), nil
	}
	return EvaluateLoaded(policy, req, bundle, graphIndex, awarenessMutation, opts.Repo, repoRev)
}

func EvaluateLoaded(policy Policy, req Request, bundle Bundle, graph closure.GraphIndex, awarenessMutation *closure.AwarenessMutationReceipt, repoRoot, repoRev string) (Decision, error) {
	reasons := []Reason{}
	if !bindingsEqual(req.Binding, bundle.Session.Binding) ||
		!bindingsEqual(req.Binding, bundle.ClosureAfter.ObservedBinding) ||
		!bindingsEqual(req.Binding, bundle.MaintainedClaims.Binding) ||
		!bindingsEqual(req.Binding, bundle.Dialogue.Binding) ||
		!bindingsEqual(req.Binding, bundle.Probes.Binding) {
		reasons = append(reasons, Reason{Code: ReasonBindingMismatch, Detail: "request, session, closure, claims, dialogue, and probes must share one resolved binding"})
	}
	if req.Binding.Revision != repoRev {
		reasons = append(reasons, Reason{Code: ReasonRepositoryRevisionUnverified, Detail: "repository HEAD does not match request binding revision"})
	}
	if req.Convergence.SessionID != bundle.Session.SessionID ||
		req.Convergence.IterationDigestSHA256 != bundle.LatestIteration.IterationDigestSHA256 ||
		req.Convergence.SemanticStateDigestSHA256 != bundle.LatestIteration.SemanticStateDigestSHA256 {
		reasons = append(reasons, Reason{Code: ReasonSessionStaleIteration, Detail: "request convergence binding does not match latest session iteration"})
	}
	if bundle.ClosureAfter.Request.Scope.TaskClass != "" && req.TaskClass != bundle.ClosureAfter.Request.Scope.TaskClass {
		reasons = append(reasons, Reason{Code: ReasonTaskClassMismatch, Detail: "request task class differs from closure request task class"})
	}
	if _, err := closure.DirectionBootstrapForRequest(bundle.ClosureAfter.Request, time.Now().UTC()); err != nil && bundle.ClosureAfter.Request.DirectionBootstrap != nil {
		reasons = append(reasons, Reason{Code: ReasonBootstrapDirectionInvalid, Detail: err.Error()})
	} else if bundle.ClosureAfter.Request.DirectionBootstrap != nil {
		if err := closure.ValidateDirectionBootstrapApproval(*bundle.ClosureAfter.Request.DirectionBootstrap, repoRoot); err != nil {
			reasons = append(reasons, Reason{Code: ReasonBootstrapDirectionInvalid, Detail: err.Error()})
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, scopeContainment(req, bundle, graph, repoRoot)...)
		reasons = append(reasons, accessContainment(req, bundle.ClosureAfter.Request.Scope.AccessMode)...)
	}
	inspection := inspectionCapability(req, reasons)
	mutation := mutationCapability(policy, req, bundle, reasons)
	top := inspection
	if req.Mode == ModeModify {
		top = mutation
	}
	if len(reasons) == 0 && top == DecisionAdmitted {
		reasons = append(reasons, Reason{Code: ReasonClosedScope})
	}
	if len(reasons) == 0 && top == DecisionAdmittedWithConditions {
		reasons = append(reasons, Reason{Code: ReasonConditionalScope})
	}
	decision := buildDecision(policy, req, bundle, graph, awarenessMutation, top, inspection, mutation, reasons)
	return finalizeDecision(decision, bundle, req)
}

func loadAwarenessMutationReceipt(repoRoot string, req Request) (*closure.AwarenessMutationReceipt, error) {
	if req.AwarenessMutation == nil {
		return nil, nil
	}
	doc, err := closure.LoadAwarenessMutationEnforcement(filepath.Join(repoRoot, filepath.FromSlash(req.AwarenessMutation.Path)))
	if err != nil {
		return nil, err
	}
	if doc.PolicyID != closure.AwarenessMutationEnforcementPolicyV1 || req.AwarenessMutation.PolicyID != closure.AwarenessMutationEnforcementPolicyV1 {
		return nil, errors.New("awareness mutation policy is unknown")
	}
	if doc.TaskID != req.AwarenessMutation.TaskID {
		return nil, errors.New("awareness mutation task_id mismatch")
	}
	if doc.RepositoryRevision != req.Binding.Revision {
		return nil, errors.New("awareness mutation revision mismatch")
	}
	if doc.GraphDigestSHA256 != req.Binding.GraphDigestSHA256 {
		return nil, errors.New("awareness mutation graph digest mismatch")
	}
	digest, err := closure.AwarenessMutationEnforcementDigest(doc)
	if err != nil {
		return nil, err
	}
	if digest != req.AwarenessMutation.PlanDigestSHA256 {
		return nil, errors.New("awareness mutation plan digest mismatch")
	}
	plans := make([]closure.AwarenessMutationPlanReceipt, 0, len(doc.Plans))
	for _, plan := range doc.Plans {
		plans = append(plans, closure.AwarenessMutationPlanReceipt{
			SourcePath:           plan.SourcePath,
			SourceClass:          plan.SourceClass,
			ImporterID:           plan.ImporterID,
			RequiredVerification: plan.RequiredVerification,
		})
	}
	return closure.NormalizeAwarenessMutationReceiptForExternal(&closure.AwarenessMutationReceipt{
		Status:           "consumed",
		PolicyID:         closure.AwarenessMutationEnforcementPolicyV1,
		PlanDigestSHA256: digest,
		Plans:            plans,
		Limitations: []string{
			"awareness mutation enforcement proves deterministic validation and graph compilation coverage, not observed behavior or design correctness",
		},
	}), nil
}

func LoadBundle(dir string) (Bundle, error) {
	session, err := convergence.LoadSession(filepath.Join(dir, "session.yaml"))
	if err != nil {
		return Bundle{}, err
	}
	if err := convergence.VerifyBundle(dir, session); err != nil {
		return Bundle{}, err
	}
	status, err := convergence.Status(session)
	if err != nil {
		return Bundle{}, err
	}
	if len(session.Iterations) == 0 {
		return Bundle{}, errors.New("session has no iterations")
	}
	latest := session.Iterations[len(session.Iterations)-1]
	b := Bundle{Root: dir, Session: session, Status: status, LatestIteration: latest, ArtifactDigests: map[string]string{}, StageBytes: map[string][]byte{}}
	for _, r := range latest.StageReceipts {
		if filepath.IsAbs(r.ArtifactPath) {
			return Bundle{}, fmt.Errorf("absolute artifact path %s", r.ArtifactPath)
		}
		base := filepath.Base(r.ArtifactPath)
		rel := "latest/" + base
		data, err := readBundleFile(dir, rel)
		if err != nil {
			return Bundle{}, err
		}
		if digest(data) != r.DigestSHA256 {
			return Bundle{}, fmt.Errorf("latest artifact digest mismatch for %s", rel)
		}
		b.ArtifactDigests[base] = r.DigestSHA256
		b.StageBytes[base] = data
	}
	if err := loadBundleArtifacts(&b); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func VerifyBundle(dir string) error {
	session, err := convergence.LoadSession(filepath.Join(dir, "session.yaml"))
	if err != nil {
		return err
	}
	return convergence.VerifyBundle(dir, session)
}

func loadBundleArtifacts(b *Bundle) error {
	var err error
	if b.MaintainedClaims, err = architecture.UnmarshalClaimDocumentYAML(b.StageBytes["maintained-claims.yaml"]); err != nil {
		return fmt.Errorf("load maintained claims: %w", err)
	}
	if b.Maintenance, err = maintenance.UnmarshalReportYAML(b.StageBytes["maintenance-report.yaml"]); err != nil {
		return fmt.Errorf("load maintenance report: %w", err)
	}
	if b.PlaneAssessment, err = plane.UnmarshalReportYAML(b.StageBytes["plane-assessment.yaml"]); err != nil {
		return fmt.Errorf("load plane report: %w", err)
	}
	if b.ClosureBefore, err = unmarshalClosureReport(b.StageBytes["closure-before-dialogue.yaml"]); err != nil {
		return fmt.Errorf("load closure-before-dialogue: %w", err)
	}
	if b.Dialogue, err = architecture.UnmarshalDialogueDocumentYAML(b.StageBytes["dialogue.yaml"]); err != nil {
		return fmt.Errorf("load dialogue: %w", err)
	}
	if b.QuestionReport, err = unmarshalQuestionReport(b.StageBytes["question-generation.yaml"]); err != nil {
		return fmt.Errorf("load question generation report: %w", err)
	}
	if b.ClosureAfter, err = unmarshalClosureReport(b.StageBytes["closure-after-dialogue.yaml"]); err != nil {
		return fmt.Errorf("load closure-after-dialogue: %w", err)
	}
	if b.Probes, err = probe.UnmarshalDocumentYAML(b.StageBytes["probes.yaml"], nil); err != nil {
		return fmt.Errorf("load probes: %w", err)
	}
	if b.ProbeReport, err = unmarshalProbeReport(b.StageBytes["probe-generation.yaml"]); err != nil {
		return fmt.Errorf("load probe generation report: %w", err)
	}
	return nil
}

func unmarshalClosureReport(data []byte) (closure.Report, error) {
	var env struct {
		ArchitectureClosureAssessment closure.Report `yaml:"architecture_closure_assessment" json:"architecture_closure_assessment"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return closure.Report{}, err
	}
	if env.ArchitectureClosureAssessment.SchemaVersion == "" {
		return closure.Report{}, errors.New("missing architecture_closure_assessment report")
	}
	return env.ArchitectureClosureAssessment, nil
}

func unmarshalQuestionReport(data []byte) (questiongen.Report, error) {
	var env struct {
		ArchitectureQuestionGeneration questiongen.Report `yaml:"architecture_question_generation" json:"architecture_question_generation"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return questiongen.Report{}, err
	}
	if env.ArchitectureQuestionGeneration.SchemaVersion == "" {
		return questiongen.Report{}, errors.New("missing architecture_question_generation report")
	}
	return env.ArchitectureQuestionGeneration, nil
}

func unmarshalProbeReport(data []byte) (probe.GenerationReport, error) {
	var env struct {
		ArchitectureProbeGeneration probe.GenerationReport `yaml:"architecture_probe_generation" json:"architecture_probe_generation"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return probe.GenerationReport{}, err
	}
	if env.ArchitectureProbeGeneration.SchemaVersion == "" {
		return probe.GenerationReport{}, errors.New("missing architecture_probe_generation report")
	}
	return env.ArchitectureProbeGeneration, nil
}

func scopeContainment(req Request, b Bundle, graph closure.GraphIndex, repoRoot string) []Reason {
	var reasons []Reason
	scope := b.ClosureAfter.ScopeReceipt
	missing := set(scope.MissingFiles)
	files := set(scope.Files)
	for _, f := range req.Scope.Files {
		if !files[f.Path] {
			reasons = append(reasons, Reason{Code: ReasonScopeOutsideClosedScope, Detail: f.Path})
			continue
		}
		if missing[f.Path] {
			reasons = append(reasons, Reason{Code: ReasonScopeMissingFile, Detail: f.Path})
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(f.Path))); err != nil {
			reasons = append(reasons, Reason{Code: ReasonScopeMissingFile, Detail: f.Path})
			continue
		}
		if graph.FilesByPath[f.Path] == "" && !fileRepresentedByClaim(f.Path, b.MaintainedClaims.Claims) && !fileRepresentedByClosureNode(f.Path, b.ClosureAfter, graph, repoRoot) {
			reasons = append(reasons, Reason{Code: ReasonScopeUnrepresented, Detail: f.Path})
		}
	}
	symbols := set(scope.Symbols)
	for _, s := range req.Scope.Symbols {
		if !symbols[s] || graph.SymbolsByID[s] == "" {
			reasons = append(reasons, Reason{Code: ReasonScopeOutsideClosedScope, Detail: s})
		}
	}
	components := set(scope.Components)
	for _, c := range req.Scope.Components {
		if !components[c] || !graphHasClass(graph, c, "Component") {
			reasons = append(reasons, Reason{Code: ReasonScopeOutsideClosedScope, Detail: c})
		}
	}
	claims := set(scope.ClaimIDs)
	docClaims := map[string]bool{}
	for _, c := range b.MaintainedClaims.Claims {
		docClaims[c.ID] = true
	}
	for _, id := range req.Scope.ClaimIDs {
		if !claims[id] || !docClaims[id] {
			reasons = append(reasons, Reason{Code: ReasonScopeOutsideClosedScope, Detail: id})
		}
	}
	props := set(scope.PropositionKeys)
	for _, key := range req.Scope.PropositionKeys {
		if !props[key] {
			reasons = append(reasons, Reason{Code: ReasonScopeOutsideClosedScope, Detail: key})
		}
	}
	if req.Mode == ModeModify && len(modifyPaths(req.Scope.Files)) == 0 {
		reasons = append(reasons, Reason{Code: ReasonScopeNoExactModifyPath})
	}
	if req.Mode == ModeModify && len(req.Scope.Files) == 0 {
		reasons = append(reasons, Reason{Code: ReasonScopeNoExactModifyPath, Detail: "symbol-only mutation is not admitted"})
	}
	return reasons
}

func accessContainment(req Request, access string) []Reason {
	switch req.Mode {
	case ModeInspect:
		if access == closure.AccessRead || access == closure.AccessReadWrite {
			return nil
		}
	case ModeModify:
		if access == closure.AccessWrite || access == closure.AccessReadWrite {
			return nil
		}
	}
	return []Reason{{Code: ReasonAccessExceedsClosedScope, Detail: "closure access is " + strings.TrimSpace(access)}}
}

func inspectionCapability(req Request, reasons []Reason) string {
	if hasUncertifiableReason(reasons) {
		return CapabilityUncertifiable
	}
	for _, r := range reasons {
		if r.Code == ReasonAccessExceedsClosedScope || r.Code == ReasonScopeOutsideClosedScope || r.Code == ReasonScopeMissingFile || r.Code == ReasonScopeUnrepresented || r.Code == ReasonTaskClassMismatch || r.Code == ReasonOperationUnsupported {
			return CapabilityRefused
		}
	}
	if req.Mode == ModeInspect {
		for _, f := range req.Scope.Files {
			if f.Operation != OperationRead {
				return CapabilityRefused
			}
		}
	}
	return CapabilityAdmitted
}

func mutationCapability(policy Policy, req Request, b Bundle, reasons []Reason) string {
	if hasUncertifiableReason(reasons) {
		return CapabilityUncertifiable
	}
	for _, r := range reasons {
		switch r.Code {
		case ReasonScopeOutsideClosedScope, ReasonScopeMissingFile, ReasonScopeUnrepresented, ReasonAccessExceedsClosedScope, ReasonOperationUnsupported, ReasonTaskClassMismatch, ReasonScopeNoExactModifyPath:
			return CapabilityRefused
		}
	}
	status := b.LatestIteration.Status
	verdict := b.ClosureAfter.Verdict
	switch {
	case status == convergence.StatusClosed && verdict == closure.VerdictClosed:
		return CapabilityAdmitted
	case status == convergence.StatusConditionallyClosed && verdict == closure.VerdictConditionallyClosed:
		if !policy.AllowConditionalMutation {
			return CapabilityRefused
		}
		current := conditionIDs(b.ClosureAfter.Conditions)
		accepted := set(req.AcceptedConditionIDs)
		for id := range accepted {
			if !current[id] {
				return CapabilityRefused
			}
		}
		for id := range current {
			if !accepted[id] {
				return CapabilityWaiting
			}
		}
		return CapabilityAdmittedWithConditions
	case status == convergence.StatusWaiting && verdict == closure.VerdictOpen:
		return CapabilityWaiting
	case status == convergence.StatusStalled && verdict == closure.VerdictOpen:
		return CapabilityRefused
	case status == convergence.StatusOscillating && verdict == closure.VerdictOpen:
		return CapabilityRefused
	case status == convergence.StatusBudgetExhausted && verdict == closure.VerdictOpen:
		return CapabilityRefused
	case status == convergence.StatusUncertifiable && verdict == closure.VerdictUncertifiable:
		return CapabilityUncertifiable
	default:
		return CapabilityUncertifiable
	}
}

func buildDecision(policy Policy, req Request, b Bundle, graph closure.GraphIndex, awarenessMutation *closure.AwarenessMutationReceipt, top, inspection, mutation string, reasons []Reason) Decision {
	reasons = append(reasons, conditionReasons(req, b, mutation)...)
	reasons = append(reasons, sessionReasons(b, mutation)...)
	next := append([]convergence.NextAction{}, b.LatestIteration.NextActions...)
	envelope := envelopeFromRequest(req)
	d := Decision{
		SchemaVersion:            SchemaVersion,
		GeneratedBy:              GeneratedBy,
		PolicyID:                 policy.ID,
		PolicyVersion:            policy.Version,
		Decision:                 top,
		RequestedMode:            req.Mode,
		Binding:                  req.Binding,
		SessionReceipt:           sessionReceipt(b),
		RequestReceipt:           requestReceipt(req),
		InspectionCapability:     inspection,
		MutationCapability:       mutation,
		Envelope:                 envelope,
		Authority:                projectAuthority(b, graph),
		MustPreserve:             projectMustPreserve(b, graph),
		ForbiddenMoves:           projectForbiddenMoves(b, graph),
		RequiredTests:            projectRequiredTests(b, graph),
		ProofObligations:         projectProof(b, graph),
		RequiredRuntimeEvidence:  projectRuntimeEvidence(b, graph),
		Conditions:               normalizeConditions(b.ClosureAfter.Conditions),
		AwarenessMutationBinding: req.AwarenessMutation,
		AwarenessMutation:        closure.NormalizeAwarenessMutationReceiptForExternal(awarenessMutation),
		NextActions:              normalizeNextActions(next),
		FilesToRead:              filesToRead(req, b, graph),
		Reasons:                  normalizeReasons(reasons),
		Limitations:              collectLimitations(policy, b),
		ScopeOnly:                true,
		CorrectnessCertified:     false,
	}
	return d
}

func finalizeDecision(d Decision, b Bundle, req Request) (Decision, error) {
	closureDigest := digest(b.StageBytes["closure-after-dialogue.yaml"])
	requestDigest := d.RequestReceipt.DigestSHA256
	sessionToken := shortToken(b.Session.SessionID)
	idInput := canonicalJSON(struct {
		PolicyID        string
		PolicyVersion   string
		SessionID       string
		IterationDigest string
		SemanticDigest  string
		ClosureDigest   string
		RequestDigest   string
		Revision        string
		GraphDigest     string
	}{d.PolicyID, d.PolicyVersion, b.Session.SessionID, b.LatestIteration.IterationDigestSHA256, b.LatestIteration.SemanticStateDigestSHA256, closureDigest, requestDigest, req.Binding.Revision, req.Binding.GraphDigestSHA256})
	d.AdmissionID = "admission." + sessionToken + "." + digest(idInput)[:12]
	d.DecisionDigestSHA256 = decisionDigest(d)
	return d, nil
}

func LoadDecision(path string) (Decision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Decision{}, err
	}
	return UnmarshalDecisionYAML(data)
}

func UnmarshalDecisionYAML(data []byte) (Decision, error) {
	var env decisionEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Decision{}, err
	}
	if env.ArchitectureAdmissionDecision.SchemaVersion == "" {
		return Decision{}, errors.New("missing architecture_admission_decision document")
	}
	d := normalizeDecision(env.ArchitectureAdmissionDecision)
	if decisionDigest(d) != d.DecisionDigestSHA256 {
		return Decision{}, errors.New("decision digest invalid")
	}
	return d, nil
}

func MarshalCanonicalDecisionYAML(d Decision) ([]byte, error) {
	d = normalizeDecision(d)
	d.DecisionDigestSHA256 = decisionDigest(d)
	return yaml.Marshal(decisionEnvelope{ArchitectureAdmissionDecision: d})
}

func MarshalCanonicalDecisionJSON(d Decision) ([]byte, error) {
	d = normalizeDecision(d)
	d.DecisionDigestSHA256 = decisionDigest(d)
	return json.MarshalIndent(decisionEnvelope{ArchitectureAdmissionDecision: d}, "", "  ")
}

func Verify(opts VerifyOptions) (Verification, error) {
	decisionBytes, err := os.ReadFile(opts.DecisionPath)
	if err != nil {
		return Verification{}, err
	}
	var env decisionEnvelope
	if err := yaml.Unmarshal(decisionBytes, &env); err != nil {
		return Verification{}, err
	}
	d := normalizeDecision(env.ArchitectureAdmissionDecision)
	if d.SchemaVersion == "" {
		return Verification{}, errors.New("missing architecture_admission_decision document")
	}
	if decisionDigest(d) != d.DecisionDigestSHA256 {
		return finalizeVerification(verificationFromDecision(d, VerificationUncertifiable, []Reason{{Code: VerifyDecisionDigestInvalid}}, nil, nil), d), nil
	}
	b, err := LoadBundle(opts.BundleDir)
	if err != nil {
		return finalizeVerification(verificationFromDecision(d, VerificationUncertifiable, []Reason{{Code: ReasonBundleInvalid, Detail: err.Error()}}, nil, nil), d), nil
	}
	status := VerificationScopeCompliant
	var reasons []Reason
	var violations []Violation
	if d.SessionReceipt.SessionID != b.Session.SessionID || d.SessionReceipt.IterationDigestSHA256 != b.LatestIteration.IterationDigestSHA256 {
		status = VerificationStale
		violations = append(violations, Violation{Code: VerifySessionAdvanced, Detail: "convergence bundle latest iteration changed"})
		reasons = append(reasons, Reason{Code: VerifySessionAdvanced})
	}
	head, err := gitHead(opts.Repo)
	if err != nil {
		return finalizeVerification(verificationFromDecision(d, VerificationUncertifiable, []Reason{{Code: ReasonRepositoryRevisionUnverified, Detail: err.Error()}}, nil, nil), d), nil
	}
	if head != d.Binding.Revision {
		status = VerificationStale
		violations = append(violations, Violation{Code: VerifyBaseRevisionChanged, Detail: "working tree HEAD differs from admission base revision"})
		reasons = append(reasons, Reason{Code: VerifyBaseRevisionChanged})
	}
	changes, patchDigest, err := CaptureChanges(opts.Repo, d.Binding.Revision)
	if err != nil {
		return finalizeVerification(verificationFromDecision(d, VerificationUncertifiable, []Reason{{Code: "admission.verify.git_unavailable", Detail: err.Error()}}, nil, nil), d), nil
	}
	if status == VerificationScopeCompliant {
		violations = envelopeViolations(d, changes)
		violations = append(violations, bootstrapMutationViolations(opts.Repo, b)...)
		if len(violations) > 0 {
			status = VerificationScopeViolated
			for _, v := range violations {
				reasons = append(reasons, Reason{Code: v.Code, Detail: v.Path})
			}
		}
	}
	v := verificationFromDecision(d, status, reasons, changes, violations)
	if status == VerificationScopeCompliant && d.AwarenessMutationBinding != nil {
		receipt, vr, rr := verifyAwarenessMutation(opts.Repo, d)
		v.AwarenessMutationVerification = receipt
		if len(vr) > 0 {
			status = VerificationScopeViolated
			v.Violations = append(v.Violations, vr...)
			v.Reasons = append(v.Reasons, rr...)
			v.Status = status
		}
	}
	v.PatchDigestSHA256 = patchDigest
	v = finalizeVerification(v, d)
	if v.Status == VerificationScopeCompliant {
		if err := recordBootstrapDirectionConsumption(opts.Repo, filepath.Dir(filepath.Clean(opts.BundleDir)), d, b.ClosureAfter.Request.DirectionBootstrap, v); err != nil {
			v.Status = VerificationScopeViolated
			v.Violations = append(v.Violations, Violation{Code: VerifyBootstrapAuthorizationReused, Path: closure.DirectionBootstrapFile, Detail: err.Error()})
			v.Reasons = append(v.Reasons, Reason{Code: VerifyBootstrapAuthorizationReused, Detail: closure.DirectionBootstrapFile})
			v = finalizeVerification(v, d)
		}
	}
	return v, nil
}

func verifyAwarenessMutation(repo string, d Decision) (*AwarenessMutationVerification, []Violation, []Reason) {
	if d.AwarenessMutationBinding == nil || d.AwarenessMutation == nil {
		return nil, nil, nil
	}
	doc, err := closure.LoadAwarenessMutationEnforcement(filepath.Join(repo, filepath.FromSlash(d.AwarenessMutationBinding.Path)))
	if err != nil {
		return nil, []Violation{{Code: VerifyAwarenessMutation, Detail: err.Error()}}, []Reason{{Code: VerifyAwarenessMutation, Detail: err.Error()}}
	}
	planDigest, err := closure.AwarenessMutationEnforcementDigest(doc)
	if err != nil {
		return nil, []Violation{{Code: VerifyAwarenessMutation, Detail: err.Error()}}, []Reason{{Code: VerifyAwarenessMutation, Detail: err.Error()}}
	}
	receipt := &AwarenessMutationVerification{
		SchemaVersion:            SchemaVersion,
		PolicyID:                 closure.AwarenessMutationEnforcementPolicyV1,
		PlanDigestSHA256:         planDigest,
		RepositoryRevisionBefore: d.Binding.Revision,
		GraphDigestBefore:        d.Binding.GraphDigestSHA256,
		SenseiCheck:              "passed",
		SenseiValidate:           "passed",
		StrictBuild:              "passed",
		CanonicalGraphPurity:     "passed",
		OwnerResolution:          "passed",
		AuthorityScopeValidation: "passed",
		Limitations: []string{
			"this verification proves schema, reference, graph, and policy enforcement coverage",
		},
	}
	for _, plan := range doc.Plans {
		receipt.SourcePaths = append(receipt.SourcePaths, plan.SourcePath)
	}
	sort.Strings(receipt.SourcePaths)
	if planDigest != d.AwarenessMutationBinding.PlanDigestSHA256 {
		return receipt, []Violation{{Code: VerifyAwarenessMutation, Detail: "awareness mutation plan digest mismatch"}}, []Reason{{Code: VerifyAwarenessMutation, Detail: "awareness mutation plan digest mismatch"}}
	}
	return receipt, nil, nil
}

func DirectionBootstrapMutationDigest(repo, baseRevision, path string, recordIDs []string, postContent []byte) (string, error) {
	baseDigest, err := gitBlobDigest(repo, baseRevision, path)
	if err != nil {
		return "", err
	}
	m := DirectionBootstrapMutation{
		SchemaVersion:           "1",
		TaskID:                  "",
		BaseRevision:            strings.TrimSpace(baseRevision),
		File:                    normalizePath(path),
		Operation:               OperationModify,
		GovernedRecordIDs:       clean(recordIDs),
		BaseContentDigestSHA256: baseDigest,
		PostContentDigestSHA256: digest(postContent),
	}
	return digest(canonicalJSON(m)), nil
}

func DirectionBootstrapMutationDigestFromFile(repo, baseRevision, path, proposedPath string, recordIDs []string) (string, error) {
	data, err := os.ReadFile(proposedPath)
	if err != nil {
		return "", err
	}
	return DirectionBootstrapMutationDigest(repo, baseRevision, path, recordIDs, data)
}

func bootstrapMutationViolations(repo string, b Bundle) []Violation {
	auth, err := closure.DirectionBootstrapForRequest(b.ClosureAfter.Request, time.Now().UTC())
	if err != nil {
		return []Violation{{
			Code:   VerifyBootstrapAuthorizationInvalid,
			Path:   closure.DirectionBootstrapFile,
			Detail: err.Error(),
		}}
	}
	if auth == nil {
		return nil
	}
	if err := closure.ValidateDirectionBootstrapApproval(*auth, repo); err != nil {
		return []Violation{{
			Code:   VerifyBootstrapAuthorizationInvalid,
			Path:   auth.File,
			Detail: err.Error(),
		}}
	}
	path := filepath.Join(repo, filepath.FromSlash(auth.File))
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return []Violation{{
			Code:   VerifyBootstrapMutationMismatch,
			Path:   auth.File,
			Detail: "authorized bootstrap file is unreadable in the working tree",
		}}
	}
	got, digestErr := DirectionBootstrapMutationDigest(repo, auth.BaseRevision, auth.File, auth.GovernedRecordIDs, data)
	if digestErr != nil {
		return []Violation{{
			Code:   VerifyBootstrapMutationMismatch,
			Path:   auth.File,
			Detail: digestErr.Error(),
		}}
	}
	if auth.ExpectedMutationDigestSHA256 == got {
		return nil
	}
	return []Violation{{
		Code:   VerifyBootstrapMutationMismatch,
		Path:   auth.File,
		Detail: "canonical mutation digest does not match bootstrap direction authorization",
	}}
}

func LoadBootstrapDirectionConsumption(path string) (BootstrapDirectionConsumption, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BootstrapDirectionConsumption{}, err
	}
	var env bootstrapDirectionConsumptionEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return BootstrapDirectionConsumption{}, err
	}
	receipt := normalizeBootstrapDirectionConsumption(env.ArchitectureBootstrapDirectionConsumption)
	if receipt.SchemaVersion == "" {
		return BootstrapDirectionConsumption{}, errors.New("missing architecture_bootstrap_direction_consumption document")
	}
	if bootstrapDirectionConsumptionDigest(receipt) != receipt.ReceiptDigestSHA256 {
		return BootstrapDirectionConsumption{}, errors.New("bootstrap direction consumption receipt digest invalid")
	}
	return receipt, nil
}

func MarshalCanonicalBootstrapDirectionConsumptionYAML(receipt BootstrapDirectionConsumption) ([]byte, error) {
	receipt = normalizeBootstrapDirectionConsumption(receipt)
	receipt.ReceiptDigestSHA256 = bootstrapDirectionConsumptionDigest(receipt)
	return yaml.Marshal(bootstrapDirectionConsumptionEnvelope{ArchitectureBootstrapDirectionConsumption: receipt})
}

func gitBlobDigest(repo, revision, path string) (string, error) {
	spec := strings.TrimSpace(revision) + ":" + normalizePath(path)
	data, err := git(repo, "show", spec)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func CaptureChanges(repo, baseRevision string) ([]ChangeReceipt, string, error) {
	if _, err := git(repo, "rev-parse", "HEAD"); err != nil {
		return nil, "", err
	}
	diffBytes, err := git(repo, "diff", "--no-ext-diff", "--binary", baseRevision)
	if err != nil {
		return nil, "", err
	}
	nameStatus, err := git(repo, "diff", "--name-status", "--find-renames", baseRevision)
	if err != nil {
		return nil, "", err
	}
	statusOut, err := git(repo, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, "", err
	}
	changes := parseNameStatus(repo, string(nameStatus))
	changes = append(changes, parsePorcelainUntracked(repo, string(statusOut))...)
	changes = dedupeChanges(changes)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].ChangeType < changes[j].ChangeType
	})
	return changes, digest(diffBytes), nil
}

func MarshalCanonicalVerificationYAML(v Verification) ([]byte, error) {
	v = normalizeVerification(v)
	v.VerificationDigestSHA256 = verificationDigest(v)
	return yaml.Marshal(verificationEnvelope{ArchitectureAdmissionVerification: v})
}

func MarshalCanonicalVerificationJSON(v Verification) ([]byte, error) {
	v = normalizeVerification(v)
	v.VerificationDigestSHA256 = verificationDigest(v)
	return json.MarshalIndent(verificationEnvelope{ArchitectureAdmissionVerification: v}, "", "  ")
}

func LoadVerification(path string) (Verification, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Verification{}, err
	}
	var env verificationEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Verification{}, err
	}
	v := normalizeVerification(env.ArchitectureAdmissionVerification)
	if v.SchemaVersion == "" {
		return Verification{}, errors.New("missing architecture_admission_verification document")
	}
	if verificationDigest(v) != v.VerificationDigestSHA256 {
		return Verification{}, errors.New("verification digest invalid")
	}
	return v, nil
}

func RenderText(d Decision, detail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Admission: %s\n", d.Decision)
	fmt.Fprintf(&b, "Safe to inspect: %s\n", yesNo(d.InspectionCapability == CapabilityAdmitted))
	fmt.Fprintf(&b, "Safe to modify: %s\n\n", yesNo(d.MutationCapability == CapabilityAdmitted || d.MutationCapability == CapabilityAdmittedWithConditions))
	if d.Decision == DecisionAdmitted || d.Decision == DecisionAdmittedWithConditions {
		fmt.Fprintf(&b, "Envelope:\n")
		if len(d.Envelope.ModifyPaths) > 0 {
			fmt.Fprintf(&b, "  modify:\n")
			for _, p := range cappedStrings(d.Envelope.ModifyPaths, 8) {
				fmt.Fprintf(&b, "    - %s\n", p)
			}
		}
		if len(d.MustPreserve) > 0 {
			fmt.Fprintf(&b, "\nMust preserve:\n")
			for _, item := range capItems(d.MustPreserve, capFor(detail, 8)) {
				fmt.Fprintf(&b, "  - %s\n", item.ID)
			}
		}
		if len(d.RequiredTests) > 0 || len(d.ProofObligations) > 0 {
			fmt.Fprintf(&b, "\nRequired proof after change:\n")
			for _, item := range capItems(d.RequiredTests, capFor(detail, 8)) {
				fmt.Fprintf(&b, "  - %s\n", item.ID)
			}
			for _, item := range capProof(d.ProofObligations, capFor(detail, 8)) {
				fmt.Fprintf(&b, "  - %s\n", item.ID)
			}
		}
		return strings.TrimRight(b.String(), "\n") + "\n"
	}
	fmt.Fprintf(&b, "Scope:\n  read: %d files\n  modify: %d files\n", len(d.Envelope.ReadPaths), len(d.Envelope.ModifyPaths))
	if len(d.Reasons) > 0 {
		fmt.Fprintf(&b, "\nBlocking:\n")
		for _, r := range capReasons(d.Reasons, capFor(detail, 8)) {
			if r.Detail != "" {
				fmt.Fprintf(&b, "  %s: %s\n", r.Code, r.Detail)
			} else {
				fmt.Fprintf(&b, "  %s\n", r.Code)
			}
		}
	}
	waits := waitingClasses(d.Reasons)
	if len(waits) > 0 {
		fmt.Fprintf(&b, "\nWaiting on:\n  %s\n", strings.Join(waits, ", "))
	}
	if len(d.NextActions) > 0 {
		fmt.Fprintf(&b, "\nNext:\n")
		for _, n := range capNext(d.NextActions, capFor(detail, 8)) {
			fmt.Fprintf(&b, "  %s %s\n", n.Class, n.Reference)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func RenderVerificationText(v Verification) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Scope verification: %s\n", v.Status)
	fmt.Fprintf(&b, "Correctness certified: no\n")
	if len(v.Changes) > 0 {
		fmt.Fprintf(&b, "Changes: %d\n", len(v.Changes))
	}
	if len(v.Violations) > 0 {
		fmt.Fprintf(&b, "Violations:\n")
		for _, violation := range v.Violations {
			fmt.Fprintf(&b, "  %s %s\n", violation.Code, violation.Path)
		}
	}
	return b.String()
}

func StatusText(d Decision, v *Verification) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Admission: %s\n", d.Decision)
	fmt.Fprintf(&b, "Inspection: %s\n", d.InspectionCapability)
	fmt.Fprintf(&b, "Mutation: %s\n", d.MutationCapability)
	if v != nil {
		fmt.Fprintf(&b, "Scope verification: %s\n", v.Status)
	}
	fmt.Fprintf(&b, "Conditions: %d\n", len(d.Conditions))
	fmt.Fprintf(&b, "Required tests: %d\n", len(d.RequiredTests))
	fmt.Fprintf(&b, "Proof obligations: %d\n", len(d.ProofObligations))
	fmt.Fprintf(&b, "Correctness certified: no\n")
	return b.String()
}

func protectedOutputPath(path string) bool {
	rel := filepath.ToSlash(filepath.Clean(path))
	for _, root := range []string{"docs/awareness", "docs/intent"} {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			for _, part := range strings.Split(rel, "/") {
				if part == "candidates" {
					return false
				}
			}
			return true
		}
	}
	return false
}

func WriteCanonicalDecision(path string, d Decision) error {
	if protectedOutputPath(path) {
		return errors.New("output under docs/awareness or docs/intent must be inside a candidates directory")
	}
	data, err := MarshalCanonicalDecisionYAML(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func WriteCanonicalVerification(path string, v Verification) error {
	if protectedOutputPath(path) {
		return errors.New("output under docs/awareness or docs/intent must be inside a candidates directory")
	}
	data, err := MarshalCanonicalVerificationYAML(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func CanonicalDecisionBytes(d Decision) ([]byte, error) { return MarshalCanonicalDecisionYAML(d) }
func CanonicalVerificationBytes(v Verification) ([]byte, error) {
	return MarshalCanonicalVerificationYAML(v)
}

func readBundleFile(root, rel string) ([]byte, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(rel)), "../") {
		return nil, fmt.Errorf("bundle path must be relative: %s", rel)
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("bundle artifact must not be a symlink: %s", rel)
	}
	return os.ReadFile(path)
}

func uncertifiableDecision(policy Policy, req Request, reason Reason) Decision {
	d := Decision{
		SchemaVersion:        SchemaVersion,
		GeneratedBy:          GeneratedBy,
		PolicyID:             policy.ID,
		PolicyVersion:        policy.Version,
		Decision:             DecisionUncertifiable,
		RequestedMode:        req.Mode,
		Binding:              req.Binding,
		RequestReceipt:       requestReceipt(req),
		InspectionCapability: CapabilityUncertifiable,
		MutationCapability:   CapabilityUncertifiable,
		Envelope:             envelopeFromRequest(req),
		Reasons:              []Reason{reason},
		ScopeOnly:            true,
		CorrectnessCertified: false,
	}
	d.AdmissionID = "admission.unverified." + digest(canonicalJSON(req))[:12]
	d.DecisionDigestSHA256 = decisionDigest(d)
	return d
}

func requestReceipt(req Request) RequestReceipt {
	return RequestReceipt{DigestSHA256: requestIdentityDigest(req), Scope: req.Scope, Mode: req.Mode, TaskClass: req.TaskClass}
}

func requestIdentityDigest(req Request) string {
	req.RequestedBy = ""
	req.Note = ""
	data, _ := MarshalCanonicalRequestYAML(req)
	return digest(data)
}

func sessionReceipt(b Bundle) SessionReceipt {
	return SessionReceipt{SessionID: b.Session.SessionID, LatestIteration: b.LatestIteration.Index, IterationDigestSHA256: b.LatestIteration.IterationDigestSHA256, SemanticStateDigestSHA256: b.LatestIteration.SemanticStateDigestSHA256, Status: b.LatestIteration.Status, ClosureVerdict: b.ClosureAfter.Verdict}
}

func envelopeFromRequest(req Request) ChangeEnvelope {
	var e ChangeEnvelope
	for _, f := range req.Scope.Files {
		switch f.Operation {
		case OperationRead:
			e.ReadPaths = append(e.ReadPaths, f.Path)
		case OperationModify:
			e.ModifyPaths = append(e.ModifyPaths, f.Path)
		default:
			e.UnsupportedOperations = append(e.UnsupportedOperations, f.Operation)
		}
	}
	e.ReadPaths = clean(e.ReadPaths)
	e.ModifyPaths = clean(e.ModifyPaths)
	e.UnsupportedOperations = clean(e.UnsupportedOperations)
	e.Symbols = clean(req.Scope.Symbols)
	e.Components = clean(req.Scope.Components)
	e.ClaimIDs = clean(req.Scope.ClaimIDs)
	e.PropositionKeys = clean(req.Scope.PropositionKeys)
	return e
}

func projectAuthority(b Bundle, graph closure.GraphIndex) []GuidanceItem {
	var out []GuidanceItem
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		if hasAnyClass(n, "AuthorityDomain") || len(n.OwnerServices)+len(n.OwnsStates)+len(n.MayRead)+len(n.MayWrite)+len(n.MustMutateVia)+len(n.MustReadVia) > 0 {
			out = append(out, itemFromNode(n, "authority"))
		}
	}
	return sortItems(out)
}

func projectMustPreserve(b Bundle, graph closure.GraphIndex) []GuidanceItem {
	var out []GuidanceItem
	for _, c := range b.MaintainedClaims.Claims {
		if c.EpistemicStatus == architecture.StatusSupported && (c.ArchitecturalPlane == architecture.PlaneIntended || c.ArchitecturalPlane == architecture.PlaneEnforced) {
			out = append(out, GuidanceItem{ID: c.ID, Class: "ArchitectureClaim", Label: claimStatementText(c.Statement), Status: c.EpistemicStatus, Plane: c.ArchitecturalPlane, SourceIDs: clean(c.PremiseFacts)})
		}
	}
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		if hasAnyClass(n, "Invariant", "Contract", "Decision", "Intent") && n.Status != "historical" && n.Status != "superseded" {
			out = append(out, itemFromNode(n, "must_preserve"))
		}
	}
	return sortItems(out)
}

func projectForbiddenMoves(b Bundle, graph closure.GraphIndex) []GuidanceItem {
	var out []GuidanceItem
	for _, blocker := range b.ClosureAfter.Blockers {
		out = append(out, GuidanceItem{ID: blocker.ID, Class: "ClosureBlocker", Label: blocker.Summary, Status: blocker.Severity, SourceIDs: clean(append(append([]string{}, blocker.ClaimIDs...), blocker.NodeIDs...)), Details: []string{blocker.Code, blocker.RequiredNextAction}})
	}
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		if hasAnyClass(n, "ForbiddenFix") || len(n.ForbidsBypass)+len(n.Forbids) > 0 {
			out = append(out, itemFromNode(n, "forbidden"))
		}
	}
	return sortItems(out)
}

func projectRequiredTests(b Bundle, graph closure.GraphIndex) []GuidanceItem {
	seen := map[string]bool{}
	var out []GuidanceItem
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		for _, id := range n.RequiresTests {
			if !seen[id] {
				seen[id] = true
				out = append(out, GuidanceItem{ID: id, Class: "Test", SourceIDs: []string{n.ID}})
			}
		}
		if hasAnyClass(n, "Test") && !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, itemFromNode(n, "test"))
		}
	}
	return sortItems(out)
}

func projectProof(b Bundle, graph closure.GraphIndex) []ProofReceipt {
	seen := map[string]bool{}
	var out []ProofReceipt
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok || !hasAnyClass(n, "ProofObligation", "ProofSlot") {
			continue
		}
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		out = append(out, ProofReceipt{ID: n.ID, EvidenceLane: n.Kind, RequiredSlotIDs: clean(n.DependsOn), SlotKinds: clean(n.TruthLayers)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func projectRuntimeEvidence(b Bundle, graph closure.GraphIndex) []GuidanceItem {
	var out []GuidanceItem
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if ok && hasAnyClass(n, "RuntimeEvidence", "Evidence") {
			out = append(out, itemFromNode(n, "runtime_evidence"))
		}
	}
	for _, p := range b.Probes.Probes {
		if p.EvidenceLane != "" {
			out = append(out, GuidanceItem{ID: p.ID, Class: "EvidenceProbe", Label: p.QuestionID, Status: p.Status, Details: clean([]string{p.EvidenceLane, p.SafetyClass, p.ApprovalGate})})
		}
	}
	return sortItems(out)
}

func filesToRead(req Request, b Bundle, graph closure.GraphIndex) []string {
	out := append([]string{}, req.Scope.Symbols...)
	for _, f := range req.Scope.Files {
		if f.Operation == OperationRead {
			out = append(out, f.Path)
		}
	}
	for _, nr := range b.ClosureAfter.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		out = append(out, n.SourcePath)
		out = append(out, n.AuthoredIn...)
		out = append(out, n.CoversPath...)
	}
	return cleanPathStrings(out)
}

func conditionReasons(req Request, b Bundle, mutation string) []Reason {
	if b.ClosureAfter.Verdict != closure.VerdictConditionallyClosed {
		return nil
	}
	current := conditionIDs(b.ClosureAfter.Conditions)
	accepted := set(req.AcceptedConditionIDs)
	var out []Reason
	for id := range accepted {
		if !current[id] {
			out = append(out, Reason{Code: ReasonConditionUnknownOrStale, Detail: id})
		}
	}
	if mutation == CapabilityWaiting {
		for id := range current {
			if !accepted[id] {
				out = append(out, Reason{Code: ReasonConditionAcknowledgementMissing, Detail: id})
			}
		}
	}
	return out
}

func sessionReasons(b Bundle, mutation string) []Reason {
	var out []Reason
	if mutation == CapabilityWaiting {
		for _, w := range b.LatestIteration.WaitClasses {
			switch w {
			case convergence.WaitArchitect:
				out = append(out, Reason{Code: ReasonWaitingArchitect})
			case convergence.WaitEvidence:
				out = append(out, Reason{Code: ReasonWaitingEvidence})
			case convergence.WaitGovernance:
				out = append(out, Reason{Code: ReasonWaitingGovernance})
			case convergence.WaitMechanicalRepair:
				out = append(out, Reason{Code: ReasonWaitingMechanicalRepair})
			}
		}
	}
	if mutation == CapabilityRefused {
		switch b.LatestIteration.Status {
		case convergence.StatusStalled:
			out = append(out, Reason{Code: ReasonSessionStalled})
		case convergence.StatusOscillating:
			out = append(out, Reason{Code: ReasonSessionOscillating})
		case convergence.StatusBudgetExhausted:
			out = append(out, Reason{Code: ReasonSessionBudgetExhausted})
		}
	}
	if mutation == CapabilityUncertifiable && b.LatestIteration.Status == convergence.StatusUncertifiable {
		out = append(out, Reason{Code: ReasonSessionUncertifiable})
	}
	if mutation == CapabilityUncertifiable && b.LatestIteration.Status != convergence.StatusUncertifiable {
		out = append(out, Reason{Code: ReasonSessionInconsistentStatus})
	}
	return out
}

func verificationFromDecision(d Decision, status string, reasons []Reason, changes []ChangeReceipt, violations []Violation) Verification {
	return Verification{
		SchemaVersion:           SchemaVersion,
		GeneratedBy:             GeneratedBy,
		AdmissionID:             d.AdmissionID,
		DecisionDigestSHA256:    d.DecisionDigestSHA256,
		Status:                  status,
		Binding:                 d.Binding,
		SessionID:               d.SessionReceipt.SessionID,
		IterationDigestSHA256:   d.SessionReceipt.IterationDigestSHA256,
		Changes:                 changes,
		Violations:              violations,
		PendingConditions:       d.Conditions,
		PendingTests:            d.RequiredTests,
		PendingProofObligations: d.ProofObligations,
		PendingRuntimeEvidence:  d.RequiredRuntimeEvidence,
		AwarenessMutation:       d.AwarenessMutation,
		Reasons:                 normalizeReasons(reasons),
		Limitations:             d.Limitations,
		ScopeOnly:               true,
		CorrectnessCertified:    false,
	}
}

func finalizeVerification(v Verification, d Decision) Verification {
	v = normalizeVerification(v)
	v.VerificationDigestSHA256 = verificationDigest(v)
	return v
}

func envelopeViolations(d Decision, changes []ChangeReceipt) []Violation {
	modify := set(d.Envelope.ModifyPaths)
	var out []Violation
	for _, c := range changes {
		if d.MutationCapability != CapabilityAdmitted && d.MutationCapability != CapabilityAdmittedWithConditions {
			out = append(out, Violation{Code: VerifyReadOnlyMutation, Path: c.Path, ObservedOperation: c.ChangeType, ExpectedOperation: "none"})
			continue
		}
		if !modify[c.Path] {
			code := VerifyPathOutsideEnvelope
			if c.ChangeType == ChangeUntracked {
				code = VerifyUntrackedFile
			}
			out = append(out, Violation{Code: code, Path: c.Path, ObservedOperation: c.ChangeType, ExpectedOperation: OperationModify})
			continue
		}
		if c.ChangeType != ChangeModified {
			out = append(out, Violation{Code: verifyCodeForChange(c.ChangeType), Path: c.Path, ObservedOperation: c.ChangeType, ExpectedOperation: ChangeModified})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func verifyCodeForChange(t string) string {
	switch t {
	case ChangeAdded:
		return VerifyOperationNotAdmitted
	case ChangeDeleted:
		return VerifyDeletedFile
	case ChangeRenamed:
		return VerifyRenamedFile
	case ChangeCopied:
		return VerifyCopiedFile
	case ChangeTypeChanged:
		return VerifyTypeChanged
	case ChangeUnmerged:
		return VerifyUnmergedFile
	case ChangeUntracked:
		return VerifyUntrackedFile
	default:
		return VerifyOperationNotAdmitted
	}
}

func parseNameStatus(repo, out string) []ChangeReceipt {
	var changes []ChangeReceipt
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		changeType := ChangeModified
		path := fields[1]
		old := ""
		switch status[0] {
		case 'A':
			changeType = ChangeAdded
		case 'D':
			changeType = ChangeDeleted
		case 'R':
			changeType = ChangeRenamed
			if len(fields) >= 3 {
				old = fields[1]
				path = fields[2]
			}
		case 'C':
			changeType = ChangeCopied
			if len(fields) >= 3 {
				old = fields[1]
				path = fields[2]
			}
		case 'T':
			changeType = ChangeTypeChanged
		case 'U':
			changeType = ChangeUnmerged
		}
		changes = append(changes, changeReceipt(repo, normalizePath(path), normalizePath(old), changeType))
	}
	return changes
}

func parsePorcelainUntracked(repo, out string) []ChangeReceipt {
	var changes []ChangeReceipt
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "?? ") {
			changes = append(changes, changeReceipt(repo, normalizePath(strings.TrimSpace(strings.TrimPrefix(line, "?? "))), "", ChangeUntracked))
		}
	}
	return changes
}

func changeReceipt(repo, path, oldPath, typ string) ChangeReceipt {
	r := ChangeReceipt{Path: path, OldPath: oldPath, ChangeType: typ}
	if typ == ChangeDeleted {
		return r
	}
	full := filepath.Join(repo, filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if err != nil {
		return r
	}
	r.CurrentDigestSHA256 = digest(data)
	r.CurrentSize = int64(len(data))
	return r
}

func dedupeChanges(in []ChangeReceipt) []ChangeReceipt {
	seen := map[string]ChangeReceipt{}
	for _, c := range in {
		key := c.Path + "\x00" + c.ChangeType
		seen[key] = c
	}
	out := make([]ChangeReceipt, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	return cmd.Output()
}

func gitHead(repo string) (string, error) {
	data, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func normalizeDecision(d Decision) Decision {
	d.SchemaVersion = strings.TrimSpace(d.SchemaVersion)
	d.GeneratedBy = strings.TrimSpace(d.GeneratedBy)
	d.AdmissionID = strings.TrimSpace(d.AdmissionID)
	d.PolicyID = strings.TrimSpace(d.PolicyID)
	d.PolicyVersion = strings.TrimSpace(d.PolicyVersion)
	d.Decision = strings.TrimSpace(d.Decision)
	d.RequestedMode = strings.TrimSpace(d.RequestedMode)
	d.Binding = normalizeBinding(d.Binding)
	d.RequestReceipt.Scope = normalizeScopeNoValidate(d.RequestReceipt.Scope)
	d.Envelope.ReadPaths = cleanPathStrings(d.Envelope.ReadPaths)
	d.Envelope.ModifyPaths = cleanPathStrings(d.Envelope.ModifyPaths)
	d.Envelope.Symbols = clean(d.Envelope.Symbols)
	d.Envelope.Components = clean(d.Envelope.Components)
	d.Envelope.ClaimIDs = clean(d.Envelope.ClaimIDs)
	d.Envelope.PropositionKeys = clean(d.Envelope.PropositionKeys)
	d.Envelope.UnsupportedOperations = clean(d.Envelope.UnsupportedOperations)
	d.Authority = sortItems(d.Authority)
	d.MustPreserve = sortItems(d.MustPreserve)
	d.ForbiddenMoves = sortItems(d.ForbiddenMoves)
	d.RequiredTests = sortItems(d.RequiredTests)
	sort.SliceStable(d.ProofObligations, func(i, j int) bool { return d.ProofObligations[i].ID < d.ProofObligations[j].ID })
	d.RequiredRuntimeEvidence = sortItems(d.RequiredRuntimeEvidence)
	d.Conditions = normalizeConditions(d.Conditions)
	if d.AwarenessMutationBinding != nil {
		d.AwarenessMutationBinding.TaskID = strings.TrimSpace(d.AwarenessMutationBinding.TaskID)
		d.AwarenessMutationBinding.Path = filepath.ToSlash(strings.TrimSpace(d.AwarenessMutationBinding.Path))
		d.AwarenessMutationBinding.PlanDigestSHA256 = strings.TrimSpace(d.AwarenessMutationBinding.PlanDigestSHA256)
		d.AwarenessMutationBinding.PolicyID = strings.TrimSpace(d.AwarenessMutationBinding.PolicyID)
	}
	d.AwarenessMutation = closure.NormalizeAwarenessMutationReceiptForExternal(d.AwarenessMutation)
	d.NextActions = normalizeNextActions(d.NextActions)
	d.FilesToRead = cleanPathStrings(d.FilesToRead)
	d.Reasons = normalizeReasons(d.Reasons)
	d.ScopeOnly = true
	d.CorrectnessCertified = false
	return d
}

func normalizeVerification(v Verification) Verification {
	v.SchemaVersion = strings.TrimSpace(v.SchemaVersion)
	v.GeneratedBy = strings.TrimSpace(v.GeneratedBy)
	v.AdmissionID = strings.TrimSpace(v.AdmissionID)
	v.DecisionDigestSHA256 = strings.TrimSpace(v.DecisionDigestSHA256)
	v.Status = strings.TrimSpace(v.Status)
	v.Binding = normalizeBinding(v.Binding)
	sort.SliceStable(v.Changes, func(i, j int) bool {
		if v.Changes[i].Path != v.Changes[j].Path {
			return v.Changes[i].Path < v.Changes[j].Path
		}
		return v.Changes[i].ChangeType < v.Changes[j].ChangeType
	})
	sort.SliceStable(v.Violations, func(i, j int) bool {
		if v.Violations[i].Path != v.Violations[j].Path {
			return v.Violations[i].Path < v.Violations[j].Path
		}
		return v.Violations[i].Code < v.Violations[j].Code
	})
	v.AwarenessMutation = closure.NormalizeAwarenessMutationReceiptForExternal(v.AwarenessMutation)
	v.PendingConditions = normalizeConditions(v.PendingConditions)
	v.PendingTests = sortItems(v.PendingTests)
	sort.SliceStable(v.PendingProofObligations, func(i, j int) bool { return v.PendingProofObligations[i].ID < v.PendingProofObligations[j].ID })
	v.PendingRuntimeEvidence = sortItems(v.PendingRuntimeEvidence)
	if v.AwarenessMutationVerification != nil {
		v.AwarenessMutationVerification.SchemaVersion = strings.TrimSpace(v.AwarenessMutationVerification.SchemaVersion)
		v.AwarenessMutationVerification.PolicyID = strings.TrimSpace(v.AwarenessMutationVerification.PolicyID)
		v.AwarenessMutationVerification.PlanDigestSHA256 = strings.TrimSpace(v.AwarenessMutationVerification.PlanDigestSHA256)
		v.AwarenessMutationVerification.RepositoryRevisionBefore = strings.TrimSpace(v.AwarenessMutationVerification.RepositoryRevisionBefore)
		v.AwarenessMutationVerification.GraphDigestBefore = strings.TrimSpace(v.AwarenessMutationVerification.GraphDigestBefore)
		v.AwarenessMutationVerification.SourcePaths = cleanPathStrings(v.AwarenessMutationVerification.SourcePaths)
		v.AwarenessMutationVerification.GeneratedNodeIDs = clean(v.AwarenessMutationVerification.GeneratedNodeIDs)
		v.AwarenessMutationVerification.RejectedNodeIDs = clean(v.AwarenessMutationVerification.RejectedNodeIDs)
		v.AwarenessMutationVerification.Limitations = clean(v.AwarenessMutationVerification.Limitations)
	}
	v.Reasons = normalizeReasons(v.Reasons)
	v.ScopeOnly = true
	v.CorrectnessCertified = false
	return v
}

func normalizeBootstrapDirectionConsumption(receipt BootstrapDirectionConsumption) BootstrapDirectionConsumption {
	receipt.SchemaVersion = strings.TrimSpace(receipt.SchemaVersion)
	if receipt.SchemaVersion == "" {
		receipt.SchemaVersion = SchemaVersion
	}
	receipt.GeneratedBy = strings.TrimSpace(receipt.GeneratedBy)
	if receipt.GeneratedBy == "" {
		receipt.GeneratedBy = GeneratedBy
	}
	receipt.TaskID = strings.TrimSpace(receipt.TaskID)
	receipt.AdmissionID = strings.TrimSpace(receipt.AdmissionID)
	receipt.VerificationDigestSHA256 = strings.TrimSpace(strings.ToLower(receipt.VerificationDigestSHA256))
	receipt.AuthorizationDigestSHA256 = strings.TrimSpace(strings.ToLower(receipt.AuthorizationDigestSHA256))
	receipt.ApprovalSourcePath = filepath.Clean(strings.TrimSpace(receipt.ApprovalSourcePath))
	receipt.ApprovalSourceDigestSHA256 = strings.TrimSpace(strings.ToLower(receipt.ApprovalSourceDigestSHA256))
	receipt.ConsumedAt = strings.TrimSpace(receipt.ConsumedAt)
	receipt.ReceiptDigestSHA256 = strings.TrimSpace(strings.ToLower(receipt.ReceiptDigestSHA256))
	return receipt
}

func normalizeScopeNoValidate(scope ChangeScope) ChangeScope {
	scope.Files = dedupeFileOps(scope.Files)
	scope.Symbols = clean(scope.Symbols)
	scope.Components = clean(scope.Components)
	scope.ClaimIDs = clean(scope.ClaimIDs)
	scope.PropositionKeys = clean(scope.PropositionKeys)
	return scope
}

func decisionDigest(d Decision) string {
	d.DecisionDigestSHA256 = ""
	return digest(canonicalJSON(d))
}

func verificationDigest(v Verification) string {
	v.VerificationDigestSHA256 = ""
	return digest(canonicalJSON(v))
}

func bootstrapDirectionConsumptionDigest(receipt BootstrapDirectionConsumption) string {
	receipt = normalizeBootstrapDirectionConsumption(receipt)
	receipt.ReceiptDigestSHA256 = ""
	return digest(canonicalJSON(receipt))
}

func recordBootstrapDirectionConsumption(repoRoot, taskRoot string, d Decision, auth *closure.DirectionBootstrapAuthorization, v Verification) error {
	if auth == nil {
		return nil
	}
	receipt := BootstrapDirectionConsumption{
		SchemaVersion:              SchemaVersion,
		GeneratedBy:                GeneratedBy,
		TaskID:                     auth.TaskID,
		AdmissionID:                d.AdmissionID,
		VerificationDigestSHA256:   v.VerificationDigestSHA256,
		AuthorizationDigestSHA256:  auth.AuthorizationDigestSHA256,
		ApprovalSourcePath:         auth.ApprovalSourcePath,
		ApprovalSourceDigestSHA256: auth.ApprovalSourceDigestSHA256,
		ConsumedAt:                 time.Now().UTC().Format(time.RFC3339),
	}
	globalPath := bootstrapDirectionConsumptionLedgerPath(repoRoot, auth.AuthorizationDigestSHA256)
	if err := writeBootstrapDirectionConsumptionIfAbsent(globalPath, receipt); err != nil {
		return err
	}
	path := filepath.Join(taskRoot, "receipts", "bootstrap-direction-consumption.yaml")
	if existing, err := LoadBootstrapDirectionConsumption(path); err == nil {
		if existing.AuthorizationDigestSHA256 != receipt.AuthorizationDigestSHA256 {
			return errors.New("different bootstrap authorization already consumed for this task")
		}
		if existing.TaskID != receipt.TaskID || existing.AdmissionID != receipt.AdmissionID {
			return errors.New("bootstrap authorization already consumed by another task or admission")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := MarshalCanonicalBootstrapDirectionConsumptionYAML(receipt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func bootstrapDirectionConsumptionLedgerPath(repoRoot, authDigest string) string {
	return filepath.Join(repoRoot, ".sensei", "bootstrap-direction-consumption", authDigest+".yaml")
}

func writeBootstrapDirectionConsumptionIfAbsent(path string, receipt BootstrapDirectionConsumption) error {
	if existing, err := LoadBootstrapDirectionConsumption(path); err == nil {
		if existing.AuthorizationDigestSHA256 != receipt.AuthorizationDigestSHA256 {
			return errors.New("different bootstrap authorization already consumed")
		}
		if existing.TaskID != receipt.TaskID || existing.AdmissionID != receipt.AdmissionID {
			return fmt.Errorf("bootstrap authorization already consumed by task %s", existing.TaskID)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := MarshalCanonicalBootstrapDirectionConsumptionYAML(receipt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			existing, loadErr := LoadBootstrapDirectionConsumption(path)
			if loadErr != nil {
				return loadErr
			}
			if existing.AuthorizationDigestSHA256 != receipt.AuthorizationDigestSHA256 {
				return errors.New("different bootstrap authorization already consumed")
			}
			if existing.TaskID != receipt.TaskID || existing.AdmissionID != receipt.AdmissionID {
				return fmt.Errorf("bootstrap authorization already consumed by task %s", existing.TaskID)
			}
			return nil
		}
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Close()
}

func collectLimitations(policy Policy, b Bundle) []architecture.Limitation {
	var out []architecture.Limitation
	for _, l := range policy.KnownLimitations {
		out = append(out, architecture.Limitation{Source: "admission.policy", Reason: l})
	}
	out = append(out, b.MaintainedClaims.Limitations...)
	out = append(out, b.Maintenance.Limitations...)
	out = append(out, b.PlaneAssessment.Limitations...)
	out = append(out, b.ClosureAfter.Limitations...)
	out = append(out, b.QuestionReport.Limitations...)
	out = append(out, b.Probes.Limitations...)
	out = append(out, b.ProbeReport.Limitations...)
	return out
}

func itemFromNode(n closure.Node, fallbackClass string) GuidanceItem {
	class := fallbackClass
	if len(n.Classes) > 0 {
		class = n.Classes[0]
	}
	details := clean(append(append(append(append(append([]string{}, n.OwnerServices...), n.OwnsStates...), n.MayRead...), n.MayWrite...), append(n.MustMutateVia, append(n.MustReadVia, n.ForbidsBypass...)...)...))
	return GuidanceItem{ID: n.ID, Class: class, Label: firstNonEmpty(n.Label, n.Comment), Status: n.Status, Plane: n.ArchitecturalPlane, SourceIDs: clean(append([]string{n.IRI}, n.AnchoredIn...)), Paths: cleanPathStrings(append(append([]string{n.SourcePath}, n.AuthoredIn...), n.CoversPath...)), Details: details}
}

// nodeByReceipt delegates to the single shared definition of scoped-node
// resolution so admission guidance and Phase 7 proof composition can never
// disagree on which node a receipt resolves to.
func nodeByReceipt(graph closure.GraphIndex, nr closure.NodeReceipt) (closure.Node, bool) {
	return proofrequirements.NodeByReceipt(graph, nr)
}

func graphHasClass(graph closure.GraphIndex, id, class string) bool {
	iri := graph.NodesByID[id]
	if iri == "" {
		iri = graph.NodesByID[class+":"+id]
	}
	n, ok := graph.Nodes[iri]
	return ok && hasAnyClass(n, class)
}

// hasAnyClass delegates to the single shared classification predicate.
func hasAnyClass(n closure.Node, classes ...string) bool {
	return proofrequirements.HasAnyClass(n, classes...)
}

func fileRepresentedByClaim(path string, claims []architecture.Claim) bool {
	for _, c := range claims {
		for _, f := range c.Scope.Files {
			if normalizePath(f) == path {
				return true
			}
		}
	}
	return false
}

func fileRepresentedByClosureNode(path string, report closure.Report, graph closure.GraphIndex, repoRoot string) bool {
	for _, nr := range report.RelevantNodes {
		n, ok := nodeByReceipt(graph, nr)
		if !ok {
			continue
		}
		if closure.CanonicallyRepresentsFile(graph, n, path, repoRoot) {
			return true
		}
	}
	return false
}

func normalizeConditions(in []closure.Condition) []closure.Condition {
	out := append([]closure.Condition{}, in...)
	for i := range out {
		out[i].QuestionIDs = clean(out[i].QuestionIDs)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func normalizeNextActions(in []convergence.NextAction) []convergence.NextAction {
	out := append([]convergence.NextAction{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		return out[i].Reference < out[j].Reference
	})
	return out
}

func normalizeReasons(in []Reason) []Reason {
	seen := map[string]Reason{}
	for _, r := range in {
		r.Code = strings.TrimSpace(r.Code)
		r.Detail = strings.TrimSpace(r.Detail)
		if r.Code == "" {
			continue
		}
		seen[r.Code+"\x00"+r.Detail] = r
	}
	out := make([]Reason, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

func sortItems(in []GuidanceItem) []GuidanceItem {
	seen := map[string]GuidanceItem{}
	for _, item := range in {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			continue
		}
		item.SourceIDs = clean(item.SourceIDs)
		item.Paths = cleanPathStrings(item.Paths)
		item.Details = clean(item.Details)
		seen[item.Class+"\x00"+item.ID] = item
	}
	out := make([]GuidanceItem, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func clean(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func cleanPathStrings(in []string) []string {
	for i := range in {
		in[i] = normalizePath(in[i])
	}
	return clean(in)
}

func dedupeFileOps(in []FileOperation) []FileOperation {
	seen := map[string]FileOperation{}
	for _, f := range in {
		f.Path = normalizePath(f.Path)
		f.Operation = strings.TrimSpace(f.Operation)
		if f.Path == "" && f.Operation == "" {
			continue
		}
		seen[f.Path+"\x00"+f.Operation] = f
	}
	out := make([]FileOperation, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Operation < out[j].Operation
	})
	return out
}

func normalizePath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	if p == "." {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func safeRelPath(p string) bool {
	p = filepath.ToSlash(strings.TrimSpace(p))
	return p != "" && !filepath.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") && !strings.Contains(p, "/../")
}

func normalizeBinding(b architecture.ClaimDocumentBinding) architecture.ClaimDocumentBinding {
	b.RepositoryDomain = strings.TrimSpace(b.RepositoryDomain)
	b.Revision = strings.TrimSpace(b.Revision)
	b.RevisionStatus = strings.TrimSpace(b.RevisionStatus)
	b.GraphDigestSHA256 = strings.TrimSpace(b.GraphDigestSHA256)
	b.GraphDigestStatus = strings.TrimSpace(b.GraphDigestStatus)
	return b
}

func requireResolvedBinding(b architecture.ClaimDocumentBinding) error {
	if b.RepositoryDomain == "" || b.Revision == "" || b.GraphDigestSHA256 == "" {
		return errors.New("repository_domain, revision, and graph_digest_sha256 are required")
	}
	if b.RevisionStatus != architecture.RevisionResolved {
		return errors.New("revision_status must be resolved")
	}
	if b.GraphDigestStatus != architecture.GraphDigestResolved {
		return errors.New("graph_digest_status must be resolved")
	}
	if !isSHA256(b.GraphDigestSHA256) {
		return errors.New("graph_digest_sha256 must be lowercase SHA-256")
	}
	return nil
}

func bindingsEqual(a, b architecture.ClaimDocumentBinding) bool {
	a = normalizeBinding(a)
	b = normalizeBinding(b)
	return a.RepositoryDomain == b.RepositoryDomain && a.Revision == b.Revision && a.RevisionStatus == b.RevisionStatus && a.GraphDigestSHA256 == b.GraphDigestSHA256 && a.GraphDigestStatus == b.GraphDigestStatus
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func hasDuplicates(in []string) bool {
	seen := map[string]bool{}
	for _, v := range in {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

func set(in []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range in {
		out[v] = true
	}
	return out
}

func conditionIDs(conditions []closure.Condition) map[string]bool {
	out := map[string]bool{}
	for _, c := range conditions {
		out[c.ID] = true
	}
	return out
}

func modifyPaths(files []FileOperation) []string {
	var out []string
	for _, f := range files {
		if f.Operation == OperationModify {
			out = append(out, f.Path)
		}
	}
	return clean(out)
}

func hasUncertifiableReason(reasons []Reason) bool {
	for _, r := range reasons {
		switch r.Code {
		case ReasonBundleInvalid, ReasonGraphUnverified, ReasonRepositoryRevisionUnverified, ReasonSessionStaleIteration, ReasonBindingMismatch, ReasonBootstrapDirectionInvalid:
			return true
		}
	}
	return false
}

func joinGraphReasons(reasons []graphsnapshot.Reason) string {
	var parts []string
	for _, r := range reasons {
		parts = append(parts, strings.TrimSpace(r.Code+" "+r.Detail))
	}
	return strings.Join(clean(parts), "; ")
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func claimStatementText(s architecture.ClaimStatement) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{s.Subject, s.Predicate, s.Object} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return strings.Join(parts, " ")
}

func shortToken(s string) string {
	s = strings.Trim(strings.TrimPrefix(s, "convergence."), ".")
	if s == "" {
		return "session"
	}
	parts := strings.Split(s, ".")
	return parts[len(parts)-1]
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func cappedStrings(in []string, n int) []string {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[:n]
}

func capFor(detail string, compact int) int {
	if detail == "full" {
		return 0
	}
	return compact
}

func capItems(in []GuidanceItem, n int) []GuidanceItem {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[:n]
}

func capProof(in []ProofReceipt, n int) []ProofReceipt {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[:n]
}

func capReasons(in []Reason, n int) []Reason {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[:n]
}

func capNext(in []convergence.NextAction, n int) []convergence.NextAction {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[:n]
}

func waitingClasses(reasons []Reason) []string {
	var out []string
	for _, r := range reasons {
		switch r.Code {
		case ReasonWaitingArchitect:
			out = append(out, "architect")
		case ReasonWaitingEvidence:
			out = append(out, "evidence")
		case ReasonWaitingGovernance:
			out = append(out, "governance")
		case ReasonWaitingMechanicalRepair:
			out = append(out, "mechanical_repair")
		}
	}
	return clean(out)
}

func writeIfCheck(path string, got []byte) error {
	want, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return errors.New("check failed: canonical output differs")
	}
	return nil
}

func CheckDecision(path string, d Decision) error {
	data, err := CanonicalDecisionBytes(d)
	if err != nil {
		return err
	}
	return writeIfCheck(path, data)
}

func CheckVerification(path string, v Verification) error {
	data, err := CanonicalVerificationBytes(v)
	if err != nil {
		return err
	}
	return writeIfCheck(path, data)
}
