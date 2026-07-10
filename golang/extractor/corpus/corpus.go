// SPDX-License-Identifier: AGPL-3.0-only

// Package corpus implements the human-gated path from a grounded dry-run finding
// to a candidate AWG corpus entry — the design of docs/corpus-integration-design.md.
//
// TRUST RULE (enforced structurally here): reports can be generated
// automatically; corpus truth cannot. This package:
//   - PLAN classifies findings into integrate | hold | never (read-only);
//   - MATERIALIZE writes ONLY status:candidate YAML entries for human-SELECTED
//     findings, under a candidates/ tree — it never writes the seed, never PUTs a
//     graph, never promotes, and never mints a meta-principle;
//   - VALIDATE checks an entry's metadata, status, and citation resolution.
//
// Promotion to reviewed/active and the minimal-owned-triples seed append are
// SEPARATE, human/PR steps (see the design §6). Nothing here mutates the graph.
package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor/coldsource"
	"gopkg.in/yaml.v3"
)

// Finding is one grounded dry-run result (from coldsource or intent-mine),
// carrying the provenance the grounder computed. This is the interchange shape a
// report YAML holds under `findings:`.
type Finding struct {
	ID                    string   `yaml:"id"`
	Claim                 string   `yaml:"claim"`
	OutputClass           string   `yaml:"output_class"` // intent-mine class; "" for raw coldsource
	GroundingTier         string   `yaml:"grounding_tier"`
	Domain                string   `yaml:"domain"`
	Repo                  string   `yaml:"repo"`
	SourceSet             string   `yaml:"source_set"`
	Provenance            string   `yaml:"provenance"` // coldsource | intent_mine | manual | pilot
	EvidenceCitations     []string `yaml:"evidence_citations"`
	RelatedSymbols        []string `yaml:"related_symbols"`
	RelatedTests          []string `yaml:"related_tests"`
	RelatedInvariants     []string `yaml:"related_invariants"`
	RelatedMetaPrinciples []string `yaml:"related_meta_principles"`
	ReviewLabel           string   `yaml:"review_label"`
	ReviewerNote          string   `yaml:"reviewer_note"`
	SourceRun             string   `yaml:"source_run"`
}

// FindingsReport is a report file: `findings: [ ... ]`.
type FindingsReport struct {
	Findings []Finding `yaml:"findings"`
}

// Action is the plan verdict for a finding.
type Action string

const (
	ActionIntegrate Action = "integrate" // eligible to become a candidate entry
	ActionHold      Action = "hold"      // not now (weak/unknown) — re-derive first
	ActionNever     Action = "never"     // must never auto-integrate (design §2)
)

// Status values for a corpus entry.
const (
	StatusCandidate = "candidate"
	StatusReviewed  = "reviewed"
	StatusActive    = "active"
)

// Verdict is the classification of one finding.
type Verdict struct {
	Finding   Finding
	Action    Action
	EntryType string // intent|invariant|failure_mode|forbidden_fix|required_test|sibling_evidence|candidate_principle|pilot_rule|drift_warning
	MaxStatus string // the strongest status this could EVER reach (candidate first regardless)
	Reason    string
}

// groundedTier reports whether a tier name means "grounded at >= landed". It
// accepts BOTH grounding vocabularies: coldsource's ProvenanceTier
// (test_encoded / landed_commit) and intent-mine's TrustTier
// (executable_truth / landed_behavior). Missing either set silently
// under-classifies the other tool's findings.
func groundedTier(t string) bool {
	switch t {
	case "test_encoded", "landed_commit", "executable_truth", "landed_behavior":
		return true
	}
	return false
}

