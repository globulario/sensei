// SPDX-License-Identifier: AGPL-3.0-only

package phase10score

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ScoreSchemaVersion identifies the score artifact's shape.
const ScoreSchemaVersion = "sensei.phase10_score.v2"

// Availability states. A metric is one of these, and "zero" is never used to
// mean any of them.
const (
	// Computed means the metric has a real denominator.
	Computed = "computed"
	// NoAdjudicableSample means the lane was sampled but nothing in it reached
	// a label that can enter a denominator.
	NoAdjudicableSample = "no_adjudicable_sample"
	// NotSampled means this reference set drew no items for the metric.
	NotSampled = "not_sampled"
	// NotCapturedByContainer means the label container carries no field the
	// metric needs. It is a property of the ruler, not of the system graded.
	NotCapturedByContainer = "not_captured_by_label_container"
	// SecondAdjudicatorUnavailable is section 13's typed absence.
	SecondAdjudicatorUnavailable = "second_adjudicator_unavailable"
)

// Metric is a number that may legitimately not exist, with the reason it does
// not. A bare float cannot distinguish "measured zero" from "never measured",
// and section 20 exists to stop exactly that confusion.
type Metric struct {
	Availability string   `json:"availability"`
	Numerator    int      `json:"numerator"`
	Denominator  int      `json:"denominator"`
	Value        *float64 `json:"value"`
	Reason       string   `json:"reason,omitempty"`
}

func computed(num, den int) Metric {
	if den == 0 {
		return Metric{Availability: NoAdjudicableSample, Numerator: num, Denominator: den,
			Reason: "no label in this stratum entered the denominator"}
	}
	v := float64(num) / float64(den)
	return Metric{Availability: Computed, Numerator: num, Denominator: den, Value: &v}
}

func absent(state, reason string) Metric {
	return Metric{Availability: state, Reason: reason}
}

// LabelCounts is the full precision-label distribution, never collapsed.
type LabelCounts struct {
	Supported        int `json:"supported"`
	Unsupported      int `json:"unsupported"`
	Ambiguous        int `json:"ambiguous"`
	OutsideScope     int `json:"outside_scope"`
	CannotAdjudicate int `json:"cannot_adjudicate"`
	Unlabelled       int `json:"unlabelled"`
	Unrecognised     int `json:"unrecognised"`
}

func (c *LabelCounts) add(label *string) {
	if label == nil || strings.TrimSpace(*label) == "" {
		c.Unlabelled++
		return
	}
	switch *label {
	case LabelSupported:
		c.Supported++
	case LabelUnsupported:
		c.Unsupported++
	case LabelAmbiguous:
		c.Ambiguous++
	case LabelOutsideScope:
		c.OutsideScope++
	case LabelCannotAdjudicate:
		c.CannotAdjudicate++
	default:
		// An unrecognised label is counted on its own rather than mapped to
		// the nearest known one. Guessing what a human meant is how an answer
		// key acquires answers nobody gave.
		c.Unrecognised++
	}
}

func (c LabelCounts) resolved() int { return c.Supported + c.Unsupported }

// ProviderScore is one (world, provider) precision stratum — the unit section
// 6.3 makes the headline, so a thin architecture-relevant provider stays
// visible beside a dominant one.
type ProviderScore struct {
	World      string      `json:"world"`
	ProviderID string      `json:"provider_id"`
	Population int         `json:"population"`
	Sampled    int         `json:"sampled"`
	Labels     LabelCounts `json:"labels"`
	Precision  Metric      `json:"precision"`
	// Observations weights this stratum for the micro figure, using each
	// item's multiplicity rather than counting sampled rows.
	Observations int `json:"observations_represented"`
}

// WorldScore aggregates one world without hiding its strata.
type WorldScore struct {
	World                    string          `json:"world"`
	RepositoryDomain         string          `json:"repository_domain"`
	Revision                 string          `json:"revision"`
	WorldBindingDigestSHA256 string          `json:"world_binding_digest_sha256"`
	Providers                []ProviderScore `json:"provider_strata"`
	MacroPrecision           Metric          `json:"macro_precision"`
	MicroPrecision           Metric          `json:"micro_precision"`
	UnsupportedRate          Metric          `json:"unsupported_claim_rate"`
	Recall                   Metric          `json:"recall"`
	RecallUnits              int             `json:"recall_units_sampled"`
	RecallLabels             map[string]int  `json:"recall_expected_state_counts"`
	ChallengeUsefulness      Usefulness      `json:"challenge_usefulness"`
	Burden                   Burden          `json:"operator_burden"`
}

