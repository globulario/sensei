// SPDX-License-Identifier: AGPL-3.0-only

// Promotion quality gate (Phase 6).
//
// The graph accretes many candidate / extracted intents — that is fine while
// they stay candidates. The danger is a candidate silently reaching `active`
// or `accepted` without the structure that makes it useful: an activation
// trigger to retrieve it, a related node to ground it, a recipe to follow.
// Low-signal active knowledge is worse than none — it dilutes every briefing.
//
// This file holds pure validators that decide whether a node MEETS the bar for
// promotion, plus a directory pass (ValidatePromotions) the yaml2nt
// -validate-promotion flag runs. The gate is opt-in (mirrors -validate-refs):
// it surfaces violations and, when enabled, fails the build — so no new
// low-signal active node enters the graph silently.
package extractor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PromotionViolation is one reason a node fails the promotion bar.
type PromotionViolation struct {
	NodeID string // bare id
	Kind   string // "implementation_pattern" | "intent"
	Path   string // authored source file
	Rule   string // short machine-readable rule key
	Detail string // human-readable explanation
}

func (v PromotionViolation) String() string {
	return fmt.Sprintf("%s %q (%s): %s [%s]", v.Kind, v.NodeID, v.Path, v.Detail, v.Rule)
}

// NOTE: promotedStatus is deliberately gone.
//
// It reported whether `status:` meant "promoted", treating "", active and
// accepted as live — so an authored node with no explicit status governed by
// default, and one word in a YAML file could exempt a node from the quality bar
// it was supposed to meet. That is caller-editable metadata deciding authority,
// which #166 forbids.
//
// Scope is now decided before these validators run: ValidatePromotions consults
// the signed admission set (or an explicit pre-admission target). Anything that
// reaches here governs, so it meets the bar regardless of what `status:` claims.
// retiredStatus survives because deprecated/superseded is genuine LIFECYCLE
// semantics about a node that still governs — it selects which bar applies, not
// whether one applies at all.

// retiredStatus reports whether a status means the node is deprecated or
// superseded — allowed to be incomplete, but should explain its retirement.
func retiredStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "deprecated", "superseded", "retired":
		return true
	}
	return false
}

// validateImplementationPatternPromotion checks an ImplementationPattern.
// Active patterns must declare a trigger, must-follow guidance, and a
// reference (or an explicit rationale standing in for one). Retired patterns
// are exempt but should carry a note.
func validateImplementationPatternPromotion(p yamlImplementationPattern, path string) []PromotionViolation {
	var out []PromotionViolation
	add := func(rule, detail string) {
		out = append(out, PromotionViolation{NodeID: p.ID, Kind: "implementation_pattern", Path: path, Rule: rule, Detail: detail})
	}

	if retiredStatus(p.Status) {
		if strings.TrimSpace(p.Rationale) == "" {
			add("retired_without_note", "deprecated/superseded pattern should carry a supersession note in rationale")
		}
		return out
	}

	if strings.TrimSpace(p.Label) == "" {
		add("missing_label", "active pattern must have a label")
	}
	if countNonEmpty(p.WhenToUse) == 0 {
		add("missing_activation_trigger", "active pattern must declare at least one when_to_use activation trigger")
	}
	if countNonEmpty(p.MustFollow) == 0 {
		add("missing_must_follow", "active pattern must declare at least one must_follow step")
	}
	if countReferenceFiles(p.ReferenceFiles) == 0 && strings.TrimSpace(p.Rationale) == "" {
		add("missing_reference", "active pattern must cite a reference file or explain in rationale why none exists")
	}
	return out
}

// validateIntentPromotion checks an Intent. Active/accepted intents must have a
// title, an activation trigger or bad smell to retrieve them by, and at least
// one related link to ground them. Retired intents are exempt but should note
// their retirement. The authored source path is always present (it is the file
// being validated), so it is not separately checked.
func validateIntentPromotion(i yamlIntent, path string) []PromotionViolation {
	var out []PromotionViolation
	add := func(rule, detail string) {
		out = append(out, PromotionViolation{NodeID: i.ID, Kind: "intent", Path: path, Rule: rule, Detail: detail})
	}

	if retiredStatus(i.Status) {
		// A retired intent should explain itself; related links or zoom edges
		// to a successor count as the note.
		if !intentHasRelatedLink(i) && strings.TrimSpace(i.Intent) == "" {
			add("retired_without_note", "deprecated/superseded intent should link a successor or carry a note")
		}
		return out
	}

	if strings.TrimSpace(i.Title) == "" && strings.TrimSpace(i.Intent) == "" {
		add("missing_label", "active intent must have a title")
	}
	if countNonEmpty(i.ActivationTriggers) == 0 && countNonEmpty(i.BadSmells) == 0 {
		add("missing_trigger_or_smell", "active intent must declare an activation_trigger or a bad_smell")
	}
	if !intentHasRelatedLink(i) {
		add("missing_related_link", "active intent must link at least one related invariant/intent/failure_mode/test")
	}
	return out
}

