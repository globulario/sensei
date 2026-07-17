// SPDX-License-Identifier: Apache-2.0

package proofrequirements

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Document schema + vocabularies.
const (
	DocumentSchemaVersion = "proofrequirements.document/v1"

	// Extraction completeness.
	ExtractionComplete      = "complete"
	ExtractionIncomplete    = "incomplete"
	ExtractionUncertifiable = "uncertifiable"

	// Proving disposition.
	ProvingReady         = "ready"
	ProvingBlocked       = "blocked"
	ProvingUncertifiable = "uncertifiable"
)

// Closed requirement-source set. Each appears exactly once in a document's
// SourceCoverage.
var mandatorySources = []string{
	OriginAuthorityResolution,
	OriginAdmission,
	"generated_artifacts",
	"repository_proof_obligations",
	OriginResultGraph,
	OriginClosure,
	OriginArchitectQuestions,
}

// Source coverage statuses.
const (
	CoverageConsulted    = "consulted"
	CoverageUnavailable  = "unavailable"
	CoverageInvalid      = "invalid"
	CoverageIncompatible = "incompatible"
)

// SourceCoverage records that a mandatory source was actually computed, not that
// its output array happened to be nonempty.
type SourceCoverage struct {
	Source       string `json:"source" yaml:"source"`
	Status       string `json:"status" yaml:"status"`
	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	Detail       string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// RequirementChange records a requirement that changed relative to admission,
// e.g. an admission requirement the result graph no longer represents (retained,
// never deleted).
type RequirementChange struct {
	Class             string   `json:"class" yaml:"class"`
	ID                string   `json:"id" yaml:"id"`
	Origins           []string `json:"origins,omitempty" yaml:"origins,omitempty"`
	ResultGraphStatus string   `json:"result_graph_status,omitempty" yaml:"result_graph_status,omitempty"`
	Disposition       string   `json:"disposition,omitempty" yaml:"disposition,omitempty"`
}

// Document is the canonical result-bound proof-requirement document.
type Document struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string `json:"generated_by" yaml:"generated_by"`

	ResultBindingDigestSHA256 string `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	CompletionPolicyID        string `json:"completion_policy_id,omitempty" yaml:"completion_policy_id,omitempty"`

	SourceAuthorityResolutionDigestSHA256 string `json:"source_authority_resolution_digest_sha256" yaml:"source_authority_resolution_digest_sha256"`
	SourceAdmissionDecisionDigestSHA256   string `json:"source_admission_decision_digest_sha256" yaml:"source_admission_decision_digest_sha256"`
	SourceGeneratedArtifactsDigestSHA256  string `json:"source_generated_artifacts_digest_sha256" yaml:"source_generated_artifacts_digest_sha256"`
	SourceRepositoryProofDigestSHA256     string `json:"source_repository_proof_digest_sha256" yaml:"source_repository_proof_digest_sha256"`
	SourceGraphDigestSHA256               string `json:"source_graph_digest_sha256" yaml:"source_graph_digest_sha256"`
	SourceClosureDigestSHA256             string `json:"source_closure_digest_sha256" yaml:"source_closure_digest_sha256"`
	SourceQuestionsDigestSHA256           string `json:"source_questions_digest_sha256" yaml:"source_questions_digest_sha256"`

	SourceCoverage []SourceCoverage `json:"source_coverage" yaml:"source_coverage"`

	ExtractionCompleteness string `json:"extraction_completeness" yaml:"extraction_completeness"`
	ProvingDisposition     string `json:"proving_disposition" yaml:"proving_disposition"`

	Obligations               []ObligationRequirement `json:"obligations" yaml:"obligations"`
	RequiredSlots             []Requirement           `json:"required_slots" yaml:"required_slots"`
	RequiredTests             []Requirement           `json:"required_tests" yaml:"required_tests"`
	RuntimeEvidenceProfiles   []Requirement           `json:"runtime_evidence_profiles" yaml:"runtime_evidence_profiles"`
	RequiredRuntimeMechanisms []Requirement           `json:"required_runtime_mechanisms" yaml:"required_runtime_mechanisms"`
	RequiredResultRebuilds    []Requirement           `json:"required_result_rebuilds" yaml:"required_result_rebuilds"`
	ForbiddenMoves            []Requirement           `json:"forbidden_moves" yaml:"forbidden_moves"`
	ClosureBlockers           []Requirement           `json:"closure_blockers" yaml:"closure_blockers"`
	ArchitectQuestions        []Requirement           `json:"architect_questions" yaml:"architect_questions"`

	RequirementChanges []RequirementChange `json:"requirement_changes" yaml:"requirement_changes"`
	Limitations        []string            `json:"limitations" yaml:"limitations"`
}

// QuestionInput is the neutral architect-question accounting the composer needs,
// so proofrequirements never imports the result-pipeline bundle type.
type QuestionInput struct {
	CurrentBlockerIDs              []string
	AccountedBlockerIDs            []string
	UnaccountedBlockerIDs          []string
	DuplicateAccountingIDs         []string
	UnsupportedCriticalIDs         []string
	UnresolvedArchitectQuestionIDs []string
	Actionable                     bool
}

// RepositoryProofOutput is the neutral view of the verified Stage 2 proof
// obligations output (avoids a proofrequirements->generatedartifact cycle).
type RepositoryProofOutput struct {
	Path                 string
	Bytes                []byte
	SemanticDigestSHA256 string
	ByteDigestSHA256     string
}

// GeneratedArtifactSummary is the neutral view of the Stage 2 verification.
type GeneratedArtifactSummary struct {
	ManifestDigestSHA256 string
	VerifiedPaths        []string
	AllRequiredMatched   bool
}

// ComposeInput is the neutral, cycle-free composition input.
type ComposeInput struct {
	ResultBindingDigestSHA256 string

	AuthorityResolution               closureprotocol.AuthorityResolution
	ExpectedAuthorityResolutionDigest string
	AdmissionDecision                 closureprotocol.AdmissionDecision
	ExpectedAdmissionDecisionDigest   string

	GeneratedArtifacts    GeneratedArtifactSummary
	RepositoryProofOutput RepositoryProofOutput

	Graph         closure.GraphIndex
	ClosureReport closure.Report

	Questions QuestionInput
}

// Compose builds the complete result-bound proof-requirement document from the
// seven authoritative sources. It is deterministic and offline: no ledger read,
// no repository read, no second authority extraction. It calls ValidateDocument
// before returning.
func Compose(_ context.Context, in ComposeInput) (Document, error) {
	doc := Document{
		SchemaVersion:             DocumentSchemaVersion,
		GeneratedBy:               "sensei.proofrequirements",
		ResultBindingDigestSHA256: strings.TrimSpace(in.ResultBindingDigestSHA256),
		CompletionPolicyID:        strings.TrimSpace(in.AdmissionDecision.CompletionPolicyID),
	}
	cov := map[string]SourceCoverage{}
	uncertifiable := false

	// --- authority_resolution ---
	authDigest, err := closureprotocol.AuthorityResolutionDigest(in.AuthorityResolution)
	if err != nil {
		return Document{}, err
	}
	doc.SourceAuthorityResolutionDigestSHA256 = authDigest
	if exp := strings.TrimSpace(in.ExpectedAuthorityResolutionDigest); exp != "" && exp != authDigest {
		cov[OriginAuthorityResolution] = SourceCoverage{Source: OriginAuthorityResolution, Status: CoverageInvalid, Detail: "carried authority resolution digest does not recompute"}
	} else {
		mechs, conflict := composeAuthorityMechanisms(in.AuthorityResolution)
		doc.RequiredRuntimeMechanisms = mechs
		if conflict {
			cov[OriginAuthorityResolution] = SourceCoverage{Source: OriginAuthorityResolution, Status: CoverageIncompatible, DigestSHA256: authDigest, Detail: "conflicting runtime mechanism definitions"}
			uncertifiable = true
		} else {
			cov[OriginAuthorityResolution] = SourceCoverage{Source: OriginAuthorityResolution, Status: CoverageConsulted, DigestSHA256: authDigest}
		}
	}

	// --- admission_decision ---
	decDigest := closureprotocol.MustSemanticDigest(in.AdmissionDecision)
	doc.SourceAdmissionDecisionDigestSHA256 = decDigest
	admissionInvalid := false
	if exp := strings.TrimSpace(in.ExpectedAdmissionDecisionDigest); exp != "" && exp != decDigest {
		cov[OriginAdmission] = SourceCoverage{Source: OriginAdmission, Status: CoverageInvalid, Detail: "carried admission decision digest does not recompute"}
		admissionInvalid = true
	}

	// --- generated_artifacts ---
	doc.SourceGeneratedArtifactsDigestSHA256 = strings.TrimSpace(in.GeneratedArtifacts.ManifestDigestSHA256)
	if in.GeneratedArtifacts.AllRequiredMatched {
		cov["generated_artifacts"] = SourceCoverage{Source: "generated_artifacts", Status: CoverageConsulted, DigestSHA256: doc.SourceGeneratedArtifactsDigestSHA256}
	} else {
		cov["generated_artifacts"] = SourceCoverage{Source: "generated_artifacts", Status: CoverageInvalid, Detail: "generated artifacts did not all verify"}
	}

	// --- repository_proof_obligations ---
	doc.SourceRepositoryProofDigestSHA256 = strings.TrimSpace(in.RepositoryProofOutput.SemanticDigestSHA256)
	repoObligations, repoErr := composeRepositoryObligations(in.RepositoryProofOutput)
	if repoErr != nil {
		cov["repository_proof_obligations"] = SourceCoverage{Source: "repository_proof_obligations", Status: CoverageInvalid, Detail: repoErr.Error()}
	} else {
		doc.Obligations = append(doc.Obligations, repoObligations...)
		cov["repository_proof_obligations"] = SourceCoverage{Source: "repository_proof_obligations", Status: CoverageConsulted, DigestSHA256: doc.SourceRepositoryProofDigestSHA256}
	}

	// --- result_graph ---
	graphProj, gerr := ProjectScopedGraph(in.ClosureReport, in.Graph)
	if gerr != nil {
		cov[OriginResultGraph] = SourceCoverage{Source: OriginResultGraph, Status: CoverageInvalid, Detail: gerr.Error()}
	} else {
		doc.Obligations = append(doc.Obligations, graphProj.Obligations...)
		doc.RequiredTests = append(doc.RequiredTests, graphProj.RequiredTests...)
		doc.ForbiddenMoves = append(doc.ForbiddenMoves, graphProj.ForbiddenMoves...)
		cov[OriginResultGraph] = SourceCoverage{Source: OriginResultGraph, Status: CoverageConsulted}
	}

	// --- closure_assessment ---
	doc.SourceClosureDigestSHA256 = closureprotocol.MustSemanticDigest(in.ClosureReport)
	doc.ClosureBlockers = composeClosureBlockers(in.ClosureReport)
	cov[OriginClosure] = SourceCoverage{Source: OriginClosure, Status: CoverageConsulted, DigestSHA256: doc.SourceClosureDigestSHA256}

	// --- architect_questions ---
	doc.SourceQuestionsDigestSHA256 = closureprotocol.MustSemanticDigest(in.Questions)
	doc.ArchitectQuestions = composeArchitectQuestions(in.Questions)
	cov[OriginArchitectQuestions] = SourceCoverage{Source: OriginArchitectQuestions, Status: CoverageConsulted, DigestSHA256: doc.SourceQuestionsDigestSHA256}

	// Admission is the monotonic floor. Its requirements are composed and merged
	// last so the merge can attach definitions from graph and repository.
	slotsIncomplete := false
	if !admissionInvalid {
		admissionSlots, evidence, rebuilds, changes, incomplete, uncert := composeAdmission(in, graphProj, repoObligations)
		doc.RequiredSlots = append(doc.RequiredSlots, admissionSlots...)
		doc.RuntimeEvidenceProfiles = append(doc.RuntimeEvidenceProfiles, evidence...)
		doc.RequiredResultRebuilds = append(doc.RequiredResultRebuilds, rebuilds...)
		doc.RequirementChanges = append(doc.RequirementChanges, changes...)
		slotsIncomplete = incomplete
		if uncert {
			uncertifiable = true
		}
		cov[OriginAdmission] = SourceCoverage{Source: OriginAdmission, Status: CoverageConsulted, DigestSHA256: decDigest}
	}
	// Graph-defined slots/evidence merge into the admission floor.
	doc.RequiredSlots = append(doc.RequiredSlots, graphProj.RequiredSlots...)
	doc.RuntimeEvidenceProfiles = append(doc.RuntimeEvidenceProfiles, graphProj.RuntimeEvidenceProfiles...)

	// Monotonic merge by class + id.
	doc.RequiredSlots = mergeRequirements(doc.RequiredSlots)
	doc.RequiredTests = mergeRequirements(doc.RequiredTests)
	doc.RuntimeEvidenceProfiles = mergeRequirements(doc.RuntimeEvidenceProfiles)
	doc.RequiredRuntimeMechanisms = mergeRequirements(doc.RequiredRuntimeMechanisms)
	doc.RequiredResultRebuilds = mergeRequirements(doc.RequiredResultRebuilds)
	doc.ForbiddenMoves = mergeRequirements(doc.ForbiddenMoves)
	doc.ClosureBlockers = mergeRequirements(doc.ClosureBlockers)
	doc.ArchitectQuestions = mergeRequirements(doc.ArchitectQuestions)
	doc.Obligations = mergeObligations(doc.Obligations)

	// Finalize coverage in the closed order.
	for _, s := range mandatorySources {
		c, ok := cov[s]
		if !ok {
			c = SourceCoverage{Source: s, Status: CoverageUnavailable}
		}
		doc.SourceCoverage = append(doc.SourceCoverage, c)
	}

	// Extraction completeness and proving disposition.
	doc.ExtractionCompleteness, doc.ProvingDisposition = decideDisposition(in, doc, uncertifiable, slotsIncomplete)

	if doc.Limitations == nil {
		doc.Limitations = []string{}
	}
	if err := ValidateDocument(doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ValidateDocument enforces the document's structural completeness laws: the
// schema is stamped, every mandatory source is covered exactly once, the
// completeness and disposition axes hold recognized values, and no requirement
// carries an empty id or an empty origin set. It is called from Compose and is
// the single validation gate a caller can re-run.
func ValidateDocument(doc Document) error {
	if doc.SchemaVersion != DocumentSchemaVersion {
		return fmt.Errorf("proofrequirements: document schema version %q, want %q", doc.SchemaVersion, DocumentSchemaVersion)
	}
	if strings.TrimSpace(doc.ResultBindingDigestSHA256) == "" {
		return fmt.Errorf("proofrequirements: document missing result binding digest")
	}
	// Every mandatory source appears exactly once.
	want := map[string]bool{}
	for _, s := range mandatorySources {
		want[s] = true
	}
	seen := map[string]bool{}
	for _, c := range doc.SourceCoverage {
		if !want[c.Source] {
			return fmt.Errorf("proofrequirements: unexpected source %q in coverage", c.Source)
		}
		if seen[c.Source] {
			return fmt.Errorf("proofrequirements: duplicate source %q in coverage", c.Source)
		}
		seen[c.Source] = true
		switch c.Status {
		case CoverageConsulted, CoverageUnavailable, CoverageInvalid, CoverageIncompatible:
		default:
			return fmt.Errorf("proofrequirements: source %q has unknown status %q", c.Source, c.Status)
		}
	}
	for s := range want {
		if !seen[s] {
			return fmt.Errorf("proofrequirements: mandatory source %q not covered", s)
		}
	}
	switch doc.ExtractionCompleteness {
	case ExtractionComplete, ExtractionIncomplete, ExtractionUncertifiable:
	default:
		return fmt.Errorf("proofrequirements: unknown extraction completeness %q", doc.ExtractionCompleteness)
	}
	switch doc.ProvingDisposition {
	case ProvingReady, ProvingBlocked, ProvingUncertifiable:
	default:
		return fmt.Errorf("proofrequirements: unknown proving disposition %q", doc.ProvingDisposition)
	}
	for _, group := range [][]Requirement{
		doc.RequiredSlots, doc.RequiredTests, doc.RuntimeEvidenceProfiles,
		doc.RequiredRuntimeMechanisms, doc.RequiredResultRebuilds, doc.ForbiddenMoves,
		doc.ClosureBlockers, doc.ArchitectQuestions,
	} {
		for _, r := range group {
			if strings.TrimSpace(r.ID) == "" {
				return fmt.Errorf("proofrequirements: %s requirement with empty id", r.Class)
			}
			if len(r.Origins) == 0 {
				return fmt.Errorf("proofrequirements: requirement %q carries no origin", r.ID)
			}
		}
	}
	for _, o := range doc.Obligations {
		if strings.TrimSpace(o.ID) == "" {
			return fmt.Errorf("proofrequirements: obligation with empty id")
		}
		if len(o.Origins) == 0 {
			return fmt.Errorf("proofrequirements: obligation %q carries no origin", o.ID)
		}
	}
	return nil
}

func decideDisposition(in ComposeInput, doc Document, uncertifiable, slotsIncomplete bool) (string, string) {
	verdict := strings.TrimSpace(in.ClosureReport.Verdict)
	allConsulted := true
	for _, c := range doc.SourceCoverage {
		if c.Status != CoverageConsulted {
			allConsulted = false
		}
	}
	switch {
	case uncertifiable || verdict == closure.VerdictUncertifiable:
		return ExtractionUncertifiable, ProvingUncertifiable
	case !allConsulted || slotsIncomplete:
		return ExtractionIncomplete, ProvingBlocked
	}
	// Extraction is complete. Proving readiness is a separate axis: pending proof
	// work does not block, but an unresolved architect/governance/mechanical
	// decision does.
	if !in.Questions.Actionable ||
		len(in.Questions.UnresolvedArchitectQuestionIDs) > 0 ||
		len(in.Questions.UnaccountedBlockerIDs) > 0 ||
		len(in.Questions.DuplicateAccountingIDs) > 0 ||
		len(in.Questions.UnsupportedCriticalIDs) > 0 {
		return ExtractionComplete, ProvingBlocked
	}
	return ExtractionComplete, ProvingReady
}