// Usefulness is section 10's rating distribution. It is never compressed into
// pass/fail, and the action distribution travels with it.
type Usefulness struct {
	Availability string         `json:"availability"`
	Reason       string         `json:"reason,omitempty"`
	Distribution map[string]int `json:"rating_distribution"`
	Rated        int            `json:"rated"`
	Mean         *float64       `json:"mean"`
	Median       *float64       `json:"median"`
	Actions      Metric         `json:"action_distribution_availability"`
	ActionCounts map[string]int `json:"action_counts,omitempty"`
}

// Burden is section 11.
type Burden struct {
	ItemsReviewed              int    `json:"items_reviewed"`
	RequiringEvidenceLookup    int    `json:"requiring_evidence_lookup"`
	AmbiguousOrCannot          int    `json:"ambiguous_or_cannot_adjudicate"`
	EvidenceLookupsPer100      Metric `json:"evidence_lookups_per_100_items"`
	AmbiguousRate              Metric `json:"ambiguous_or_cannot_adjudicate_rate"`
	CorrectionsPer100          Metric `json:"corrections_per_100_items"`
	MedianActiveSecondsPerItem Metric `json:"median_active_seconds_per_item"`
}

// Agreement is section 13.
type Agreement struct {
	Availability string `json:"availability"`
	Reason       string `json:"reason,omitempty"`
	OverlapItems int    `json:"overlap_items"`
	Compared     int    `json:"compared"`
	Agreed       int    `json:"agreed"`
	RawAgreement Metric `json:"raw_agreement"`
	// DisagreementsByPair preserves both labels rather than resolving them.
	DisagreementsByPair map[string]int `json:"disagreements_by_label_pair,omitempty"`
}

// Score is the complete result. There is no field for a single aggregate,
// because section 20 says no single aggregate score is sufficient evidence for
// #131 completion — and a field able to hold one will eventually be quoted
// alone.
type Score struct {
	SchemaVersion string `json:"schema_version"`
	ProtocolID    string `json:"protocol_id"`

	ProtocolDigestSHA256     string `json:"protocol_digest_sha256"`
	SampleManifestDeclaredID string `json:"sample_manifest_declared_identity_sha256"`
	SampleManifestFileDigest string `json:"sample_manifest_file_digest_sha256"`
	ReferenceSetDigestSHA256 string `json:"reference_set_digest_sha256"`
	AdjudicatorOverlapDigest string `json:"adjudicator_overlap_digest_sha256,omitempty"`
	SelectionSeed            string `json:"selection_seed"`

	Worlds []WorldScore `json:"worlds"`

	// Headline is the macro-of-macros across worlds, reported with the worlds
	// it covers named. It is a summary of the table above it, not a substitute.
	HeadlineMacroPrecision Metric   `json:"headline_macro_precision"`
	HeadlineWorlds         []string `json:"headline_contributing_worlds"`

	ContradictionPreservation Metric    `json:"contradiction_preservation_rate"`
	ModelDelta                Metric    `json:"optional_model_delta"`
	SecondAdjudicator         Agreement `json:"second_adjudicator"`

	// Uncomputable names every metric this reference-set version cannot
	// produce, with the reason. It is a required part of the report rather
	// than an appendix: a metric that vanishes silently flatters by omission.
	Uncomputable []string `json:"metrics_not_computable"`

	TotalItems    int    `json:"total_sampled_items"`
	TotalLabelled int    `json:"total_labelled_items"`
	DigestSHA256  string `json:"digest_sha256"`
}

