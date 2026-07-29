// SPDX-License-Identifier: AGPL-3.0-only

// Package workspacecontract is the canonical producer-side data model for
// the sensei.workspace.identity.v1 and sensei.workspace.admission.v1
// contracts (docs/design/workspace-identity-admission-contracts.md). It
// composes or projects existing Sensei-owned facts — checkout binding,
// graph authority, coverage, task identity, and admission decision/
// verification — into two closed external wire contracts. It never decides
// closure, invents a policy, infers a task, broadens a change envelope, or
// independently certifies correctness: every fact carried here comes from
// an existing typed owner (architecture.ClaimDocumentBinding, the metadata/
// graph-authority client, golang/architecture/tasksession, and
// golang/architecture/admission).
//
// Workspace identity is evidence, not permission. Admission is permission
// to attempt, not correctness. Scope compliance is not correctness
// certification. See the two schemas under docs/schemas/workspace/v1/ for
// the complete, closed external shape this package produces and validates
// against.
package workspacecontract

// IdentitySchemaVersion is the exact, adopted workspace identity schema
// identifier. Schema evolution is strict: any field addition, removal,
// rename, or semantic change requires a new identifier.
const IdentitySchemaVersion = "sensei.workspace.identity.v1"

// AdmissionSchemaVersion is the exact, adopted workspace admission schema
// identifier.
const AdmissionSchemaVersion = "sensei.workspace.admission.v1"

// GeneratedBy identifies this package as the producer in every receipt it
// composes, mirroring the admission package's own GeneratedBy convention.
const GeneratedBy = "sensei workspacecontract"

// CompositionState reports only whether an identity receipt could be
// composed completely. It is never mutation admission, correctness
// certification, provider authorization, or a merge recommendation.
type CompositionState string

const (
	CompositionComplete    CompositionState = "complete"
	CompositionPartial     CompositionState = "partial"
	CompositionUnavailable CompositionState = "unavailable"
)

// RepositoryDomainSource identifies where binding.repository_domain came
// from. "configured" is the only source that can produce
// CompositionComplete — environment variables and git remote origin are
// never a source of governed workspace identity, even when observed
// elsewhere as diagnostic context.
type RepositoryDomainSource string

const (
	RepositoryDomainConfigured RepositoryDomainSource = "configured"
	RepositoryDomainUnbound    RepositoryDomainSource = "unbound"
)

// TaskIdentityState distinguishes why task_id is or is not present.
type TaskIdentityState string

const (
	TaskIdentityNotRequested TaskIdentityState = "not_requested"
	TaskIdentityResolved     TaskIdentityState = "resolved"
	TaskIdentityUnavailable  TaskIdentityState = "unavailable"
)

// RevisionStatus mirrors architecture package's exact revision-status
// vocabulary (architecture.RevisionResolved etc.) as a closed local type.
type RevisionStatus string

const (
	RevisionResolved     RevisionStatus = "resolved"
	RevisionUnavailable  RevisionStatus = "unavailable"
	RevisionNotGit       RevisionStatus = "not_git"
	RevisionNotRequested RevisionStatus = "not_requested"
)

// GraphDigestStatus mirrors architecture package's exact graph-digest-status
// vocabulary (architecture.GraphDigestResolved etc.) as a closed local type.
type GraphDigestStatus string

const (
	GraphDigestResolved     GraphDigestStatus = "resolved"
	GraphDigestUnavailable  GraphDigestStatus = "unavailable"
	GraphDigestNotRequested GraphDigestStatus = "not_requested"
)

// Binding mirrors architecture.ClaimDocumentBinding's exact semantics as a
// closed external shape: every field is always present in JSON (never
// omitted), and revision/tree_digest_sha256/graph_digest_sha256 are
// nullable rather than optional so "unresolved" is never indistinguishable
// from "the producer forgot to serialize this key."
type Binding struct {
	RepositoryDomain  string            `json:"repository_domain"`
	Revision          *string           `json:"revision"`
	RevisionStatus    RevisionStatus    `json:"revision_status"`
	TreeDigestSHA256  *string           `json:"tree_digest_sha256"`
	GraphDigestSHA256 *string           `json:"graph_digest_sha256"`
	GraphDigestStatus GraphDigestStatus `json:"graph_digest_status"`
}

