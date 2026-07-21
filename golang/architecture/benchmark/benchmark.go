// SPDX-License-Identifier: AGPL-3.0-only

package benchmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei external benchmark"

	FreezePolicyID      = "benchmark.freeze.ancestors.v1"
	FreezePolicyVersion = "v1"

	AllowedSourceSource      = "source"
	AllowedSourceTests       = "tests"
	AllowedSourceDocs        = "documentation"
	AllowedSourceComments    = "comments"
	AllowedSourceBaseHistory = "history_reachable_from_base"

	AccessRead      = "read"
	AccessWrite     = "write"
	AccessReadWrite = "read_write"
	AccessUnknown   = "unknown"

	DirectionPreserve = "preserve"
	DirectionEvolve   = "evolve"
	DirectionMigrate  = "migrate"
	DirectionUnknown  = "unknown"

	ExpectedModeInspect = "inspect"
	ExpectedModeModify  = "modify"

	ContaminationClean         = "clean"
	ContaminationContaminated  = "contaminated"
	ContaminationUncertifiable = "uncertifiable"

	ReconstructionFrozen       = "frozen"
	ReconstructionUnavailable  = "unavailable"
	ReconstructionContaminated = "contaminated"

	OutcomeDemonstrated          = "demonstrated"
	OutcomePartiallyDemonstrated = "partially_demonstrated"
	OutcomeCorrectlyOpen         = "correctly_open"
	OutcomeNotDemonstrated       = "not_demonstrated"
	OutcomeUncertifiable         = "uncertifiable"

	InterventionArchitectAnswer      = "architect_answer"
	InterventionEvidencePointer      = "evidence_pointer"
	InterventionEvidenceStateUpdate  = "evidence_state_update"
	InterventionKnowledgeOverlay     = "governed_knowledge_overlay"
	InterventionScopeClarification   = "scope_clarification"
	InterventionAcceptedUnknown      = "accepted_unknown"
	InterventionManualQuestionReview = "manual_question_review"

	InterventionLoadBearing = "load_bearing"
	InterventionHelpful     = "helpful"
	InterventionRedundant   = "redundant"
	InterventionNoEffect    = "no_effect"
	InterventionHarmful     = "harmful"
	InterventionNotAssessed = "not_assessed"

	QuestionValidArchitect     = "valid_architect_question"
	QuestionValidEvidence      = "valid_evidence_question"
	QuestionAnswerableBaseline = "already_answerable_from_baseline"
	QuestionDuplicate          = "duplicate"
	QuestionTooBroad           = "too_broad"
	QuestionIrrelevant         = "irrelevant_to_task"
	QuestionInsufficientGround = "insufficiently_grounded"
	QuestionIncorrectPremise   = "incorrect_premise"
	QuestionAcceptedUnknown    = "accepted_unknown"
	QuestionNotReviewed        = "not_reviewed"

	AlignmentAnticipated          = "anticipated"
	AlignmentPartiallyAnticipated = "partially_anticipated"
	AlignmentMissed               = "missed"
	AlignmentUnrelated            = "unrelated"
	AlignmentAmbiguous            = "ambiguous"

	FindingCriticalFalseGreen = "benchmark.false_green.critical"
)

type Corpus struct {
	SchemaVersion string       `json:"schema_version" yaml:"schema_version"`
	CorpusID      string       `json:"corpus_id" yaml:"corpus_id"`
	Description   string       `json:"description,omitempty" yaml:"description,omitempty"`
	Repositories  []Repository `json:"repositories" yaml:"repositories"`
}

type Repository struct {
	RepositoryID        string   `json:"repository_id" yaml:"repository_id"`
	RepositoryDomain    string   `json:"repository_domain" yaml:"repository_domain"`
	LocalRepositoryPath string   `json:"local_repository_path,omitempty" yaml:"local_repository_path,omitempty"`
	TaskManifestPaths   []string `json:"task_manifest_paths,omitempty" yaml:"task_manifest_paths,omitempty"`
}

type Task struct {
	SchemaVersion        string               `json:"schema_version" yaml:"schema_version"`
	TaskID               string               `json:"task_id" yaml:"task_id"`
	RepositoryID         string               `json:"repository_id" yaml:"repository_id"`
	RepositoryDomain     string               `json:"repository_domain" yaml:"repository_domain"`
	BaseRevision         string               `json:"base_revision" yaml:"base_revision"`
	BaseRevisionStatus   string               `json:"base_revision_status" yaml:"base_revision_status"`
	TaskClass            string               `json:"task_class" yaml:"task_class"`
	RiskClass            string               `json:"risk_class" yaml:"risk_class"`
	AccessMode           string               `json:"access_mode" yaml:"access_mode"`
	DirectionRequirement string               `json:"direction_requirement" yaml:"direction_requirement"`
	TaskText             string               `json:"task_text" yaml:"task_text"`
	InitialScope         Scope                `json:"initial_scope" yaml:"initial_scope"`
	AllowedSources       []string             `json:"allowed_sources" yaml:"allowed_sources"`
	ProhibitedSources    []string             `json:"prohibited_sources,omitempty" yaml:"prohibited_sources,omitempty"`
	ExpectedActionMode   string               `json:"expected_action_mode" yaml:"expected_action_mode"`
	ReviewerRequirements ReviewerRequirements `json:"reviewer_requirements,omitempty" yaml:"reviewer_requirements,omitempty"`
	Note                 string               `json:"note,omitempty" yaml:"note,omitempty"`
}

type ReviewerRequirements struct {
	MinimumReviewers           int  `json:"minimum_reviewers,omitempty" yaml:"minimum_reviewers,omitempty"`
	RequiresArchitectureReview bool `json:"requires_architecture_review,omitempty" yaml:"requires_architecture_review,omitempty"`
}