// Compute scores a loaded reference set. It reads the frozen artifacts and
// nothing else; it never consults the system being graded.
func Compute(rs *ReferenceSet, second *ReferenceSet) (Score, error) {
	items := rs.ItemsByKey()
	worlds := map[string]*WorldScore{}
	order := []string{}
	for _, w := range rs.Manifest.Worlds {
		worlds[w.World] = &WorldScore{
			World: w.World, RepositoryDomain: w.RepositoryDomain, Revision: w.Revision,
			WorldBindingDigestSHA256: w.WorldBindingDigestSHA256,
			RecallLabels:             map[string]int{},
		}
		order = append(order, w.World)
	}

	type provKey struct{ world, provider string }
	provLabels := map[provKey]*LabelCounts{}
	provObs := map[provKey]int{}
	var (
		expectedTotal     = map[string]int{}
		expectedMatched   = map[string]int{}
		expectedFactsSeen bool
		extendedLanes     = map[string]map[string]bool{}
		ratings           = map[string][]float64{}
		actionCounts      = map[string]map[string]int{}
		actionsSeen       bool
		correctionSeen    bool
		corrections       = map[string]int{}
		activeSeconds     = map[string][]float64{}
		burden            = map[string]*Burden{}
		labelled          int
	)
	for _, w := range order {
		burden[w] = &Burden{}
		actionCounts[w] = map[string]int{}
	}

	for _, lf := range rs.Labels {
		if extendedLanes[lf.World] == nil {
			extendedLanes[lf.World] = map[string]bool{}
		}
		extendedLanes[lf.World][lf.Lane] = lf.HoldsExtendedFields()
		ws, ok := worlds[lf.World]
		if !ok {
			return Score{}, fmt.Errorf("label container %s names world %q, which the manifest does not bind", lf.Path, lf.World)
		}
		for _, rec := range lf.Labels {
			item, known := items[rec.ItemKey]
			if !known {
				return Score{}, fmt.Errorf("label container %s labels %s, which is not in the sample manifest", lf.Path, rec.ItemKey)
			}
			b := burden[lf.World]
			b.ItemsReviewed++
			if rec.Label != nil && strings.TrimSpace(*rec.Label) != "" {
				labelled++
			}
			if len(rec.EvidenceIDsInspected) > 0 {
				b.RequiringEvidenceLookup++
			}
			if rec.RequiredCorrection != nil {
				correctionSeen = true
				if *rec.RequiredCorrection {
					corrections[lf.World]++
				}
			}
			if rec.ActiveSeconds != nil {
				activeSeconds[lf.World] = append(activeSeconds[lf.World], *rec.ActiveSeconds)
			}

			switch lf.Lane {
			case LanePrecision:
				k := provKey{lf.World, item.ProviderID}
				if provLabels[k] == nil {
					provLabels[k] = &LabelCounts{}
				}
				provLabels[k].add(rec.Label)
				mult := item.Multiplicity
				if mult <= 0 {
					mult = 1
				}
				provObs[k] += mult
				if rec.Label != nil && (*rec.Label == LabelAmbiguous || *rec.Label == LabelCannotAdjudicate) {
					b.AmbiguousOrCannot++
				}
			case LaneRecallUnit:
				ws.RecallUnits++
				state := "unlabelled"
				if rec.Label != nil && strings.TrimSpace(*rec.Label) != "" {
					state = *rec.Label
				}
				ws.RecallLabels[state]++
				for _, f := range rec.ExpectedFacts {
					expectedFactsSeen = true
					if f.State != ExpectedSupported {
						continue
					}
					expectedTotal[lf.World]++
					if f.Matched != nil && *f.Matched {
						expectedMatched[lf.World]++
					}
				}
			case LaneChallenge:
				if rec.Label != nil && strings.TrimSpace(*rec.Label) != "" {
					if v, err := strconv.ParseFloat(strings.TrimSpace(*rec.Label), 64); err == nil {
						ratings[lf.World] = append(ratings[lf.World], v)
					}
				}
				if rec.ActionTaken != nil && strings.TrimSpace(*rec.ActionTaken) != "" {
					actionsSeen = true
					actionCounts[lf.World][*rec.ActionTaken]++
				}
			}
		}
	}

	strataByKey := map[provKey]Stratum{}
	for _, st := range rs.Manifest.Strata {
		if st.Lane == LanePrecision {
			strataByKey[provKey{st.World, st.ProviderID}] = st
		}
	}

	score := Score{
		SchemaVersion:            ScoreSchemaVersion,
		ProtocolID:               rs.Manifest.ProtocolID,
		ProtocolDigestSHA256:     rs.Manifest.ProtocolDigestSHA256,
		SampleManifestDeclaredID: rs.Manifest.DigestSHA256,
		SampleManifestFileDigest: rs.ManifestFileDigestSHA256,
		AdjudicatorOverlapDigest: rs.Overlap.FileDigestSHA256,
		SelectionSeed:            rs.Manifest.SelectionSeed,
		TotalItems:               len(rs.Manifest.Items),
		TotalLabelled:            labelled,
	}

	var worldMacros []float64
	for _, name := range order {
		ws := worlds[name]
		keys := []provKey{}
		for k := range provLabels {
			if k.world == name {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].provider < keys[j].provider })
		var macro []float64
		microNum, microDen := 0, 0
		total := LabelCounts{}
		for _, k := range keys {
			c := *provLabels[k]
			ps := ProviderScore{
				World: name, ProviderID: k.provider, Labels: c,
				Population:   strataByKey[k].Population,
				Sampled:      c.Supported + c.Unsupported + c.Ambiguous + c.OutsideScope + c.CannotAdjudicate + c.Unlabelled + c.Unrecognised,
				Observations: provObs[k],
				Precision:    computed(c.Supported, c.resolved()),
			}
			if ps.Precision.Value != nil {
				macro = append(macro, *ps.Precision.Value)
				// Micro weights by the observations each stratum represents,
				// which is exactly the aggregation section 6.3 makes secondary.
				microNum += c.Supported * ps.Observations / max(1, ps.Sampled)
				microDen += c.resolved() * ps.Observations / max(1, ps.Sampled)
			}
			total.Supported += c.Supported
			total.Unsupported += c.Unsupported
			total.Ambiguous += c.Ambiguous
			total.OutsideScope += c.OutsideScope
			total.CannotAdjudicate += c.CannotAdjudicate
			total.Unlabelled += c.Unlabelled
			total.Unrecognised += c.Unrecognised
			ws.Providers = append(ws.Providers, ps)
		}
		ws.MacroPrecision = meanMetric(macro, len(keys))
		ws.MicroPrecision = computed(microNum, microDen)
		ws.UnsupportedRate = computed(total.Unsupported, total.resolved())
		if ws.MacroPrecision.Value != nil {
			worldMacros = append(worldMacros, *ws.MacroPrecision.Value)
			score.HeadlineWorlds = append(score.HeadlineWorlds, name)
		}

		switch {
		case expectedFactsSeen:
			ws.Recall = computed(expectedMatched[name], expectedTotal[name])
		case extendedLanes[name][LaneRecallUnit]:
			ws.Recall = absent(NoAdjudicableSample,
				"the recall container can hold an expected-fact set (section 5.2) but no unit has one recorded yet")
		default:
			ws.Recall = absent(NotCapturedByContainer,
				"primary recall is matched expected_supported facts over total expected_supported facts (section 5.2), and this container version carries one label per unit with no field for the frozen expected-fact set")
		}
		_, hasChallengeLane := extendedLanes[name][LaneChallenge]
		ws.ChallengeUsefulness = usefulness(ratings[name], actionsSeen, hasChallengeLane, extendedLanes[name][LaneChallenge], actionCounts[name])
		b := burden[name]
		b.EvidenceLookupsPer100 = per100(b.RequiringEvidenceLookup, b.ItemsReviewed)
		b.AmbiguousRate = computed(b.AmbiguousOrCannot, b.ItemsReviewed)
		switch {
		case correctionSeen:
			b.CorrectionsPer100 = per100(corrections[name], b.ItemsReviewed)
		case anyExtended(extendedLanes[name]):
			b.CorrectionsPer100 = absent(NoAdjudicableSample,
				"the container can record whether an item required correction (section 11) but no item does yet")
		default:
			b.CorrectionsPer100 = absent(NotCapturedByContainer,
				"section 11 counts items requiring correction, and this container version carries no field recording one")
		}
		b.MedianActiveSecondsPerItem = medianMetric(activeSeconds[name])
		ws.Burden = *b
		score.Worlds = append(score.Worlds, *ws)
	}

	score.HeadlineMacroPrecision = meanMetric(worldMacros, len(order))
	score.ContradictionPreservation = absent(NotSampled,
		"this reference set draws no contradiction-preservation lane, so section 8 has no adjudicable case here")
	score.ModelDelta = absent(NotSampled,
		"no model-lane items were drawn under this seed and no model provider was bound, so section 18 has no second population to compare against")
	score.SecondAdjudicator = agreement(rs, second)
	score.Uncomputable = uncomputable(score)

	digest, err := referenceSetDigest(rs)
	if err != nil {
		return Score{}, err
	}
	score.ReferenceSetDigestSHA256 = digest
	return seal(score)
}

