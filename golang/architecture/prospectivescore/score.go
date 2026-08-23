// SPDX-License-Identifier: AGPL-3.0-only

package prospectivescore

import (
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture/prospective"
	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
)

// ScoreSchemaVersion identifies the score artifact's shape.
const ScoreSchemaVersion = "sensei.prospective_score.v1"

// Rate is a metric that can legitimately not exist.
//
// A stratum with no applicable labels has no recall — not a recall of zero.
// Encoding that as *float64 rather than 0.0 is what stops an empty denominator
// from being read as total failure, and stops a report from averaging a number
// nobody measured into one somebody did.
type Rate struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
}

func rate(num, den int) Rate {
	r := Rate{Numerator: num, Denominator: den}
	if den > 0 {
		v := float64(num) / float64(den)
		r.Value = &v
	}
	return r
}

// LabelCounts is the full label distribution, never collapsed.
type LabelCounts struct {
	Applicable       int `json:"applicable"`
	NotApplicable    int `json:"not_applicable"`
	Ambiguous        int `json:"ambiguous"`
	OutsideScope     int `json:"outside_scope"`
	CannotAdjudicate int `json:"cannot_adjudicate"`
	Unlabelled       int `json:"unlabelled"`
}

func (lc *LabelCounts) add(label string) {
	switch label {
	case prospectivelabel.LabelApplicable:
		lc.Applicable++
	case prospectivelabel.LabelNotApplicable:
		lc.NotApplicable++
	case prospectivelabel.LabelAmbiguous:
		lc.Ambiguous++
	case prospectivelabel.LabelOutsideScope:
		lc.OutsideScope++
	case prospectivelabel.LabelCannotAdjudicate:
		lc.CannotAdjudicate++
	default:
		lc.Unlabelled++
	}
}

// resolved counts the labels that can be on both sides of a nuisance ratio.
func (lc LabelCounts) resolved() int { return lc.Applicable + lc.NotApplicable }

// unresolved counts labels that can never reach a nuisance numerator's top.
func (lc LabelCounts) unresolved() int { return lc.Ambiguous + lc.OutsideScope + lc.CannotAdjudicate }

// Miss is one applicable item production did not surface.
//
// The missed set emitted by this package is COMPLETE, never a selection.
// Protocol section 12 freezes examples with the labels because choosing
// illustrative misses after seeing the scores is editing the answer key with
// extra steps; emitting all of them removes the choice rather than regulating
// it.
type Miss struct {
	ItemKey      string `json:"item_key"`
	Stratum      string `json:"stratum"`
	CorpusItemID string `json:"corpus_item_id"`
	// RetrievalStatus travels with the miss: a miss under an honest `degraded`
	// is a different finding from a miss under a confident empty result.
	RetrievalStatus string `json:"retrieval_status"`
}

// ChangeScore is one pinned change's contribution, kept visible so no stratum
// figure has to be taken on trust.
type ChangeScore struct {
	ItemKey         string      `json:"item_key"`
	Stratum         string      `json:"stratum"`
	RetrievalStatus string      `json:"retrieval_status"`
	ApplicableTotal int         `json:"applicable_total"`
	ApplicableHit   int         `json:"applicable_surfaced"`
	SurfacedTotal   int         `json:"surfaced_total"`
	SurfacedLabels  LabelCounts `json:"surfaced_labels"`
	Recall          Rate        `json:"recall"`
}

// StratumScore is the protocol's per-stratum report unit.
type StratumScore struct {
	Stratum     string `json:"stratum"`
	ChangeCount int    `json:"changes"`

	// Recall over `applicable` labels only (section 7.1).
	Recall Rate `json:"applicable_item_prospective_recall"`

	// The three nuisance numbers of section 7.2, always together.
	PrimaryNuisance        Rate `json:"primary_nuisance"`
	UnresolvedSurfacedRate Rate `json:"unresolved_surfaced_rate"`
	ConservativeNuisance   Rate `json:"conservative_nuisance"`

	SurfacedTotal  int         `json:"surfaced_total"`
	SurfacedLabels LabelCounts `json:"surfaced_labels"`
	// AdjudicatedLabels is the full distribution over every (change, item) pair
	// in this stratum, so a reader can see the denominator the recall figure
	// was cut from.
	AdjudicatedLabels LabelCounts `json:"adjudicated_labels"`

	RetrievalStatusCounts map[string]int `json:"retrieval_status_counts"`
	ContextAvailability   map[string]int `json:"context_availability"`

	PerChange []ChangeScore `json:"per_change"`
	Misses    []Miss        `json:"applicable_items_missed"`
}

