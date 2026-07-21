// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
)

// Bundle export / import for the session/manual drafter path.
//
// The drafter is the only key-dependent step in the pipeline. When no API key
// is available, AWG can EXPORT triangulated bundles (+ schema + prompt) so an
// external session/human can draft candidates, then IMPORT those drafts and run
// them through the SAME validation cage as the API/echo drafters.
//
// SAFETY: a manual draft is untrusted. It is bound to a content-addressed
// BundleID; on import the cold-bootstrap command RE-EXTRACTS from the live repo,
// so the draft is validated against the live bundle — never against the
// submitted/exported one — and can never widen its own allowed-citation set.

// BundleID is a deterministic content hash of a bundle's evidence: its theme
// plus the sorted set of citation strings and matched texts. Triangulation is
// deterministic, so the id is stable across runs; a drifted bundle (evidence
// added/removed/changed) yields a different id, so a draft bound to stale
// evidence fails to match on import and is rejected.
func (b Bundle) BundleID() string {
	cites := sortedKeys(b.AllowedCitations())
	var texts []string
	for _, s := range b.Signals {
		if t := strings.TrimSpace(s.MatchedText); t != "" {
			texts = append(texts, t)
		}
	}
	sort.Strings(texts)

	var sb strings.Builder
	sb.WriteString("theme\x1f" + b.ThemeKey + "\x1e")
	for _, c := range cites {
		sb.WriteString("cite\x1f" + c + "\x1e")
	}
	for _, t := range texts {
		sb.WriteString("text\x1f" + t + "\x1e")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// ExportSignal is one evidence line in an exported bundle.
type ExportSignal struct {
	SourceType  string   `json:"source_type"`
	Citations   []string `json:"citations"`
	MatchedText string   `json:"matched_text"`
}

// BundleExport is a triangulated bundle serialized for external drafting.
type BundleExport struct {
	BundleID         string         `json:"bundle_id"`
	Theme            string         `json:"theme"`
	SourceTypes      []string       `json:"source_types"`
	Signals          []ExportSignal `json:"signals"`
	AllowedCitations []string       `json:"allowed_citations"`
}

// ExportEnvelope is the full --drafter export payload: the candidate schema and
// prompt contract (so an external drafter uses the same contract the validator
// enforces) plus the bounded set of bundles.
type ExportEnvelope struct {
	CandidateSchema json.RawMessage `json:"candidate_schema"`
	PromptContract  string          `json:"prompt_contract"`
	Bundles         []BundleExport  `json:"bundles"`
}

// ExportBundle serializes one bundle.
func ExportBundle(b Bundle) BundleExport {
	sigs := make([]ExportSignal, 0, len(b.Signals))
	for _, s := range b.Signals {
		sigs = append(sigs, ExportSignal{
			SourceType:  string(s.SourceType),
			Citations:   s.Citations(),
			MatchedText: strings.TrimSpace(s.MatchedText),
		})
	}
	return BundleExport{
		BundleID:         b.BundleID(),
		Theme:            b.ThemeKey,
		SourceTypes:      sourceTypeStrings(b.SourceTypes),
		Signals:          sigs,
		AllowedCitations: sortedKeys(b.AllowedCitations()),
	}
}

// NewExportEnvelope builds the export payload for a (bounded) set of bundles.
func NewExportEnvelope(bundles []Bundle) ExportEnvelope {
	out := make([]BundleExport, 0, len(bundles))
	for _, b := range bundles {
		out = append(out, ExportBundle(b))
	}
	return ExportEnvelope{
		CandidateSchema: json.RawMessage(candidateSchema),
		PromptContract:  promptContract,
		Bundles:         out,
	}
}

// SubmittedDraft is one externally-drafted candidate, keyed to its bundle.
// Dual json/yaml tags so it can be read from JSON (stdin) or a YAML/JSON file.
type SubmittedDraft struct {
	BundleID          string   `json:"bundle_id" yaml:"bundle_id"`
	CandidateClass    string   `json:"candidate_class" yaml:"candidate_class"`
	Theme             string   `json:"theme" yaml:"theme"`
	Reason            string   `json:"reason" yaml:"reason"`
	Confidence        string   `json:"confidence" yaml:"confidence"`
	ActivationTrigger string   `json:"activation_trigger" yaml:"activation_trigger"`
	RequiredTests     []string `json:"required_tests" yaml:"required_tests"`
	SourcePaths       []string `json:"source_paths" yaml:"source_paths"`
}

// ParseSubmittedDrafts reads a JSON array (or single object) of drafts from r.
func ParseSubmittedDrafts(r io.Reader) ([]SubmittedDraft, error) {
	data, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("empty draft input")
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []SubmittedDraft
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, fmt.Errorf("parse drafts array: %w", err)
		}
		return arr, nil
	}
	var one SubmittedDraft
	if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
		return nil, fmt.Errorf("parse draft: %w", err)
	}
	return []SubmittedDraft{one}, nil
}