func uncomputable(s Score) []string {
	var out []string
	for _, w := range s.Worlds {
		if w.Recall.Availability != Computed {
			out = append(out, fmt.Sprintf("%s recall: %s — %s", w.World, w.Recall.Availability, w.Recall.Reason))
		}
		if w.Burden.CorrectionsPer100.Availability != Computed {
			out = append(out, fmt.Sprintf("%s corrections per 100: %s — %s", w.World, w.Burden.CorrectionsPer100.Availability, w.Burden.CorrectionsPer100.Reason))
		}
		if w.ChallengeUsefulness.Actions.Availability != Computed {
			out = append(out, fmt.Sprintf("%s challenge action distribution: %s — %s", w.World, w.ChallengeUsefulness.Actions.Availability, w.ChallengeUsefulness.Actions.Reason))
		}
	}
	if s.ContradictionPreservation.Availability != Computed {
		out = append(out, "contradiction preservation: "+s.ContradictionPreservation.Availability+" — "+s.ContradictionPreservation.Reason)
	}
	if s.ModelDelta.Availability != Computed {
		out = append(out, "optional model delta: "+s.ModelDelta.Availability+" — "+s.ModelDelta.Reason)
	}
	if s.SecondAdjudicator.Availability != Computed {
		out = append(out, "second-adjudicator agreement: "+s.SecondAdjudicator.Availability+" — "+s.SecondAdjudicator.Reason)
	}
	sort.Strings(out)
	return out
}