// Score is the complete result. There is deliberately no single headline
// number anywhere in this type: section 13 lists six distinct readings that a
// blended score would erase, and a field able to hold one is a field that will
// eventually be quoted alone.
type Score struct {
	SchemaVersion string `json:"schema_version"`
	ProtocolID    string `json:"protocol_id"`

	SampleManifestDigestSHA256 string `json:"sample_manifest_digest_sha256"`
	BlindCorpusDigestSHA256    string `json:"blind_corpus_digest_sha256"`
	LabelsDigestSHA256         string `json:"labels_digest_sha256"`
	RunDigestSHA256            string `json:"run_digest_sha256"`
	WorldRevision              string `json:"world_revision"`
	GraphDigestSHA256          string `json:"graph_digest_sha256"`

	RetrievalSurfaceID      string `json:"retrieval_surface_id"`
	Adjudicator             string `json:"adjudicator"`
	SecondAdjudicator       string `json:"second_adjudicator,omitempty"`
	SecondAdjudicatorStatus string `json:"second_adjudicator_status"`
	LabelsFrozenAt          string `json:"labels_frozen_at"`
	RunExecutedAt           string `json:"run_executed_at"`

	// Strata is ordered by prospective.Strata and always contains every
	// stratum, including empty ones. A and B are separate entries and no
	// function in this package merges them.
	Strata []StratumScore `json:"strata"`

	// Macro keeps every stratum visible beside the average, per section 7.1's
	// "a macro average may be secondary; it may never be the headline".
	Macro MacroSummary `json:"macro_summary"`

	// SurfacedOutsideCorpusTotal is reported so a reader can see how much of
	// what production surfaced the ruler could not judge at all.
	SurfacedOutsideCorpusTotal int `json:"surfaced_outside_corpus_total"`
	// MatchRuleCounts shows how many hits rest on the unqualified-id fallback.
	MatchRuleCounts map[string]int `json:"match_rule_counts"`

	DigestSHA256 string `json:"digest_sha256"`
}

// MacroSummary is the unweighted mean over strata that have the metric, with
// the contributing strata named. A mean over a subset of strata that does not
// say which subset is a number pretending to cover everything.
type MacroSummary struct {
	RecallStrata               []string `json:"recall_contributing_strata"`
	RecallMacroAverage         *float64 `json:"recall_macro_average"`
	PrimaryNuisanceStrata      []string `json:"primary_nuisance_contributing_strata"`
	PrimaryNuisanceMacro       *float64 `json:"primary_nuisance_macro_average"`
	ConservativeNuisanceStrata []string `json:"conservative_nuisance_contributing_strata"`
	ConservativeNuisanceMacro  *float64 `json:"conservative_nuisance_macro_average"`
	// StrataWithoutRecall names the strata that had no applicable labels at
	// all, so an absent metric is visible rather than merely missing.
	StrataWithoutRecall []string `json:"strata_without_recall"`
}

// Input is everything scoring needs, and nothing it must not have.
//
// There is no graph handle, no retrieval client and no corpus with anchors in
// it: scoring compares two frozen artifacts and may not consult the system
// being graded while doing it.
type Input struct {
	Manifest prospective.Manifest
	Labels   prospectivelabel.LabelSet
	Run      Run
	// EligibleItemIDs is the blind corpus's identity set — the bound on what
	// could have been marked applicable.
	EligibleItemIDs []string
}

