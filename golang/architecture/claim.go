// SPDX-License-Identifier: Apache-2.0

package architecture

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	PlaneObserved   = "observed"
	PlaneEnforced   = "enforced"
	PlaneIntended   = "intended"
	PlaneHistorical = "historical"
	PlaneDesired    = "desired"

	OriginObserved = "observed"
	OriginDerived  = "derived"
	OriginAuthored = "authored"
	OriginPromoted = "promoted"

	StatusUnknown    = "unknown"
	StatusSupported  = "supported"
	StatusContested  = "contested"
	StatusRefuted    = "refuted"
	StatusStale      = "stale"
	StatusSuperseded = "superseded"

	PromotionCandidate = "candidate"

	GraphDigestResolved     = "resolved"
	GraphDigestUnavailable  = "unavailable"
	GraphDigestNotRequested = "not_requested"
)

var claimPredicateRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

type Claim struct {
	ID          string `json:"id" yaml:"id"`
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	Statement ClaimStatement `json:"statement" yaml:"statement"`
	Scope     ClaimScope     `json:"scope" yaml:"scope"`

	ArchitecturalPlane string `json:"architectural_plane" yaml:"architectural_plane"`
	AssertionOrigin    string `json:"assertion_origin" yaml:"assertion_origin"`
	EpistemicStatus    string `json:"epistemic_status" yaml:"epistemic_status"`
	InferenceRule      string `json:"inference_rule,omitempty" yaml:"inference_rule,omitempty"`

	PremiseFacts       []string `json:"premise_facts,omitempty" yaml:"premise_facts,omitempty"`
	DependsOnClaims    []string `json:"depends_on_claims,omitempty" yaml:"depends_on_claims,omitempty"`
	SupportingEvidence []string `json:"supporting_evidence,omitempty" yaml:"supporting_evidence,omitempty"`
	RefutingEvidence   []string `json:"refuting_evidence,omitempty" yaml:"refuting_evidence,omitempty"`
	ConflictsWith      []string `json:"conflicts_with,omitempty" yaml:"conflicts_with,omitempty"`
	SupersededBy       string   `json:"superseded_by,omitempty" yaml:"superseded_by,omitempty"`
	AboutNodes         []string `json:"about_nodes,omitempty" yaml:"about_nodes,omitempty"`

	AlternativeExplanations []string `json:"alternative_explanations,omitempty" yaml:"alternative_explanations,omitempty"`
	Unknowns                []string `json:"unknowns,omitempty" yaml:"unknowns,omitempty"`
	InvalidationConditions  []string `json:"invalidation_conditions,omitempty" yaml:"invalidation_conditions,omitempty"`

	Confidence          float64 `json:"confidence" yaml:"confidence"`
	Freshness           string  `json:"freshness,omitempty" yaml:"freshness,omitempty"`
	LastValidatedAt     string  `json:"last_validated_at,omitempty" yaml:"last_validated_at,omitempty"`
	HumanReviewRequired bool    `json:"human_review_required" yaml:"human_review_required"`
	PromotionStatus     string  `json:"promotion_status" yaml:"promotion_status"`
}

type ClaimStatement struct {
	Subject   string `json:"subject" yaml:"subject"`
	Predicate string `json:"predicate" yaml:"predicate"`
	Object    string `json:"object" yaml:"object"`
}