func anyExtended(lanes map[string]bool) bool {
	for _, v := range lanes {
		if v {
			return true
		}
	}
	return false
}

func usefulness(ratings []float64, actionsSeen, laneExists, containerHolds bool, actions map[string]int) Usefulness {
	u := Usefulness{Distribution: map[string]int{}, Rated: len(ratings)}
	switch {
	case !laneExists:
		u.Availability = NotSampled
		u.Reason = "this world draws no challenge lane, so section 10 has no item to rate here"
	case len(ratings) == 0:
		u.Availability = NoAdjudicableSample
		u.Reason = "no challenge item in this world carries a rating"
	default:
		u.Availability = Computed
		for _, r := range ratings {
			u.Distribution[strconv.Itoa(int(r))]++
		}
		u.Mean = mean(ratings)
		u.Median = median(ratings)
	}
	switch {
	case !laneExists:
		u.Actions = absent(NotSampled, "this world draws no challenge lane")
	case actionsSeen:
		u.Actions = Metric{Availability: Computed, Denominator: len(actions)}
		u.ActionCounts = actions
	case containerHolds:
		u.Actions = absent(NoAdjudicableSample,
			"the challenge container can record the action a challenge caused (section 10) but no item does yet")
	default:
		u.Actions = absent(NotCapturedByContainer,
			"section 10 requires recording whether a challenge caused no action, an evidence lookup, code inspection, a correction, or an escalation, and this container version carries no field for it")
	}
	return u
}