// Classify applies the design §1/§2 rules to one finding. It is conservative:
// anything unproven, review-only, unresolved, or ungrounded is held or refused.
func Classify(f Finding) Verdict {
	v := Verdict{Finding: f}
	tier := strings.TrimSpace(f.GroundingTier)
	grounded := groundedTier(tier)

	// Divergence / gap findings are classified FIRST and are EXEMPT from the
	// unresolved refusal below: their absent or missing anchor IS the finding. A
	// stale_intent is stale precisely because the code lacks what the doc states;
	// an ambiguous_owner is a contested truth; a missing_invariant is implied by
	// scars with nothing yet encoding it. They integrate ONLY as candidate-level
	// evidence — never as active truth — but they must not be dropped as "never"
	// just because their evidence does not resolve (that resolution gap is the
	// point of the finding).
	switch f.OutputClass {
	case "stale_intent":
		return integrate(v, "drift_warning", StatusCandidate, "drift evidence only — recorded, never an active rule")
	case "ambiguous_owner":
		return integrate(v, "drift_warning", StatusCandidate, "ownership-conflict evidence only — never an active rule")
	case "missing_invariant":
		return integrate(v, "candidate_principle", StatusCandidate, "scars imply it; candidate only until a real anchor + human review")
	}

	// Hard refusal: any OTHER finding whose evidence does not resolve (design §2).
	if tier == "unresolved" {
		return refuse(v, "unresolved evidence — citations do not resolve")
	}

	switch f.OutputClass {
	case "ungrounded_claim":
		return refuse(v, "ungrounded_claim — no anchor proves it")
	case "strong_intent":
		v.Action, v.EntryType = ActionIntegrate, "intent"
		v.MaxStatus = maxStatusFor(grounded)
		v.Reason = "doc + code/test agree" + statusNote(grounded)
		return v
	case "hidden_intent":
		v.Action, v.EntryType = ActionIntegrate, "invariant"
		v.MaxStatus = maxStatusFor(grounded)
		v.Reason = "code/test encode it, undocumented — integration adds the doc" + statusNote(grounded)
		return v
	}

	// No intent class → raw coldsource finding. Integrate only load-bearing,
	// grounded scars; everything else holds.
	if f.Provenance == "coldsource" {
		if tier == "review_suggestion" {
			return refuse(v, "review_suggestion-only coldsource finding — re-derive against the tree")
		}
		if grounded && strings.EqualFold(f.ReviewLabel, "load-bearing") {
			v.Action, v.EntryType, v.MaxStatus = ActionIntegrate, coldsourceEntryType(f), StatusActive
			v.Reason = "load-bearing scar-rule, grounded — eligible (starts candidate)"
			return v
		}
		v.Action, v.MaxStatus = ActionHold, StatusCandidate
		v.Reason = "coldsource finding not yet load-bearing+grounded — review/re-derive first"
		return v
	}

	if tier == "review_suggestion" {
		return refuse(v, "review_suggestion-only — a proposal, not an encoded rule")
	}
	v.Action, v.MaxStatus = ActionHold, StatusCandidate
	v.Reason = "unclassified finding — human review before integration"
	return v
}

func refuse(v Verdict, reason string) Verdict {
	v.Action, v.EntryType, v.MaxStatus, v.Reason = ActionNever, "", "", reason
	return v
}

func integrate(v Verdict, entryType, maxStatus, reason string) Verdict {
	v.Action, v.EntryType, v.MaxStatus, v.Reason = ActionIntegrate, entryType, maxStatus, reason
	return v
}

func maxStatusFor(grounded bool) string {
	if grounded {
		return StatusActive // eligible to reach active — but materialize still writes candidate
	}
	return StatusCandidate
}

func statusNote(grounded bool) string {
	if grounded {
		return " — active-eligible (>= landed_commit)"
	}
	return " — candidate only (below landed_commit)"
}

// coldsourceEntryType maps a coldsource finding's review/class hint to an entry
// type. Defaults to invariant.
func coldsourceEntryType(f Finding) string {
	switch strings.ToLower(f.OutputClass) {
	case "forbidden_fix":
		return "forbidden_fix"
	case "failure_mode":
		return "failure_mode"
	}
	return "invariant"
}

// Plan classifies every finding in a report.
func Plan(r FindingsReport) []Verdict {
	out := make([]Verdict, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, Classify(f))
	}
	return out
}

// ── Corpus entry (the design §4 metadata) ────────────────────────────────────

// Entry is the materialized corpus entry. status is ALWAYS candidate when written
// by Materialize; promotion is a separate human edit.
type Entry struct {
	ID        string `yaml:"id"`
	Type      string `yaml:"type"`
	Claim     string `yaml:"claim"`
	Domain    string `yaml:"domain"`
	Repo      string `yaml:"repo,omitempty"`
	SourceSet string `yaml:"source_set,omitempty"`
	Status    string `yaml:"status"`
	Grounding struct {
		Tier string `yaml:"tier"`
	} `yaml:"grounding"`
	EvidenceCitations     []string `yaml:"evidence_citations"`
	SourceRun             string   `yaml:"source_run,omitempty"`
	RelatedSymbols        []string `yaml:"related_symbols,omitempty"`
	RelatedTests          []string `yaml:"related_tests,omitempty"`
	RelatedInvariants     []string `yaml:"related_invariants,omitempty"`
	RelatedMetaPrinciples []string `yaml:"related_meta_principles,omitempty"`
	Provenance            string   `yaml:"provenance"`
	Review                struct {
		Label        string `yaml:"label,omitempty"`
		ReviewerNote string `yaml:"reviewer_note,omitempty"`
	} `yaml:"review"`
	// Promotion is intentionally empty at materialize; a human fills it on
	// promotion to reviewed/active.
	Promotion struct {
		PromotedAt string `yaml:"promoted_at,omitempty"`
		PromotedIn string `yaml:"promoted_in,omitempty"`
	} `yaml:"promotion,omitempty"`
}