// Compute produces the score. It reads labels and a run, and nothing else.
func Compute(in Input) (Score, error) {
	if err := in.Run.Validate(); err != nil {
		return Score{}, err
	}
	if err := in.Labels.VerifyBinding(in.Manifest.DigestSHA256, in.Manifest.BlindCorpusDigestSHA256); err != nil {
		return Score{}, err
	}
	if in.Run.LabelsDigestSHA256 != in.Labels.DigestSHA256 {
		return Score{}, fmt.Errorf("this run was executed against frozen labels %s but is being scored against %s: the run must postdate the exact answer key it is graded by",
			in.Run.LabelsDigestSHA256, in.Labels.DigestSHA256)
	}
	if in.Run.SampleManifestDigestSHA256 != in.Manifest.DigestSHA256 {
		return Score{}, fmt.Errorf("run answers sample manifest %s, not %s", in.Run.SampleManifestDigestSHA256, in.Manifest.DigestSHA256)
	}
	if len(in.EligibleItemIDs) == 0 {
		return Score{}, fmt.Errorf("the eligible corpus is empty: it bounds the denominator, so a score computed without it is not the protocol's metric")
	}

	stratumOf := map[string]string{}
	for _, it := range in.Manifest.Items {
		stratumOf[it.ItemKey] = it.Stratum
	}

	// Index labels once. A pair with no label is counted as unlabelled and
	// never silently treated as not_applicable.
	type pair struct{ item, corpus string }
	labelOf := map[pair]string{}
	for _, l := range in.Labels.Labels {
		labelOf[pair{l.ItemKey, l.CorpusItemID}] = l.Label
	}

	byStratum := map[string]*StratumScore{}
	for _, s := range prospective.Strata {
		byStratum[s] = &StratumScore{
			Stratum:               s,
			RetrievalStatusCounts: map[string]int{},
			ContextAvailability:   map[string]int{},
		}
	}
	matchRules := map[string]int{}
	outsideTotal := 0

	// Iterate the manifest's changes, not the run's. A change the runner
	// dropped must still reach the denominator: silently scoring only what was
	// executed is how a hard case leaves the experiment.
	runByKey := map[string]ChangeRun{}
	for _, c := range in.Run.Changes {
		runByKey[c.ItemKey] = c
	}
	for _, item := range in.Manifest.Items {
		st, ok := byStratum[item.Stratum]
		if !ok {
			return Score{}, fmt.Errorf("sampled change %s carries stratum %q, which is outside the closed vocabulary", item.ItemKey, item.Stratum)
		}
		cr, executed := runByKey[item.ItemKey]
		if !executed {
			// An unexecuted change is recorded as unavailable, with its
			// applicable labels still in the denominator. It is a miss with an
			// honest status, which is exactly how the protocol treats a change
			// production had no channel for.
			cr = ChangeRun{ItemKey: item.ItemKey, Stratum: item.Stratum, RetrievalStatus: StatusUnavailable,
				StatusDetail: "no retrieval was recorded for this change"}
		}
		st.ChangeCount++
		st.RetrievalStatusCounts[cr.RetrievalStatus]++
		for _, c := range cr.ContextAvailable {
			st.ContextAvailability[c]++
		}
		outsideTotal += len(cr.SurfacedOutsideCorpus)
		for _, s := range cr.Surfaced {
			if s.CorpusItemID != "" {
				matchRules[s.MatchRule]++
			}
		}

		surfaced := map[string]bool{}
		for _, id := range cr.SurfacedIDs() {
			surfaced[id] = true
		}

		cs := ChangeScore{ItemKey: item.ItemKey, Stratum: item.Stratum, RetrievalStatus: cr.RetrievalStatus}
		for _, corpusID := range in.EligibleItemIDs {
			label := labelOf[pair{item.ItemKey, corpusID}]
			st.AdjudicatedLabels.add(label)
			isApplicable := label == prospectivelabel.LabelApplicable
			if isApplicable {
				cs.ApplicableTotal++
			}
			if !surfaced[corpusID] {
				if isApplicable {
					st.Misses = append(st.Misses, Miss{
						ItemKey: item.ItemKey, Stratum: item.Stratum,
						CorpusItemID: corpusID, RetrievalStatus: cr.RetrievalStatus,
					})
				}
				continue
			}
			cs.SurfacedTotal++
			cs.SurfacedLabels.add(label)
			st.SurfacedTotal++
			st.SurfacedLabels.add(label)
			if isApplicable {
				cs.ApplicableHit++
			}
		}
		cs.Recall = rate(cs.ApplicableHit, cs.ApplicableTotal)
		st.Recall.Numerator += cs.ApplicableHit
		st.Recall.Denominator += cs.ApplicableTotal
		st.PerChange = append(st.PerChange, cs)
	}

	score := Score{
		SchemaVersion:              ScoreSchemaVersion,
		ProtocolID:                 in.Manifest.ProtocolID,
		SampleManifestDigestSHA256: in.Manifest.DigestSHA256,
		BlindCorpusDigestSHA256:    in.Manifest.BlindCorpusDigestSHA256,
		LabelsDigestSHA256:         in.Labels.DigestSHA256,
		RunDigestSHA256:            in.Run.DigestSHA256,
		WorldRevision:              in.Manifest.World.Revision,
		GraphDigestSHA256:          in.Run.GraphDigestSHA256,
		RetrievalSurfaceID:         in.Manifest.RetrievalSurface.ID,
		Adjudicator:                in.Labels.Adjudicator,
		SecondAdjudicator:          in.Labels.SecondAdjudicator,
		SecondAdjudicatorStatus:    in.Labels.SecondAdjudicatorStatus,
		LabelsFrozenAt:             in.Labels.FrozenAt,
		RunExecutedAt:              in.Run.ExecutedAt,
		SurfacedOutsideCorpusTotal: outsideTotal,
		MatchRuleCounts:            matchRules,
	}
	for _, name := range prospective.Strata {
		st := byStratum[name]
		st.Recall = rate(st.Recall.Numerator, st.Recall.Denominator)
		// Primary nuisance: resolved labels only. Admitting unresolved labels
		// into this denominator would let a retriever lower its reported
		// nuisance by flooding with unjudgeable items, defeating the one check
		// this metric exists to perform.
		st.PrimaryNuisance = rate(st.SurfacedLabels.NotApplicable, st.SurfacedLabels.resolved())
		st.UnresolvedSurfacedRate = rate(st.SurfacedLabels.unresolved()+st.SurfacedLabels.Unlabelled, st.SurfacedTotal)
		st.ConservativeNuisance = rate(
			st.SurfacedLabels.NotApplicable+st.SurfacedLabels.unresolved()+st.SurfacedLabels.Unlabelled,
			st.SurfacedTotal)
		sort.SliceStable(st.Misses, func(i, j int) bool {
			if st.Misses[i].ItemKey != st.Misses[j].ItemKey {
				return st.Misses[i].ItemKey < st.Misses[j].ItemKey
			}
			return st.Misses[i].CorpusItemID < st.Misses[j].CorpusItemID
		})
		score.Strata = append(score.Strata, *st)
	}
	score.Macro = macro(score.Strata)
	return score.seal()
}