type ClaimScope struct {
	Repository string   `json:"repository,omitempty" yaml:"repository,omitempty"`
	Repo       string   `json:"repo,omitempty" yaml:"repo,omitempty"`
	Domain     string   `json:"domain,omitempty" yaml:"domain,omitempty"`
	SourceSet  string   `json:"source_set,omitempty" yaml:"source_set,omitempty"`
	Files      []string `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols    []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components []string `json:"components,omitempty" yaml:"components,omitempty"`
}

func StableClaimID(c Claim) string {
	c = canonicalizeClaim(c)
	repo := c.Scope.Repository
	if repo == "" {
		repo = c.Scope.Repo
	}
	parts := []string{
		repo,
		c.Statement.Subject,
		c.Statement.Predicate,
		c.Statement.Object,
		c.ArchitecturalPlane,
		c.InferenceRule,
		strings.Join(c.PremiseFacts, ","),
		strings.Join(c.DependsOnClaims, ","),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "claim." + hex.EncodeToString(sum[:])[:16]
}

func NormalizeClaims(claims []Claim) ([]Claim, error) {
	out := make([]Claim, 0, len(claims))
	for _, in := range claims {
		c := canonicalizeClaim(in)
		if c.ID == "" {
			c.ID = StableClaimID(c)
		}
		if err := ValidateClaim(c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	seen := map[string]Claim{}
	dedup := out[:0]
	for _, c := range out {
		if existing, ok := seen[c.ID]; ok {
			if !claimsEqual(existing, c) {
				return nil, fmt.Errorf("claim id collision for %s", c.ID)
			}
			continue
		}
		seen[c.ID] = c
		dedup = append(dedup, c)
	}
	return dedup, nil
}

// CompactClaims merges mechanically duplicated claims only when their exact
// proposition, scope, plane, origin, and inference rule match. Different
// objects or scopes remain distinct. Premise facts and evidence are unioned;
// mixed supporting/refuting evidence makes the merged proposition contested.
func CompactClaims(claims []Claim) ([]Claim, error) {
	normalized, err := NormalizeClaims(claims)
	if err != nil {
		return nil, err
	}
	groups := map[string][]Claim{}
	var keys []string
	for _, claim := range normalized {
		key := claimCompactionKey(claim)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], claim)
	}
	sort.Strings(keys)

	oldToNew := map[string]string{}
	out := make([]Claim, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		merged := group[0]
		for _, claim := range group[1:] {
			merged.PremiseFacts = append(merged.PremiseFacts, claim.PremiseFacts...)
			merged.DependsOnClaims = append(merged.DependsOnClaims, claim.DependsOnClaims...)
			merged.SupportingEvidence = append(merged.SupportingEvidence, claim.SupportingEvidence...)
			merged.RefutingEvidence = append(merged.RefutingEvidence, claim.RefutingEvidence...)
			merged.ConflictsWith = append(merged.ConflictsWith, claim.ConflictsWith...)
			merged.AboutNodes = append(merged.AboutNodes, claim.AboutNodes...)
			merged.AlternativeExplanations = append(merged.AlternativeExplanations, claim.AlternativeExplanations...)
			merged.Unknowns = append(merged.Unknowns, claim.Unknowns...)
			merged.InvalidationConditions = append(merged.InvalidationConditions, claim.InvalidationConditions...)
			if claim.Confidence > merged.Confidence {
				merged.Confidence = claim.Confidence
			}
			if claim.LastValidatedAt > merged.LastValidatedAt {
				merged.LastValidatedAt = claim.LastValidatedAt
			}
		}
		merged = canonicalizeClaim(merged)
		merged.EpistemicStatus = compactedEpistemicStatus(group, merged)
		if len(group) > 1 && len(merged.DependsOnClaims) == 0 {
			merged.ID = ""
			merged.ID = StableClaimID(merged)
		}
		for _, claim := range group {
			oldToNew[claim.ID] = merged.ID
		}
		out = append(out, merged)
	}

	for i := range out {
		out[i].DependsOnClaims = remapClaimRefs(out[i].DependsOnClaims, oldToNew, out[i].ID)
		out[i].ConflictsWith = remapClaimRefs(out[i].ConflictsWith, oldToNew, out[i].ID)
		if replacement := oldToNew[out[i].SupersededBy]; replacement != "" {
			out[i].SupersededBy = replacement
		}
		if err := ValidateClaim(out[i]); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func claimCompactionKey(in Claim) string {
	c := canonicalizeClaim(in)
	return strings.Join([]string{
		c.Scope.Repository, c.Scope.Domain, c.Scope.SourceSet,
		strings.Join(c.Scope.Files, "\x00"), strings.Join(c.Scope.Symbols, "\x00"), strings.Join(c.Scope.Components, "\x00"),
		c.Statement.Subject, c.Statement.Predicate, c.Statement.Object,
		c.ArchitecturalPlane, c.AssertionOrigin, c.InferenceRule,
	}, "\x1f")
}

func compactedEpistemicStatus(group []Claim, merged Claim) string {
	statuses := map[string]bool{}
	for _, claim := range group {
		statuses[claim.EpistemicStatus] = true
	}
	if statuses[StatusContested] || (len(merged.RefutingEvidence) > 0 && (len(merged.SupportingEvidence) > 0 || len(merged.PremiseFacts) > 0)) || (statuses[StatusSupported] && statuses[StatusRefuted]) {
		return StatusContested
	}
	for _, status := range []string{StatusSupported, StatusRefuted, StatusUnknown, StatusStale, StatusSuperseded} {
		if statuses[status] {
			return status
		}
	}
	return merged.EpistemicStatus
}

func remapClaimRefs(refs []string, oldToNew map[string]string, self string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if replacement := oldToNew[ref]; replacement != "" {
			ref = replacement
		}
		if ref != self {
			out = append(out, ref)
		}
	}
	return cleanStringList(out, false)
}

func ValidateClaim(c Claim) error {
	var errs []string
	if c.Statement.Subject == "" || c.Statement.Predicate == "" || c.Statement.Object == "" {
		errs = append(errs, "statement subject, predicate, and object are required")
	}
	if c.Statement.Predicate != "" && !claimPredicateRE.MatchString(c.Statement.Predicate) {
		errs = append(errs, "statement predicate must be a conservative token")
	}
	if !oneOf(c.ArchitecturalPlane, PlaneObserved, PlaneEnforced, PlaneIntended, PlaneHistorical, PlaneDesired) {
		errs = append(errs, "unknown architectural plane")
	}
	if !oneOf(c.AssertionOrigin, OriginObserved, OriginDerived, OriginAuthored, OriginPromoted) {
		errs = append(errs, "unknown assertion origin")
	}
	if !oneOf(c.EpistemicStatus, StatusUnknown, StatusSupported, StatusContested, StatusRefuted, StatusStale, StatusSuperseded) {
		errs = append(errs, "unknown epistemic status")
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		errs = append(errs, "confidence must be between 0 and 1")
	}
	if c.PromotionStatus != PromotionCandidate {
		errs = append(errs, "promotion status must be candidate")
	}
	if !c.HumanReviewRequired {
		errs = append(errs, "human review is required")
	}
	if c.AssertionOrigin == OriginDerived && c.InferenceRule == "" {
		errs = append(errs, "derived claim requires inference rule")
	}
	for _, f := range c.Scope.Files {
		if filepath.IsAbs(f) || strings.HasPrefix(f, "../") || strings.Contains(f, "/../") || f == ".." {
			errs = append(errs, "file path must be repository-relative and non-escaping")
			break
		}
	}
	if contains(c.DependsOnClaims, c.ID) {
		errs = append(errs, "claim must not depend on itself")
	}
	if contains(c.ConflictsWith, c.ID) {
		errs = append(errs, "claim must not conflict with itself")
	}
	if c.SupersededBy == c.ID && c.ID != "" {
		errs = append(errs, "claim must not supersede itself")
	}
	for _, ref := range c.AboutNodes {
		if _, _, ok := ParseClassQualifiedReference(ref); !ok {
			errs = append(errs, "unsupported about_nodes reference")
			break
		}
	}
	if c.LastValidatedAt != "" {
		if _, err := time.Parse(time.RFC3339, c.LastValidatedAt); err != nil {
			errs = append(errs, "last_validated_at must be RFC3339")
		}
	}
	if len(c.PremiseFacts)+len(c.SupportingEvidence)+len(c.RefutingEvidence)+len(c.DependsOnClaims) == 0 {
		errs = append(errs, "claim requires at least one fact, evidence reference, or dependent claim")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func ParseClassQualifiedReference(ref string) (class, id string, ok bool) {
	class, id, found := strings.Cut(strings.TrimSpace(ref), ":")
	if !found || strings.TrimSpace(class) == "" || strings.TrimSpace(id) == "" {
		return "", "", false
	}
	class = strings.ToLower(strings.TrimSpace(class))
	id = strings.TrimSpace(id)
	switch class {
	case "invariant", "failure_mode", "incident_pattern", "intent", "symbol", "source_file",
		"code_symbol", "forbidden_fix", "test", "meta_principle", "component", "boundary",
		"contract", "decision", "evidence", "proof_obligation", "proof_slot",
		"design_pattern", "implementation_pattern", "pattern_misuse", "architecture_claim",
		"open_question", "architect_answer", "evidence_probe", "runtime_evidence",
		"repair_plan", "authority_domain", "authority_surface":
		return class, id, true
	default:
		return "", "", false
	}
}

func canonicalizeClaim(in Claim) Claim {
	c := in
	c.ID = strings.TrimSpace(c.ID)
	c.Label = strings.TrimSpace(c.Label)
	c.Description = strings.TrimSpace(c.Description)
	c.Statement.Subject = strings.TrimSpace(c.Statement.Subject)
	c.Statement.Predicate = strings.TrimSpace(c.Statement.Predicate)
	c.Statement.Object = strings.TrimSpace(c.Statement.Object)
	c.Scope.Repository = strings.TrimSpace(c.Scope.Repository)
	c.Scope.Repo = strings.TrimSpace(c.Scope.Repo)
	if c.Scope.Repository == "" {
		c.Scope.Repository = c.Scope.Repo
	}
	if c.Scope.Repo == "" {
		c.Scope.Repo = c.Scope.Repository
	}
	c.Scope.Domain = strings.TrimSpace(c.Scope.Domain)
	c.Scope.SourceSet = strings.TrimSpace(c.Scope.SourceSet)
	c.Scope.Files = cleanStringList(c.Scope.Files, true)
	c.Scope.Symbols = cleanStringList(c.Scope.Symbols, false)
	c.Scope.Components = cleanStringList(c.Scope.Components, false)
	c.ArchitecturalPlane = strings.TrimSpace(c.ArchitecturalPlane)
	c.AssertionOrigin = strings.TrimSpace(c.AssertionOrigin)
	c.EpistemicStatus = strings.TrimSpace(c.EpistemicStatus)
	c.InferenceRule = strings.TrimSpace(c.InferenceRule)
	c.PremiseFacts = cleanStringList(c.PremiseFacts, false)
	c.DependsOnClaims = cleanStringList(c.DependsOnClaims, false)
	c.SupportingEvidence = normalizeClassRefs(c.SupportingEvidence)
	c.RefutingEvidence = normalizeClassRefs(c.RefutingEvidence)
	c.ConflictsWith = cleanStringList(c.ConflictsWith, false)
	c.SupersededBy = strings.TrimSpace(c.SupersededBy)
	c.AboutNodes = normalizeClassRefs(c.AboutNodes)
	c.AlternativeExplanations = cleanStringList(c.AlternativeExplanations, false)
	c.Unknowns = cleanStringList(c.Unknowns, false)
	c.InvalidationConditions = cleanStringList(c.InvalidationConditions, false)
	c.Freshness = strings.TrimSpace(c.Freshness)
	c.LastValidatedAt = strings.TrimSpace(c.LastValidatedAt)
	c.PromotionStatus = strings.TrimSpace(c.PromotionStatus)
	return c
}

func normalizeClassRefs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, ref := range in {
		class, id, ok := ParseClassQualifiedReference(ref)
		if ok {
			out = append(out, class+":"+id)
			continue
		}
		if strings.TrimSpace(ref) != "" {
			out = append(out, strings.TrimSpace(ref))
		}
	}
	return cleanStringList(out, false)
}

func claimsEqual(a, b Claim) bool {
	aj, _ := json.Marshal(canonicalizeClaim(a))
	bj, _ := json.Marshal(canonicalizeClaim(b))
	return string(aj) == string(bj)
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
