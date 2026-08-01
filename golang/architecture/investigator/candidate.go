// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// CandidateKind defines the vocabulary of candidate types.
type CandidateKind string

const (
	KindInvariant      CandidateKind = "invariant"
	KindContract       CandidateKind = "contract"
	KindBoundary       CandidateKind = "boundary"
	KindOwner          CandidateKind = "owner"
	KindFailureMode    CandidateKind = "failure_mode"
	KindGovernanceDebt CandidateKind = "governance_debt"
)

// IsValidCandidateKind validates if a kind belongs to the closed vocabulary.
func IsValidCandidateKind(k CandidateKind) bool {
	switch k {
	case KindInvariant, KindContract, KindBoundary, KindOwner, KindFailureMode, KindGovernanceDebt:
		return true
	default:
		return false
	}
}

// ConfidenceFactor represents one metric scoring parameter.
type ConfidenceFactor struct {
	Metric string `json:"metric" yaml:"metric"`
	Value  int    `json:"value" yaml:"value"`
}

// CandidateEnvelope holds investigation-only candidate metadata.
type CandidateEnvelope struct {
	CandidateID string        `json:"candidate_id" yaml:"candidate_id"`
	ClaimID     string        `json:"claim_id" yaml:"claim_id"`
	OutputKind  CandidateKind `json:"output_kind" yaml:"output_kind"`

	ObservationRefIDs        []string `json:"observation_ref_ids,omitempty" yaml:"observation_ref_ids,omitempty"`
	SupportingEvidenceRefIDs []string `json:"supporting_evidence_ref_ids,omitempty" yaml:"supporting_evidence_ref_ids,omitempty"`
	RefutingEvidenceRefIDs   []string `json:"refuting_evidence_ref_ids,omitempty" yaml:"refuting_evidence_ref_ids,omitempty"`

	FalsificationConditions   []string           `json:"falsification_conditions" yaml:"falsification_conditions"`
	MissingEvidenceRequestIDs []string           `json:"missing_evidence_request_ids,omitempty" yaml:"missing_evidence_request_ids,omitempty"`
	ConfidenceBasis           []ConfidenceFactor `json:"confidence_basis,omitempty" yaml:"confidence_basis,omitempty"`
}

type candidateDraft struct {
	Kind                     CandidateKind
	Claim                    architecture.Claim
	ObservationRefIDs        []string
	SupportingEvidenceRefIDs []string
	RefutingEvidenceRefIDs   []string
	FalsificationConditions  []string
	MissingEvidenceCategory  investigation.EvidenceCategory
	MissingEvidenceReason    string
	RequiredProofStrength    investigation.ProofStrength
}

type ruleDefinition struct {
	FactKind      string
	Predicate     string
	CandidateKind CandidateKind
	InferenceRule string
	Description   string
}

var deterministicFactRules = []ruleDefinition{
	{
		FactKind:      "boundary",
		Predicate:     "crosses_component_boundary_to",
		CandidateKind: KindBoundary,
		InferenceRule: "investigator.boundary_from_component_crossing.v1",
		Description:   "A bound call path crosses a component boundary.",
	},
	{
		FactKind:      "boundary",
		Predicate:     "crosses_package_boundary_to",
		CandidateKind: KindBoundary,
		InferenceRule: "investigator.boundary_from_package_crossing.v1",
		Description:   "A bound call path crosses a package boundary.",
	},
	{
		FactKind:      "contract_seam",
		Predicate:     "implements_interface",
		CandidateKind: KindContract,
		InferenceRule: "investigator.contract_from_interface_implementation.v1",
		Description:   "A bound type implements an interface seam.",
	},
	{
		FactKind:      "contract_seam",
		Predicate:     "exports_interface",
		CandidateKind: KindContract,
		InferenceRule: "investigator.contract_from_exported_interface.v1",
		Description:   "A bound package exports an interface seam.",
	},
	{
		FactKind:      "guard",
		Predicate:     "refuses_when",
		CandidateKind: KindInvariant,
		InferenceRule: "investigator.invariant_from_guard.v1",
		Description:   "A bound guard refuses execution under an explicit condition.",
	},
	{
		FactKind:      "transition",
		Predicate:     "rejects_transition_when",
		CandidateKind: KindInvariant,
		InferenceRule: "investigator.invariant_from_transition_guard.v1",
		Description:   "A bound transition guard rejects an explicit transition.",
	},
	{
		FactKind:      "generation_check",
		Predicate:     "compares_generation",
		CandidateKind: KindInvariant,
		InferenceRule: "investigator.invariant_from_generation_comparison.v1",
		Description:   "A bound path compares generation before proceeding.",
	},
	{
		FactKind:      "generation_check",
		Predicate:     "increments_generation",
		CandidateKind: KindInvariant,
		InferenceRule: "investigator.invariant_from_generation_increment.v1",
		Description:   "A bound mutation increments generation.",
	},
}