func (s Score) seal() (Score, error) {
	s.DigestSHA256 = ""
	d, err := prospective.DigestOf(s)
	if err != nil {
		return Score{}, err
	}
	s.DigestSHA256 = d
	return s, nil
}

// macro averages only over strata that have the metric, and names them.
func macro(strata []StratumScore) MacroSummary {
	m := MacroSummary{}
	var recall, primary, conservative []float64
	for _, st := range strata {
		if st.Recall.Value != nil {
			m.RecallStrata = append(m.RecallStrata, st.Stratum)
			recall = append(recall, *st.Recall.Value)
		} else {
			m.StrataWithoutRecall = append(m.StrataWithoutRecall, st.Stratum)
		}
		if st.PrimaryNuisance.Value != nil {
			m.PrimaryNuisanceStrata = append(m.PrimaryNuisanceStrata, st.Stratum)
			primary = append(primary, *st.PrimaryNuisance.Value)
		}
		if st.ConservativeNuisance.Value != nil {
			m.ConservativeNuisanceStrata = append(m.ConservativeNuisanceStrata, st.Stratum)
			conservative = append(conservative, *st.ConservativeNuisance.Value)
		}
	}
	m.RecallMacroAverage = mean(recall)
	m.PrimaryNuisanceMacro = mean(primary)
	m.ConservativeNuisanceMacro = mean(conservative)
	return m
}

func mean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	v := sum / float64(len(xs))
	return &v
}
