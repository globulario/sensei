// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei assess-planes"

	BasisFact                    = "fact"
	BasisGovernedNode            = "governed_node"
	BasisEvidence                = "evidence"
	BasisRuntimeEvidence         = "runtime_evidence"
	BasisTest                    = "test"
	BasisGate                    = "gate"
	BasisHistoricalRecord        = "historical_record"
	BasisExplicitPlaneAnnotation = "explicit_plane_annotation"

	BasisAccepted = "accepted"
	BasisRejected = "rejected"
	BasisUnknown  = "unknown"
	BasisStale    = "stale"

	StateJustified      = "justified"
	StateUnderSupported = "under_supported"
	StateInvalid        = "invalid"
	StateUnknown        = "unknown"
	StateStale          = "stale"
)

var PlaneOrder = []string{
	architecture.PlaneObserved,
	architecture.PlaneEnforced,
	architecture.PlaneIntended,
	architecture.PlaneHistorical,
	architecture.PlaneDesired,
}

type Policy struct {
	Plane                           string   `json:"plane" yaml:"plane"`
	AllowedFactKinds                []string `json:"allowed_fact_kinds,omitempty" yaml:"allowed_fact_kinds,omitempty"`
	AllowedFactPredicates           []string `json:"allowed_fact_predicates,omitempty" yaml:"allowed_fact_predicates,omitempty"`
	AllowedGovernedClasses          []string `json:"allowed_governed_classes,omitempty" yaml:"allowed_governed_classes,omitempty"`
	RequiredNodeStatuses            []string `json:"required_node_statuses,omitempty" yaml:"required_node_statuses,omitempty"`
	RequiresExplicitPlaneAnnotation bool     `json:"requires_explicit_plane_annotation,omitempty" yaml:"requires_explicit_plane_annotation,omitempty"`
	RequiresCurrentEvidence         bool     `json:"requires_current_evidence,omitempty" yaml:"requires_current_evidence,omitempty"`
	TruthLayerIsSeparateAxis        bool     `json:"truth_layer_is_separate_axis" yaml:"truth_layer_is_separate_axis"`
	KnownLimitations                []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
}

type Basis struct {
	Kind       string `json:"kind" yaml:"kind"`
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	Class      string `json:"class,omitempty" yaml:"class,omitempty"`
	Status     string `json:"status,omitempty" yaml:"status,omitempty"`
	Freshness  string `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	SourcePath string `json:"source_path,omitempty" yaml:"source_path,omitempty"`
	Detail     string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type BasisAssessment struct {
	Basis Basis  `json:"basis" yaml:"basis"`
	State string `json:"state" yaml:"state"`
}

type GovernedNode struct {
	IRI                 string   `json:"iri" yaml:"iri"`
	Class               string   `json:"class" yaml:"class"`
	ID                  string   `json:"id" yaml:"id"`
	Label               string   `json:"label,omitempty" yaml:"label,omitempty"`
	Comment             string   `json:"comment,omitempty" yaml:"comment,omitempty"`
	Status              string   `json:"status,omitempty" yaml:"status,omitempty"`
	AssertionMethod     string   `json:"assertion_method,omitempty" yaml:"assertion_method,omitempty"`
	ArchitecturalPlane  string   `json:"architectural_plane,omitempty" yaml:"architectural_plane,omitempty"`
	Freshness           string   `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	LastValidatedAt     string   `json:"last_validated_at,omitempty" yaml:"last_validated_at,omitempty"`
	AuthoredIn          []string `json:"authored_in,omitempty" yaml:"authored_in,omitempty"`
	SupersededBy        []string `json:"superseded_by,omitempty" yaml:"superseded_by,omitempty"`
	SupportedByEvidence []string `json:"supported_by_evidence,omitempty" yaml:"supported_by_evidence,omitempty"`
	Supports            []string `json:"supports,omitempty" yaml:"supports,omitempty"`
	RequiresTests       []string `json:"requires_tests,omitempty" yaml:"requires_tests,omitempty"`
	ProducedByTests     []string `json:"produced_by_tests,omitempty" yaml:"produced_by_tests,omitempty"`
	SourcePath          string   `json:"source_path,omitempty" yaml:"source_path,omitempty"`
}

type GraphIndex struct {
	Nodes map[string]GovernedNode `json:"nodes" yaml:"nodes"`
}