type Scope struct {
	Files           []string `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols         []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components      []string `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs        []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys []string `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
}

type Oracle struct {
	SchemaVersion          string         `json:"schema_version" yaml:"schema_version"`
	TaskID                 string         `json:"task_id" yaml:"task_id"`
	RepositoryID           string         `json:"repository_id" yaml:"repository_id"`
	OracleKind             string         `json:"oracle_kind" yaml:"oracle_kind"`
	OracleRevision         string         `json:"oracle_revision,omitempty" yaml:"oracle_revision,omitempty"`
	OraclePatchSHA256      string         `json:"oracle_patch_sha256,omitempty" yaml:"oracle_patch_sha256,omitempty"`
	OracleTaskSourceSHA256 string         `json:"oracle_task_source_sha256,omitempty" yaml:"oracle_task_source_sha256,omitempty"`
	OracleMaterial         OracleMaterial `json:"oracle_material,omitempty" yaml:"oracle_material,omitempty"`
	Revealed               bool           `json:"revealed" yaml:"revealed"`
}

type OracleMaterial struct {
	PatchPath        string   `json:"patch_path,omitempty" yaml:"patch_path,omitempty"`
	DiscussionPaths  []string `json:"discussion_paths,omitempty" yaml:"discussion_paths,omitempty"`
	TestReceiptPaths []string `json:"test_receipt_paths,omitempty" yaml:"test_receipt_paths,omitempty"`
}

type Reason struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type FreezeReceipt struct {
	SchemaVersion              string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                string                    `json:"generated_by" yaml:"generated_by"`
	WorkspaceID                string                    `json:"workspace_id" yaml:"workspace_id"`
	PolicyID                   string                    `json:"policy_id" yaml:"policy_id"`
	PolicyVersion              string                    `json:"policy_version" yaml:"policy_version"`
	TaskID                     string                    `json:"task_id" yaml:"task_id"`
	RepositoryID               string                    `json:"repository_id" yaml:"repository_id"`
	RepositoryDomain           string                    `json:"repository_domain" yaml:"repository_domain"`
	SourceBaseRevision         string                    `json:"source_base_revision" yaml:"source_base_revision"`
	BlindRevision              string                    `json:"blind_revision" yaml:"blind_revision"`
	SourceTreeDigestSHA256     string                    `json:"source_tree_digest_sha256" yaml:"source_tree_digest_sha256"`
	BlindTreeDigestSHA256      string                    `json:"blind_tree_digest_sha256" yaml:"blind_tree_digest_sha256"`
	TaskManifestDigestSHA256   string                    `json:"task_manifest_digest_sha256" yaml:"task_manifest_digest_sha256"`
	OracleManifestDigestSHA256 string                    `json:"oracle_manifest_digest_sha256" yaml:"oracle_manifest_digest_sha256"`
	ContaminationStatus        string                    `json:"contamination_status" yaml:"contamination_status"`
	CommitCount                int                       `json:"commit_count" yaml:"commit_count"`
	OldestReachableCommit      string                    `json:"oldest_reachable_commit,omitempty" yaml:"oldest_reachable_commit,omitempty"`
	NewestReachableCommit      string                    `json:"newest_reachable_commit,omitempty" yaml:"newest_reachable_commit,omitempty"`
	HistoryExportDigestSHA256  string                    `json:"history_export_digest_sha256" yaml:"history_export_digest_sha256"`
	Limitations                []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	ReceiptDigestSHA256        string                    `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
}

