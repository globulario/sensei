// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei assess-closure"

	VerdictClosed              = "closed"
	VerdictConditionallyClosed = "conditionally_closed"
	VerdictOpen                = "open"
	VerdictUncertifiable       = "uncertifiable"

	StateClosed        = "closed"
	StateConditional   = "conditional"
	StateOpen          = "open"
	StateUncertifiable = "uncertifiable"
	StateNotApplicable = "not_applicable"

	DimensionStructural    = "structural"
	DimensionAuthority     = "authority"
	DimensionContract      = "contract"
	DimensionBehavioral    = "behavioral"
	DimensionEvidence      = "evidence"
	DimensionContradiction = "contradiction"
	DimensionDirection     = "direction"
	DimensionAgent         = "agent"

	RiskLowRisk               = "low_risk"
	RiskArchitectureSensitive = "architecture_sensitive"
	RiskConvergence           = "convergence_risk"
	RiskSecurity              = "security_risk"
	RiskDataLoss              = "data_loss_risk"
	RiskUnknownImpact         = "unknown_impact"

	AccessRead      = "read"
	AccessWrite     = "write"
	AccessReadWrite = "read_write"
	AccessUnknown   = "unknown"

	DirectionPreserve      = "preserve"
	DirectionEvolve        = "evolve"
	DirectionMigrate       = "migrate"
	DirectionNotApplicable = "not_applicable"
	DirectionUnknown       = "unknown"

	DirectionBootstrapSchemaVersion  = "1"
	DirectionBootstrapPolicyID       = "closure.direction.bootstrap.v1"
	DirectionBootstrapFile           = "docs/awareness/architecture/decisions.yaml"
	DirectionBootstrapUsageOneUse    = "one_use"
	DirectionBootstrapMechanismFile  = "external_authorization_file.v1"
	DirectionBootstrapApprovalDirEnv = "SENSEI_BOOTSTRAP_APPROVAL_DIR"
)

var DimensionOrder = []string{
	DimensionStructural,
	DimensionAuthority,
	DimensionContract,
	DimensionBehavioral,
	DimensionEvidence,
	DimensionContradiction,
	DimensionDirection,
	DimensionAgent,
}

type Request struct {
	SchemaVersion      string                            `json:"schema_version" yaml:"schema_version"`
	TaskID             string                            `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	Binding            architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Scope              Scope                             `json:"scope" yaml:"scope"`
	DirectionBootstrap *DirectionBootstrapAuthorization  `json:"direction_bootstrap,omitempty" yaml:"direction_bootstrap,omitempty"`
	RequestedBy        string                            `json:"requested_by,omitempty" yaml:"requested_by,omitempty"`
	Note               string                            `json:"note,omitempty" yaml:"note,omitempty"`
}

type DirectionBootstrapAuthorization struct {
	SchemaVersion                string   `json:"schema_version" yaml:"schema_version"`
	PolicyID                     string   `json:"policy_id" yaml:"policy_id"`
	TaskID                       string   `json:"task_id" yaml:"task_id"`
	BaseRevision                 string   `json:"base_revision" yaml:"base_revision"`
	GraphDigestSHA256            string   `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	File                         string   `json:"file" yaml:"file"`
	GovernedRecordIDs            []string `json:"governed_record_ids" yaml:"governed_record_ids"`
	ExpectedMutationDigestSHA256 string   `json:"expected_mutation_digest_sha256" yaml:"expected_mutation_digest_sha256"`
	ApprovedBy                   string   `json:"approved_by" yaml:"approved_by"`
	ApprovalMechanism            string   `json:"approval_mechanism" yaml:"approval_mechanism"`
	ApprovalStatement            string   `json:"approval_statement" yaml:"approval_statement"`
	UsagePolicy                  string   `json:"usage_policy" yaml:"usage_policy"`
	IssuedAt                     string   `json:"issued_at" yaml:"issued_at"`
	ExpiresAt                    string   `json:"expires_at" yaml:"expires_at"`
	ApprovalSourcePath           string   `json:"approval_source_path,omitempty" yaml:"approval_source_path,omitempty"`
	ApprovalSourceDigestSHA256   string   `json:"approval_source_digest_sha256,omitempty" yaml:"approval_source_digest_sha256,omitempty"`
	AuthorizationDigestSHA256    string   `json:"authorization_digest_sha256,omitempty" yaml:"authorization_digest_sha256,omitempty"`
}

type Scope struct {
	Domain               string   `json:"domain" yaml:"domain"`
	SourceSet            string   `json:"source_set,omitempty" yaml:"source_set,omitempty"`
	TaskClass            string   `json:"task_class" yaml:"task_class"`
	RiskClass            string   `json:"risk_class" yaml:"risk_class"`
	AccessMode           string   `json:"access_mode" yaml:"access_mode"`
	DirectionRequirement string   `json:"direction_requirement" yaml:"direction_requirement"`
	DomainWide           bool     `json:"domain_wide,omitempty" yaml:"domain_wide,omitempty"`
	Files                []string `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols              []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components           []string `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs             []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys      []string `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
	AdditionalDimensions []string `json:"additional_required_dimensions,omitempty" yaml:"additional_required_dimensions,omitempty"`
}

type Policy struct {
	RiskClass                    string   `json:"risk_class" yaml:"risk_class"`
	RequiredDimensions           []string `json:"required_dimensions" yaml:"required_dimensions"`
	ConditionalAllowed           bool     `json:"conditional_allowed" yaml:"conditional_allowed"`
	ConditionalDimensions        []string `json:"conditional_dimensions,omitempty" yaml:"conditional_dimensions,omitempty"`
	AcceptedUnknownMaxPriority   string   `json:"accepted_unknown_max_priority,omitempty" yaml:"accepted_unknown_max_priority,omitempty"`
	RequiresCurrentEvidence      bool     `json:"requires_current_evidence" yaml:"requires_current_evidence"`
	RequiresGovernedDirection    bool     `json:"requires_governed_direction" yaml:"requires_governed_direction"`
	RequiresFailureSurface       bool     `json:"requires_failure_surface" yaml:"requires_failure_surface"`
	KnownLimitations             []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
	AllowsNotApplicableDirection bool     `json:"allows_not_applicable_direction,omitempty" yaml:"allows_not_applicable_direction,omitempty"`
}

type Context struct {
	Request          Request
	Claims           architecture.ClaimDocument
	Maintenance      *maintenance.Report
	Plane            *plane.Report
	Dialogue         *architecture.DialogueDocument
	Evidence         *maintenance.EvidenceStateDocument
	Graph            GraphIndex
	GraphReceipt     graphsnapshot.Receipt
	RepositoryRoot   string
	RepositoryRev    string
	RepositoryStatus string
	MissingInputs    map[string]bool
}

type Report struct {
	SchemaVersion   string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy     string                            `json:"generated_by" yaml:"generated_by"`
	Request         Request                           `json:"request" yaml:"request"`
	ObservedBinding architecture.ClaimDocumentBinding `json:"observed_binding" yaml:"observed_binding"`
	Verdict         string                            `json:"verdict" yaml:"verdict"`
	ScopeReceipt    ScopeReceipt                      `json:"scope_receipt" yaml:"scope_receipt"`
	Dimensions      []DimensionAssessment             `json:"dimensions" yaml:"dimensions"`
	Blockers        []Blocker                         `json:"blockers" yaml:"blockers"`
	Conditions      []Condition                       `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	RelevantClaims  []ClaimReceipt                    `json:"relevant_claims,omitempty" yaml:"relevant_claims,omitempty"`
	RelevantNodes   []NodeReceipt                     `json:"relevant_nodes,omitempty" yaml:"relevant_nodes,omitempty"`
	Questions       []QuestionReceipt                 `json:"questions,omitempty" yaml:"questions,omitempty"`
	Limitations     []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ScopeReceipt struct {
	Files               []string                    `json:"files,omitempty" yaml:"files,omitempty"`
	RepresentedFiles    []FileRepresentationReceipt `json:"represented_files,omitempty" yaml:"represented_files,omitempty"`
	Symbols             []string                    `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components          []string                    `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs            []string                    `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys     []string                    `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
	NodeIDs             []string                    `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	MissingFiles        []string                    `json:"missing_files,omitempty" yaml:"missing_files,omitempty"`
	MissingSymbols      []string                    `json:"missing_symbols,omitempty" yaml:"missing_symbols,omitempty"`
	MissingComponents   []string                    `json:"missing_components,omitempty" yaml:"missing_components,omitempty"`
	MissingClaims       []string                    `json:"missing_claims,omitempty" yaml:"missing_claims,omitempty"`
	MissingPropositions []string                    `json:"missing_propositions,omitempty" yaml:"missing_propositions,omitempty"`
}

type FileRepresentationReceipt struct {
	Path               string   `json:"path" yaml:"path"`
	RepresentationKind string   `json:"representation_kind" yaml:"representation_kind"`
	AnchorNodeIDs      []string `json:"anchor_node_ids,omitempty" yaml:"anchor_node_ids,omitempty"`
}

type DimensionAssessment struct {
	Dimension    string   `json:"dimension" yaml:"dimension"`
	Required     bool     `json:"required" yaml:"required"`
	Applicable   bool     `json:"applicable" yaml:"applicable"`
	State        string   `json:"state" yaml:"state"`
	Reasons      []Reason `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	BlockerIDs   []string `json:"blocker_ids,omitempty" yaml:"blocker_ids,omitempty"`
	ConditionIDs []string `json:"condition_ids,omitempty" yaml:"condition_ids,omitempty"`
}

type Reason struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type Blocker struct {
	ID                 string   `json:"id" yaml:"id"`
	Dimension          string   `json:"dimension" yaml:"dimension"`
	Severity           string   `json:"severity" yaml:"severity"`
	Code               string   `json:"code" yaml:"code"`
	Summary            string   `json:"summary" yaml:"summary"`
	ClaimIDs           []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	NodeIDs            []string `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	QuestionIDs        []string `json:"question_ids,omitempty" yaml:"question_ids,omitempty"`
	EvidenceIDs        []string `json:"evidence_ids,omitempty" yaml:"evidence_ids,omitempty"`
	Files              []string `json:"files,omitempty" yaml:"files,omitempty"`
	RequiredNextAction string   `json:"required_next_action" yaml:"required_next_action"`
}

type Condition struct {
	ID                 string   `json:"id" yaml:"id"`
	Dimension          string   `json:"dimension" yaml:"dimension"`
	Code               string   `json:"code" yaml:"code"`
	Summary            string   `json:"summary" yaml:"summary"`
	QuestionIDs        []string `json:"question_ids,omitempty" yaml:"question_ids,omitempty"`
	RequiredNextAction string   `json:"required_next_action" yaml:"required_next_action"`
}

type ClaimReceipt struct {
	ID                 string `json:"id" yaml:"id"`
	PropositionKey     string `json:"proposition_key" yaml:"proposition_key"`
	ArchitecturalPlane string `json:"architectural_plane" yaml:"architectural_plane"`
	EpistemicStatus    string `json:"epistemic_status" yaml:"epistemic_status"`
	PlaneState         string `json:"plane_state,omitempty" yaml:"plane_state,omitempty"`
}

type NodeReceipt struct {
	ID      string   `json:"id" yaml:"id"`
	IRI     string   `json:"iri,omitempty" yaml:"iri,omitempty"`
	Classes []string `json:"classes" yaml:"classes"`
}

type QuestionReceipt struct {
	ID         string   `json:"id" yaml:"id"`
	Status     string   `json:"status" yaml:"status"`
	Priority   string   `json:"priority" yaml:"priority"`
	Dimensions []string `json:"dimensions" yaml:"dimensions"`
	ClaimIDs   []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	NodeIDs    []string `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	BlockerIDs []string `json:"blocker_ids,omitempty" yaml:"blocker_ids,omitempty"`
	TemplateID string   `json:"template_id,omitempty" yaml:"template_id,omitempty"`
}

type requestEnvelope struct {
	ArchitectureClosureRequest Request `json:"architecture_closure_request" yaml:"architecture_closure_request"`
}

type directionBootstrapEnvelope struct {
	ArchitectureDirectionBootstrapAuthorization DirectionBootstrapAuthorization `json:"architecture_direction_bootstrap_authorization" yaml:"architecture_direction_bootstrap_authorization"`
}

type reportEnvelope struct {
	ArchitectureClosureAssessment Report `json:"architecture_closure_assessment" yaml:"architecture_closure_assessment"`
}

func LoadRequest(path string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, err
	}
	return UnmarshalRequestYAML(data)
}