func deriveCandidateDrafts(how, why investigation.Document, binding Binding) ([]candidateDraft, []architecture.Limitation, error) {
	facts := sortedFacts(how.Observations)
	allEvidence := append(append([]investigation.EvidenceReceipt(nil), how.RawEvidence...), why.RawEvidence...)

	var drafts []candidateDraft
	for _, fact := range facts {
		rule, ok := ruleForFact(fact)
		if !ok {
			continue
		}
		draft, err := draftFromFact(rule, fact, allEvidence, why.RawEvidence)
		if err != nil {
			return nil, nil, err
		}
		drafts = append(drafts, draft)
	}

	ownerDrafts, err := deriveSingleWriterDrafts(facts, allEvidence, why.RawEvidence)
	if err != nil {
		return nil, nil, err
	}
	drafts = append(drafts, ownerDrafts...)

	drafts, err = normalizeDrafts(drafts)
	if err != nil {
		return nil, nil, err
	}

	var limitations []architecture.Limitation
	if !hasStructuredFailureModeInputs(why) {
		limitations = append(limitations, architecture.Limitation{
			Source:   "investigator.failure_mode_rules",
			Scope:    binding.Repository.RepositoryDomain,
			Reason:   "no structured failure-mode observation was supplied; prose evidence was not interpreted heuristically",
			Blocking: false,
		})
	}
	return drafts, limitations, nil
}

func ruleForFact(fact architecture.Fact) (ruleDefinition, bool) {
	for _, rule := range deterministicFactRules {
		if fact.Kind == rule.FactKind && fact.Predicate == rule.Predicate {
			return rule, true
		}
	}
	return ruleDefinition{}, false
}

func draftFromFact(
	rule ruleDefinition,
	fact architecture.Fact,
	allEvidence []investigation.EvidenceReceipt,
	whyEvidence []investigation.EvidenceReceipt,
) (candidateDraft, error) {
	scope := claimScopeFromFacts([]architecture.Fact{fact}, allEvidence)
	supporting := evidenceForFacts([]architecture.Fact{fact}, allEvidence)
	whySupporting, whyRefuting := classifiedWhyEvidence([]string{fact.ID}, whyEvidence)
	supporting = sortedUnique(append(supporting, whySupporting...))
	refuting := sortedUnique(whyRefuting)

	claim := architecture.Claim{
		Label:               candidateLabel(rule.CandidateKind, fact.Subject, fact.Object),
		Description:         rule.Description + " This remains advisory until governed review.",
		Statement:           architecture.ClaimStatement{Subject: fact.Subject, Predicate: fact.Predicate, Object: fact.Object},
		Scope:               scope,
		ArchitecturalPlane:  architecture.PlaneObserved,
		AssertionOrigin:     architecture.OriginDerived,
		EpistemicStatus:     architecture.StatusUnknown,
		InferenceRule:       rule.InferenceRule,
		PremiseFacts:        []string{fact.ID},
		SupportingEvidence:  supporting,
		RefutingEvidence:    refuting,
		Confidence:          0,
		HumanReviewRequired: true,
		PromotionStatus:     architecture.PromotionCandidate,
		Unknowns:            []string{"historical intent and authoritative ownership remain unresolved"},
		InvalidationConditions: []string{
			"the bound observation is invalidated",
			"the cited evidence no longer resolves in the bound snapshot",
		},
	}
	claim.ID = architecture.StableClaimID(claim)
	if err := architecture.ValidateClaim(claim); err != nil {
		return candidateDraft{}, fmt.Errorf("candidate claim from observation %s is invalid: %w", fact.ID, err)
	}

	category, reason, strength := missingEvidencePolicy(rule.CandidateKind)
	return candidateDraft{
		Kind:                     rule.CandidateKind,
		Claim:                    claim,
		ObservationRefIDs:        []string{fact.ID},
		SupportingEvidenceRefIDs: supporting,
		RefutingEvidenceRefIDs:   refuting,
		FalsificationConditions: []string{
			"a bound counterexample contradicts the proposition",
			"the target disappears from the exact repository tree",
		},
		MissingEvidenceCategory: category,
		MissingEvidenceReason:   reason,
		RequiredProofStrength:   strength,
	}, nil
}

