// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// GroundingSnapshot defines the deterministic grounding context to check candidates offline.
type GroundingSnapshot struct {
	Files               []string `json:"files" yaml:"files"`
	Symbols             []string `json:"symbols" yaml:"symbols"`
	GraphNodeIDs        []string `json:"graph_node_ids" yaml:"graph_node_ids"`
	ClaimIDs            []string `json:"claim_ids" yaml:"claim_ids"`
	ObservationIDs      []string `json:"observation_ids" yaml:"observation_ids"`
	EvidenceReceiptIDs  []string `json:"evidence_receipt_ids" yaml:"evidence_receipt_ids"`
	ExistingQuestionIDs []string `json:"existing_question_ids" yaml:"existing_question_ids"`
}

// Result binds the canonical investigation document plus Phase 10 sidecar structures.
type Result struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string `json:"generated_by" yaml:"generated_by"`

	Binding Binding `json:"binding" yaml:"binding"`

	Document investigation.Document `json:"document" yaml:"document"`

	Candidates       []CandidateEnvelope    `json:"candidates" yaml:"candidates"`
	Challenges       []ChallengeReceipt     `json:"challenges" yaml:"challenges"`
	EvidenceRequests []EvidenceRequest      `json:"evidence_requests" yaml:"evidence_requests"`
	Rankings         []RankingRecord        `json:"rankings" yaml:"rankings"`
	Counterexamples  []CounterexampleRecord `json:"counterexamples,omitempty" yaml:"counterexamples,omitempty"`

	Limitations []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Receipt     RunReceipt                `json:"receipt" yaml:"receipt"`
}

const (
	ComposerSchemaVersion             = "investigator.result.v1"
	ComposerGeneratedBy               = "sensei.investigator.composer"
	DefaultComposerGeneratorVersion   = "composer.v1"
	DefaultComposerRulesetVersion     = "deterministic-rules.v1"
	ComposerPostProcessingVersion     = "investigator.compose.v1"
	ComposerNondeterminismDeclaration = "deterministic_only"
)

// InputDigests bind repository-wide inputs that are not copied into the
// investigation document. They remain exact inputs to candidate identity and
// the final run receipt.
type InputDigests struct {
	GraphDigestSHA256             string
	CurrentClaimsDigestSHA256     string
	ClosureStateDigestSHA256      string
	ExistingQuestionsDigestSHA256 string
	ReviewHistoryDigestSHA256     string
}

// ComposeInput contains only frozen documents and exact digests. It performs
// no repository, network, or wall-clock reads.
type ComposeInput struct {
	How       investigation.Document
	Why       investigation.Document
	Grounding GroundingSnapshot
	Digests   InputDigests
}

// ComposeOptions are caller-provided deterministic run metadata.
type ComposeOptions struct {
	GeneratorVersion string
	RulesetVersion   string
	TimestampSource  string
	ResourceLimits   map[string]string
}