// highRiskBlastRadius reports whether a blast radius is severe enough to
// require an approval gate (or an explicit reason for none).
func highRiskBlastRadius(br string) bool {
	switch strings.ToLower(strings.TrimSpace(br)) {
	case "cluster", "security", "data_loss", "external", "node":
		return true
	}
	return false
}

// validateRepairPlanPromotion checks a RepairPlan against the promotion bar.
// An active plan must say what it repairs, how to gate it, how to verify it,
// and what it is bound to. Retired plans are exempt but should carry a note.
func validateRepairPlanPromotion(p yamlRepairPlan, path string) []PromotionViolation {
	var out []PromotionViolation
	add := func(rule, detail string) {
		out = append(out, PromotionViolation{NodeID: p.ID, Kind: "repair_plan", Path: path, Rule: rule, Detail: detail})
	}

	if retiredStatus(p.Status) {
		if strings.TrimSpace(p.Notes) == "" {
			add("retired_without_note", "deprecated/superseded repair plan should carry a supersession note")
		}
		return out
	}

	if strings.TrimSpace(p.Label) == "" {
		add("missing_label", "active repair plan must have a label")
	}
	if countNonEmpty(p.RepairsFailureModes) == 0 && countNonEmpty(p.RepairsFindingClasses) == 0 {
		add("missing_applicability", "active repair plan must repair at least one failure mode or finding class")
	}
	if countNonEmpty(p.Preconditions) == 0 {
		add("missing_precondition", "active repair plan must declare at least one precondition")
	}
	if countNonEmpty(p.VerificationSteps) == 0 {
		add("missing_verification", "active repair plan must declare at least one verification step")
	}
	// High-risk plans must gate or explain why not (notes stands in as the reason).
	if highRiskBlastRadius(p.BlastRadius) {
		gate := strings.ToLower(strings.TrimSpace(p.ApprovalGate))
		if (gate == "" || gate == "none") && strings.TrimSpace(p.Notes) == "" {
			add("high_risk_without_gate", "high blast-radius plan must declare an approval gate or explain in notes why none is needed")
		}
	}
	// Must be bound to at least one knowledge node.
	if !repairPlanHasBinding(p) {
		add("missing_binding", "active repair plan must link at least one implementation pattern, authority domain, invariant, or required test")
	}
	return out
}

// repairPlanHasBinding reports whether the plan links any knowledge node.
func repairPlanHasBinding(p yamlRepairPlan) bool {
	return countNonEmpty(p.UsesImplementationPattern) > 0 ||
		countNonEmpty(p.AppliesToAuthorityDomains) > 0 ||
		countNonEmpty(p.MustNotViolateInvariants) > 0 ||
		countNonEmpty(p.RequiredTests) > 0
}

// intentHasRelatedLink reports whether the intent carries any grounding edge.
func intentHasRelatedLink(i yamlIntent) bool {
	return countNonEmpty(i.RelatedInvariants) > 0 ||
		countNonEmpty(i.RelatedTo) > 0 ||
		countNonEmpty(i.ExpressedBy) > 0 ||
		countNonEmpty(i.ZoomsOutTo) > 0 ||
		countNonEmpty(i.ZoomsInTo) > 0
}

func countNonEmpty(ss []string) int {
	n := 0
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			n++
		}
	}
	return n
}

func countReferenceFiles(refs []yamlImplementationPatternReference) int {
	n := 0
	for _, r := range refs {
		if strings.TrimSpace(r.Path) != "" {
			n++
		}
	}
	return n
}

// AdmissionScope reports whether a stable knowledge identity governs.
//
// A narrow interface rather than a dependency on the admission package: this
// validator needs the DECISION, not the machinery that produces it, and
// knowledgeadmission.Admitted satisfies it structurally.
type AdmissionScope interface {
	IsAuthoritativelyAdmitted(identity string) bool
}