// GraphAuthority is a bounded external projection of the current
// authoritative GraphAuthority/MetadataResponse facts a runner needs. Field
// names and enum spellings are verbatim proto names (awareness_graph.proto)
// so this receipt can be cross-checked directly against the wire schema.
type GraphAuthority struct {
	Authoritative                   bool   `json:"authoritative"`
	GraphFreshnessState             string `json:"graph_freshness_state"`
	GraphFreshnessDetail            string `json:"graph_freshness_detail"`
	SeedState                       string `json:"seed_state"`
	BuildProvenanceState            string `json:"build_provenance_state"`
	LiveStoreGraphDigestSHA256      string `json:"live_store_graph_digest_sha256"`
	LiveStoreGraphTripleCount       int64  `json:"live_store_graph_triple_count"`
	EmbeddedSeedDigestSHA256        string `json:"embedded_seed_digest_sha256"`
	EmbeddedTransactionStampPresent bool   `json:"embedded_transaction_stamp_present"`
	EmbeddedTransactionMatchesSeed  bool   `json:"embedded_transaction_matches_seed"`
	CertifiedAwarenessGraphCommit   string `json:"certified_awareness_graph_commit"`
	CertifiedServicesRepoCommit     string `json:"certified_services_repo_commit"`
}

// TaskIdentity distinguishes not_requested/resolved/unavailable and carries
// TaskID only when State is resolved.
type TaskIdentity struct {
	State  TaskIdentityState `json:"state"`
	TaskID *string           `json:"task_id"`
}

// Limitation mirrors architecture.Limitation verbatim.
type Limitation struct {
	Source   string `json:"source"`
	Scope    string `json:"scope"`
	Reason   string `json:"reason"`
	Blocking bool   `json:"blocking"`
}

// Identity is the top-level sensei.workspace.identity.v1 document.
type Identity struct {
	SchemaVersion          string                 `json:"schema_version"`
	GeneratedBy            string                 `json:"generated_by"`
	CompositionState       CompositionState       `json:"composition_state"`
	Binding                Binding                `json:"binding"`
	RepositoryDomainSource RepositoryDomainSource `json:"repository_domain_source"`
	GraphAuthority         *GraphAuthority        `json:"graph_authority"`
	CoverageState          string                 `json:"coverage_state"`
	TaskIdentity           TaskIdentity           `json:"task_identity"`
	Limitations            []Limitation           `json:"limitations"`
}

// --- sensei.workspace.admission.v1 ---

// RecordKind distinguishes a decision record from a verification record.
type RecordKind string

const (
	RecordKindDecision     RecordKind = "decision"
	RecordKindVerification RecordKind = "verification"
)

// DecisionOutcome mirrors admission's exact decision/capability vocabulary
// (admission.DecisionAdmitted etc.) as a closed local type — used for the
// top-level decision as well as inspection_capability/mutation_capability,
// exactly as admission.Decision itself reuses one vocabulary for all three.
type DecisionOutcome string

const (
	DecisionAdmitted               DecisionOutcome = "admitted"
	DecisionAdmittedWithConditions DecisionOutcome = "admitted_with_conditions"
	DecisionWaiting                DecisionOutcome = "waiting"
	DecisionRefused                DecisionOutcome = "refused"
	DecisionUncertifiable          DecisionOutcome = "uncertifiable"
)

// RequestedMode mirrors admission.ModeInspect/admission.ModeModify.
type RequestedMode string

const (
	ModeInspect RequestedMode = "inspect"
	ModeModify  RequestedMode = "modify"
)

// VerificationStatus mirrors admission's exact verification-status
// vocabulary (admission.VerificationScopeCompliant etc.) verbatim.
type VerificationStatus string

const (
	VerificationScopeCompliant VerificationStatus = "scope_compliant"
	VerificationScopeViolated  VerificationStatus = "scope_violated"
	VerificationStale          VerificationStatus = "stale"
	VerificationUncertifiable  VerificationStatus = "uncertifiable"
)

// SessionReceipt mirrors admission.SessionReceipt verbatim.
type SessionReceipt struct {
	SessionID                 string `json:"session_id"`
	LatestIteration           int    `json:"latest_iteration"`
	IterationDigestSHA256     string `json:"iteration_digest_sha256"`
	SemanticStateDigestSHA256 string `json:"semantic_state_digest_sha256"`
	Status                    string `json:"status"`
	ClosureVerdict            string `json:"closure_verdict"`
}

// FileOperation mirrors admission.FileOperation verbatim.
type FileOperation struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