func LoadDirectionBootstrapAuthorization(path string) (DirectionBootstrapAuthorization, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DirectionBootstrapAuthorization{}, err
	}
	return UnmarshalDirectionBootstrapYAML(data)
}

func UnmarshalRequestYAML(data []byte) (Request, error) {
	var env requestEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Request{}, err
	}
	if env.ArchitectureClosureRequest.SchemaVersion == "" {
		return Request{}, errors.New("missing architecture_closure_request document")
	}
	return NormalizeRequest(env.ArchitectureClosureRequest)
}

func UnmarshalDirectionBootstrapYAML(data []byte) (DirectionBootstrapAuthorization, error) {
	var env directionBootstrapEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return DirectionBootstrapAuthorization{}, err
	}
	if env.ArchitectureDirectionBootstrapAuthorization.SchemaVersion == "" {
		return DirectionBootstrapAuthorization{}, errors.New("missing architecture_direction_bootstrap_authorization document")
	}
	return NormalizeDirectionBootstrap(env.ArchitectureDirectionBootstrapAuthorization)
}

func MarshalCanonicalRequestYAML(req Request) ([]byte, error) {
	req, err := NormalizeRequest(req)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(requestEnvelope{ArchitectureClosureRequest: req})
}

func MarshalCanonicalDirectionBootstrapYAML(auth DirectionBootstrapAuthorization) ([]byte, error) {
	auth, err := NormalizeDirectionBootstrap(auth)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(directionBootstrapEnvelope{ArchitectureDirectionBootstrapAuthorization: auth})
}

func MarshalCanonicalReportYAML(report Report) ([]byte, error) {
	report = normalizeReport(report)
	return yaml.Marshal(reportEnvelope{ArchitectureClosureAssessment: report})
}

func MarshalCanonicalReportJSON(report Report) ([]byte, error) {
	report = normalizeReport(report)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reportEnvelope{ArchitectureClosureAssessment: report}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func LoadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var env reportEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Report{}, err
	}
	if env.ArchitectureClosureAssessment.SchemaVersion == "" {
		return Report{}, errors.New("missing architecture_closure_assessment report")
	}
	return normalizeReport(env.ArchitectureClosureAssessment), nil
}

var taskTokenRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func NormalizeRequest(in Request) (Request, error) {
	req := in
	req.SchemaVersion = strings.TrimSpace(req.SchemaVersion)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Binding.RepositoryDomain = strings.TrimSpace(req.Binding.RepositoryDomain)
	req.Binding.Revision = strings.TrimSpace(req.Binding.Revision)
	req.Binding.RevisionStatus = strings.TrimSpace(req.Binding.RevisionStatus)
	req.Binding.GraphDigestSHA256 = strings.TrimSpace(req.Binding.GraphDigestSHA256)
	req.Binding.GraphDigestStatus = strings.TrimSpace(req.Binding.GraphDigestStatus)
	req.Scope.Domain = strings.TrimSpace(req.Scope.Domain)
	req.Scope.SourceSet = strings.TrimSpace(req.Scope.SourceSet)
	req.Scope.TaskClass = strings.TrimSpace(req.Scope.TaskClass)
	req.Scope.RiskClass = strings.TrimSpace(req.Scope.RiskClass)
	req.Scope.AccessMode = strings.TrimSpace(req.Scope.AccessMode)
	req.Scope.DirectionRequirement = strings.TrimSpace(req.Scope.DirectionRequirement)
	if duplicateNormalizedPaths(req.Scope.Files) ||
		duplicateNormalized(req.Scope.Symbols) ||
		duplicateNormalized(req.Scope.Components) ||
		duplicateNormalized(req.Scope.ClaimIDs) ||
		duplicateNormalized(req.Scope.PropositionKeys) ||
		duplicateNormalized(req.Scope.AdditionalDimensions) {
		return Request{}, errors.New("scope anchors must not duplicate after normalization")
	}
	req.Scope.Files = cleanPathList(req.Scope.Files)
	req.Scope.Symbols = cleanList(req.Scope.Symbols)
	req.Scope.Components = cleanList(req.Scope.Components)
	req.Scope.ClaimIDs = cleanList(req.Scope.ClaimIDs)
	req.Scope.PropositionKeys = cleanList(req.Scope.PropositionKeys)
	req.Scope.AdditionalDimensions = cleanList(req.Scope.AdditionalDimensions)
	if req.DirectionBootstrap != nil {
		auth, err := NormalizeDirectionBootstrap(*req.DirectionBootstrap)
		if err != nil {
			return Request{}, fmt.Errorf("direction_bootstrap: %w", err)
		}
		req.DirectionBootstrap = &auth
	}
	req.RequestedBy = strings.TrimSpace(req.RequestedBy)
	req.Note = strings.TrimSpace(req.Note)
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
	if req.Binding.RepositoryDomain == "" {
		errs = append(errs, "binding repository_domain is required")
	}
	if !oneOf(req.Binding.RevisionStatus, architecture.RevisionResolved, architecture.RevisionUnavailable, architecture.RevisionNotGit, architecture.RevisionNotRequested) {
		errs = append(errs, "binding revision_status is required and must be explicit")
	}
	if !oneOf(req.Binding.GraphDigestStatus, architecture.GraphDigestResolved, architecture.GraphDigestUnavailable, architecture.GraphDigestNotRequested) {
		errs = append(errs, "binding graph_digest_status is required and must be explicit")
	}
	if req.Scope.Domain == "" {
		errs = append(errs, "scope domain is required")
	}
	if req.TaskID != "" && !strings.HasPrefix(req.TaskID, "task.") {
		errs = append(errs, "task_id must use task.* identity")
	}
	if req.Scope.TaskClass == "" {
		errs = append(errs, "scope task_class is required")
	} else if !taskTokenRE.MatchString(req.Scope.TaskClass) {
		errs = append(errs, "scope task_class must be a conservative token")
	}
	if _, ok := PolicyForRisk(req.Scope.RiskClass); !ok {
		errs = append(errs, "scope risk_class is unknown")
	}
	if !oneOf(req.Scope.AccessMode, AccessRead, AccessWrite, AccessReadWrite, AccessUnknown) {
		errs = append(errs, "scope access_mode is unknown")
	}
	if !oneOf(req.Scope.DirectionRequirement, DirectionPreserve, DirectionEvolve, DirectionMigrate, DirectionNotApplicable, DirectionUnknown) {
		errs = append(errs, "scope direction_requirement is unknown")
	}
	anchors := len(req.Scope.Files) + len(req.Scope.Symbols) + len(req.Scope.Components) + len(req.Scope.ClaimIDs) + len(req.Scope.PropositionKeys)
	if anchors == 0 && !req.Scope.DomainWide {
		errs = append(errs, "scope requires at least one anchor or domain_wide")
	}
	for _, f := range req.Scope.Files {
		if filepath.IsAbs(f) || f == ".." || strings.HasPrefix(f, "../") || strings.Contains(f, "/../") {
			errs = append(errs, "scope file path must be repository-relative and non-escaping")
			break
		}
	}
	for _, id := range req.Scope.ClaimIDs {
		if id == "" {
			errs = append(errs, "scope claim_ids must not contain empty values")
			break
		}
	}
	for _, key := range req.Scope.PropositionKeys {
		if key == "" {
			errs = append(errs, "scope proposition_keys must not contain empty values")
			break
		}
	}
	for _, dim := range req.Scope.AdditionalDimensions {
		if !isDimension(dim) {
			errs = append(errs, "additional_required_dimensions contains unknown dimension")
			break
		}
	}
	if hasDuplicates(req.Scope.Files) || hasDuplicates(req.Scope.Symbols) || hasDuplicates(req.Scope.Components) ||
		hasDuplicates(req.Scope.ClaimIDs) || hasDuplicates(req.Scope.PropositionKeys) || hasDuplicates(req.Scope.AdditionalDimensions) {
		errs = append(errs, "scope anchors must not duplicate after normalization")
	}
	if req.Scope.DomainWide && anchors > 0 && req.Scope.Domain != "repository" && req.Scope.Domain != req.Binding.RepositoryDomain {
		errs = append(errs, "domain_wide scope must not narrow another domain")
	}
	if req.Binding.RepositoryDomain != "" && req.Scope.Domain != "" && req.Scope.Domain != "repository" && req.Scope.Domain != req.Binding.RepositoryDomain {
		errs = append(errs, "binding repository domain conflicts with scope domain")
	}
	if req.DirectionBootstrap != nil {
		if req.TaskID == "" {
			errs = append(errs, "task_id is required when direction_bootstrap is present")
		} else if req.DirectionBootstrap.TaskID != req.TaskID {
			errs = append(errs, "direction_bootstrap task_id must match request task_id")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func NormalizeDirectionBootstrap(in DirectionBootstrapAuthorization) (DirectionBootstrapAuthorization, error) {
	auth := in
	auth.SchemaVersion = strings.TrimSpace(auth.SchemaVersion)
	if auth.SchemaVersion == "" {
		auth.SchemaVersion = DirectionBootstrapSchemaVersion
	}
	auth.PolicyID = strings.TrimSpace(auth.PolicyID)
	if auth.PolicyID == "" {
		auth.PolicyID = DirectionBootstrapPolicyID
	}
	auth.TaskID = strings.TrimSpace(auth.TaskID)
	auth.BaseRevision = strings.TrimSpace(strings.ToLower(auth.BaseRevision))
	auth.GraphDigestSHA256 = strings.TrimSpace(strings.ToLower(auth.GraphDigestSHA256))
	auth.File = normalizeSinglePath(auth.File)
	auth.GovernedRecordIDs = cleanList(auth.GovernedRecordIDs)
	auth.ExpectedMutationDigestSHA256 = strings.TrimSpace(strings.ToLower(auth.ExpectedMutationDigestSHA256))
	auth.ApprovedBy = strings.TrimSpace(auth.ApprovedBy)
	auth.ApprovalMechanism = strings.TrimSpace(auth.ApprovalMechanism)
	auth.ApprovalStatement = strings.TrimSpace(auth.ApprovalStatement)
	auth.UsagePolicy = strings.TrimSpace(auth.UsagePolicy)
	if auth.UsagePolicy == "" {
		auth.UsagePolicy = DirectionBootstrapUsageOneUse
	}
	auth.IssuedAt = strings.TrimSpace(auth.IssuedAt)
	auth.ExpiresAt = strings.TrimSpace(auth.ExpiresAt)
	auth.ApprovalSourcePath = strings.TrimSpace(auth.ApprovalSourcePath)
	if auth.ApprovalSourcePath != "" {
		auth.ApprovalSourcePath = filepath.Clean(auth.ApprovalSourcePath)
	}
	auth.ApprovalSourceDigestSHA256 = strings.TrimSpace(strings.ToLower(auth.ApprovalSourceDigestSHA256))
	auth.AuthorizationDigestSHA256 = strings.TrimSpace(strings.ToLower(auth.AuthorizationDigestSHA256))
	if err := ValidateDirectionBootstrap(auth); err != nil {
		return DirectionBootstrapAuthorization{}, err
	}
	return auth, nil
}

func ValidateDirectionBootstrap(auth DirectionBootstrapAuthorization) error {
	var errs []string
	if auth.SchemaVersion != DirectionBootstrapSchemaVersion {
		errs = append(errs, "unsupported schema_version")
	}
	if auth.PolicyID != DirectionBootstrapPolicyID {
		errs = append(errs, "policy_id is unknown")
	}
	if auth.TaskID == "" {
		errs = append(errs, "task_id is required")
	}
	if !isHexLen(auth.BaseRevision, 40) {
		errs = append(errs, "base_revision must be lowercase git sha")
	}
	if !isSHA256(auth.GraphDigestSHA256) {
		errs = append(errs, "graph_digest_sha256 must be lowercase SHA-256")
	}
	if auth.File == "" || !safeRelPath(auth.File) {
		errs = append(errs, "file must be repository-relative and non-escaping")
	}
	if len(auth.GovernedRecordIDs) == 0 {
		errs = append(errs, "governed_record_ids is required")
	}
	if !isSHA256(auth.ExpectedMutationDigestSHA256) {
		errs = append(errs, "expected_mutation_digest_sha256 must be lowercase SHA-256")
	}
	if auth.ApprovedBy == "" {
		errs = append(errs, "approved_by is required")
	}
	if auth.ApprovalMechanism == "" {
		errs = append(errs, "approval_mechanism is required")
	}
	if auth.ApprovalStatement == "" {
		errs = append(errs, "approval_statement is required")
	}
	if auth.UsagePolicy != DirectionBootstrapUsageOneUse {
		errs = append(errs, "usage_policy must be one_use")
	}
	if auth.ApprovalMechanism != DirectionBootstrapMechanismFile {
		errs = append(errs, "approval_mechanism is unknown")
	}
	if auth.ApprovalSourceDigestSHA256 != "" && !isSHA256(auth.ApprovalSourceDigestSHA256) {
		errs = append(errs, "approval_source_digest_sha256 must be lowercase SHA-256")
	}
	if auth.AuthorizationDigestSHA256 != "" && !isSHA256(auth.AuthorizationDigestSHA256) {
		errs = append(errs, "authorization_digest_sha256 must be lowercase SHA-256")
	}
	issued, issuedErr := time.Parse(time.RFC3339, auth.IssuedAt)
	expires, expiresErr := time.Parse(time.RFC3339, auth.ExpiresAt)
	if issuedErr != nil {
		errs = append(errs, "issued_at must be RFC3339")
	}
	if expiresErr != nil {
		errs = append(errs, "expires_at must be RFC3339")
	}
	if issuedErr == nil && expiresErr == nil && !expires.After(issued) {
		errs = append(errs, "expires_at must be after issued_at")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func DirectionBootstrapAuthorizationDigest(auth DirectionBootstrapAuthorization) string {
	auth, _ = NormalizeDirectionBootstrap(auth)
	auth.AuthorizationDigestSHA256 = ""
	return digest(canonicalJSON(auth))
}

func ValidateDirectionBootstrapApproval(auth DirectionBootstrapAuthorization, repoRoot string) error {
	auth, err := NormalizeDirectionBootstrap(auth)
	if err != nil {
		return err
	}
	if auth.ApprovalSourcePath == "" {
		return errors.New("approval_source_path is required")
	}
	if !filepath.IsAbs(auth.ApprovalSourcePath) {
		return errors.New("approval_source_path must be absolute")
	}
	if !isSHA256(auth.ApprovalSourceDigestSHA256) {
		return errors.New("approval_source_digest_sha256 must be lowercase SHA-256")
	}
	if !isSHA256(auth.AuthorizationDigestSHA256) {
		return errors.New("authorization_digest_sha256 must be lowercase SHA-256")
	}
	sourcePath, err := filepath.EvalSymlinks(auth.ApprovalSourcePath)
	if err != nil {
		return fmt.Errorf("approval_source_path: %w", err)
	}
	repoPath, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return fmt.Errorf("repository root: %w", err)
	}
	if pathWithinRoot(repoPath, sourcePath) {
		return errors.New("approval_source_path must be outside the repository root")
	}
	trustedRoot, err := trustedBootstrapApprovalRoot()
	if err != nil {
		return err
	}
	if !pathWithinRoot(trustedRoot, sourcePath) {
		return fmt.Errorf("approval_source_path must be inside trusted approval root %s", trustedRoot)
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("approval_source_path must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("approval_source_path must be a regular file")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("approval_source_path must not be group/world writable")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if digest(data) != auth.ApprovalSourceDigestSHA256 {
		return errors.New("approval source digest mismatch")
	}
	if DirectionBootstrapAuthorizationDigest(auth) != auth.AuthorizationDigestSHA256 {
		return errors.New("authorization digest mismatch")
	}
	return nil
}

func DirectionBootstrapForRequest(req Request, now time.Time) (*DirectionBootstrapAuthorization, error) {
	if req.DirectionBootstrap == nil {
		return nil, nil
	}
	auth, err := NormalizeDirectionBootstrap(*req.DirectionBootstrap)
	if err != nil {
		return nil, err
	}
	if req.TaskID == "" {
		return nil, errors.New("request task_id is required")
	}
	if auth.TaskID != req.TaskID {
		return nil, errors.New("task_id mismatch")
	}
	if auth.BaseRevision != req.Binding.Revision {
		return nil, errors.New("base_revision mismatch")
	}
	if auth.GraphDigestSHA256 != req.Binding.GraphDigestSHA256 {
		return nil, errors.New("graph_digest_sha256 mismatch")
	}
	if auth.File != DirectionBootstrapFile {
		return nil, fmt.Errorf("file must be %s", DirectionBootstrapFile)
	}
	if auth.ApprovalSourcePath == "" || !filepath.IsAbs(auth.ApprovalSourcePath) {
		return nil, errors.New("approval_source_path is required")
	}
	if !isSHA256(auth.ApprovalSourceDigestSHA256) {
		return nil, errors.New("approval_source_digest_sha256 is required")
	}
	if !isSHA256(auth.AuthorizationDigestSHA256) {
		return nil, errors.New("authorization_digest_sha256 is required")
	}
	if DirectionBootstrapAuthorizationDigest(auth) != auth.AuthorizationDigestSHA256 {
		return nil, errors.New("authorization_digest_sha256 mismatch")
	}
	if len(req.Scope.Files) != 1 || !contains(req.Scope.Files, auth.File) {
		return nil, errors.New("request scope must contain only the authorized file")
	}
	expiresAt, _ := time.Parse(time.RFC3339, auth.ExpiresAt)
	if !now.IsZero() && !now.Before(expiresAt) {
		return nil, errors.New("authorization expired")
	}
	return &auth, nil
}

func pathWithinRoot(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == "" || target == "" {
		return false
	}
	if root == target {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

func trustedBootstrapApprovalRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(DirectionBootstrapApprovalDirEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve trusted bootstrap approval root: %w", err)
		}
		root = filepath.Join(home, ".sensei", "approvals")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve trusted bootstrap approval root: %w", err)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("trusted bootstrap approval root must not be a symlink")
	}
	if !info.IsDir() {
		return "", errors.New("trusted bootstrap approval root must be a directory")
	}
	return filepath.Clean(resolved), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func DefaultPolicies() ([]Policy, error) {
	return []Policy{
		{
			RiskClass: RiskLowRisk, RequiredDimensions: []string{DimensionStructural, DimensionEvidence, DimensionContradiction, DimensionAgent},
			ConditionalAllowed: true, ConditionalDimensions: []string{DimensionStructural, DimensionContract, DimensionBehavioral, DimensionDirection, DimensionAgent},
			AcceptedUnknownMaxPriority: "medium", AllowsNotApplicableDirection: true,
		},
		{
			RiskClass: RiskArchitectureSensitive, RequiredDimensions: append([]string{}, DimensionOrder...),
			ConditionalAllowed: true, ConditionalDimensions: []string{DimensionStructural, DimensionContract, DimensionBehavioral, DimensionDirection, DimensionAgent},
			AcceptedUnknownMaxPriority: "medium", RequiresCurrentEvidence: true, RequiresGovernedDirection: true, RequiresFailureSurface: true,
		},
		{RiskClass: RiskConvergence, RequiredDimensions: append([]string{}, DimensionOrder...), RequiresCurrentEvidence: true, RequiresGovernedDirection: true, RequiresFailureSurface: true},
		{RiskClass: RiskSecurity, RequiredDimensions: append([]string{}, DimensionOrder...), RequiresCurrentEvidence: true, RequiresGovernedDirection: true, RequiresFailureSurface: true},
		{RiskClass: RiskDataLoss, RequiredDimensions: append([]string{}, DimensionOrder...), RequiresCurrentEvidence: true, RequiresGovernedDirection: true, RequiresFailureSurface: true},
		{RiskClass: RiskUnknownImpact, RequiredDimensions: append([]string{}, DimensionOrder...), KnownLimitations: []string{"closure.risk.unknown"}},
	}, nil
}

func PolicyForRisk(risk string) (Policy, bool) {
	policies, _ := DefaultPolicies()
	for _, p := range policies {
		if p.RiskClass == strings.TrimSpace(risk) {
			return p, true
		}
	}
	return Policy{}, false
}

type Node struct {
	IRI                     string
	ID                      string
	Classes                 []string
	Label                   string
	Comment                 string
	Status                  string
	PromotionStatus         string
	ReviewStatus            string
	SourceKind              string
	Severity                string
	Kind                    string
	SourcePath              string
	AuthoredIn              []string
	AnchoredIn              []string
	CoversPath              []string
	OwnerServices           []string
	OwnsStates              []string
	MayWrite                []string
	MayRead                 []string
	MustMutateVia           []string
	MustReadVia             []string
	ObservesVia             []string
	TruthLayers             []string
	ForbidsBypass           []string
	DependsOn               []string
	ReadsFrom               []string
	WritesTo                []string
	ProtectedByBoundaries   []string
	ExposesContracts        []string
	Separates               []string
	ExposedBy               []string
	ConsumedBy              []string
	ConstrainedByInvariants []string
	RequiresTests           []string
	SupportedByEvidence     []string
	Forbids                 []string
	VulnerableTo            []string
	ReadOrWrite             string
	Stability               string
	ArchitecturalPlane      string
}

type GraphIndex struct {
	Nodes       map[string]Node
	NodesByID   map[string]string
	FilesByPath map[string]string
	SymbolsByID map[string]string
}

func LoadGraphIndex(path string) (GraphIndex, error) {
	triples, err := graphsnapshot.Load(path)
	if err != nil {
		return GraphIndex{}, err
	}
	return BuildGraphIndex(triples), nil
}

func BuildGraphIndex(triples []graphsnapshot.Triple) GraphIndex {
	idx := GraphIndex{Nodes: map[string]Node{}, NodesByID: map[string]string{}, FilesByPath: map[string]string{}, SymbolsByID: map[string]string{}}
	classes := map[string]map[string]bool{}
	for _, t := range triples {
		if t.Predicate == rdf.PropType && t.ObjectIsIRI {
			c := indexedClass(t.Object)
			if c == "" {
				continue
			}
			if classes[t.Subject] == nil {
				classes[t.Subject] = map[string]bool{}
			}
			classes[t.Subject][c] = true
		}
	}
	for iri, set := range classes {
		n := Node{IRI: iri, Classes: sortedClassSet(set)}
		n.ID = nodeID(iri, n.Classes)
		idx.Nodes[iri] = n
		idx.NodesByID[n.ID] = iri
		if len(n.Classes) > 0 {
			idx.NodesByID[n.Classes[0]+":"+n.ID] = iri
		}
	}
	for _, t := range triples {
		n, ok := idx.Nodes[t.Subject]
		if !ok {
			continue
		}
		lit := strings.TrimSpace(t.Object)
		obj := objectIDFromObject(t.Object, t.ObjectIsIRI)
		switch t.Predicate {
		case rdf.PropLabel:
			if !t.ObjectIsIRI {
				n.Label = lit
			}
		case rdf.PropComment:
			if !t.ObjectIsIRI {
				n.Comment = lit
			}
		case rdf.PropStatus:
			if !t.ObjectIsIRI {
				n.Status = strings.ToLower(lit)
			}
		case rdf.PropPromotionStatus:
			if !t.ObjectIsIRI {
				n.PromotionStatus = strings.ToLower(lit)
			}
		case rdf.PropReviewStatus:
			if !t.ObjectIsIRI {
				n.ReviewStatus = strings.ToLower(lit)
			}
		case rdf.PropSourceKind:
			if !t.ObjectIsIRI {
				n.SourceKind = strings.ToLower(lit)
			}
		case rdf.PropSeverity:
			if !t.ObjectIsIRI {
				n.Severity = strings.ToLower(lit)
			}
		case rdf.PropKind:
			if !t.ObjectIsIRI {
				n.Kind = lit
			}
		case rdf.PropSourcePath:
			if !t.ObjectIsIRI {
				n.SourcePath = normalizePath(lit)
			}
		case rdf.PropAuthoredIn:
			if !t.ObjectIsIRI {
				n.AuthoredIn = append(n.AuthoredIn, normalizePath(lit))
			}
		case rdf.PropAnchoredIn:
			n.AnchoredIn = append(n.AnchoredIn, obj)
		case rdf.PropCoversPath:
			if !t.ObjectIsIRI {
				n.CoversPath = append(n.CoversPath, normalizePath(lit))
			}
		case rdf.PropOwnerService:
			n.OwnerServices = append(n.OwnerServices, obj)
		case rdf.PropOwnsState:
			n.OwnsStates = append(n.OwnsStates, obj)
		case rdf.PropMayWrite:
			n.MayWrite = append(n.MayWrite, obj)
		case rdf.PropMayRead:
			n.MayRead = append(n.MayRead, obj)
		case rdf.PropMustMutateVia:
			n.MustMutateVia = append(n.MustMutateVia, obj)
		case rdf.PropMustReadVia:
			n.MustReadVia = append(n.MustReadVia, obj)
		case rdf.PropObservesVia:
			n.ObservesVia = append(n.ObservesVia, obj)
		case rdf.PropHasTruthLayer:
			n.TruthLayers = append(n.TruthLayers, obj)
		case rdf.PropForbidsBypass:
			n.ForbidsBypass = append(n.ForbidsBypass, obj)
		case rdf.PropDependsOn:
			n.DependsOn = append(n.DependsOn, obj)
		case rdf.PropReadsFrom:
			n.ReadsFrom = append(n.ReadsFrom, obj)
		case rdf.PropWritesTo:
			n.WritesTo = append(n.WritesTo, obj)
		case rdf.PropProtectedByBoundary:
			n.ProtectedByBoundaries = append(n.ProtectedByBoundaries, obj)
		case rdf.PropExposesContract:
			n.ExposesContracts = append(n.ExposesContracts, obj)
		case rdf.PropSeparates:
			n.Separates = append(n.Separates, obj)
		case rdf.PropExposedBy:
			n.ExposedBy = append(n.ExposedBy, obj)
		case rdf.PropConsumedBy:
			n.ConsumedBy = append(n.ConsumedBy, obj)
		case rdf.PropConstrainedByInvariant:
			n.ConstrainedByInvariants = append(n.ConstrainedByInvariants, obj)
		case rdf.PropRequiresTest:
			n.RequiresTests = append(n.RequiresTests, obj)
		case rdf.PropSupportedByEvidence:
			n.SupportedByEvidence = append(n.SupportedByEvidence, obj)
		case rdf.PropForbids:
			n.Forbids = append(n.Forbids, obj)
		case rdf.PropVulnerableTo:
			n.VulnerableTo = append(n.VulnerableTo, obj)
		case rdf.PropReadOrWrite:
			if !t.ObjectIsIRI {
				n.ReadOrWrite = strings.ToLower(lit)
			}
		case rdf.PropStability:
			if !t.ObjectIsIRI {
				n.Stability = strings.ToLower(lit)
			}
		case rdf.PropArchitecturalPlane:
			if !t.ObjectIsIRI {
				n.ArchitecturalPlane = strings.ToLower(lit)
			}
		}
		idx.Nodes[t.Subject] = normalizeNode(n)
	}
	for iri, n := range idx.Nodes {
		if hasClass(n, "source_file") {
			n.SourcePath = canonicalSourceFilePath(n.IRI, n.SourcePath)
			idx.Nodes[iri] = normalizeNode(n)
		}
		if hasClass(n, "source_file") && n.SourcePath != "" {
			idx.FilesByPath[n.SourcePath] = iri
		}
		if hasClass(n, "code_symbol") || hasClass(n, "symbol") {
			idx.SymbolsByID[n.ID] = iri
		}
	}
	return idx
}

func canonicalSourceFilePath(iri, explicit string) string {
	if explicit != "" {
		return normalizePath(explicit)
	}
	prefix := strings.TrimSuffix(strings.Trim(rdf.MintIRI(rdf.ClassSourceFile, ""), "<>"), "/") + "/"
	if !strings.HasPrefix(iri, prefix) {
		return ""
	}
	encoded := strings.TrimPrefix(iri, prefix)
	if encoded == "" {
		return ""
	}
	decoded := normalizePath(rdf.DecodeIRIPath(encoded))
	if decoded == "" || decoded == "." || filepath.IsAbs(decoded) || strings.HasPrefix(decoded, "../") || strings.Contains(decoded, "/../") || decoded == ".." {
		return ""
	}
	if strings.Trim(rdf.MintIRI(rdf.ClassSourceFile, decoded), "<>") != iri {
		return ""
	}
	return decoded
}

func Evaluate(ctx Context) (Report, error) {
	req, err := NormalizeRequest(ctx.Request)
	if err != nil {
		return Report{}, err
	}
	ctx.Request = req
	if ctx.MissingInputs == nil {
		ctx.MissingInputs = map[string]bool{}
	}
	policy, ok := PolicyForRisk(req.Scope.RiskClass)
	if !ok {
		policy = Policy{RiskClass: RiskUnknownImpact, RequiredDimensions: append([]string{}, DimensionOrder...)}
	}
	resolved := resolveScope(ctx)
	builder := &assessmentBuilder{ctx: ctx, policy: policy, scope: resolved}
	builder.verifyBindings()
	builder.evaluateQuestions()
	builder.evaluateClaimConsistency()
	for _, dim := range DimensionOrder {
		builder.evaluateDimension(dim)
	}
	report := Report{
		SchemaVersion:   SchemaVersion,
		GeneratedBy:     GeneratedBy,
		Request:         req,
		ObservedBinding: observedBinding(ctx),
		ScopeReceipt:    resolved.Receipt,
		Dimensions:      builder.dimensions,
		Blockers:        builder.blockers,
		Conditions:      builder.conditions,
		RelevantClaims:  claimReceipts(resolved.Claims, builder.planeByClaim),
		RelevantNodes:   nodeReceipts(resolved.Nodes),
		Questions:       builder.questionReceipts,
		Limitations:     builder.limitations(),
	}
	report.Verdict = calculateVerdict(report, policy)
	return normalizeReport(report), nil
}

type resolvedScope struct {
	Claims       []architecture.Claim
	Nodes        []Node
	ByClaimID    map[string]architecture.Claim
	ByNodeID     map[string]Node
	Receipt      ScopeReceipt
	EmptySurface bool
}

type assessmentBuilder struct {
	ctx              Context
	policy           Policy
	scope            resolvedScope
	authority        authorityProjection
	authorityReady   bool
	behavioral       behavioralProjection
	behavioralReady  bool
	failureSurface   failureSurfaceProjection
	failureReady     bool
	blockers         []Blocker
	conditions       []Condition
	dimensions       []DimensionAssessment
	questionReceipts []QuestionReceipt
	globalReasons    []Reason
	maintByClaim     map[string]maintenance.ClaimEvaluation
	planeByClaim     map[string]plane.ClaimAssessment
	questionsByClaim map[string][]architecture.OpenQuestion
	dimQuestionIDs   map[string][]string
	uncertifiable    map[string]bool
}

func (b *assessmentBuilder) authorityProjection() authorityProjection {
	if !b.authorityReady {
		b.authority = projectApplicableAuthority(b.ctx.Request.Scope, b.scope, b.ctx.Graph)
		b.authorityReady = true
	}
	return b.authority
}

func (b *assessmentBuilder) behavioralProjection() behavioralProjection {
	if !b.behavioralReady {
		b.behavioral = projectApplicableBehavioral(b.ctx.Request.Scope, b.scope, factReceiptsByID(b.ctx.Claims), b.planeByClaim)
		b.behavioralReady = true
	}
	return b.behavioral
}

func (b *assessmentBuilder) failureSurfaceProjection() failureSurfaceProjection {
	if !b.failureReady {
		b.failureSurface = projectApplicableFailureModes(b.ctx.Request.Scope, b.scope, b.ctx.Graph)
		b.failureReady = true
	}
	return b.failureSurface
}

func (b *assessmentBuilder) verifyBindings() {
	b.maintByClaim = map[string]maintenance.ClaimEvaluation{}
	b.planeByClaim = map[string]plane.ClaimAssessment{}
	b.questionsByClaim = map[string][]architecture.OpenQuestion{}
	b.dimQuestionIDs = map[string][]string{}
	b.uncertifiable = map[string]bool{}
	req := b.ctx.Request.Binding
	if !bindingResolved(req) {
		b.addUncertifiable(DimensionEvidence, "closure.binding.request_unresolved", "request binding is not resolved", "provide_input")
	}
	check := func(name string, binding architecture.ClaimDocumentBinding, code string) {
		if !bindingResolved(binding) {
			b.addUncertifiable(DimensionEvidence, code, name+" binding is not resolved", "provide_input")
			return
		}
		if !bindingsEqual(req, binding) {
			b.addUncertifiable(DimensionEvidence, code, name+" binding does not match request binding", "repair_binding")
		}
	}
	check("claim document", b.ctx.Claims.Binding, "closure.binding.request_claim_mismatch")
	if b.ctx.Maintenance == nil {
		b.addUncertifiable(DimensionEvidence, "closure.input.maintenance_missing", "maintenance report was not supplied", "provide_input")
	} else {
		check("maintenance current", b.ctx.Maintenance.CurrentBinding, "closure.binding.maintenance_mismatch")
		check("maintenance observed", b.ctx.Maintenance.ObservedBinding, "closure.binding.maintenance_mismatch")
		for _, ev := range b.ctx.Maintenance.ClaimEvaluations {
			b.maintByClaim[ev.ClaimID] = ev
		}
	}
	if b.ctx.Plane == nil {
		b.addUncertifiable(DimensionEvidence, "closure.input.plane_assessment_missing", "plane assessment was not supplied", "provide_input")
	} else {
		check("plane claim", claimReportBinding(b.ctx.Plane.ClaimBinding), "closure.binding.plane_mismatch")
		if b.ctx.Plane.GraphSnapshot.DigestStatus != architecture.GraphDigestResolved || b.ctx.Plane.GraphSnapshot.DigestSHA256 != req.GraphDigestSHA256 {
			b.addUncertifiable(DimensionEvidence, "closure.binding.plane_mismatch", "plane graph snapshot digest does not match request", "repair_binding")
		}
		for _, a := range b.ctx.Plane.ClaimAssessments {
			b.planeByClaim[a.ClaimID] = a
		}
	}
	if b.ctx.Dialogue == nil {
		b.addUncertifiable(DimensionEvidence, "closure.input.dialogue_missing", "dialogue document was not supplied", "provide_input")
	} else {
		check("dialogue", b.ctx.Dialogue.Binding, "closure.binding.dialogue_mismatch")
	}
	if b.ctx.Evidence == nil {
		b.addUncertifiable(DimensionEvidence, "closure.input.evidence_state_missing", "evidence-state document was not supplied", "provide_input")
	} else {
		check("evidence-state", b.ctx.Evidence.Binding, "closure.binding.evidence_mismatch")
	}
	if b.ctx.GraphReceipt.Status != architecture.GraphDigestResolved || !b.ctx.GraphReceipt.Verified {
		b.addUncertifiable(DimensionEvidence, "closure.binding.graph_digest_unverified", "graph snapshot digest is not verified", "provide_input")
	} else if b.ctx.GraphReceipt.DigestSHA256 != req.GraphDigestSHA256 {
		b.addUncertifiable(DimensionEvidence, "closure.binding.graph_digest_unverified", "graph snapshot digest does not match request", "repair_binding")
	}
	if b.ctx.RepositoryStatus != architecture.RevisionResolved || b.ctx.RepositoryRev == "" || req.RevisionStatus != architecture.RevisionResolved || req.Revision == "" {
		b.addUncertifiable(DimensionEvidence, "closure.binding.repository_revision_unverified", "repository revision is not verified", "provide_input")
	} else if b.ctx.RepositoryRev != req.Revision {
		b.addUncertifiable(DimensionEvidence, "closure.binding.repository_revision_unverified", "repository checkout revision does not match request", "repair_binding")
	}
}

func (b *assessmentBuilder) evaluateClaimConsistency() {
	for _, c := range b.scope.Claims {
		if ev, ok := b.maintByClaim[c.ID]; ok && ev.EvaluatedStatus != c.EpistemicStatus {
			b.addUncertifiable(DimensionEvidence, "closure.binding.claim_status_mismatch", "claim document status does not match maintenance evaluated status", "repair_binding", c.ID)
		}
		a, ok := b.planeByClaim[c.ID]
		if !ok {
			b.addOpen(DimensionEvidence, "high", "closure.evidence.plane_under_supported", "relevant claim has no plane assessment", "repair_claim", []string{c.ID}, nil, nil, nil)
			continue
		}
		if a.EpistemicStatus != c.EpistemicStatus || a.PromotionStatus != c.PromotionStatus {
			b.addUncertifiable(DimensionEvidence, "closure.binding.plane_mismatch", "plane assessment status does not match claim document", "repair_binding", c.ID)
		}
		if a.PropositionKey != plane.PropositionKey(c) {
			b.addUncertifiable(DimensionEvidence, "closure.binding.plane_proposition_mismatch", "plane proposition key does not match recomputed key", "repair_binding", c.ID)
		}
	}
}

func (b *assessmentBuilder) evaluateQuestions() {
	if b.ctx.Dialogue == nil {
		return
	}
	byID := map[string]architecture.OpenQuestion{}
	for _, q := range b.ctx.Dialogue.OpenQuestions {
		byID[q.ID] = q
	}
	for _, q := range b.ctx.Dialogue.OpenQuestions {
		if !questionRelevant(q, b.scope) {
			continue
		}
		b.questionReceipts = append(b.questionReceipts, QuestionReceipt{
			ID: q.ID, Status: q.Status, Priority: q.Priority,
			Dimensions: cleanList([]string{q.BlocksClosureDimension}),
			ClaimIDs:   cleanList(q.BlocksClaims), NodeIDs: cleanList(q.BlocksNodes),
			BlockerIDs: cleanList(q.BlocksClosureBlockers), TemplateID: q.QuestionTemplateID,
		})
		for _, claimID := range q.BlocksClaims {
			b.questionsByClaim[claimID] = append(b.questionsByClaim[claimID], q)
		}
		if q.Status == architecture.QuestionStatusSuperseded {
			if _, ok := byID[q.SupersededByQuestion]; !ok {
				b.addUncertifiable(q.BlocksClosureDimension, "closure.question.superseding_missing", "superseded question replacement is missing", "provide_input", "", q.ID)
			}
			continue
		}
		if q.Status == architecture.QuestionStatusResolved {
			continue
		}
		dim := q.BlocksClosureDimension
		if q.Status == architecture.QuestionStatusAcceptedUnknown {
			if questionPriorityBlocks(q.Priority) || !b.conditionAllowed(dim, q) {
				b.addOpen(dim, severityForPriority(q.Priority), "closure.question.accepted_unknown_blocks", "accepted unknown remains blocking for this policy", "answer_open_question", nil, nil, []string{q.ID}, nil)
			} else {
				b.addCondition(dim, "closure.question.accepted_unknown", "accepted unknown remains visible", q.ID)
			}
			continue
		}
		if oneOf(q.Status, architecture.QuestionStatusOpen, architecture.QuestionStatusAwaitingArchitect, architecture.QuestionStatusAwaitingEvidence, architecture.QuestionStatusAnswered) {
			b.addOpen(dim, severityForPriority(q.Priority), "closure."+dim+".question_unresolved", "question remains unresolved", "answer_open_question", nil, nil, []string{q.ID}, nil)
		}
	}
}

func (b *assessmentBuilder) evaluateDimension(dim string) {
	required := b.dimensionRequired(dim)
	applicable := required || b.dimensionApplicable(dim)
	state := StateClosed
	var reasons []Reason
	if !applicable {
		state = StateNotApplicable
		reasons = append(reasons, Reason{Code: "closure." + dim + ".not_applicable"})
	}
	if applicable {
		switch dim {
		case DimensionStructural:
			b.evalStructural()
		case DimensionAuthority:
			b.evalAuthority()
		case DimensionContract:
			b.evalContract()
		case DimensionBehavioral:
			b.evalBehavioral()
		case DimensionEvidence:
			b.evalEvidence()
		case DimensionContradiction:
			b.evalContradiction()
		case DimensionDirection:
			b.evalDirection()
		case DimensionAgent:
			b.evalAgent()
		}
		b.applyMachineAdoptedRiskPolicy(dim)
	}
	blockers := b.blockerIDsFor(dim)
	conditions := b.conditionIDsFor(dim)
	if applicable {
		if b.hasUncertifiable(dim) {
			state = StateUncertifiable
		} else if len(blockers) > 0 {
			state = StateOpen
		} else if len(conditions) > 0 {
			state = StateConditional
		} else {
			state = StateClosed
			reasons = append(reasons, Reason{Code: "closure." + dim + ".closed"})
		}
	}
	b.dimensions = append(b.dimensions, DimensionAssessment{
		Dimension: dim, Required: required, Applicable: applicable, State: state,
		Reasons: reasons, BlockerIDs: blockers, ConditionIDs: conditions,
	})
}

func (b *assessmentBuilder) applyMachineAdoptedRiskPolicy(dim string) {
	nodeIDs := machineAdoptedNodeIDsForDimension(b.scope.Nodes, dim)
	if len(nodeIDs) == 0 {
		return
	}
	switch b.ctx.Request.Scope.RiskClass {
	case RiskArchitectureSensitive:
		if !b.policy.ConditionalAllowed || !contains(b.policy.ConditionalDimensions, dim) {
			return
		}
		condition := Condition{
			Dimension: dim, Code: "closure.machine_adopted." + dim + ".conditional",
			Summary:            "task-relevant machine-adopted knowledge is usable under class-specific policy but remains not human-governed",
			RequiredNextAction: "review_machine_adopted_knowledge",
		}
		condition.ID = conditionID(condition)
		b.conditions = append(b.conditions, condition)
	case RiskConvergence, RiskSecurity, RiskDataLoss:
		b.addOpen(dim, "high", "closure.machine_adopted."+dim+".stronger_basis_required",
			"high-risk scope requires stronger Evidence, governed knowledge, or explicit delegated policy",
			"add_evidence_or_govern_knowledge", nil, nodeIDs, nil, nil)
	}
}

func machineAdoptedNodeIDsForDimension(nodes []Node, dim string) []string {
	var ids []string
	for _, node := range nodes {
		if node.Status == "machine_adopted" && machineAdoptedClassApplies(node, dim) {
			ids = append(ids, node.ID)
		}
	}
	return cleanList(ids)
}

func machineAdoptedClassApplies(node Node, dim string) bool {
	switch dim {
	case DimensionStructural:
		return hasClass(node, "boundary") || hasClass(node, "contract")
	case DimensionAuthority:
		return hasClass(node, "boundary")
	case DimensionContract:
		return hasClass(node, "contract") || hasClass(node, "boundary")
	case DimensionBehavioral:
		return hasClass(node, "invariant") || hasClass(node, "failure_mode") || hasClass(node, "forbidden_fix") ||
			hasClass(node, "contract") || hasClass(node, "decision") || hasClass(node, "incident")
	case DimensionEvidence:
		return hasClass(node, "incident")
	case DimensionDirection:
		return hasClass(node, "intent") || hasClass(node, "decision") || hasClass(node, "contract") || hasClass(node, "invariant")
	default:
		return false
	}
}

func (b *assessmentBuilder) evalStructural() {
	req := b.ctx.Request
	if len(req.Scope.Files)+len(req.Scope.Symbols)+len(req.Scope.Components)+len(req.Scope.ClaimIDs)+len(req.Scope.PropositionKeys) == 0 && !req.Scope.DomainWide {
		b.addOpen(DimensionStructural, "critical", "closure.scope.empty_measured_surface", "request scope is empty", "reassess_scope", nil, nil, nil, nil)
	}
	if b.ctx.RepositoryRoot == "" {
		b.addUncertifiable(DimensionStructural, "closure.input.repository_checkout_missing", "repository checkout was not supplied", "provide_input")
	}
	for _, f := range req.Scope.Files {
		if b.ctx.RepositoryRoot != "" {
			if _, err := os.Stat(filepath.Join(b.ctx.RepositoryRoot, filepath.FromSlash(f))); err != nil {
				b.addOpen(DimensionStructural, "high", "closure.structural.file_missing", "requested file does not exist", "reassess_scope", nil, nil, nil, []string{f})
			}
		}
		if !contains(b.scope.Receipt.Files, f) {
			b.addOpen(DimensionStructural, "high", "closure.structural.file_unrepresented", "requested file has no relevant claim or SourceFile/governed anchor", "promote_architectural_knowledge", nil, nil, nil, []string{f})
		}
	}
	for _, s := range req.Scope.Symbols {
		if !contains(b.scope.Receipt.Symbols, s) {
			b.addOpen(DimensionStructural, "high", "closure.structural.symbol_unrepresented", "requested symbol has no relevant claim or CodeSymbol", "promote_architectural_knowledge", nil, nil, nil, nil)
		}
	}
	for _, c := range req.Scope.Components {
		if !contains(b.scope.Receipt.Components, c) {
			b.addOpen(DimensionStructural, "high", "closure.structural.component_missing", "requested component is missing", "promote_architectural_knowledge", nil, []string{c}, nil, nil)
		}
	}
	for _, id := range b.scope.Receipt.MissingClaims {
		b.addOpen(DimensionStructural, "high", "closure.structural.claim_missing", "explicit claim ID is missing", "repair_claim", []string{id}, nil, nil, nil)
	}
	for range b.scope.Receipt.MissingPropositions {
		b.addOpen(DimensionStructural, "high", "closure.structural.proposition_missing", "explicit proposition key is missing", "repair_claim", nil, nil, nil, nil)
	}
	if b.scope.EmptySurface {
		b.addOpen(DimensionStructural, "critical", "closure.scope.empty_measured_surface", "no relevant claim or governed architecture node was found", "reassess_scope", nil, nil, nil, nil)
	}
	if crossWithoutBoundaryOrContract(b.scope.Nodes) {
		b.addOpen(DimensionStructural, "high", "closure.structural.cross_component_boundary_missing", "cross-component dependency lacks a boundary or contract", "define_contract", nil, nil, nil, nil)
	}
}

func (b *assessmentBuilder) evalAuthority() {
	if b.ctx.Request.Scope.AccessMode == AccessUnknown && b.ctx.Request.Scope.RiskClass != RiskLowRisk {
		b.addUncertifiable(DimensionAuthority, "closure.agent.access_mode_unknown", "authority cannot be evaluated with unknown access mode", "provide_input")
		return
	}
	if !b.dimensionApplicable(DimensionAuthority) && b.ctx.Request.Scope.RiskClass == RiskLowRisk {
		return
	}
	proj := b.authorityProjection()
	authNodeIDs := map[string]bool{}
	for _, binding := range proj.Bindings {
		authNodeIDs[binding.AuthorityNodeID] = true
	}
	for _, contradiction := range proj.Contradictions {
		b.addOpen(DimensionAuthority, "critical", "closure.authority.applicable_records_contradict", "applicable authority records disagree", "define_authority", nil, contradiction.AuthorityNodeIDs, nil, cleanPathList([]string{contradiction.TargetFile}))
		for _, id := range contradiction.AuthorityNodeIDs {
			authNodeIDs[id] = true
		}
	}
	for _, gap := range proj.Unmapped {
		b.addOpen(DimensionAuthority, "critical", "closure.authority.state_unmapped", "task reaches governed state but no applicable authority domain was found", "define_authority", nil, gap.SurfaceNodeIDs, nil, cleanPathList([]string{gap.TargetFile}))
	}
	for id := range authNodeIDs {
		n, ok := findNode(b.ctx.Graph, id)
		if !ok {
			continue
		}
		if authorityBindingStatusForNode(n) == authorityStale {
			b.addOpen(DimensionAuthority, "high", "closure.authority.stale", "applicable authority record is stale", "define_authority", nil, []string{n.ID}, nil, nil)
		}
		if len(n.OwnerServices) == 0 {
			b.addOpen(DimensionAuthority, "critical", "closure.authority.owner_missing", "authority domain has no owner service", "define_authority", nil, []string{n.ID}, nil, nil)
		}
		if len(n.OwnerServices) > 1 {
			b.addOpen(DimensionAuthority, "critical", "closure.authority.owner_ambiguous", "authority domain has multiple owner services", "define_authority", nil, []string{n.ID}, nil, nil)
		}
		if len(n.TruthLayers) == 0 {
			b.addOpen(DimensionAuthority, "high", "closure.authority.truth_layer_missing", "authority domain lacks truth layer", "define_authority", nil, []string{n.ID}, nil, nil)
		}
		if oneOf(b.ctx.Request.Scope.AccessMode, AccessWrite, AccessReadWrite) {
			if len(n.MayWrite) == 0 {
				b.addOpen(DimensionAuthority, "high", "closure.authority.allowed_writer_missing", "write scope has no allowed writer", "define_authority", nil, []string{n.ID}, nil, nil)
			}
			if len(n.MustMutateVia) == 0 {
				b.addOpen(DimensionAuthority, "high", "closure.authority.mutation_path_missing", "write scope has no legal mutation path", "define_authority", nil, []string{n.ID}, nil, nil)
			}
		}
		if oneOf(b.ctx.Request.Scope.AccessMode, AccessRead, AccessReadWrite) {
			if len(n.MayRead) == 0 {
				b.addOpen(DimensionAuthority, "high", "closure.authority.allowed_reader_missing", "read scope has no allowed reader", "define_authority", nil, []string{n.ID}, nil, nil)
			}
			if len(n.MustReadVia)+len(n.ObservesVia) == 0 {
				b.addOpen(DimensionAuthority, "high", "closure.authority.read_path_missing", "read scope has no legal read or observation path", "define_authority", nil, []string{n.ID}, nil, nil)
			}
		}
	}
}

func (b *assessmentBuilder) evalContract() {
	if !b.dimensionApplicable(DimensionContract) && b.ctx.Request.Scope.RiskClass == RiskLowRisk {
		return
	}
	contracts := filterNodes(b.scope.Nodes, "contract")
	for _, n := range contracts {
		if n.Stability == "" {
			b.addOpen(DimensionContract, "high", "closure.contract.stability_unknown", "contract stability is unknown", "define_contract", nil, []string{n.ID}, nil, nil)
		}
		if n.Stability == "deprecated" || n.Status == "deprecated" || n.Status == "retired" || n.Status == "historical" || n.Status == "superseded" {
			b.addOpen(DimensionContract, "high", "closure.contract.deprecated", "contract is not current", "define_contract", nil, []string{n.ID}, nil, nil)
		}
		if n.ReadOrWrite == "unknown" {
			b.addOpen(DimensionContract, "medium", "closure.contract.read_write_unknown", "contract read/write semantics are unknown", "define_contract", nil, []string{n.ID}, nil, nil)
		}
		if len(n.ConstrainedByInvariants)+len(n.SupportedByEvidence) == 0 {
			b.addOpen(DimensionContract, "high", "closure.contract.invariant_or_evidence_missing", "contract lacks invariant or current evidence", "define_contract", nil, []string{n.ID}, nil, nil)
		}
		for _, tid := range n.RequiresTests {
			if !b.nodeExists(tid, "test") {
				b.addOpen(DimensionContract, "high", "closure.contract.required_test_missing", "contract required test is missing", "add_test", nil, []string{n.ID}, nil, nil)
			}
		}
	}
	if crossingPresent(b.scope.Nodes) && len(contracts) == 0 {
		b.addOpen(DimensionContract, "high", "closure.contract.crossing_without_contract", "cross-component crossing has no explicit contract", "define_contract", nil, nil, nil, nil)
	}
}

func (b *assessmentBuilder) evalBehavioral() {
	projection := b.behavioralProjection()
	failureSurface := b.failureSurfaceProjection()
	observedOrEnforced := false
	for _, binding := range projection.Applicable {
		c, ok := b.scope.ByClaimID[binding.ClaimID]
		if !ok {
			continue
		}
		if c.ArchitecturalPlane == architecture.PlaneObserved || c.ArchitecturalPlane == architecture.PlaneEnforced {
			observedOrEnforced = true
		}
		switch c.EpistemicStatus {
		case architecture.StatusUnknown:
			b.addOpen(DimensionBehavioral, "high", "closure.behavior.claim_unknown", "behavioral claim is unknown", "create_open_question", []string{c.ID}, nil, nil, []string{binding.TargetFile})
		case architecture.StatusStale:
			b.addOpen(DimensionBehavioral, "high", "closure.behavior.claim_stale", "behavioral claim is stale", "create_open_question", []string{c.ID}, nil, nil, []string{binding.TargetFile})
		case architecture.StatusContested:
			b.addOpen(DimensionBehavioral, "critical", "closure.behavior.claim_contested", "behavioral claim is contested", "resolve_contradiction", []string{c.ID}, nil, nil, []string{binding.TargetFile})
		case architecture.StatusRefuted:
			b.addOpen(DimensionBehavioral, "critical", "closure.behavior.claim_refuted", "behavioral claim is refuted", "repair_claim", []string{c.ID}, nil, nil, []string{binding.TargetFile})
		}
		if a, ok := b.planeByClaim[c.ID]; ok && a.PlaneState != plane.StateJustified {
			b.addOpen(DimensionBehavioral, "high", "closure.behavior.plane_invalid", "behavioral claim plane is not justified", "repair_claim", []string{c.ID}, nil, nil, []string{binding.TargetFile})
		}
	}
	if len(projection.Applicable) == 0 && len(failureSurface.Applicable) == 0 {
		b.addOpen(DimensionBehavioral, "high", "closure.behavior.surface_empty", "no relevant behavioral claim or governed failure surface exists", "repair_claim", nil, nil, nil, nil)
	}
	if !observedOrEnforced && len(failureSurface.Applicable) == 0 {
		b.addOpen(DimensionBehavioral, "high", "closure.behavior.observed_or_enforced_missing", "no observed or enforced claim exists", "add_evidence", nil, nil, nil, nil)
	}
	if b.policy.RequiresFailureSurface && len(failureSurface.Applicable) == 0 {
		b.addOpen(DimensionBehavioral, "medium", "closure.behavior.failure_mode_missing", "high-risk scope lacks relevant failure surface", "add_failure_mode", nil, nil, nil, nil)
	}
	if oneOf(b.ctx.Request.Scope.RiskClass, RiskConvergence, RiskDataLoss) && len(filterNodes(b.scope.Nodes, "repair_plan")) == 0 && !b.hasCurrentTestOrEvidence() {
		b.addOpen(DimensionBehavioral, "high", "closure.behavior.recovery_or_verification_missing", "convergence/data-loss scope lacks recovery or verification path", "add_test", nil, nil, nil, nil)
	}
}

func (b *assessmentBuilder) evalEvidence() {
	for _, c := range b.scope.Claims {
		ev, ok := b.maintByClaim[c.ID]
		if !ok {
			b.addOpen(DimensionEvidence, "high", "closure.evidence.support_missing", "relevant claim has no maintenance evaluation", "add_evidence", []string{c.ID}, nil, nil, nil)
			continue
		}
		if ev.EvaluatedStatus == architecture.StatusUnknown {
			b.addOpen(DimensionEvidence, "high", "closure.evidence.claim_unknown", "claim maintenance status is unknown", "create_open_question", []string{c.ID}, nil, nil, nil)
		}
		if ev.EvaluatedStatus == architecture.StatusStale {
			b.addOpen(DimensionEvidence, "high", "closure.evidence.claim_stale", "claim maintenance status is stale", "add_evidence", []string{c.ID}, nil, nil, nil)
		}
		if ev.ProofLanes.SupportingEvidence.State == maintenance.LaneUnknown || ev.ProofLanes.SupportingEvidence.State == maintenance.LaneStale {
			b.addOpen(DimensionEvidence, "high", "closure.evidence.support_missing", "supporting evidence lane is not current", "add_evidence", []string{c.ID}, nil, nil, nil)
		}
	}
	for _, n := range b.scope.Nodes {
		for _, tid := range n.RequiresTests {
			if !b.nodeExists(tid, "test") {
				b.addOpen(DimensionEvidence, "high", "closure.evidence.required_test_missing", "governed required test is missing", "add_test", nil, []string{n.ID}, nil, nil)
			}
		}
	}
	if oneOf(b.ctx.Request.Scope.RiskClass, RiskConvergence, RiskSecurity, RiskDataLoss) && !b.hasCurrentTestOrEvidence() {
		b.addOpen(DimensionEvidence, "high", "closure.evidence.current_test_or_evidence_missing", "high-risk scope lacks current Test or Evidence", "add_test", nil, nil, nil, nil)
	}
}

func (b *assessmentBuilder) evalContradiction() {
	for _, c := range b.scope.Claims {
		if c.EpistemicStatus == architecture.StatusContested {
			b.addOpen(DimensionContradiction, "critical", "closure.contradiction.claim_contested", "claim is contested", "resolve_contradiction", []string{c.ID}, nil, nil, nil)
		}
		if len(c.ConflictsWith) > 0 {
			b.addOpen(DimensionContradiction, "critical", "closure.contradiction.explicit_conflict", "claim has explicit conflict", "resolve_contradiction", []string{c.ID}, nil, nil, nil)
		}
		if c.EpistemicStatus == architecture.StatusRefuted && c.ArchitecturalPlane == architecture.PlaneIntended {
			b.addOpen(DimensionContradiction, "critical", "closure.contradiction.current_intent_refuted", "current intended claim is refuted", "resolve_contradiction", []string{c.ID}, nil, nil, nil)
		}
		if c.EpistemicStatus == architecture.StatusRefuted && c.ArchitecturalPlane == architecture.PlaneDesired {
			b.addOpen(DimensionContradiction, "critical", "closure.contradiction.current_desired_refuted", "desired claim is refuted", "resolve_contradiction", []string{c.ID}, nil, nil, nil)
		}
	}
}

func (b *assessmentBuilder) evalDirection() {
	req := b.ctx.Request.Scope.DirectionRequirement
	if req == DirectionUnknown && b.ctx.Request.Scope.RiskClass != RiskLowRisk {
		b.addUncertifiable(DimensionDirection, "closure.agent.direction_unknown", "direction requirement is unknown", "provide_input")
		return
	}
	bootstrap, bootstrapErr := DirectionBootstrapForRequest(b.ctx.Request, time.Now().UTC())
	if bootstrapErr != nil {
		b.addUncertifiable(DimensionDirection, "closure.direction.bootstrap_invalid", "direction bootstrap authorization is invalid or stale", "repair_binding")
		return
	}
	intended := b.hasPlane(architecture.PlaneIntended)
	desired := b.hasPlane(architecture.PlaneDesired)
	historical := b.hasPlane(architecture.PlaneHistorical)
	switch req {
	case DirectionPreserve:
		if !intended && !hasNodePlane(b.scope.Nodes, architecture.PlaneIntended) {
			if bootstrap != nil {
				b.addBootstrapDirectionCondition("closure.direction.intended.bootstrap", "bootstrap direction authorization conditionally waives missing intended basis while introducing governed direction records")
			} else {
				b.addOpen(DimensionDirection, "high", "closure.direction.intended_missing", "preserve requires current intended basis", "promote_architectural_knowledge", nil, nil, nil, nil)
			}
		}
	case DirectionEvolve:
		if !intended {
			if bootstrap != nil {
				b.addBootstrapDirectionCondition("closure.direction.intended.bootstrap", "bootstrap direction authorization conditionally waives missing intended basis while introducing governed direction records")
			} else {
				b.addOpen(DimensionDirection, "high", "closure.direction.intended_missing", "evolve requires current intended basis", "promote_architectural_knowledge", nil, nil, nil, nil)
			}
		}
		if !desired {
			if bootstrap != nil {
				b.addBootstrapDirectionCondition("closure.direction.desired.bootstrap", "bootstrap direction authorization conditionally waives missing desired basis while introducing governed direction records")
			} else {
				b.addOpen(DimensionDirection, "high", "closure.direction.desired_missing", "evolve requires explicit desired basis", "promote_architectural_knowledge", nil, nil, nil, nil)
			}
		}
	case DirectionMigrate:
		if !historical {
			b.addOpen(DimensionDirection, "high", "closure.direction.historical_missing", "migrate requires historical basis", "promote_architectural_knowledge", nil, nil, nil, nil)
		}
		if !desired {
			b.addOpen(DimensionDirection, "high", "closure.direction.desired_missing", "migrate requires desired target", "promote_architectural_knowledge", nil, nil, nil, nil)
		}
		if !intended && len(filterNodes(b.scope.Nodes, "contract")) == 0 {
			b.addOpen(DimensionDirection, "high", "closure.direction.migration_constraint_missing", "migrate requires current migration constraint", "define_contract", nil, nil, nil, nil)
		}
	case DirectionNotApplicable:
		if b.ctx.Request.Scope.RiskClass != RiskLowRisk {
			b.addUncertifiable(DimensionDirection, "closure.agent.direction_unknown", "not_applicable direction is allowed only for low risk", "provide_input")
		}
	}
	for _, c := range b.scope.Claims {
		if oneOf(c.ArchitecturalPlane, architecture.PlaneIntended, architecture.PlaneDesired, architecture.PlaneHistorical) {
			a, ok := b.planeByClaim[c.ID]
			if ok && a.PlaneState != plane.StateJustified {
				b.addOpen(DimensionDirection, "high", "closure.direction.plane_invalid", "direction claim plane is not justified", "repair_claim", []string{c.ID}, nil, nil, nil)
			}
			if c.EpistemicStatus == architecture.StatusStale {
				b.addOpen(DimensionDirection, "high", "closure.direction.target_stale", "direction claim is stale", "add_evidence", []string{c.ID}, nil, nil, nil)
			}
			if c.EpistemicStatus == architecture.StatusRefuted {
				b.addOpen(DimensionDirection, "critical", "closure.direction.target_refuted", "direction claim is refuted", "resolve_contradiction", []string{c.ID}, nil, nil, nil)
			}
		}
	}
}

func (b *assessmentBuilder) evalAgent() {
	if b.ctx.Request.Scope.TaskClass == "" {
		b.addOpen(DimensionAgent, "high", "closure.agent.task_class_missing", "task class is missing", "provide_input", nil, nil, nil, nil)
	}
	if b.ctx.Request.Scope.RiskClass == RiskUnknownImpact {
		b.addUncertifiable(DimensionAgent, "closure.risk.unknown", "risk class is unknown impact", "provide_input")
	}
	if b.ctx.Request.Scope.AccessMode == AccessUnknown && b.dimensionRequired(DimensionAuthority) {
		b.addOpen(DimensionAgent, "high", "closure.agent.access_mode_unknown", "access mode is unknown", "provide_input", nil, nil, nil, nil)
	}
	if b.ctx.Request.Scope.DirectionRequirement == DirectionUnknown && b.dimensionRequired(DimensionDirection) {
		b.addOpen(DimensionAgent, "high", "closure.agent.direction_unknown", "direction requirement is unknown", "provide_input", nil, nil, nil, nil)
	}
	if b.scope.EmptySurface {
		b.addOpen(DimensionAgent, "critical", "closure.agent.guidance_surface_empty", "no relevant supported claim or governed node exists", "reassess_scope", nil, nil, nil, nil)
	}
	if len(b.scope.Receipt.MissingFiles)+len(b.scope.Receipt.MissingSymbols)+len(b.scope.Receipt.MissingComponents) > 0 {
		b.addOpen(DimensionAgent, "high", "closure.agent.scope_unrepresented", "requested scope has unrepresented anchors", "reassess_scope", nil, nil, nil, b.scope.Receipt.MissingFiles)
	}
	for _, c := range b.scope.Claims {
		_, hasQ := b.questionsByClaim[c.ID]
		a := b.planeByClaim[c.ID]
		if !hasQ && (oneOf(c.EpistemicStatus, architecture.StatusUnknown, architecture.StatusStale, architecture.StatusContested, architecture.StatusRefuted) ||
			oneOf(a.PlaneState, plane.StateUnderSupported, plane.StateInvalid, plane.StateUnknown, plane.StateStale)) {
			b.addOpen(DimensionAgent, "high", "closure.question.missing_artifact", "claim or plane unknown lacks OpenQuestion artifact", "create_open_question", []string{c.ID}, nil, nil, nil)
		}
	}
	if oneOf(b.ctx.Request.Scope.AccessMode, AccessWrite, AccessReadWrite) || b.ctx.Request.Scope.RiskClass != RiskLowRisk {
		if !b.hasAnyRequiredTest() {
			b.addOpen(DimensionAgent, "medium", "closure.agent.required_test_unidentified", "write or high-risk scope has no identifiable required Test", "add_test", nil, nil, nil, nil)
		}
	}
}

func resolveScope(ctx Context) resolvedScope {
	req := ctx.Request
	out := resolvedScope{ByClaimID: map[string]architecture.Claim{}, ByNodeID: map[string]Node{}}
	claimByID := map[string]architecture.Claim{}
	claimByProp := map[string]architecture.Claim{}
	for _, c := range ctx.Claims.Claims {
		claimByID[c.ID] = c
		claimByProp[plane.PropositionKey(c)] = c
		relevant := req.Scope.DomainWide && claimDomainMatches(c, req.Binding.RepositoryDomain)
		relevant = relevant || intersects(c.Scope.Files, req.Scope.Files) || intersects(c.Scope.Symbols, req.Scope.Symbols) || intersects(c.Scope.Components, req.Scope.Components)
		for _, id := range req.Scope.ClaimIDs {
			if c.ID == id {
				relevant = true
			}
		}
		for _, key := range req.Scope.PropositionKeys {
			if plane.PropositionKey(c) == key {
				relevant = true
			}
		}
		if relevant {
			out.ByClaimID[c.ID] = c
		}
	}
	for _, id := range req.Scope.ClaimIDs {
		if c, ok := claimByID[id]; ok {
			out.ByClaimID[id] = c
		} else {
			out.Receipt.MissingClaims = append(out.Receipt.MissingClaims, id)
		}
	}
	for _, key := range req.Scope.PropositionKeys {
		if c, ok := claimByProp[key]; ok {
			out.ByClaimID[c.ID] = c
		} else {
			out.Receipt.MissingPropositions = append(out.Receipt.MissingPropositions, key)
		}
	}
	for _, n := range ctx.Graph.Nodes {
		relevant := req.Scope.DomainWide
		if n.SourcePath != "" && contains(req.Scope.Files, n.SourcePath) {
			relevant = true
		}
		if intersects(n.AuthoredIn, req.Scope.Files) || intersects(n.AnchoredIn, req.Scope.Symbols) {
			relevant = true
		}
		for _, f := range req.Scope.Files {
			for _, prefix := range n.CoversPath {
				if strings.HasPrefix(f, strings.TrimSuffix(prefix, "/")+"/") || f == prefix {
					relevant = true
				}
			}
		}
		if intersects([]string{n.ID}, req.Scope.Components) && hasClass(n, "component") {
			relevant = true
		}
		if relevant {
			out.ByNodeID[n.ID] = n
		}
	}
	for _, n := range out.ByNodeID {
		for _, id := range oneEdgeIDs(n) {
			if next, ok := findNode(ctx.Graph, id); ok {
				out.ByNodeID[next.ID] = next
			}
		}
	}
	for _, c := range out.ByClaimID {
		for _, f := range c.Scope.Files {
			if contains(req.Scope.Files, f) {
				out.Receipt.Files = append(out.Receipt.Files, f)
			}
		}
		for _, s := range c.Scope.Symbols {
			if contains(req.Scope.Symbols, s) {
				out.Receipt.Symbols = append(out.Receipt.Symbols, s)
			}
		}
		for _, comp := range c.Scope.Components {
			if contains(req.Scope.Components, comp) {
				out.Receipt.Components = append(out.Receipt.Components, comp)
			}
		}
		out.Receipt.ClaimIDs = append(out.Receipt.ClaimIDs, c.ID)
		out.Receipt.PropositionKeys = append(out.Receipt.PropositionKeys, plane.PropositionKey(c))
	}
	for _, n := range out.ByNodeID {
		out.Receipt.NodeIDs = append(out.Receipt.NodeIDs, n.ID)
		for _, path := range req.Scope.Files {
			rep, ok := CanonicalFileRepresentation(ctx.Graph, n, path, ctx.RepositoryRoot)
			if !ok {
				continue
			}
			out.Receipt.Files = append(out.Receipt.Files, rep.Path)
			out.Receipt.RepresentedFiles = append(out.Receipt.RepresentedFiles, rep)
		}
		if hasClass(n, "component") && contains(req.Scope.Components, n.ID) {
			out.Receipt.Components = append(out.Receipt.Components, n.ID)
		}
		if (hasClass(n, "code_symbol") || hasClass(n, "symbol")) && contains(req.Scope.Symbols, n.ID) {
			out.Receipt.Symbols = append(out.Receipt.Symbols, n.ID)
		}
	}
	out.Claims = sortedClaimMap(out.ByClaimID)
	out.Nodes = sortedNodeMap(out.ByNodeID)
	out.Receipt = normalizeScopeReceipt(out.Receipt)
	for _, f := range req.Scope.Files {
		if !contains(out.Receipt.Files, f) {
			out.Receipt.MissingFiles = append(out.Receipt.MissingFiles, f)
		}
	}
	for _, s := range req.Scope.Symbols {
		if !contains(out.Receipt.Symbols, s) {
			out.Receipt.MissingSymbols = append(out.Receipt.MissingSymbols, s)
		}
	}
	for _, c := range req.Scope.Components {
		if !contains(out.Receipt.Components, c) {
			out.Receipt.MissingComponents = append(out.Receipt.MissingComponents, c)
		}
	}
	out.Receipt = normalizeScopeReceipt(out.Receipt)
	out.EmptySurface = len(out.Claims) == 0 && len(out.Nodes) == 0
	return out
}

func (b *assessmentBuilder) addUncertifiable(dim, code, summary, action string, refs ...string) {
	claimIDs, questionIDs := splitRefs(refs)
	b.uncertifiable[dim] = true
	b.addBlocker(Blocker{Dimension: dim, Severity: "critical", Code: code, Summary: summary, ClaimIDs: claimIDs, QuestionIDs: questionIDs, RequiredNextAction: action})
}

func (b *assessmentBuilder) addOpen(dim, severity, code, summary, action string, claimIDs, nodeIDs, questionIDs, files []string) {
	b.addBlocker(Blocker{Dimension: dim, Severity: severity, Code: code, Summary: summary, ClaimIDs: cleanList(claimIDs), NodeIDs: cleanList(nodeIDs), QuestionIDs: cleanList(questionIDs), Files: cleanPathList(files), RequiredNextAction: action})
}

func (b *assessmentBuilder) addBlocker(bl Blocker) {
	bl.ClaimIDs = cleanList(bl.ClaimIDs)
	bl.NodeIDs = cleanList(bl.NodeIDs)
	bl.QuestionIDs = cleanList(bl.QuestionIDs)
	bl.EvidenceIDs = cleanList(bl.EvidenceIDs)
	bl.Files = cleanPathList(bl.Files)
	bl.ID = blockerID(bl)
	b.blockers = append(b.blockers, bl)
}

func (b *assessmentBuilder) addCondition(dim, code, summary, questionID string) {
	c := Condition{Dimension: dim, Code: code, Summary: summary, QuestionIDs: cleanList([]string{questionID}), RequiredNextAction: "answer_open_question"}
	c.ID = conditionID(c)
	b.conditions = append(b.conditions, c)
}

func (b *assessmentBuilder) addBootstrapDirectionCondition(code, summary string) {
	c := Condition{
		Dimension:          DimensionDirection,
		Code:               code,
		Summary:            summary,
		RequiredNextAction: "acknowledge_bootstrap_direction_authorization",
	}
	c.ID = conditionID(c)
	b.conditions = append(b.conditions, c)
}

func blockerID(bl Blocker) string {
	parts := []string{bl.Dimension, bl.Code, strings.Join(cleanList(bl.ClaimIDs), ","), strings.Join(cleanList(bl.NodeIDs), ","), strings.Join(cleanList(bl.QuestionIDs), ","), strings.Join(cleanPathList(bl.Files), ",")}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "blocker." + bl.Dimension + "." + hex.EncodeToString(sum[:])[:12]
}

func conditionID(c Condition) string {
	parts := []string{c.Dimension, c.Code, strings.Join(cleanList(c.QuestionIDs), ",")}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "condition." + c.Dimension + "." + hex.EncodeToString(sum[:])[:12]
}

func calculateVerdict(report Report, policy Policy) string {
	if report.Request.Scope.RiskClass == RiskUnknownImpact {
		return VerdictUncertifiable
	}
	for _, d := range report.Dimensions {
		if d.Required && d.State == StateUncertifiable {
			return VerdictUncertifiable
		}
	}
	for _, d := range report.Dimensions {
		if d.Required && d.State == StateOpen {
			return VerdictOpen
		}
	}
	for _, d := range report.Dimensions {
		if d.Required && d.State == StateConditional {
			if !policy.ConditionalAllowed {
				return VerdictOpen
			}
			return VerdictConditionallyClosed
		}
	}
	return VerdictClosed
}

func normalizeReport(in Report) Report {
	r := in
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = GeneratedBy
	}
	r.ScopeReceipt = normalizeScopeReceipt(r.ScopeReceipt)
	sort.SliceStable(r.Dimensions, func(i, j int) bool {
		return dimensionRank(r.Dimensions[i].Dimension) < dimensionRank(r.Dimensions[j].Dimension)
	})
	for i := range r.Dimensions {
		r.Dimensions[i].Reasons = dedupeReasons(r.Dimensions[i].Reasons)
		r.Dimensions[i].BlockerIDs = cleanList(r.Dimensions[i].BlockerIDs)
		r.Dimensions[i].ConditionIDs = cleanList(r.Dimensions[i].ConditionIDs)
	}
	for i := range r.Blockers {
		r.Blockers[i].ClaimIDs = cleanList(r.Blockers[i].ClaimIDs)
		r.Blockers[i].NodeIDs = cleanList(r.Blockers[i].NodeIDs)
		r.Blockers[i].QuestionIDs = cleanList(r.Blockers[i].QuestionIDs)
		r.Blockers[i].EvidenceIDs = cleanList(r.Blockers[i].EvidenceIDs)
		r.Blockers[i].Files = cleanPathList(r.Blockers[i].Files)
	}
	sort.SliceStable(r.Blockers, func(i, j int) bool {
		a := severityRank(r.Blockers[i].Severity)
		b := severityRank(r.Blockers[j].Severity)
		if a != b {
			return a < b
		}
		return r.Blockers[i].Dimension+"\x00"+r.Blockers[i].ID < r.Blockers[j].Dimension+"\x00"+r.Blockers[j].ID
	})
	for i := range r.Conditions {
		r.Conditions[i].QuestionIDs = cleanList(r.Conditions[i].QuestionIDs)
	}
	sort.SliceStable(r.Conditions, func(i, j int) bool { return r.Conditions[i].ID < r.Conditions[j].ID })
	sort.SliceStable(r.RelevantClaims, func(i, j int) bool { return r.RelevantClaims[i].ID < r.RelevantClaims[j].ID })
	sort.SliceStable(r.RelevantNodes, func(i, j int) bool { return r.RelevantNodes[i].ID < r.RelevantNodes[j].ID })
	sort.SliceStable(r.Questions, func(i, j int) bool { return r.Questions[i].ID < r.Questions[j].ID })
	return r
}
