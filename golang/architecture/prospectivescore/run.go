// SPDX-License-Identifier: AGPL-3.0-only

// Package prospectivescore is Slice 2 of the prospective authoring-recall
// harness (#259): the record of what production retrieval surfaced, and the
// metrics of docs/evaluation/prospective-recall-protocol-v1.md section 7.
//
// It is written before any applicability label exists and run after they are
// frozen. That order is the point rather than an accident of scheduling — a
// grader authored after seeing scores is not a grader, and every threshold,
// denominator and rounding rule in here was fixed while the numbers were still
// unknown.
//
// Three properties are structural rather than conventional:
//
//   - Only `applicable` reaches the recall denominator. The other four labels
//     are counted and reported, never collapsed into it. Collapsing `ambiguous`
//     or `cannot_adjudicate` into `not_applicable` would shrink the denominator
//     and inflate recall, which is the cheapest way to make this instrument lie.
//   - Nuisance is three numbers, never one. Primary nuisance over resolved
//     labels alone can be driven down by flooding with unjudgeable items, so the
//     unresolved-surfaced rate and the conservative upper bound travel with it.
//   - A rate whose denominator is zero is absent, not zero. A stratum with no
//     applicable labels has no recall; reporting 0.0 would read as total failure
//     and reporting 1.0 as success, and both would be inventions.
package prospectivescore

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// RunSchemaVersion identifies the retrieval-run artifact's shape.
const RunSchemaVersion = "sensei.prospective_run.v1"

// Retrieval statuses (protocol section 7.3). A miss accompanied by an honest
// `degraded` is a different defect from a miss reported as a confident empty
// result, and only the second launders absence into apparent coverage.
const (
	StatusResolved    = "resolved"
	StatusDegraded    = "degraded"
	StatusNoAnchors   = "no_anchors"
	StatusEmpty       = "empty"
	StatusUnavailable = "unavailable"
	// StatusNoProspectiveChannel is reused from the frozen manifest rather than
	// redeclared, so the runner cannot coin a different spelling for the case
	// the sample already named.
	StatusNoProspectiveChannel = prospective.StatusNoProspectiveChannel
)

// RetrievalStatuses is the closed vocabulary, in report order. Reports iterate
// this so a status can never vanish from a distribution by being absent.
var RetrievalStatuses = []string{
	StatusResolved, StatusDegraded, StatusNoAnchors, StatusEmpty,
	StatusUnavailable, StatusNoProspectiveChannel,
}

func KnownStatus(s string) bool {
	for _, k := range RetrievalStatuses {
		if k == s {
			return true
		}
	}
	return false
}

// Context classes (protocol section 7.4). Descriptive: they exist so a low
// stratum-A score can be attributed to missing context rather than to
// reasoning, or shown not to be.
const (
	CtxChangeContents       = "change_contents"
	CtxPackageIdentity      = "package_or_module_identity"
	CtxImports              = "imports"
	CtxDirectoryOwnership   = "directory_ownership_and_risk_class"
	CtxNeighbouringAnchored = "neighbouring_anchored_components"
	CtxTouchedContracts     = "touched_contracts"
	CtxGlobalScars          = "repository_wide_scars"
	CtxHistory              = "git_history"
)

// ContextClasses is the closed vocabulary, in report order.
var ContextClasses = []string{
	CtxChangeContents, CtxPackageIdentity, CtxImports, CtxDirectoryOwnership,
	CtxNeighbouringAnchored, CtxTouchedContracts, CtxGlobalScars, CtxHistory,
}

// MatchRule records HOW a surfaced node was tied to an eligible corpus item.
//
// It is carried per item rather than assumed because the two vocabularies do
// not line up perfectly: MetaPrinciple nodes are dual-typed meta.* invariants
// and production surfaces them in the invariant partition, so a strict
// class:id match would silently score 164 of the 866 eligible items as
// permanently unreachable. The fallback is recorded rather than hidden, so a
// reader can see how much of a recall figure rests on it.
const (
	MatchExact  = "qualified_id"
	MatchIDOnly = "unqualified_id"
)

// Surfaced is one item production retrieval put in front of the author.
type Surfaced struct {
	// CorpusItemID is the eligible-corpus identity, when the surfaced node is
	// in the corpus at all.
	CorpusItemID string `json:"corpus_item_id"`
	// SurfacedAs is what production called it, verbatim.
	SurfacedAs string `json:"surfaced_as"`
	MatchRule  string `json:"match_rule"`
	// Channel is the response field it arrived on, kept so a reader can tell
	// a direct anchor from an architectural neighbour.
	Channel string `json:"channel"`
}