// ErrAdmissionUnavailable reports that authoritative scope could not be
// established, so no authoritative validation verdict exists.
//
// Typed absence, never an empty pass. "Zero violations because nothing was
// checked" and "zero violations because everything checked out" are different
// worlds, and reporting the first as the second is how a gate becomes theatre.
var ErrAdmissionUnavailable = errors.New("promotion validation scope unavailable: no verified knowledge admission")

// PromotionRequest states which nodes are being validated, and why.
//
// The operation is explicit because the two uses have OPPOSITE preconditions:
//
//   - normal build: validate the quality of identities that already govern, so
//     scope comes from the signed admission set.
//   - pre-admission review: validate a candidate to decide whether it MAY be
//     admitted. Requiring prior admission here would be circular — nothing could
//     ever qualify for the admission it must already hold.
//
// So candidates are named explicitly in CandidateTargets rather than inferred
// from `status: accepted` or from living outside candidates/. Inference is what
// let location and caller-editable metadata decide scope in the first place.
type PromotionRequest struct {
	Dirs []string
	// Scope is the authoritative admission decision. Required unless every node
	// under validation is named in CandidateTargets.
	Scope AdmissionScope
	// CandidateTargets are identities under PRE-ADMISSION review. They are
	// validated despite not governing, and are the only nodes exempt from Scope.
	CandidateTargets []string
}

// ValidatePromotions validates the promotion bar for authoritative knowledge.
//
// Scope is decided by admission, not by pathname and not by `status:`. The old
// rule skipped the candidates/ subtree and then treated "", active and accepted
// as the promoted set — reconstructing "live knowledge" from location plus
// caller-editable metadata, which is the semantic shadow #166 removes. An empty
// status still means "live" for the QUALITY bar it must then meet, but it no
// longer decides whether the node governs.
func ValidatePromotions(req PromotionRequest) ([]PromotionViolation, error) {
	targets := map[string]bool{}
	for _, id := range req.CandidateTargets {
		if id = strings.TrimSpace(id); id != "" {
			targets[id] = true
		}
	}
	if req.Scope == nil && len(targets) == 0 {
		return nil, ErrAdmissionUnavailable
	}

	var out []PromotionViolation
	for _, dir := range req.Dirs {
		if dir == "" {
			continue
		}
		// No pathname filter. Discovery walks everything; admission decides
		// which discovered identities are in authoritative scope.
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml") {
				return nil
			}
			id, ok := promotionFileID(path)
			if !ok {
				return nil
			}
			if !targets[id] && (req.Scope == nil || !req.Scope.IsAuthoritativelyAdmitted(id)) {
				return nil
			}
			vs, verr := validatePromotionFile(path)
			if verr != nil {
				return verr
			}
			out = append(out, vs...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sortPromotionViolations(out)
	return out, nil
}

// promotionFileID reads the node id a promotable file declares.
func promotionFileID(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	id, _ := raw["id"].(string)
	id = strings.TrimSpace(id)
	return id, id != ""
}

// validatePromotionFile classifies a single YAML file and validates it if it is
// a promotable kind. Non-promotable files yield no violations and no error.
func validatePromotionFile(path string) ([]PromotionViolation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Not a single-doc map (could be a list-schema file) — not promotable here.
		return nil, nil
	}
	if _, hasID := raw["id"]; !hasID {
		return nil, nil
	}
	if cls, ok := raw["class"].(string); ok {
		switch cls {
		case "ImplementationPattern":
			var p yamlImplementationPattern
			if err := yaml.Unmarshal(data, &p); err != nil {
				return nil, nil
			}
			return validateImplementationPatternPromotion(p, path), nil
		case "RepairPlan":
			var p yamlRepairPlan
			if err := yaml.Unmarshal(data, &p); err != nil {
				return nil, nil
			}
			return validateRepairPlanPromotion(p, path), nil
		}
	}
	if _, hasLevel := raw["level"]; hasLevel {
		var i yamlIntent
		if err := yaml.Unmarshal(data, &i); err != nil {
			return nil, nil
		}
		return validateIntentPromotion(i, path), nil
	}
	return nil, nil
}

func sortPromotionViolations(vs []PromotionViolation) {
	// Simple insertion sort keeps it dependency-free and stable for the small
	// violation counts this gate produces.
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && promotionLess(vs[j], vs[j-1]); j-- {
			vs[j-1], vs[j] = vs[j], vs[j-1]
		}
	}
}

func promotionLess(a, b PromotionViolation) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.NodeID != b.NodeID {
		return a.NodeID < b.NodeID
	}
	return a.Rule < b.Rule
}