// ChangeScope mirrors admission.ChangeScope verbatim, with every list
// present (never omitted) as an empty array when there is nothing to carry.
type ChangeScope struct {
	Files           []FileOperation `json:"files"`
	Symbols         []string        `json:"symbols"`
	Components      []string        `json:"components"`
	ClaimIDs        []string        `json:"claim_ids"`
	PropositionKeys []string        `json:"proposition_keys"`
}

// RequestReceipt mirrors admission.RequestReceipt verbatim.
type RequestReceipt struct {
	DigestSHA256 string      `json:"digest_sha256"`
	Scope        ChangeScope `json:"scope"`
	Mode         string      `json:"mode"`
	TaskClass    string      `json:"task_class"`
}

// Envelope mirrors admission.ChangeEnvelope verbatim.
type Envelope struct {
	ReadPaths             []string `json:"read_paths"`
	ModifyPaths           []string `json:"modify_paths"`
	Symbols               []string `json:"symbols"`
	Components            []string `json:"components"`
	ClaimIDs              []string `json:"claim_ids"`
	PropositionKeys       []string `json:"proposition_keys"`
	UnsupportedOperations []string `json:"unsupported_operations"`
}

// Reason mirrors admission.Reason verbatim.
type Reason struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// ChangeReceipt mirrors admission.ChangeReceipt verbatim.
type ChangeReceipt struct {
	Path                string `json:"path"`
	OldPath             string `json:"old_path,omitempty"`
	ChangeType          string `json:"change_type"`
	CurrentDigestSHA256 string `json:"current_digest_sha256,omitempty"`
	CurrentSize         int64  `json:"current_size,omitempty"`
}

// Violation mirrors admission.Violation verbatim.
type Violation struct {
	Code              string `json:"code"`
	Path              string `json:"path,omitempty"`
	ObservedOperation string `json:"observed_operation,omitempty"`
	ExpectedOperation string `json:"expected_operation,omitempty"`
	Detail            string `json:"detail,omitempty"`
}

// Verification is the sensei.workspace.admission.v1 verification-record
// body. pending_*_ids are id-only projections of admission.Verification's
// PendingConditions/PendingTests/PendingProofObligations/
// PendingRuntimeEvidence — the external contract carries stable identity,
// not the full internal guidance objects.
type Verification struct {
	Status                    VerificationStatus `json:"status"`
	VerificationDigestSHA256  string             `json:"verification_digest_sha256"`
	IterationDigestSHA256     string             `json:"iteration_digest_sha256"`
	PatchDigestSHA256         string             `json:"patch_digest_sha256"`
	Changes                   []ChangeReceipt    `json:"changes"`
	Violations                []Violation        `json:"violations"`
	PendingConditionIDs       []string           `json:"pending_condition_ids"`
	PendingTestIDs            []string           `json:"pending_test_ids"`
	PendingProofObligationIDs []string           `json:"pending_proof_obligation_ids"`
	PendingRuntimeEvidenceIDs []string           `json:"pending_runtime_evidence_ids"`
	Reasons                   []Reason           `json:"reasons"`
	Limitations               []Limitation       `json:"limitations"`
	ScopeOnly                 bool               `json:"scope_only"`
	CorrectnessCertified      bool               `json:"correctness_certified"`
}

// Admission is the top-level sensei.workspace.admission.v1 document. A
// decision record always carries Verification: nil; a verification record
// always carries a non-nil Verification bound to this same AdmissionID/
// DecisionDigestSHA256/Binding.
type Admission struct {
	SchemaVersion        string          `json:"schema_version"`
	RecordKind           RecordKind      `json:"record_kind"`
	AdmissionID          string          `json:"admission_id"`
	DecisionDigestSHA256 string          `json:"decision_digest_sha256"`
	PolicyID             string          `json:"policy_id"`
	PolicyVersion        string          `json:"policy_version"`
	Decision             DecisionOutcome `json:"decision"`
	RequestedMode        RequestedMode   `json:"requested_mode"`
	Binding              Binding         `json:"binding"`
	SessionReceipt       SessionReceipt  `json:"session_receipt"`
	RequestReceipt       RequestReceipt  `json:"request_receipt"`
	InspectionCapability DecisionOutcome `json:"inspection_capability"`
	MutationCapability   DecisionOutcome `json:"mutation_capability"`
	Envelope             Envelope        `json:"envelope"`
	Reasons              []Reason        `json:"reasons"`
	Limitations          []Limitation    `json:"limitations"`
	ScopeOnly            bool            `json:"scope_only"`
	CorrectnessCertified bool            `json:"correctness_certified"`
	Verification         *Verification   `json:"verification"`
}