func agreement(rs, second *ReferenceSet) Agreement {
	a := Agreement{OverlapItems: rs.Overlap.TotalOverlapItems}
	if second == nil {
		a.Availability = SecondAdjudicatorUnavailable
		a.Reason = "no second adjudicator's labels were supplied; section 13 records the absence rather than manufacturing agreement"
		a.RawAgreement = absent(SecondAdjudicatorUnavailable, a.Reason)
		return a
	}
	if second.Manifest.DigestSHA256 != rs.Manifest.DigestSHA256 {
		a.Availability = SecondAdjudicatorUnavailable
		a.Reason = "the second adjudicator's labels answer a different sample manifest"
		a.RawAgreement = absent(SecondAdjudicatorUnavailable, a.Reason)
		return a
	}
	overlap := map[string]bool{}
	for _, w := range rs.Overlap.Worlds {
		for _, k := range w.ItemKeys {
			overlap[k] = true
		}
	}
	first := labelIndex(rs)
	other := labelIndex(second)
	a.DisagreementsByPair = map[string]int{}
	for key := range overlap {
		l1, ok1 := first[key]
		l2, ok2 := other[key]
		if !ok1 || !ok2 {
			continue
		}
		a.Compared++
		if l1 == l2 {
			a.Agreed++
			continue
		}
		// Both original labels are preserved in the pair key. Section 13
		// forbids overwriting one adjudicator with the other.
		a.DisagreementsByPair[l1+" vs "+l2]++
	}
	a.RawAgreement = computed(a.Agreed, a.Compared)
	a.Availability = a.RawAgreement.Availability
	if a.Availability != Computed {
		a.Reason = "the overlap subset carries no pair labelled by both adjudicators"
	}
	return a
}

func labelIndex(rs *ReferenceSet) map[string]string {
	out := map[string]string{}
	for _, lf := range rs.Labels {
		for _, rec := range lf.Labels {
			if rec.Label != nil && strings.TrimSpace(*rec.Label) != "" {
				out[rec.ItemKey] = *rec.Label
			}
		}
	}
	return out
}

// referenceSetDigest is section 17: content-addressed over the protocol
// digest, the manifest's DECLARED identity, the label file digests, the world
// binding digests, and the overlap manifest.
func referenceSetDigest(rs *ReferenceSet) (string, error) {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			fmt.Fprintf(h, "%d:%s", len(p), p)
		}
	}
	write("sensei.phase10_reference_set_identity.v2")
	write(rs.Manifest.ProtocolDigestSHA256, rs.Manifest.DigestSHA256)
	names := make([]string, 0, len(rs.Labels))
	byName := map[string]string{}
	for _, lf := range rs.Labels {
		names = append(names, lf.Path)
		byName[lf.Path] = lf.FileDigestSHA256
	}
	sort.Strings(names)
	for _, n := range names {
		write(n, byName[n])
	}
	bindings := make([]string, 0, len(rs.Manifest.Worlds))
	for _, w := range rs.Manifest.Worlds {
		bindings = append(bindings, w.World+"="+w.WorldBindingDigestSHA256)
	}
	sort.Strings(bindings)
	write(bindings...)
	write(rs.Overlap.FileDigestSHA256)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func seal(s Score) (Score, error) {
	s.DigestSHA256 = ""
	payload, err := json.Marshal(s)
	if err != nil {
		return Score{}, err
	}
	sum := sha256.Sum256(payload)
	s.DigestSHA256 = hex.EncodeToString(sum[:])
	return s, nil
}

func meanMetric(xs []float64, strata int) Metric {
	if len(xs) == 0 {
		return Metric{Availability: NoAdjudicableSample, Denominator: strata,
			Reason: "no stratum in this scope has an adjudicable precision sample"}
	}
	v := *mean(xs)
	return Metric{Availability: Computed, Numerator: len(xs), Denominator: strata, Value: &v}
}

func per100(num, den int) Metric {
	if den == 0 {
		return Metric{Availability: NoAdjudicableSample, Reason: "no items were reviewed"}
	}
	v := float64(num) * 100 / float64(den)
	return Metric{Availability: Computed, Numerator: num, Denominator: den, Value: &v}
}

func medianMetric(xs []float64) Metric {
	if len(xs) == 0 {
		return absent(NotCapturedByContainer,
			"section 11's active review time is optional and this container records none; its absence must not invalidate the rest of the burden measures")
	}
	v := *median(xs)
	return Metric{Availability: Computed, Numerator: len(xs), Denominator: len(xs), Value: &v}
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

func median(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return &s[mid]
	}
	v := (s[mid-1] + s[mid]) / 2
	return &v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func readJSONWithDigest(path string, into any) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