// StdinDrafter is a Drafter whose candidates come from externally-supplied
// drafts (e.g. an active session) keyed by BundleID, instead of an API call.
// It maps a draft to the LIVE bundle by id; downstream ValidateDraft then checks
// the draft's citations against that live bundle, so binding is enforced twice
// (id match + citation membership). Status is forced to candidate.
type StdinDrafter struct {
	byID map[string]SubmittedDraft
}

// NewStdinDrafter indexes submitted drafts by bundle_id.
func NewStdinDrafter(drafts []SubmittedDraft) StdinDrafter {
	m := make(map[string]SubmittedDraft, len(drafts))
	for _, d := range drafts {
		if strings.TrimSpace(d.BundleID) != "" {
			m[d.BundleID] = d
		}
	}
	return StdinDrafter{byID: m}
}

// Draft implements Drafter. Returns a no_draft_supplied error when the live
// bundle has no matching submitted draft (drift / missing).
func (d StdinDrafter) Draft(_ context.Context, b Bundle) (*extractor.PromotionProposal, error) {
	sd, ok := d.byID[b.BundleID()]
	if !ok {
		return nil, DraftError{
			Kind:   "no_draft_supplied",
			Reason: "no submitted draft for bundle_id " + b.BundleID() + " (theme " + b.ThemeKey + ")",
		}
	}
	class, ok := normalizeCandidateClass(sd.CandidateClass)
	if !ok {
		return nil, DraftError{Kind: "bad_class", Reason: "unknown candidate class: " + sd.CandidateClass}
	}
	trigger := strings.TrimSpace(sd.ActivationTrigger)
	if trigger == "" {
		trigger = "edit under " + strings.ReplaceAll(b.ThemeKey, ".", "/")
	}
	return &extractor.PromotionProposal{
		CandidateID:       "candidate." + b.ThemeKey,
		CandidateClass:    class,
		Status:            "candidate", // FORCED — a manual draft can never mint an active node
		Theme:             b.ThemeKey,
		SourcePaths:       sd.SourcePaths, // validated against the LIVE bundle downstream
		Reason:            strings.TrimSpace(sd.Reason),
		Confidence:        normalizeConfidence(sd.Confidence),
		ActivationTrigger: trigger,
		RequiredTests:     sd.RequiredTests,
		NonAuthorityScope: true,
	}, nil
}

// ── reconstruction (for the validate-draft oracle) ───────────────────────────

// BundleFromExport rebuilds a Bundle from an exported one so the cage functions
// (ValidateDraft / IsShallow) can run against it. It faithfully reproduces the
// citation set and matched texts. NOTE: this trusts the provided file — it is
// the debug oracle used by `validate-draft`. The authoritative import path
// (--drafter stdin) re-extracts from the live repo instead.
func BundleFromExport(e BundleExport) Bundle {
	sigs := make([]ColdSignal, 0, len(e.Signals))
	for _, es := range e.Signals {
		sigs = append(sigs, signalFromExport(es))
	}
	return Bundle{ThemeKey: e.Theme, Signals: sigs, SourceTypes: parseSourceTypes(e.SourceTypes)}
}

func signalFromExport(es ExportSignal) ColdSignal {
	s := ColdSignal{SourceType: SourceType(es.SourceType), MatchedText: es.MatchedText}
	for _, c := range es.Citations {
		switch {
		case strings.HasPrefix(c, "file:"):
			rest := strings.TrimPrefix(c, "file:")
			if i := strings.LastIndexByte(rest, ':'); i >= 0 {
				if n, err := strconv.Atoi(rest[i+1:]); err == nil {
					s.FilePath, s.Line = rest[:i], n
					continue
				}
			}
			s.FilePath = rest
		case strings.HasPrefix(c, "commit:"):
			s.CommitSHA = strings.TrimPrefix(c, "commit:")
		case strings.HasPrefix(c, "pr:"):
			rest := strings.TrimPrefix(c, "pr:")
			if i := strings.IndexByte(rest, ':'); i >= 0 {
				s.PRID, s.CommentID = rest[:i], rest[i+1:]
			} else {
				s.PRID = rest
			}
		}
	}
	return s
}

func parseSourceTypes(ss []string) []SourceType {
	out := make([]SourceType, 0, len(ss))
	for _, s := range ss {
		out = append(out, SourceType(s))
	}
	return out
}