type Context struct {
	Claims      architecture.ClaimDocument
	Maintenance *maintenance.Report
	Graph       GraphIndex
	Evidence    *maintenance.EvidenceStateDocument
	Dialogue    *architecture.DialogueDocument

	GraphSnapshotPath   string
	GraphDigest         string
	GraphDigestStatus   string
	GraphDigestVerified bool
}

type GraphSnapshotReport struct {
	Path         string `json:"path,omitempty" yaml:"path,omitempty"`
	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	DigestStatus string `json:"digest_status" yaml:"digest_status"`
}

type ClaimBindingReport struct {
	RepositoryDomain  string `json:"repository_domain,omitempty" yaml:"repository_domain,omitempty"`
	Revision          string `json:"revision,omitempty" yaml:"revision,omitempty"`
	RevisionStatus    string `json:"revision_status" yaml:"revision_status"`
	GraphDigestSHA256 string `json:"graph_digest_sha256,omitempty" yaml:"graph_digest_sha256,omitempty"`
	GraphDigestStatus string `json:"graph_digest_status" yaml:"graph_digest_status"`
}

type Reason struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type ClaimAssessment struct {
	ClaimID            string            `json:"claim_id" yaml:"claim_id"`
	PropositionKey     string            `json:"proposition_key" yaml:"proposition_key"`
	DeclaredPlane      string            `json:"declared_plane" yaml:"declared_plane"`
	AssertionOrigin    string            `json:"assertion_origin" yaml:"assertion_origin"`
	EpistemicStatus    string            `json:"epistemic_status" yaml:"epistemic_status"`
	PromotionStatus    string            `json:"promotion_status" yaml:"promotion_status"`
	Freshness          string            `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	PlaneState         string            `json:"plane_state" yaml:"plane_state"`
	Bases              []BasisAssessment `json:"bases,omitempty" yaml:"bases,omitempty"`
	Reasons            []Reason          `json:"reasons" yaml:"reasons"`
	OpenQuestions      []string          `json:"open_questions,omitempty" yaml:"open_questions,omitempty"`
	ArchitectAnswers   []string          `json:"architect_answers,omitempty" yaml:"architect_answers,omitempty"`
	MaintenanceReasons []Reason          `json:"maintenance_reasons,omitempty" yaml:"maintenance_reasons,omitempty"`
}

type PropositionGroup struct {
	PropositionKey    string              `json:"proposition_key" yaml:"proposition_key"`
	Subject           string              `json:"subject" yaml:"subject"`
	Predicate         string              `json:"predicate" yaml:"predicate"`
	Object            string              `json:"object" yaml:"object"`
	ClaimsByPlane     map[string][]string `json:"claims_by_plane" yaml:"claims_by_plane"`
	AssessmentByClaim map[string]string   `json:"assessment_by_claim" yaml:"assessment_by_claim"`
	PresentPlanes     []string            `json:"present_planes" yaml:"present_planes"`
	MissingPlanes     []string            `json:"missing_planes" yaml:"missing_planes"`
}

type Summary struct {
	Justified      int `json:"justified" yaml:"justified"`
	UnderSupported int `json:"under_supported" yaml:"under_supported"`
	Invalid        int `json:"invalid" yaml:"invalid"`
	Unknown        int `json:"unknown" yaml:"unknown"`
	Stale          int `json:"stale" yaml:"stale"`
}

type Report struct {
	SchemaVersion     string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy       string                    `json:"generated_by" yaml:"generated_by"`
	ClaimBinding      ClaimBindingReport        `json:"claim_binding" yaml:"claim_binding"`
	GraphSnapshot     GraphSnapshotReport       `json:"graph_snapshot" yaml:"graph_snapshot"`
	ClaimAssessments  []ClaimAssessment         `json:"claim_assessments" yaml:"claim_assessments"`
	PropositionGroups []PropositionGroup        `json:"proposition_groups" yaml:"proposition_groups"`
	Summary           Summary                   `json:"summary" yaml:"summary"`
	Limitations       []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

func classToken(class string) string {
	class = strings.TrimSpace(class)
	if i := strings.LastIndex(class, "#"); i >= 0 {
		class = class[i+1:]
	}
	return strings.ToLower(class)
}