type ContaminationReport struct {
	SchemaVersion      string   `json:"schema_version" yaml:"schema_version"`
	GeneratedBy        string   `json:"generated_by" yaml:"generated_by"`
	Status             string   `json:"status" yaml:"status"`
	Checks             []Check  `json:"checks" yaml:"checks"`
	Reasons            []Reason `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	ReportDigestSHA256 string   `json:"report_digest_sha256" yaml:"report_digest_sha256"`
}

type Check struct {
	Code   string `json:"code" yaml:"code"`
	Status string `json:"status" yaml:"status"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type ReconstructionReceipt struct {
	SchemaVersion               string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                 string                    `json:"generated_by" yaml:"generated_by"`
	WorkspaceID                 string                    `json:"workspace_id" yaml:"workspace_id"`
	TaskID                      string                    `json:"task_id" yaml:"task_id"`
	Status                      string                    `json:"status" yaml:"status"`
	ColdStart                   bool                      `json:"cold_start" yaml:"cold_start"`
	BlindRepositoryDigestSHA256 string                    `json:"blind_repository_digest_sha256" yaml:"blind_repository_digest_sha256"`
	FactsDigestSHA256           string                    `json:"facts_digest_sha256" yaml:"facts_digest_sha256"`
	CandidatesDigestSHA256      string                    `json:"candidates_digest_sha256" yaml:"candidates_digest_sha256"`
	GraphDigestSHA256           string                    `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	ClosureVerdict              string                    `json:"closure_verdict" yaml:"closure_verdict"`
	ConvergenceStatus           string                    `json:"convergence_status" yaml:"convergence_status"`
	AdmissionDecision           string                    `json:"admission_decision" yaml:"admission_decision"`
	Limitations                 []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	ReceiptDigestSHA256         string                    `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
}

type InterventionLedger struct {
	SchemaVersion string         `json:"schema_version" yaml:"schema_version"`
	TaskID        string         `json:"task_id" yaml:"task_id"`
	Interventions []Intervention `json:"interventions" yaml:"interventions"`
}

type Intervention struct {
	ID                     string   `json:"id" yaml:"id"`
	Kind                   string   `json:"kind" yaml:"kind"`
	ActorRole              string   `json:"actor_role" yaml:"actor_role"`
	ActorID                string   `json:"actor_id,omitempty" yaml:"actor_id,omitempty"`
	SourceArtifact         string   `json:"source_artifact" yaml:"source_artifact"`
	SourceDigestSHA256     string   `json:"source_digest_sha256" yaml:"source_digest_sha256"`
	AnswerIDs              []string `json:"answer_ids,omitempty" yaml:"answer_ids,omitempty"`
	EvidenceIDs            []string `json:"evidence_ids,omitempty" yaml:"evidence_ids,omitempty"`
	OverlayNodeRefs        []string `json:"overlay_node_refs,omitempty" yaml:"overlay_node_refs,omitempty"`
	ScopeChangeRefs        []string `json:"scope_change_refs,omitempty" yaml:"scope_change_refs,omitempty"`
	AppliedBeforeIteration int      `json:"applied_before_iteration" yaml:"applied_before_iteration"`
	Classification         string   `json:"classification" yaml:"classification"`
	Note                   string   `json:"note,omitempty" yaml:"note,omitempty"`
}

type QuestionReviewDocument struct {
	SchemaVersion string           `json:"schema_version" yaml:"schema_version"`
	TaskID        string           `json:"task_id" yaml:"task_id"`
	Reviews       []QuestionReview `json:"reviews" yaml:"reviews"`
}

type QuestionReview struct {
	QuestionID                 string   `json:"question_id" yaml:"question_id"`
	ReviewerRole               string   `json:"reviewer_role" yaml:"reviewer_role"`
	ReviewerID                 string   `json:"reviewer_id,omitempty" yaml:"reviewer_id,omitempty"`
	Label                      string   `json:"label" yaml:"label"`
	Rationale                  string   `json:"rationale" yaml:"rationale"`
	SupportingSourceRefs       []string `json:"supporting_source_refs,omitempty" yaml:"supporting_source_refs,omitempty"`
	ReviewArtifactDigestSHA256 string   `json:"review_artifact_digest_sha256" yaml:"review_artifact_digest_sha256"`
}

type OracleMapping struct {
	SchemaVersion string          `json:"schema_version" yaml:"schema_version"`
	TaskID        string          `json:"task_id" yaml:"task_id"`
	Concepts      []OracleConcept `json:"concepts" yaml:"concepts"`
}

type OracleConcept struct {
	OracleConceptID   string   `json:"oracle_concept_id" yaml:"oracle_concept_id"`
	SourceRefs        []string `json:"source_refs" yaml:"source_refs"`
	SenseiRefs        []string `json:"sensei_refs,omitempty" yaml:"sensei_refs,omitempty"`
	Alignment         string   `json:"alignment" yaml:"alignment"`
	Critical          bool     `json:"critical,omitempty" yaml:"critical,omitempty"`
	ReviewerConfirmed bool     `json:"reviewer_confirmed,omitempty" yaml:"reviewer_confirmed,omitempty"`
}

type OracleRevealReceipt struct {
	SchemaVersion              string   `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                string   `json:"generated_by" yaml:"generated_by"`
	TaskID                     string   `json:"task_id" yaml:"task_id"`
	OracleManifestDigestSHA256 string   `json:"oracle_manifest_digest_sha256" yaml:"oracle_manifest_digest_sha256"`
	PatchDigestSHA256          string   `json:"patch_digest_sha256,omitempty" yaml:"patch_digest_sha256,omitempty"`
	DiscussionArtifactDigests  []string `json:"discussion_artifact_digests,omitempty" yaml:"discussion_artifact_digests,omitempty"`
	TestReceiptDigests         []string `json:"test_receipt_digests,omitempty" yaml:"test_receipt_digests,omitempty"`
	RevealStatus               string   `json:"reveal_status" yaml:"reveal_status"`
	ReceiptDigestSHA256        string   `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
}

type Finding struct {
	Code              string `json:"code" yaml:"code"`
	Severity          string `json:"severity" yaml:"severity"`
	ConceptID         string `json:"concept_id,omitempty" yaml:"concept_id,omitempty"`
	Summary           string `json:"summary" yaml:"summary"`
	ReviewerConfirmed bool   `json:"reviewer_confirmed" yaml:"reviewer_confirmed"`
}

type TaskReport struct {
	SchemaVersion      string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy        string                    `json:"generated_by" yaml:"generated_by"`
	CorpusID           string                    `json:"corpus_id,omitempty" yaml:"corpus_id,omitempty"`
	RepositoryID       string                    `json:"repository_id" yaml:"repository_id"`
	TaskID             string                    `json:"task_id" yaml:"task_id"`
	FreezeReceipt      FreezeReceipt             `json:"freeze_receipt" yaml:"freeze_receipt"`
	Contamination      ContaminationReport       `json:"contamination" yaml:"contamination"`
	Reconstruction     ReconstructionReceipt     `json:"reconstruction" yaml:"reconstruction"`
	Interventions      []Intervention            `json:"interventions,omitempty" yaml:"interventions,omitempty"`
	QuestionReview     []QuestionReview          `json:"question_review,omitempty" yaml:"question_review,omitempty"`
	OracleReveal       OracleRevealReceipt       `json:"oracle_reveal" yaml:"oracle_reveal"`
	OracleMapping      OracleMapping             `json:"oracle_mapping" yaml:"oracle_mapping"`
	FalseGreens        []Finding                 `json:"false_greens,omitempty" yaml:"false_greens,omitempty"`
	Overblocking       []Finding                 `json:"overblocking,omitempty" yaml:"overblocking,omitempty"`
	Outcome            string                    `json:"outcome" yaml:"outcome"`
	Reasons            []Reason                  `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	Limitations        []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	ReportDigestSHA256 string                    `json:"report_digest_sha256" yaml:"report_digest_sha256"`
}

type CorpusReport struct {
	SchemaVersion           string         `json:"schema_version" yaml:"schema_version"`
	GeneratedBy             string         `json:"generated_by" yaml:"generated_by"`
	CriticalFalseGreenCount int            `json:"critical_false_green_count" yaml:"critical_false_green_count"`
	Tasks                   []TaskReport   `json:"tasks" yaml:"tasks"`
	CountsByOutcome         map[string]int `json:"counts_by_outcome" yaml:"counts_by_outcome"`
	ReportDigestSHA256      string         `json:"report_digest_sha256" yaml:"report_digest_sha256"`
}

type FreezeOptions struct {
	TaskPath   string
	SourceRepo string
	OraclePath string
	OutputDir  string
	Check      bool
}

type EvaluateOptions struct {
	Workspace              string
	OraclePath             string
	QuestionReviewPath     string
	OracleMappingPath      string
	InterventionLedgerPath string
	OutputPath             string
	Check                  bool
}

type corpusEnvelope struct {
	ArchitectureBenchmarkCorpus Corpus `yaml:"architecture_benchmark_corpus" json:"architecture_benchmark_corpus"`
}
type taskEnvelope struct {
	ArchitectureBenchmarkTask Task `yaml:"architecture_benchmark_task" json:"architecture_benchmark_task"`
}
type oracleEnvelope struct {
	ArchitectureBenchmarkOracle Oracle `yaml:"architecture_benchmark_oracle" json:"architecture_benchmark_oracle"`
}
type freezeEnvelope struct {
	ArchitectureBenchmarkFreeze FreezeReceipt `yaml:"architecture_benchmark_freeze" json:"architecture_benchmark_freeze"`
}
type contaminationEnvelope struct {
	ArchitectureBenchmarkContamination ContaminationReport `yaml:"architecture_benchmark_contamination" json:"architecture_benchmark_contamination"`
}
type reconstructionEnvelope struct {
	ArchitectureBenchmarkReconstruction ReconstructionReceipt `yaml:"architecture_benchmark_reconstruction" json:"architecture_benchmark_reconstruction"`
}
type interventionEnvelope struct {
	ArchitectureBenchmarkInterventions InterventionLedger `yaml:"architecture_benchmark_interventions" json:"architecture_benchmark_interventions"`
}
type questionReviewEnvelope struct {
	ArchitectureBenchmarkQuestionReview QuestionReviewDocument `yaml:"architecture_benchmark_question_review" json:"architecture_benchmark_question_review"`
}
type oracleMappingEnvelope struct {
	ArchitectureBenchmarkOracleMapping OracleMapping `yaml:"architecture_benchmark_oracle_mapping" json:"architecture_benchmark_oracle_mapping"`
}
type oracleRevealEnvelope struct {
	ArchitectureBenchmarkOracleReveal OracleRevealReceipt `yaml:"architecture_benchmark_oracle_reveal" json:"architecture_benchmark_oracle_reveal"`
}
type taskReportEnvelope struct {
	ArchitectureExternalBenchmarkReport TaskReport `yaml:"architecture_external_benchmark_report" json:"architecture_external_benchmark_report"`
}

func LoadTask(path string) (Task, error) {
	var env taskEnvelope
	if err := loadYAML(path, &env); err != nil {
		return Task{}, err
	}
	if env.ArchitectureBenchmarkTask.SchemaVersion == "" {
		return Task{}, errors.New("missing architecture_benchmark_task")
	}
	return NormalizeTask(env.ArchitectureBenchmarkTask)
}

func LoadOracle(path string) (Oracle, error) {
	var env oracleEnvelope
	if err := loadYAML(path, &env); err != nil {
		return Oracle{}, err
	}
	if env.ArchitectureBenchmarkOracle.SchemaVersion == "" {
		return Oracle{}, errors.New("missing architecture_benchmark_oracle")
	}
	return NormalizeOracle(env.ArchitectureBenchmarkOracle)
}

func LoadCorpus(path string) (Corpus, error) {
	var env corpusEnvelope
	if err := loadYAML(path, &env); err != nil {
		return Corpus{}, err
	}
	if env.ArchitectureBenchmarkCorpus.SchemaVersion == "" {
		return Corpus{}, errors.New("missing architecture_benchmark_corpus")
	}
	return NormalizeCorpus(env.ArchitectureBenchmarkCorpus)
}

func NormalizeTask(t Task) (Task, error) {
	t.SchemaVersion = strings.TrimSpace(t.SchemaVersion)
	t.TaskID = strings.TrimSpace(t.TaskID)
	t.RepositoryID = strings.TrimSpace(t.RepositoryID)
	t.RepositoryDomain = strings.TrimSpace(t.RepositoryDomain)
	t.BaseRevision = strings.TrimSpace(t.BaseRevision)
	t.BaseRevisionStatus = strings.TrimSpace(t.BaseRevisionStatus)
	t.TaskClass = strings.TrimSpace(t.TaskClass)
	t.RiskClass = strings.TrimSpace(t.RiskClass)
	t.AccessMode = strings.TrimSpace(t.AccessMode)
	t.DirectionRequirement = strings.TrimSpace(t.DirectionRequirement)
	t.TaskText = strings.TrimSpace(t.TaskText)
	t.ExpectedActionMode = strings.TrimSpace(t.ExpectedActionMode)
	t.AllowedSources = clean(t.AllowedSources)
	t.ProhibitedSources = clean(t.ProhibitedSources)
	t.InitialScope = normalizeScope(t.InitialScope)
	t.Note = strings.TrimSpace(t.Note)
	if err := ValidateTask(t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func ValidateTask(t Task) error {
	var errs []string
	if t.SchemaVersion != SchemaVersion {
		errs = append(errs, "unsupported schema_version")
	}
	if t.TaskID == "" || t.RepositoryID == "" || t.RepositoryDomain == "" {
		errs = append(errs, "task and repository identity are required")
	}
	if !isRevision(t.BaseRevision) || t.BaseRevisionStatus != architecture.RevisionResolved {
		errs = append(errs, "exact resolved base_revision is required")
	}
	if t.TaskClass == "" || t.RiskClass == "" {
		errs = append(errs, "task_class and risk_class are required")
	}
	if !oneOf(t.AccessMode, AccessRead, AccessWrite, AccessReadWrite, AccessUnknown) {
		errs = append(errs, "access_mode is unknown")
	}
	if !oneOf(t.DirectionRequirement, DirectionPreserve, DirectionEvolve, DirectionMigrate, DirectionUnknown) {
		errs = append(errs, "direction_requirement is unknown")
	}
	if len(t.InitialScope.Files)+len(t.InitialScope.Symbols)+len(t.InitialScope.Components)+len(t.InitialScope.ClaimIDs)+len(t.InitialScope.PropositionKeys) == 0 {
		errs = append(errs, "initial_scope is required")
	}
	for _, source := range t.AllowedSources {
		if !oneOf(source, AllowedSourceSource, AllowedSourceTests, AllowedSourceDocs, AllowedSourceComments, AllowedSourceBaseHistory) {
			errs = append(errs, "allowed_sources contains unknown source")
		}
	}
	if len(t.AllowedSources) == 0 {
		errs = append(errs, "allowed_sources is required")
	}
	if !oneOf(t.ExpectedActionMode, ExpectedModeInspect, ExpectedModeModify) {
		errs = append(errs, "expected_action_mode is unknown")
	}
	if containsLower(t.TaskText, "oracle_revision") || containsLower(t.TaskText, "oracle_patch") {
		errs = append(errs, "task_text must not contain oracle fields")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func NormalizeOracle(o Oracle) (Oracle, error) {
	o.SchemaVersion = strings.TrimSpace(o.SchemaVersion)
	o.TaskID = strings.TrimSpace(o.TaskID)
	o.RepositoryID = strings.TrimSpace(o.RepositoryID)
	o.OracleKind = strings.TrimSpace(o.OracleKind)
	o.OracleRevision = strings.TrimSpace(o.OracleRevision)
	o.OraclePatchSHA256 = strings.TrimSpace(o.OraclePatchSHA256)
	o.OracleTaskSourceSHA256 = strings.TrimSpace(o.OracleTaskSourceSHA256)
	o.OracleMaterial.PatchPath = strings.TrimSpace(o.OracleMaterial.PatchPath)
	o.OracleMaterial.DiscussionPaths = clean(o.OracleMaterial.DiscussionPaths)
	o.OracleMaterial.TestReceiptPaths = clean(o.OracleMaterial.TestReceiptPaths)
	if err := ValidateOracle(o); err != nil {
		return Oracle{}, err
	}
	return o, nil
}

func ValidateOracle(o Oracle) error {
	var errs []string
	if o.SchemaVersion != SchemaVersion {
		errs = append(errs, "unsupported schema_version")
	}
	if o.TaskID == "" || o.RepositoryID == "" {
		errs = append(errs, "oracle task binding is required")
	}
	if o.OracleKind == "" {
		errs = append(errs, "oracle_kind is required")
	}
	if o.OracleRevision != "" && !isRevision(o.OracleRevision) {
		errs = append(errs, "oracle_revision must be a hex revision")
	}
	if o.OraclePatchSHA256 != "" && !isSHA256(o.OraclePatchSHA256) {
		errs = append(errs, "oracle_patch_sha256 must be lowercase SHA-256")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func NormalizeCorpus(c Corpus) (Corpus, error) {
	c.SchemaVersion = strings.TrimSpace(c.SchemaVersion)
	c.CorpusID = strings.TrimSpace(c.CorpusID)
	for i := range c.Repositories {
		c.Repositories[i].RepositoryID = strings.TrimSpace(c.Repositories[i].RepositoryID)
		c.Repositories[i].RepositoryDomain = strings.TrimSpace(c.Repositories[i].RepositoryDomain)
		c.Repositories[i].LocalRepositoryPath = strings.TrimSpace(c.Repositories[i].LocalRepositoryPath)
		c.Repositories[i].TaskManifestPaths = clean(c.Repositories[i].TaskManifestPaths)
	}
	sort.SliceStable(c.Repositories, func(i, j int) bool { return c.Repositories[i].RepositoryID < c.Repositories[j].RepositoryID })
	if err := ValidateCorpus(c); err != nil {
		return Corpus{}, err
	}
	return c, nil
}

func ValidateCorpus(c Corpus) error {
	if c.SchemaVersion != SchemaVersion || c.CorpusID == "" {
		return errors.New("schema_version and corpus_id are required")
	}
	seenTasks := map[string]bool{}
	for _, r := range c.Repositories {
		if r.RepositoryID == "" || r.RepositoryDomain == "" {
			return errors.New("repository_id and repository_domain are required")
		}
		for _, p := range r.TaskManifestPaths {
			if seenTasks[p] {
				return fmt.Errorf("duplicate task manifest path %s", p)
			}
			seenTasks[p] = true
		}
	}
	return nil
}

func Freeze(opts FreezeOptions) (FreezeReceipt, ContaminationReport, error) {
	taskBytes, err := os.ReadFile(opts.TaskPath)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	oracleBytes, err := os.ReadFile(opts.OraclePath)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	task, err := LoadTask(opts.TaskPath)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	oracle, err := LoadOracle(opts.OraclePath)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if oracle.TaskID != task.TaskID || oracle.RepositoryID != task.RepositoryID {
		return FreezeReceipt{}, ContaminationReport{}, errors.New("oracle task binding does not match task manifest")
	}
	if opts.OutputDir == "" {
		return FreezeReceipt{}, ContaminationReport{}, errors.New("output directory is required")
	}
	if protectedOutputPath(opts.OutputDir) {
		return FreezeReceipt{}, ContaminationReport{}, errors.New("benchmark output must not be under active awareness, intent, or embeddata paths")
	}
	sourceRepo := strings.TrimSpace(opts.SourceRepo)
	if sourceRepo == "" {
		return FreezeReceipt{}, ContaminationReport{}, errors.New("source repository is required")
	}
	if _, err := os.Stat(filepath.Join(sourceRepo, ".git")); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, fmt.Errorf("source repository must be a git checkout: %w", err)
	}
	baseCommit, err := git(sourceRepo, "rev-parse", task.BaseRevision+"^{commit}")
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, fmt.Errorf("resolve base revision: %w", err)
	}
	base := strings.TrimSpace(string(baseCommit))
	sourceTree, err := treeDigest(sourceRepo, base)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	history, err := git(sourceRepo, "rev-list", "--reverse", base)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	workspaceID := workspaceID(task, digest(taskBytes), digest(oracleBytes))
	parent := filepath.Dir(opts.OutputDir)
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(opts.OutputDir)+".tmp-*")
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()
	for _, dir := range []string{"freeze", "reconstruction", "interventions/snapshots", "evaluation"} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			return FreezeReceipt{}, ContaminationReport{}, err
		}
	}
	blind := filepath.Join(tmp, "blind-repository")
	if err := os.MkdirAll(blind, 0o755); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if _, err := git(blind, "init"); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if _, err := git(blind, "fetch", "--no-tags", "--depth=2147483647", sourceRepo, base); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, fmt.Errorf("fetch base ancestors: %w", err)
	}
	if _, err := git(blind, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	_, _ = git(blind, "remote", "remove", "origin")
	blindRevBytes, err := git(blind, "rev-parse", "HEAD")
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	blindRev := strings.TrimSpace(string(blindRevBytes))
	blindTree, err := treeDigest(blind, blindRev)
	if err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "freeze", "task.yaml"), taskBytes, 0o644); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "freeze", "oracle-digest.txt"), []byte(digest(oracleBytes)+"\n"), 0o644); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	report := Contamination(blind, oracle, opts.OraclePath, digest(oracleBytes), sourceRepo)
	commits := clean(strings.Split(strings.TrimSpace(string(history)), "\n"))
	receipt := FreezeReceipt{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, WorkspaceID: workspaceID,
		PolicyID: FreezePolicyID, PolicyVersion: FreezePolicyVersion, TaskID: task.TaskID, RepositoryID: task.RepositoryID, RepositoryDomain: task.RepositoryDomain,
		SourceBaseRevision: base, BlindRevision: blindRev, SourceTreeDigestSHA256: sourceTree, BlindTreeDigestSHA256: blindTree,
		TaskManifestDigestSHA256: digest(taskBytes), OracleManifestDigestSHA256: digest(oracleBytes), ContaminationStatus: report.Status,
		CommitCount: len(commits), HistoryExportDigestSHA256: digest(history),
	}
	if len(commits) > 0 {
		receipt.OldestReachableCommit = commits[0]
		receipt.NewestReachableCommit = commits[len(commits)-1]
	}
	if sourceTree != blindTree {
		report.Status = ContaminationContaminated
		report.Checks = append(report.Checks, Check{Code: "benchmark.freeze.tree_mismatch", Status: "fail"})
		receipt.ContaminationStatus = report.Status
	}
	receipt = finalizeFreeze(receipt)
	report = finalizeContamination(report)
	if err := writeYAML(filepath.Join(tmp, "freeze", "freeze-receipt.yaml"), freezeEnvelope{receipt}); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if err := writeYAML(filepath.Join(tmp, "freeze", "contamination-report.yaml"), contaminationEnvelope{report}); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	if opts.Check {
		existingReceipt, existingReport, err := LoadWorkspaceFreeze(opts.OutputDir)
		if err != nil {
			return FreezeReceipt{}, ContaminationReport{}, err
		}
		if !bytes.Equal(canonical(existingReceipt), canonical(receipt)) || !bytes.Equal(canonical(existingReport), canonical(report)) {
			return receipt, report, errors.New("check failed: freeze artifacts differ")
		}
		return receipt, report, nil
	}
	old := opts.OutputDir + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(opts.OutputDir); err == nil {
		if err := os.Rename(opts.OutputDir, old); err != nil {
			return FreezeReceipt{}, ContaminationReport{}, err
		}
	}
	if err := os.Rename(tmp, opts.OutputDir); err != nil {
		if _, statErr := os.Stat(old); statErr == nil {
			_ = os.Rename(old, opts.OutputDir)
		}
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	_ = os.RemoveAll(old)
	cleanup = false
	return receipt, report, nil
}

func Contamination(blindRepo string, oracle Oracle, oraclePath, oracleDigest, sourceRepo string) ContaminationReport {
	r := ContaminationReport{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Status: ContaminationClean}
	add := func(code, status, detail string) {
		r.Checks = append(r.Checks, Check{Code: code, Status: status, Detail: detail})
		if status != "pass" {
			r.Status = ContaminationContaminated
			r.Reasons = append(r.Reasons, Reason{Code: code, Detail: detail})
		}
	}
	if out, err := git(blindRepo, "remote"); err == nil && strings.TrimSpace(string(out)) == "" {
		add("benchmark.contamination.no_remotes", "pass", "")
	} else {
		add("benchmark.contamination.no_remotes", "fail", strings.TrimSpace(string(out)))
	}
	if oracle.OracleRevision != "" {
		if _, err := git(blindRepo, "cat-file", "-e", oracle.OracleRevision+"^{commit}"); err != nil {
			add("benchmark.contamination.oracle_revision_absent", "pass", "")
		} else {
			add("benchmark.contamination.oracle_revision_absent", "fail", oracle.OracleRevision)
		}
	}
	if oracle.OraclePatchSHA256 != "" {
		if hasFileDigest(blindRepo, oracle.OraclePatchSHA256) {
			add("benchmark.contamination.oracle_patch_absent", "fail", oracle.OraclePatchSHA256)
		} else {
			add("benchmark.contamination.oracle_patch_absent", "pass", "")
		}
	}
	if containsPathString(blindRepo, oraclePath) || containsPathString(blindRepo, oracleDigest) {
		add("benchmark.contamination.oracle_manifest_absent", "fail", "oracle material found in blind workspace")
	} else {
		add("benchmark.contamination.oracle_manifest_absent", "pass", "")
	}
	if sourceRepo != "" && containsPathString(blindRepo, sourceRepo) {
		add("benchmark.contamination.source_path_absent", "fail", "source repo path found in blind workspace")
	} else {
		add("benchmark.contamination.source_path_absent", "pass", "")
	}
	if containsPathString(blindRepo, "github.com/globulario/sensei") {
		add("benchmark.contamination.globular_knowledge_absent", "fail", "globulario/sensei literal found")
	} else {
		add("benchmark.contamination.globular_knowledge_absent", "pass", "")
	}
	if out, err := git(blindRepo, "status", "--porcelain"); err == nil && strings.TrimSpace(string(out)) == "" {
		add("benchmark.contamination.working_tree_clean", "pass", "")
	} else {
		add("benchmark.contamination.working_tree_clean", "fail", strings.TrimSpace(string(out)))
	}
	return r
}

func Reconstruct(workspace, questionCreatedAt string, check bool) (ReconstructionReceipt, error) {
	if strings.TrimSpace(workspace) == "" {
		return ReconstructionReceipt{}, errors.New("workspace is required")
	}
	if strings.TrimSpace(questionCreatedAt) == "" {
		return ReconstructionReceipt{}, errors.New("question-created-at is required")
	}
	freeze, contamination, err := LoadWorkspaceFreeze(workspace)
	if err != nil {
		return ReconstructionReceipt{}, err
	}
	if contamination.Status != ContaminationClean {
		receipt := finalizeReconstruction(ReconstructionReceipt{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, WorkspaceID: freeze.WorkspaceID, TaskID: freeze.TaskID, Status: ReconstructionContaminated, ColdStart: true, Limitations: []architecture.Limitation{{Source: "benchmark.contamination", Reason: "workspace contamination prevents reconstruction", Blocking: true}}})
		return receipt, writeOrCheckReconstruction(workspace, receipt, check)
	}
	blind := filepath.Join(workspace, "blind-repository")
	tree, err := treeDigest(blind, freeze.BlindRevision)
	if err != nil {
		return ReconstructionReceipt{}, err
	}
	receipt := ReconstructionReceipt{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, WorkspaceID: freeze.WorkspaceID, TaskID: freeze.TaskID,
		Status: ReconstructionUnavailable, ColdStart: true, BlindRepositoryDigestSHA256: tree,
		FactsDigestSHA256: digest([]byte("not-run")), CandidatesDigestSHA256: digest([]byte("not-run")), GraphDigestSHA256: digest([]byte("isolated-empty")),
		ClosureVerdict: "uncertifiable", ConvergenceStatus: "uncertifiable", AdmissionDecision: "uncertifiable",
		Limitations: []architecture.Limitation{{Source: "benchmark.reconstruct", Reason: "full external reconstruction orchestration requires explicit extraction fixtures or real pilot inputs; no agent, tests, graph import, or network executed", Blocking: true}},
	}
	receipt = finalizeReconstruction(receipt)
	return receipt, writeOrCheckReconstruction(workspace, receipt, check)
}

func Evaluate(opts EvaluateOptions) (TaskReport, error) {
	freeze, contamination, err := LoadWorkspaceFreeze(opts.Workspace)
	if err != nil {
		return TaskReport{}, err
	}
	recon, err := LoadReconstruction(opts.Workspace)
	if err != nil {
		return TaskReport{}, err
	}
	oracleBytes, err := os.ReadFile(opts.OraclePath)
	if err != nil {
		return TaskReport{}, err
	}
	oracle, err := LoadOracle(opts.OraclePath)
	if err != nil {
		return TaskReport{}, err
	}
	review, err := LoadQuestionReview(opts.QuestionReviewPath)
	if err != nil {
		return TaskReport{}, err
	}
	mapping, err := LoadOracleMapping(opts.OracleMappingPath)
	if err != nil {
		return TaskReport{}, err
	}
	var interventions []Intervention
	if opts.InterventionLedgerPath != "" {
		ledger, err := LoadInterventionLedger(opts.InterventionLedgerPath)
		if err != nil {
			return TaskReport{}, err
		}
		interventions = ledger.Interventions
	}
	reveal := revealReceipt(oracle, digest(oracleBytes))
	falseGreens := criticalFalseGreens(recon, mapping)
	outcome := OutcomeUncertifiable
	var reasons []Reason
	if contamination.Status != ContaminationClean {
		outcome = OutcomeNotDemonstrated
		reasons = append(reasons, Reason{Code: "benchmark.contamination.invalid"})
	} else if len(falseGreens) > 0 {
		outcome = OutcomeNotDemonstrated
		reasons = append(reasons, Reason{Code: FindingCriticalFalseGreen})
	} else if recon.Status == ReconstructionFrozen && (recon.AdmissionDecision == "admitted" || recon.AdmissionDecision == "admitted_with_conditions") {
		outcome = OutcomeDemonstrated
	} else if recon.ClosureVerdict == "open" || recon.AdmissionDecision == "waiting" || recon.AdmissionDecision == "refused" {
		outcome = OutcomeCorrectlyOpen
	} else {
		reasons = append(reasons, Reason{Code: "benchmark.required_receipts.unavailable"})
	}
	report := TaskReport{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, RepositoryID: freeze.RepositoryID, TaskID: freeze.TaskID, FreezeReceipt: freeze, Contamination: contamination, Reconstruction: recon, Interventions: interventions, QuestionReview: review.Reviews, OracleReveal: reveal, OracleMapping: mapping, FalseGreens: falseGreens, Outcome: outcome, Reasons: reasons}
	report = finalizeTaskReport(report)
	if opts.Check {
		var existing taskReportEnvelope
		if err := loadYAML(opts.OutputPath, &existing); err != nil {
			return report, err
		}
		if !bytes.Equal(canonical(existing.ArchitectureExternalBenchmarkReport), canonical(report)) {
			return report, errors.New("check failed: task report differs")
		}
		return report, nil
	}
	if opts.OutputPath != "" {
		if protectedOutputPath(opts.OutputPath) {
			return report, errors.New("benchmark output must not be under active awareness, intent, or embeddata paths")
		}
		if err := writeYAML(opts.OutputPath, taskReportEnvelope{report}); err != nil {
			return report, err
		}
	}
	return report, nil
}

func Status(workspace, reportPath string) (string, error) {
	freeze, contamination, err := LoadWorkspaceFreeze(workspace)
	if err != nil {
		return "", err
	}
	recon, _ := LoadReconstruction(workspace)
	var report TaskReport
	if reportPath != "" {
		var env taskReportEnvelope
		if err := loadYAML(reportPath, &env); err != nil {
			return "", err
		}
		report = env.ArchitectureExternalBenchmarkReport
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", freeze.TaskID)
	fmt.Fprintf(&b, "Base: %s\n", freeze.SourceBaseRevision)
	fmt.Fprintf(&b, "Blind contamination: %s\n", contamination.Status)
	if recon.SchemaVersion != "" {
		fmt.Fprintf(&b, "Initial closure: %s\n", recon.ClosureVerdict)
		fmt.Fprintf(&b, "Final closure: %s\n", recon.ClosureVerdict)
		fmt.Fprintf(&b, "Admission: %s\n", recon.AdmissionDecision)
	}
	if report.SchemaVersion != "" {
		fmt.Fprintf(&b, "Human interventions: %d\n", len(report.Interventions))
		fmt.Fprintf(&b, "Critical false green: %s\n", yesNo(len(report.FalseGreens) > 0))
		fmt.Fprintf(&b, "Outcome: %s\n", report.Outcome)
	} else {
		fmt.Fprintf(&b, "Critical false green: unavailable\n")
		fmt.Fprintf(&b, "Outcome: unavailable\n")
	}
	return b.String(), nil
}

func LoadWorkspaceFreeze(workspace string) (FreezeReceipt, ContaminationReport, error) {
	var fenv freezeEnvelope
	if err := loadYAML(filepath.Join(workspace, "freeze", "freeze-receipt.yaml"), &fenv); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	var cenv contaminationEnvelope
	if err := loadYAML(filepath.Join(workspace, "freeze", "contamination-report.yaml"), &cenv); err != nil {
		return FreezeReceipt{}, ContaminationReport{}, err
	}
	f := fenv.ArchitectureBenchmarkFreeze
	c := cenv.ArchitectureBenchmarkContamination
	if freezeDigest(f) != f.ReceiptDigestSHA256 {
		return f, c, errors.New("freeze receipt digest invalid")
	}
	if contaminationDigest(c) != c.ReportDigestSHA256 {
		return f, c, errors.New("contamination report digest invalid")
	}
	return f, c, nil
}

func LoadReconstruction(workspace string) (ReconstructionReceipt, error) {
	var env reconstructionEnvelope
	if err := loadYAML(filepath.Join(workspace, "reconstruction", "reconstruction-receipt.yaml"), &env); err != nil {
		return ReconstructionReceipt{}, err
	}
	r := env.ArchitectureBenchmarkReconstruction
	if reconstructionDigest(r) != r.ReceiptDigestSHA256 {
		return r, errors.New("reconstruction receipt digest invalid")
	}
	return r, nil
}

func LoadInterventionLedger(path string) (InterventionLedger, error) {
	var env interventionEnvelope
	if err := loadYAML(path, &env); err != nil {
		return InterventionLedger{}, err
	}
	l := env.ArchitectureBenchmarkInterventions
	for _, i := range l.Interventions {
		if !oneOf(i.Kind, InterventionArchitectAnswer, InterventionEvidencePointer, InterventionEvidenceStateUpdate, InterventionKnowledgeOverlay, InterventionScopeClarification, InterventionAcceptedUnknown, InterventionManualQuestionReview) {
			return l, fmt.Errorf("unknown intervention kind %s", i.Kind)
		}
		if i.SourceArtifact == "" || !isSHA256(i.SourceDigestSHA256) {
			return l, fmt.Errorf("intervention %s requires exact artifact digest", i.ID)
		}
		if i.Classification == "" {
			i.Classification = InterventionNotAssessed
		}
	}
	return l, nil
}

func LoadQuestionReview(path string) (QuestionReviewDocument, error) {
	var env questionReviewEnvelope
	if err := loadYAML(path, &env); err != nil {
		return QuestionReviewDocument{}, err
	}
	doc := env.ArchitectureBenchmarkQuestionReview
	for _, r := range doc.Reviews {
		if r.ReviewerRole == "" || r.Rationale == "" {
			return doc, errors.New("question review requires reviewer and rationale")
		}
		if !oneOf(r.Label, QuestionValidArchitect, QuestionValidEvidence, QuestionAnswerableBaseline, QuestionDuplicate, QuestionTooBroad, QuestionIrrelevant, QuestionInsufficientGround, QuestionIncorrectPremise, QuestionAcceptedUnknown, QuestionNotReviewed) {
			return doc, fmt.Errorf("unknown question review label %s", r.Label)
		}
	}
	return doc, nil
}

func LoadOracleMapping(path string) (OracleMapping, error) {
	var env oracleMappingEnvelope
	if err := loadYAML(path, &env); err != nil {
		return OracleMapping{}, err
	}
	m := env.ArchitectureBenchmarkOracleMapping
	for _, c := range m.Concepts {
		if c.OracleConceptID == "" || len(c.SourceRefs) == 0 {
			return m, errors.New("oracle concept mapping requires concept id and source refs")
		}
		if !oneOf(c.Alignment, AlignmentAnticipated, AlignmentPartiallyAnticipated, AlignmentMissed, AlignmentUnrelated, AlignmentAmbiguous) {
			return m, fmt.Errorf("unknown oracle alignment %s", c.Alignment)
		}
	}
	return m, nil
}

func MarshalFreezeYAML(r FreezeReceipt) ([]byte, error) {
	r = finalizeFreeze(r)
	return yaml.Marshal(freezeEnvelope{r})
}
func MarshalContaminationYAML(r ContaminationReport) ([]byte, error) {
	r = finalizeContamination(r)
	return yaml.Marshal(contaminationEnvelope{r})
}
func MarshalReconstructionYAML(r ReconstructionReceipt) ([]byte, error) {
	r = finalizeReconstruction(r)
	return yaml.Marshal(reconstructionEnvelope{r})
}
func MarshalTaskReportYAML(r TaskReport) ([]byte, error) {
	r = finalizeTaskReport(r)
	return yaml.Marshal(taskReportEnvelope{r})
}

func finalizeFreeze(r FreezeReceipt) FreezeReceipt {
	r.SchemaVersion = SchemaVersion
	r.GeneratedBy = GeneratedBy
	r.PolicyID = FreezePolicyID
	r.PolicyVersion = FreezePolicyVersion
	r.ReceiptDigestSHA256 = freezeDigest(r)
	return r
}
func freezeDigest(r FreezeReceipt) string { r.ReceiptDigestSHA256 = ""; return digest(canonical(r)) }
func finalizeContamination(r ContaminationReport) ContaminationReport {
	r.SchemaVersion = SchemaVersion
	r.GeneratedBy = GeneratedBy
	if r.Status == "" {
		r.Status = ContaminationClean
	}
	sort.SliceStable(r.Checks, func(i, j int) bool { return r.Checks[i].Code < r.Checks[j].Code })
	r.Reasons = normalizeReasons(r.Reasons)
	r.ReportDigestSHA256 = contaminationDigest(r)
	return r
}
func contaminationDigest(r ContaminationReport) string {
	r.ReportDigestSHA256 = ""
	return digest(canonical(r))
}
func finalizeReconstruction(r ReconstructionReceipt) ReconstructionReceipt {
	r.SchemaVersion = SchemaVersion
	r.GeneratedBy = GeneratedBy
	r.ReceiptDigestSHA256 = reconstructionDigest(r)
	return r
}
func reconstructionDigest(r ReconstructionReceipt) string {
	r.ReceiptDigestSHA256 = ""
	return digest(canonical(r))
}
func finalizeTaskReport(r TaskReport) TaskReport {
	r.SchemaVersion = SchemaVersion
	r.GeneratedBy = GeneratedBy
	r.FalseGreens = sortFindings(r.FalseGreens)
	r.Reasons = normalizeReasons(r.Reasons)
	r.ReportDigestSHA256 = taskReportDigest(r)
	return r
}
func taskReportDigest(r TaskReport) string { r.ReportDigestSHA256 = ""; return digest(canonical(r)) }

func revealReceipt(o Oracle, oracleDigest string) OracleRevealReceipt {
	r := OracleRevealReceipt{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, TaskID: o.TaskID, OracleManifestDigestSHA256: oracleDigest, PatchDigestSHA256: o.OraclePatchSHA256, RevealStatus: "revealed"}
	for _, p := range o.OracleMaterial.DiscussionPaths {
		if data, err := os.ReadFile(p); err == nil {
			r.DiscussionArtifactDigests = append(r.DiscussionArtifactDigests, digest(data))
		}
	}
	for _, p := range o.OracleMaterial.TestReceiptPaths {
		if data, err := os.ReadFile(p); err == nil {
			r.TestReceiptDigests = append(r.TestReceiptDigests, digest(data))
		}
	}
	r.DiscussionArtifactDigests = clean(r.DiscussionArtifactDigests)
	r.TestReceiptDigests = clean(r.TestReceiptDigests)
	r.ReceiptDigestSHA256 = digest(canonical(struct{ OracleRevealReceipt }{r}))
	return r
}

func criticalFalseGreens(recon ReconstructionReceipt, mapping OracleMapping) []Finding {
	if recon.AdmissionDecision != "admitted" && recon.AdmissionDecision != "admitted_with_conditions" {
		return nil
	}
	var out []Finding
	for _, c := range mapping.Concepts {
		if c.Critical && c.Alignment == AlignmentMissed && c.ReviewerConfirmed {
			out = append(out, Finding{Code: FindingCriticalFalseGreen, Severity: "critical", ConceptID: c.OracleConceptID, Summary: "critical oracle concern was missed by Sensei surfaces", ReviewerConfirmed: true})
		}
	}
	return sortFindings(out)
}

func writeOrCheckReconstruction(workspace string, receipt ReconstructionReceipt, check bool) error {
	path := filepath.Join(workspace, "reconstruction", "reconstruction-receipt.yaml")
	data, err := MarshalReconstructionYAML(receipt)
	if err != nil {
		return err
	}
	if check {
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, data) {
			return errors.New("check failed: reconstruction receipt differs")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func workspaceID(t Task, taskDigest, oracleDigest string) string {
	return "benchmark." + t.RepositoryID + "." + digest([]byte(strings.Join([]string{t.RepositoryDomain, t.BaseRevision, taskDigest, oracleDigest, FreezePolicyVersion}, "|")))[:12]
}
func treeDigest(repo, rev string) (string, error) {
	out, err := git(repo, "ls-tree", "-r", "--full-tree", rev)
	if err != nil {
		return "", err
	}
	return digest(out), nil
}
func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}
func loadYAML(path string, out interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
func writeYAML(path string, v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
func digest(data []byte) string      { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func canonical(v interface{}) []byte { data, _ := json.Marshal(v); return data }

func hasFileDigest(root, want string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found || d.IsDir() || strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && digest(data) == want {
			found = true
		}
		return nil
	})
	return found
}
func containsPathString(root, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found || d.IsDir() || strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(data, []byte(needle)) {
			found = true
		}
		return nil
	})
	return found
}
func normalizeScope(s Scope) Scope {
	s.Files = cleanPaths(s.Files)
	s.Symbols = clean(s.Symbols)
	s.Components = clean(s.Components)
	s.ClaimIDs = clean(s.ClaimIDs)
	s.PropositionKeys = clean(s.PropositionKeys)
	return s
}
func clean(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func cleanPaths(in []string) []string {
	for i := range in {
		in[i] = filepath.ToSlash(filepath.Clean(strings.TrimSpace(in[i])))
	}
	return clean(in)
}
func normalizeReasons(in []Reason) []Reason {
	seen := map[string]Reason{}
	for _, r := range in {
		r.Code = strings.TrimSpace(r.Code)
		r.Detail = strings.TrimSpace(r.Detail)
		if r.Code != "" {
			seen[r.Code+"\x00"+r.Detail] = r
		}
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
func sortFindings(in []Finding) []Finding {
	out := append([]Finding{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].ConceptID < out[j].ConceptID
	})
	return out
}
func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}
func isRevision(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
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
func containsLower(s, needle string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
func protectedOutputPath(path string) bool {
	rel := filepath.ToSlash(filepath.Clean(path))
	for _, root := range []string{"docs/awareness", "docs/intent", "golang/server/embeddata"} {
		if rel == root || strings.HasPrefix(rel, root+"/") || strings.Contains(rel, "/"+root+"/") || strings.HasSuffix(rel, "/"+root) {
			return true
		}
	}
	return false
}
