// SPDX-License-Identifier: AGPL-3.0-only

// Package adoption defines the provenance receipt shared by every
// machine-adoptable architectural knowledge class.
package adoption

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	PromotionCandidate      = "candidate"
	PromotionMachineAdopted = "machine_adopted"
	PromotionGoverned       = "governed"

	ReviewNotHumanReviewed = "not_human_reviewed"
)

// Receipt records why a knowledge node was admitted, who or what made the
// decision, and the exact repository/graph snapshot for which it is valid.
// It is deliberately independent of knowledge identity: adding or reviewing a
// receipt must never mint a different Invariant, Decision, Contract, or other
// stable node ID.
type Receipt struct {
	Status               string   `json:"status,omitempty" yaml:"status,omitempty"`
	PromotionStatus      string   `json:"promotion_status,omitempty" yaml:"promotion_status,omitempty"`
	AssertionOrigin      string   `json:"assertion_origin,omitempty" yaml:"assertion_origin,omitempty"`
	EpistemicStatus      string   `json:"epistemic_status,omitempty" yaml:"epistemic_status,omitempty"`
	ArchitecturalPlane   string   `json:"architectural_plane,omitempty" yaml:"architectural_plane,omitempty"`
	DecisionActor        string   `json:"decision_actor,omitempty" yaml:"decision_actor,omitempty"`
	DecisionContext      string   `json:"decision_context,omitempty" yaml:"decision_context,omitempty"`
	DecisionPolicy       string   `json:"decision_policy,omitempty" yaml:"decision_policy,omitempty"`
	DecisionTimestamp    string   `json:"decision_timestamp,omitempty" yaml:"decision_timestamp,omitempty"`
	ValidForRevision     string   `json:"valid_for_revision,omitempty" yaml:"valid_for_revision,omitempty"`
	ValidForGraphDigest  string   `json:"valid_for_graph_digest,omitempty" yaml:"valid_for_graph_digest,omitempty"`
	ReviewStatus         string   `json:"review_status,omitempty" yaml:"review_status,omitempty"`
	AdoptionBasis        []string `json:"adoption_basis,omitempty" yaml:"adoption_basis,omitempty"`
	SourceReceipts       []string `json:"source_receipts,omitempty" yaml:"source_receipts,omitempty"`
	CorroborationKinds   []string `json:"corroboration_kinds,omitempty" yaml:"corroboration_kinds,omitempty"`
	RevocationConditions []string `json:"revocation_conditions,omitempty" yaml:"revocation_conditions,omitempty"`
	Limitations          []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

var (
	assertionOrigins  = values("deterministic_inference", "history_inferred", "model_inferred", "human_authored", "promoted", "observed", "derived", "authored")
	reviewStatuses    = values(ReviewNotHumanReviewed, "human_reviewed", "review_required", "rejected", "superseded")
	promotionStatuses = values(
		PromotionCandidate, PromotionMachineAdopted, PromotionGoverned, "rejected", "superseded",
		// Compatible governed lifecycle values already used by authored YAML.
		"proposed", "accepted", "active", "deprecated",
	)
	epistemicStatuses   = values("unknown", "supported", "contested", "refuted", "stale", "superseded")
	architecturalPlanes = values("observed", "enforced", "intended", "historical", "desired")
)

// Normalize returns a deterministic receipt without inventing a timestamp or
// any other evidence. Repeated values are removed and sorted.
func Normalize(in Receipt) Receipt {
	out := in
	out.Status = strings.TrimSpace(out.Status)
	out.PromotionStatus = strings.ToLower(strings.TrimSpace(out.PromotionStatus))
	out.AssertionOrigin = strings.ToLower(strings.TrimSpace(out.AssertionOrigin))
	out.EpistemicStatus = strings.ToLower(strings.TrimSpace(out.EpistemicStatus))
	out.ArchitecturalPlane = strings.ToLower(strings.TrimSpace(out.ArchitecturalPlane))
	out.DecisionActor = strings.TrimSpace(out.DecisionActor)
	out.DecisionContext = strings.TrimSpace(out.DecisionContext)
	out.DecisionPolicy = strings.TrimSpace(out.DecisionPolicy)
	out.DecisionTimestamp = normalizeTimestamp(out.DecisionTimestamp)
	out.ValidForRevision = strings.TrimSpace(out.ValidForRevision)
	out.ValidForGraphDigest = strings.ToLower(strings.TrimSpace(out.ValidForGraphDigest))
	out.ReviewStatus = strings.ToLower(strings.TrimSpace(out.ReviewStatus))
	out.AdoptionBasis = normalizeList(out.AdoptionBasis)
	out.SourceReceipts = normalizeList(out.SourceReceipts)
	out.CorroborationKinds = normalizeList(out.CorroborationKinds)
	out.RevocationConditions = normalizeList(out.RevocationConditions)
	out.Limitations = normalizeList(out.Limitations)
	return out
}

// ValidateValues checks closed vocabularies and timestamp syntax without
// requiring a complete adoption decision. Importers use this to keep staged
// and legacy documents readable.
func ValidateValues(in Receipt) error {
	r := Normalize(in)
	var errs []string
	validateClosed(&errs, "promotion_status", r.PromotionStatus, promotionStatuses)
	validateClosed(&errs, "assertion_origin", r.AssertionOrigin, assertionOrigins)
	validateClosed(&errs, "epistemic_status", r.EpistemicStatus, epistemicStatuses)
	validateClosed(&errs, "architectural_plane", r.ArchitecturalPlane, architecturalPlanes)
	validateClosed(&errs, "review_status", r.ReviewStatus, reviewStatuses)
	if r.DecisionTimestamp != "" {
		if _, err := time.Parse(time.RFC3339Nano, r.DecisionTimestamp); err != nil {
			errs = append(errs, "decision_timestamp must be RFC3339")
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("invalid adoption receipt: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ValidateMachineAdoption fails closed unless the receipt contains a complete,
// snapshot-bound decision. It does not assess whether the cited evidence is
// sufficient; that belongs to the class-specific adoption policy.
func ValidateMachineAdoption(in Receipt) error {
	r := Normalize(in)
	var errs []string
	if err := ValidateValues(r); err != nil {
		errs = append(errs, err.Error())
	}
	if r.Status != PromotionMachineAdopted {
		errs = append(errs, "status must be machine_adopted")
	}
	if r.PromotionStatus != PromotionMachineAdopted {
		errs = append(errs, "promotion_status must be machine_adopted")
	}
	for name, value := range map[string]string{
		"decision_actor":         r.DecisionActor,
		"decision_context":       r.DecisionContext,
		"decision_policy":        r.DecisionPolicy,
		"decision_timestamp":     r.DecisionTimestamp,
		"valid_for_revision":     r.ValidForRevision,
		"valid_for_graph_digest": r.ValidForGraphDigest,
		"review_status":          r.ReviewStatus,
	} {
		if value == "" {
			errs = append(errs, name+" is required")
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("machine adoption receipt incomplete: %s", strings.Join(errs, "; "))
	}
	return nil
}

func normalizeTimestamp(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func normalizeList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateClosed(errs *[]string, name, value string, allowed map[string]struct{}) {
	if value == "" {
		return
	}
	if _, ok := allowed[value]; !ok {
		*errs = append(*errs, name+" has unsupported value "+value)
	}
}

func values(items ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}