// Materialize turns an integrate-eligible verdict into a candidate Entry. It
// REFUSES a never/hold verdict, and forces status to candidate regardless of the
// verdict's max status — promotion is a separate human step.
func Materialize(v Verdict) (Entry, error) {
	if v.Action != ActionIntegrate {
		return Entry{}, fmt.Errorf("refusing to materialize a %q finding (%s): %s", v.Action, v.Finding.ID, v.Reason)
	}
	f := v.Finding
	e := Entry{
		ID:                    f.ID,
		Type:                  v.EntryType,
		Claim:                 f.Claim,
		Domain:                f.Domain,
		Repo:                  f.Repo,
		SourceSet:             f.SourceSet,
		Status:                StatusCandidate, // ALWAYS — never reviewed/active from a tool
		EvidenceCitations:     f.EvidenceCitations,
		SourceRun:             f.SourceRun,
		RelatedSymbols:        f.RelatedSymbols,
		RelatedTests:          f.RelatedTests,
		RelatedInvariants:     f.RelatedInvariants,
		RelatedMetaPrinciples: f.RelatedMetaPrinciples,
		Provenance:            f.Provenance,
	}
	e.Grounding.Tier = f.GroundingTier
	e.Review.Label = f.ReviewLabel
	e.Review.ReviewerNote = f.ReviewerNote
	return e, nil
}

const entryHeader = "# CANDIDATE corpus entry — NOT active knowledge.\n" +
	"# Materialized by `awg corpus materialize`. status:candidate; promotion to\n" +
	"# reviewed/active and the minimal-triples seed append are separate human steps.\n"

// WriteEntry writes a candidate Entry as YAML under a candidates/ tree. Like the
// cold-source emitter, it REFUSES any path not under candidates/, so corpus
// materialize can never overwrite active knowledge or the seed.
func WriteEntry(outDir string, e Entry) (string, error) {
	if !underCandidatesDir(outDir) {
		return "", fmt.Errorf("refusing to write outside a candidates/ tree (got %q): corpus writes candidates only", outDir)
	}
	if e.Status != StatusCandidate {
		return "", fmt.Errorf("WriteEntry only writes status:candidate (got %q)", e.Status)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return "", err
	}
	path := filepath.Join(outDir, sanitizeID(e.ID)+".yaml")
	if err := os.WriteFile(path, append([]byte(entryHeader), data...), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ValidateEntry checks an entry's required metadata, status, and (for active)
// grounding + citation resolution. Returns violations (empty == ok).
func ValidateEntry(e Entry, repoRoot string, git coldsource.GitVerifier) []string {
	var v []string
	req := map[string]string{"id": e.ID, "type": e.Type, "domain": e.Domain, "status": e.Status, "grounding.tier": e.Grounding.Tier, "provenance": e.Provenance}
	for name, val := range req {
		if strings.TrimSpace(val) == "" {
			v = append(v, "missing "+name)
		}
	}
	if len(e.EvidenceCitations) == 0 {
		v = append(v, "no evidence_citations")
	}
	switch e.Status {
	case StatusCandidate, StatusReviewed, StatusActive:
	default:
		v = append(v, "invalid status: "+e.Status)
	}
	// Drift/conflict and candidate_principle entries must never be active.
	if e.Status == StatusActive && (e.Type == "drift_warning" || e.Type == "candidate_principle") {
		v = append(v, "type "+e.Type+" must not be active (drift/conflict/candidate-principle is not enforceable truth)")
	}
	// Active requires grounded evidence that resolves.
	if e.Status == StatusActive {
		if !groundedTier(e.Grounding.Tier) {
			v = append(v, "active requires grounding tier >= landed_commit, got "+e.Grounding.Tier)
		}
		if ok, _ := coldsource.CheckCitations(e.EvidenceCitations, repoRoot, git); !ok {
			v = append(v, "active requires all citations to resolve, but some did not")
		}
	}
	return v
}

func underCandidatesDir(dir string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Clean(dir)), "/") {
		if seg == "candidates" {
			return true
		}
	}
	return false
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

// LoadReport reads a FindingsReport YAML.
func LoadReport(path string) (FindingsReport, error) {
	var r FindingsReport
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := yaml.Unmarshal(data, &r); err != nil {
		return r, err
	}
	return r, nil
}

// LoadEntry reads a single candidate Entry YAML.
func LoadEntry(path string) (Entry, error) {
	var e Entry
	data, err := os.ReadFile(path)
	if err != nil {
		return e, err
	}
	if err := yaml.Unmarshal(data, &e); err != nil {
		return e, err
	}
	return e, nil
}