// Compose builds advisory candidates, challenges, evidence requests,
// counterexamples, rankings, and exact receipts from bound HOW and WHY inputs.
func Compose(input ComposeInput, options ComposeOptions) (Result, error) {
	binding, err := validateComposeInputs(input, options)
	if err != nil {
		return Result{}, err
	}

	drafts, limitations, err := deriveCandidateDrafts(input.How, input.Why, binding)
	if err != nil {
		return Result{}, err
	}

	allEvidence := append(append([]investigation.EvidenceReceipt(nil), input.How.RawEvidence...), input.Why.RawEvidence...)
	claims, envelopes, requests, challenges, counterexamples, rankings, err := materializeDrafts(drafts, input.Why, allEvidence, binding)
	if err != nil {
		return Result{}, err
	}

	document, err := composeDocument(input.How, input.Why, claims, counterexamples, limitations, options)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		SchemaVersion:    ComposerSchemaVersion,
		GeneratedBy:      ComposerGeneratedBy,
		Binding:          binding,
		Document:         document,
		Candidates:       envelopes,
		Challenges:       challenges,
		EvidenceRequests: requests,
		Rankings:         rankings,
		Counterexamples:  counterexamples,
		Limitations:      limitations,
	}

	normalized, err := Normalize(result)
	if err != nil {
		return Result{}, err
	}
	result = normalized

	semantic, err := ComputeReceiptSemanticDigests(result)
	if err != nil {
		return Result{}, err
	}
	result.Receipt = RunReceipt{
		SchemaVersion:                 result.SchemaVersion,
		GeneratedBy:                   result.GeneratedBy,
		InputBinding:                  result.Binding,
		GroundingSnapshotDigestSHA256: result.Binding.GroundingSnapshotDigestSHA256,
		HowDocumentDigestSHA256:       result.Binding.HowDocumentDigestSHA256,
		WhyDocumentDigestSHA256:       result.Binding.WhyDocumentDigestSHA256,
		GraphDigestSHA256:             result.Binding.GraphDigestSHA256,
		CurrentClaimsDigestSHA256:     result.Binding.CurrentClaimsDigestSHA256,
		ClosureStateDigestSHA256:      result.Binding.ClosureStateDigestSHA256,
		ExistingQuestionsDigestSHA256: result.Binding.ExistingQuestionsDigestSHA256,
		ReviewHistoryDigestSHA256:     result.Binding.ReviewHistoryDigestSHA256,
		GeneratorVersion:              result.Binding.GeneratorVersion,
		RulesetVersion:                result.Binding.RulesetVersion,
		CandidateIDsAndDigests:        semantic.CandidateIDsAndDigests,
		ChallengeIDsAndDigests:        semantic.ChallengeIDsAndDigests,
		CounterexampleIDsAndDigests:   semantic.CounterexampleIDsAndDigests,
		EvidenceRequestIDsAndDigests:  semantic.EvidenceRequestIDsAndDigests,
		RankingDigestSHA256:           semantic.RankingDigestSHA256,
		TimestampSource:               strings.TrimSpace(options.TimestampSource),
		ResourceLimits:                normalizedStringMap(options.ResourceLimits),
		NondeterminismDeclaration:     ComposerNondeterminismDeclaration,
	}
	result.Receipt.ExactResultDigestSHA256, err = ResultDigest(result)
	if err != nil {
		return Result{}, err
	}
	if err := Validate(result, input.Grounding); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateComposeInputs(input ComposeInput, options ComposeOptions) (Binding, error) {
	if err := investigation.Validate(input.How); err != nil {
		return Binding{}, fmt.Errorf("HOW document is invalid: %w", err)
	}
	if err := investigation.Validate(input.Why); err != nil {
		return Binding{}, fmt.Errorf("WHY document is invalid: %w", err)
	}
	if input.How.Mode != investigation.ModeHow {
		return Binding{}, fmt.Errorf("HOW document mode must be %q", investigation.ModeHow)
	}
	if input.Why.Mode != investigation.ModeWhy {
		return Binding{}, fmt.Errorf("WHY document mode must be %q", investigation.ModeWhy)
	}
	if input.How.Binding.Repository != input.Why.Binding.Repository {
		return Binding{}, errors.New("HOW and WHY repository bindings must exactly match")
	}

	howDigest, err := exactDocumentDigest("HOW", input.How)
	if err != nil {
		return Binding{}, err
	}
	whyDigest, err := exactDocumentDigest("WHY", input.Why)
	if err != nil {
		return Binding{}, err
	}
	if input.Why.Binding.Why.HowDocumentDigestSHA256 != howDigest {
		return Binding{}, errors.New("WHY document is not bound to the exact HOW document")
	}

	generatorVersion := strings.TrimSpace(options.GeneratorVersion)
	if generatorVersion == "" {
		generatorVersion = DefaultComposerGeneratorVersion
	}
	rulesetVersion := strings.TrimSpace(options.RulesetVersion)
	if rulesetVersion == "" {
		rulesetVersion = DefaultComposerRulesetVersion
	}
	if strings.TrimSpace(options.TimestampSource) == "" {
		return Binding{}, errors.New("timestamp source is required")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(options.TimestampSource)); err != nil {
		return Binding{}, errors.New("timestamp source must be RFC3339")
	}
	if len(options.ResourceLimits) == 0 {
		return Binding{}, errors.New("resource limits are required")
	}

	if input.How.Binding.Repository.GraphDigestSHA256 != "" &&
		input.How.Binding.Repository.GraphDigestSHA256 != strings.TrimSpace(input.Digests.GraphDigestSHA256) {
		return Binding{}, errors.New("input graph digest must match the repository graph binding")
	}
	if !IsSHA256(strings.TrimSpace(input.Why.Binding.EvidenceSnapshotDigestSHA256)) {
		return Binding{}, errors.New("WHY evidence snapshot digest must be an explicit SHA256")
	}

	for name, value := range map[string]string{
		"graph":              input.Digests.GraphDigestSHA256,
		"current claims":     input.Digests.CurrentClaimsDigestSHA256,
		"closure state":      input.Digests.ClosureStateDigestSHA256,
		"existing questions": input.Digests.ExistingQuestionsDigestSHA256,
		"review history":     input.Digests.ReviewHistoryDigestSHA256,
	} {
		if !IsSHA256(strings.TrimSpace(value)) {
			return Binding{}, fmt.Errorf("%s digest must be an explicit SHA256", name)
		}
	}

	groundingDigest, err := GroundingSnapshotDigest(input.Grounding)
	if err != nil {
		return Binding{}, err
	}
	return Binding{
		Repository:                    input.How.Binding.Repository,
		HowDocumentDigestSHA256:       howDigest,
		WhyDocumentDigestSHA256:       whyDigest,
		GraphDigestSHA256:             strings.TrimSpace(input.Digests.GraphDigestSHA256),
		CurrentClaimsDigestSHA256:     strings.TrimSpace(input.Digests.CurrentClaimsDigestSHA256),
		ClosureStateDigestSHA256:      strings.TrimSpace(input.Digests.ClosureStateDigestSHA256),
		ExistingQuestionsDigestSHA256: strings.TrimSpace(input.Digests.ExistingQuestionsDigestSHA256),
		ReviewHistoryDigestSHA256:     strings.TrimSpace(input.Digests.ReviewHistoryDigestSHA256),
		EvidenceSnapshotDigestSHA256:  input.Why.Binding.EvidenceSnapshotDigestSHA256,
		GroundingSnapshotDigestSHA256: groundingDigest,
		GeneratorVersion:              generatorVersion,
		RulesetVersion:                rulesetVersion,
	}, nil
}