func deriveSingleWriterDrafts(
	facts []architecture.Fact,
	allEvidence []investigation.EvidenceReceipt,
	whyEvidence []investigation.EvidenceReceipt,
) ([]candidateDraft, error) {
	byTarget := map[string][]architecture.Fact{}
	for _, fact := range facts {
		if !isWriterFact(fact) || strings.TrimSpace(fact.Object) == "" || strings.TrimSpace(fact.Subject) == "" {
			continue
		}
		byTarget[fact.Object] = append(byTarget[fact.Object], fact)
	}

	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	var drafts []candidateDraft
	for _, target := range targets {
		group := byTarget[target]
		writers := map[string]bool{}
		for _, fact := range group {
			writers[fact.Subject] = true
		}
		if len(writers) != 1 {
			continue
		}
		var writer string
		for candidate := range writers {
			writer = candidate
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		factIDs := make([]string, 0, len(group))
		for _, fact := range group {
			factIDs = append(factIDs, fact.ID)
		}
		supporting := evidenceForFacts(group, allEvidence)
		whySupporting, whyRefuting := classifiedWhyEvidence(factIDs, whyEvidence)
		supporting = sortedUnique(append(supporting, whySupporting...))
		refuting := sortedUnique(whyRefuting)

		claim := architecture.Claim{
			Label:               candidateLabel(KindOwner, target, writer),
			Description:         "Exactly one writer was observed for the bound target. This is an ownership candidate, not authoritative ownership.",
			Statement:           architecture.ClaimStatement{Subject: target, Predicate: "candidate_owner_is", Object: writer},
			Scope:               claimScopeFromFacts(group, allEvidence),
			ArchitecturalPlane:  architecture.PlaneObserved,
			AssertionOrigin:     architecture.OriginDerived,
			EpistemicStatus:     architecture.StatusUnknown,
			InferenceRule:       "investigator.owner_from_single_writer.v1",
			PremiseFacts:        factIDs,
			SupportingEvidence:  supporting,
			RefutingEvidence:    refuting,
			Confidence:          0,
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
			Unknowns:            []string{"authored owner intent has not been established"},
			InvalidationConditions: []string{
				"a second writer is observed",
				"governed ownership names a different owner",
			},
		}
		claim.ID = architecture.StableClaimID(claim)
		if err := architecture.ValidateClaim(claim); err != nil {
			return nil, fmt.Errorf("owner candidate for target %s is invalid: %w", target, err)
		}

		category, reason, strength := missingEvidencePolicy(KindOwner)
		drafts = append(drafts, candidateDraft{
			Kind:                     KindOwner,
			Claim:                    claim,
			ObservationRefIDs:        factIDs,
			SupportingEvidenceRefIDs: supporting,
			RefutingEvidenceRefIDs:   refuting,
			FalsificationConditions: []string{
				"a second writer is observed in the bound tree",
				"an authored decision names a different owner",
			},
			MissingEvidenceCategory: category,
			MissingEvidenceReason:   reason,
			RequiredProofStrength:   strength,
		})
	}
	return drafts, nil
}

func isWriterFact(fact architecture.Fact) bool {
	if fact.Kind != "write" && fact.Kind != "generation_check" {
		return false
	}
	switch fact.Predicate {
	case "writes", "persists_via", "increments_generation":
		return true
	default:
		return false
	}
}

func evidenceForFacts(facts []architecture.Fact, evidence []investigation.EvidenceReceipt) []string {
	var ids []string
	for _, receipt := range evidence {
		for _, fact := range facts {
			if evidenceSupportsFact(receipt, fact) {
				ids = append(ids, receipt.ID)
				break
			}
		}
	}
	return sortedUnique(ids)
}

func evidenceSupportsFact(receipt investigation.EvidenceReceipt, fact architecture.Fact) bool {
	if receipt.ID == "" {
		return false
	}
	if fact.Evidence.SourceFile != "" {
		if receipt.SourceIdentity != fact.Evidence.SourceFile && !containsString(receipt.Scope.Files, fact.Evidence.SourceFile) {
			return false
		}
	}
	for _, symbol := range fact.Scope.Symbols {
		if !containsString(receipt.Scope.Symbols, symbol) {
			return false
		}
	}
	return true
}

func classifiedWhyEvidence(observationIDs []string, evidence []investigation.EvidenceReceipt) (supporting, refuting []string) {
	for _, receipt := range evidence {
		if !intersects(receipt.Scope.Symbols, observationIDs) {
			continue
		}
		switch receipt.Category {
		case investigation.EvidenceErrorTracking:
			refuting = append(refuting, receipt.ID)
		case investigation.EvidenceDocumentation,
			investigation.EvidenceDesignDocuments,
			investigation.EvidenceSourceControl,
			investigation.EvidenceArchitectFeedback:
			supporting = append(supporting, receipt.ID)
		}
	}
	return sortedUnique(supporting), sortedUnique(refuting)
}

func claimScopeFromFacts(facts []architecture.Fact, evidence []investigation.EvidenceReceipt) architecture.ClaimScope {
	var files, symbols, components []string
	repository := ""
	factEvidence := evidenceForFacts(facts, evidence)
	for _, receipt := range evidence {
		if !containsString(factEvidence, receipt.ID) {
			continue
		}
		files = append(files, receipt.Scope.Files...)
		symbols = append(symbols, receipt.Scope.Symbols...)
		components = append(components, receipt.Scope.Components...)
		if repository == "" {
			repository = receipt.Scope.Repository
		}
	}
	if repository == "" && len(facts) > 0 {
		repository = facts[0].Scope.Repository
	}
	return architecture.ClaimScope{
		Repository: repository,
		Files:      sortedUnique(files),
		Symbols:    sortedUnique(symbols),
		Components: sortedUnique(components),
	}
}

func missingEvidencePolicy(kind CandidateKind) (investigation.EvidenceCategory, string, investigation.ProofStrength) {
	switch kind {
	case KindInvariant, KindFailureMode:
		return investigation.EvidenceErrorTracking, ReasonRefutingEvidenceUnsearched, investigation.ProofIntegrationRuntime
	default:
		return investigation.EvidenceDesignDocuments, ReasonHistoricalRationaleUnresolved, investigation.ProofStaticSource
	}
}

func normalizeDrafts(drafts []candidateDraft) ([]candidateDraft, error) {
	byKey := map[string]candidateDraft{}
	for _, draft := range drafts {
		draft.ObservationRefIDs = sortedUnique(draft.ObservationRefIDs)
		draft.SupportingEvidenceRefIDs = sortedUnique(draft.SupportingEvidenceRefIDs)
		draft.RefutingEvidenceRefIDs = sortedUnique(draft.RefutingEvidenceRefIDs)
		draft.FalsificationConditions = sortedUnique(draft.FalsificationConditions)
		draft.Claim.PremiseFacts = sortedUnique(draft.Claim.PremiseFacts)
		draft.Claim.SupportingEvidence = sortedUnique(draft.Claim.SupportingEvidence)
		draft.Claim.RefutingEvidence = sortedUnique(draft.Claim.RefutingEvidence)
		draft.Claim.Unknowns = sortedUnique(draft.Claim.Unknowns)
		draft.Claim.InvalidationConditions = sortedUnique(draft.Claim.InvalidationConditions)

		bytes, err := json.Marshal(struct {
			Kind          CandidateKind
			Statement     architecture.ClaimStatement
			Scope         architecture.ClaimScope
			InferenceRule string
		}{
			Kind:          draft.Kind,
			Statement:     draft.Claim.Statement,
			Scope:         draft.Claim.Scope,
			InferenceRule: draft.Claim.InferenceRule,
		})
		if err != nil {
			return nil, err
		}
		key := SHA256Bytes(bytes)
		if existing, ok := byKey[key]; ok {
			existing.ObservationRefIDs = sortedUnique(append(existing.ObservationRefIDs, draft.ObservationRefIDs...))
			existing.SupportingEvidenceRefIDs = sortedUnique(append(existing.SupportingEvidenceRefIDs, draft.SupportingEvidenceRefIDs...))
			existing.RefutingEvidenceRefIDs = sortedUnique(append(existing.RefutingEvidenceRefIDs, draft.RefutingEvidenceRefIDs...))
			existing.FalsificationConditions = sortedUnique(append(existing.FalsificationConditions, draft.FalsificationConditions...))
			existing.Claim.PremiseFacts = sortedUnique(append(existing.Claim.PremiseFacts, draft.Claim.PremiseFacts...))
			existing.Claim.SupportingEvidence = sortedUnique(append(existing.Claim.SupportingEvidence, draft.Claim.SupportingEvidence...))
			existing.Claim.RefutingEvidence = sortedUnique(append(existing.Claim.RefutingEvidence, draft.Claim.RefutingEvidence...))
			existing.Claim.Unknowns = sortedUnique(append(existing.Claim.Unknowns, draft.Claim.Unknowns...))
			existing.Claim.InvalidationConditions = sortedUnique(append(existing.Claim.InvalidationConditions, draft.Claim.InvalidationConditions...))
			existing.Claim.ID = architecture.StableClaimID(existing.Claim)
			byKey[key] = existing
			continue
		}
		draft.Claim.ID = architecture.StableClaimID(draft.Claim)
		byKey[key] = draft
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]candidateDraft, 0, len(keys))
	for _, key := range keys {
		draft := byKey[key]
		if err := architecture.ValidateClaim(draft.Claim); err != nil {
			return nil, fmt.Errorf("normalized candidate claim is invalid: %w", err)
		}
		out = append(out, draft)
	}
	return out, nil
}

func hasStructuredFailureModeInputs(why investigation.Document) bool {
	for _, fact := range why.Observations {
		if fact.Kind == "failure_mode" || fact.Kind == "incident_pattern" {
			return true
		}
	}
	return false
}

func candidateLabel(kind CandidateKind, subject, object string) string {
	return fmt.Sprintf("%s candidate: %s -> %s", kind, subject, object)
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if containsString(right, value) {
			return true
		}
	}
	return false
}