// ChangeRun is what happened for one pinned change.
type ChangeRun struct {
	ItemKey string `json:"item_key"`
	// Stratum is copied from the frozen manifest, never recomputed here: a
	// re-stratification at scoring time would move changes between denominators
	// after the labels were fixed.
	Stratum         string `json:"stratum"`
	RetrievalStatus string `json:"retrieval_status"`
	StatusDetail    string `json:"status_detail,omitempty"`

	Surfaced []Surfaced `json:"surfaced"`
	// SurfacedOutsideCorpus is everything production surfaced that is not an
	// eligible item. It is reported and never scored: the corpus bounds what an
	// adjudicator could have marked applicable, so an item outside it has no
	// label and cannot be either a hit or a nuisance.
	SurfacedOutsideCorpus []string `json:"surfaced_outside_corpus,omitempty"`

	ContextAvailable []string `json:"context_available"`
	// Invocations are the exact production commands, recorded verbatim so the
	// run can be replayed and disputed.
	Invocations []Invocation `json:"invocations"`
}

// Invocation is one production command and how it answered.
type Invocation struct {
	Command  string `json:"command"`
	ExitOK   bool   `json:"exit_ok"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// Run is the complete retrieval record for one frozen sample.
type Run struct {
	SchemaVersion string `json:"schema_version"`
	ProtocolID    string `json:"protocol_id"`

	SampleManifestDigestSHA256 string `json:"sample_manifest_digest_sha256"`
	BlindCorpusDigestSHA256    string `json:"blind_corpus_digest_sha256"`
	// LabelsDigestSHA256 binds the answer key that already existed when this
	// run executed. It is the machine-checkable form of the protocol's fifth
	// arrow: a run that cannot name the labels it postdates cannot show it
	// postdates them.
	LabelsDigestSHA256 string `json:"labels_digest_sha256"`

	WorldRevision     string                       `json:"world_revision"`
	GraphDigestSHA256 string                       `json:"graph_digest_sha256"`
	RetrievalSurface  prospective.RetrievalSurface `json:"retrieval_surface"`
	ExecutedAt        string                       `json:"executed_at"`
	SenseiInvocation  string                       `json:"sensei_invocation"`

	Changes      []ChangeRun `json:"changes"`
	DigestSHA256 string      `json:"digest_sha256"`
}

// Seal content-addresses a run.
func (r Run) Seal() (Run, error) {
	r.DigestSHA256 = ""
	d, err := prospective.DigestOf(r)
	if err != nil {
		return Run{}, err
	}
	r.DigestSHA256 = d
	return r, nil
}

// Validate refuses a run that cannot be scored honestly.
func (r Run) Validate() error {
	if r.SchemaVersion != RunSchemaVersion {
		return fmt.Errorf("run carries schema %q, not %q", r.SchemaVersion, RunSchemaVersion)
	}
	if strings.TrimSpace(r.LabelsDigestSHA256) == "" {
		return fmt.Errorf("run names no frozen labels digest: a run that cannot show the answer key predated it is not evidence about recall")
	}
	if strings.TrimSpace(r.ExecutedAt) == "" {
		return fmt.Errorf("run carries no execution timestamp")
	}
	if len(r.Changes) == 0 {
		return fmt.Errorf("run covers no changes")
	}
	// A run is evidence, so it must be able to show it has not been edited
	// since it was produced. An artifact that carries no digest, or one it does
	// not hash to, is a claim about a retrieval nobody can reproduce.
	if strings.TrimSpace(r.DigestSHA256) == "" {
		return fmt.Errorf("run carries no digest: an artifact that is not content-addressed cannot be shown to be the one a score was computed from")
	}
	resealed, err := r.Seal()
	if err != nil {
		return err
	}
	if resealed.DigestSHA256 != r.DigestSHA256 {
		return fmt.Errorf("run does not hash to the digest it carries (%s vs recomputed %s): it has been edited since it was recorded",
			r.DigestSHA256, resealed.DigestSHA256)
	}
	seen := map[string]bool{}
	for _, c := range r.Changes {
		if seen[c.ItemKey] {
			return fmt.Errorf("run records %s twice", c.ItemKey)
		}
		seen[c.ItemKey] = true
		if !KnownStatus(c.RetrievalStatus) {
			return fmt.Errorf("change %s carries retrieval status %q, which is outside the closed vocabulary: an unrecognised status is a status nobody can interpret", c.ItemKey, c.RetrievalStatus)
		}
		if !knownStratum(c.Stratum) {
			return fmt.Errorf("change %s carries stratum %q", c.ItemKey, c.Stratum)
		}
		for _, s := range c.Surfaced {
			if s.MatchRule != MatchExact && s.MatchRule != MatchIDOnly {
				return fmt.Errorf("change %s surfaced %s under the unknown match rule %q", c.ItemKey, s.CorpusItemID, s.MatchRule)
			}
		}
	}
	return nil
}

func knownStratum(s string) bool {
	for _, k := range prospective.Strata {
		if k == s {
			return true
		}
	}
	return false
}

// SurfacedIDs returns the eligible-corpus items surfaced for a change, in
// deterministic order and deduplicated.
func (c ChangeRun) SurfacedIDs() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range c.Surfaced {
		if s.CorpusItemID == "" || seen[s.CorpusItemID] {
			continue
		}
		seen[s.CorpusItemID] = true
		out = append(out, s.CorpusItemID)
	}
	sort.Strings(out)
	return out
}