func exactDocumentDigest(label string, document investigation.Document) (string, error) {
	digest, err := investigation.CalculateDocumentDigest(document)
	if err != nil {
		return "", fmt.Errorf("calculate %s document digest: %w", label, err)
	}
	if document.Receipt.OutputDocumentDigestSHA256 == "" || document.Receipt.OutputDocumentDigestSHA256 != digest {
		return "", fmt.Errorf("%s document receipt digest does not match exact document", label)
	}
	return digest, nil
}

func composeDocument(
	how investigation.Document,
	why investigation.Document,
	claims []architecture.Claim,
	counterexamples []CounterexampleRecord,
	limitations []architecture.Limitation,
	options ComposeOptions,
) (investigation.Document, error) {
	document := why
	document.GeneratedBy = ComposerGeneratedBy
	document.Coverage = append(append([]investigation.CoverageEntry(nil), how.Coverage...), why.Coverage...)
	document.RawEvidence = append(append([]investigation.EvidenceReceipt(nil), how.RawEvidence...), why.RawEvidence...)
	document.Observations = append(append([]architecture.Fact(nil), how.Observations...), why.Observations...)
	document.CandidateClaims = claims
	document.CandidateQuestions = nil
	document.Counterexamples = make([]investigation.Counterexample, 0, len(counterexamples))
	for _, record := range counterexamples {
		document.Counterexamples = append(document.Counterexamples, record.Counterexample)
	}
	document.Limitations = append(append(append([]architecture.Limitation(nil), how.Limitations...), why.Limitations...), limitations...)

	candidateDigests := make(map[string]string, len(claims)+len(counterexamples))
	for _, claim := range claims {
		bytes, err := json.Marshal(claim)
		if err != nil {
			return investigation.Document{}, err
		}
		candidateDigests[claim.ID] = SHA256Bytes(bytes)
	}
	for _, record := range counterexamples {
		bytes, err := json.Marshal(record.Counterexample)
		if err != nil {
			return investigation.Document{}, err
		}
		candidateDigests[record.Counterexample.ID] = SHA256Bytes(bytes)
	}
	document.Receipt = investigation.RunReceipt{
		SchemaVersion:                document.SchemaVersion,
		GeneratedBy:                  ComposerGeneratedBy,
		Repository:                   document.Binding.Repository,
		GraphDigestSHA256:            document.Binding.Repository.GraphDigestSHA256,
		PlanDigestSHA256:             document.Binding.InvestigationPlanDigestSHA256,
		ExtractorProfileDigestSHA256: document.Binding.ExtractorProfileDigestSHA256,
		EvidenceSnapshotDigestSHA256: document.Binding.EvidenceSnapshotDigestSHA256,
		Model:                        document.Binding.Model,
		PostProcessingVersion:        ComposerPostProcessingVersion,
		OutputCandidateIDsAndDigests: candidateDigests,
		TimestampSource:              strings.TrimSpace(options.TimestampSource),
		ResourceLimits:               normalizedStringMap(options.ResourceLimits),
		NondeterminismDeclaration:    ComposerNondeterminismDeclaration,
	}

	normalized, err := investigation.Normalize(document)
	if err != nil {
		return investigation.Document{}, err
	}
	digest, err := investigation.CalculateDocumentDigest(normalized)
	if err != nil {
		return investigation.Document{}, err
	}
	normalized.Receipt.OutputDocumentDigestSHA256 = digest
	if err := investigation.Validate(normalized); err != nil {
		return investigation.Document{}, fmt.Errorf("composed investigation document is invalid: %w", err)
	}
	return normalized, nil
}

func normalizedStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func sortedFacts(facts []architecture.Fact) []architecture.Fact {
	out := append([]architecture.Fact(nil), facts...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
