// SPDX-License-Identifier: AGPL-3.0-only

package prospectivescore

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// Render writes the protocol section 12 report.
//
// It states numbers and identities and stops there. No sentence in this
// function interprets a result, because section 13's readings are the human's
// to make from the table — and a generated paragraph saying which of them
// applies would be the instrument grading its own output.
func Render(s Score, m prospective.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Prospective authoring-recall result — %s\n\n", s.ProtocolID)
	fmt.Fprintln(&b, "Per-stratum first. A and B are never merged, and the macro average is secondary to the table above it.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Identities")
	fmt.Fprintln(&b)
	identity(&b, "world revision", s.WorldRevision)
	identity(&b, "graph digest", s.GraphDigestSHA256)
	identity(&b, "sample manifest", s.SampleManifestDigestSHA256)
	identity(&b, "blind corpus", s.BlindCorpusDigestSHA256)
	identity(&b, "frozen labels", s.LabelsDigestSHA256)
	identity(&b, "retrieval run", s.RunDigestSHA256)
	identity(&b, "score", s.DigestSHA256)
	identity(&b, "retrieval surface", s.RetrievalSurfaceID)
	identity(&b, "adjudicator", s.Adjudicator)
	identity(&b, "second adjudicator", secondAdjudicator(s))
	identity(&b, "labels frozen at", s.LabelsFrozenAt)
	identity(&b, "run executed at", s.RunExecutedAt)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Per-stratum results")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| stratum | changes | recall | primary nuisance | unresolved surfaced | conservative nuisance |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	for _, st := range s.Strata {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s |\n",
			st.Stratum, st.ChangeCount,
			renderRate(st.Recall), renderRate(st.PrimaryNuisance),
			renderRate(st.UnresolvedSurfacedRate), renderRate(st.ConservativeNuisance))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "`absent` is a metric with an empty denominator. It is not zero, and it is not averaged into anything.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Adjudicated label distribution, per stratum")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| stratum | applicable | not_applicable | ambiguous | outside_scope | cannot_adjudicate | unlabelled |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|---|")
	for _, st := range s.Strata {
		c := st.AdjudicatedLabels
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d |\n",
			st.Stratum, c.Applicable, c.NotApplicable, c.Ambiguous, c.OutsideScope, c.CannotAdjudicate, c.Unlabelled)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Surfaced label distribution, per stratum")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| stratum | surfaced | applicable | not_applicable | ambiguous | outside_scope | cannot_adjudicate | unlabelled |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|")
	for _, st := range s.Strata {
		c := st.SurfacedLabels
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d |\n",
			st.Stratum, st.SurfacedTotal, c.Applicable, c.NotApplicable, c.Ambiguous, c.OutsideScope, c.CannotAdjudicate, c.Unlabelled)
	}
	fmt.Fprintf(&b, "\nSurfaced but outside the eligible corpus, and therefore unscorable in either direction: %d\n", s.SurfacedOutsideCorpusTotal)
	if len(s.MatchRuleCounts) > 0 {
		fmt.Fprintf(&b, "Hits matched by qualified id: %d; by unqualified id: %d.\n",
			s.MatchRuleCounts[MatchExact], s.MatchRuleCounts[MatchIDOnly])
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Retrieval status distribution (section 7.3)")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, "| stratum |")
	for _, st := range RetrievalStatuses {
		fmt.Fprintf(&b, " %s |", st)
	}
	fmt.Fprint(&b, "\n|---|")
	for range RetrievalStatuses {
		fmt.Fprint(&b, "---|")
	}
	fmt.Fprintln(&b)
	for _, st := range s.Strata {
		fmt.Fprintf(&b, "| %s |", st.Stratum)
		for _, name := range RetrievalStatuses {
			fmt.Fprintf(&b, " %d |", st.RetrievalStatusCounts[name])
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Context classes available to retrieval (section 7.4)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Descriptive. It exists so a low stratum-A score can be attributed to missing context, or shown not to be.")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, "| stratum |")
	for _, c := range ContextClasses {
		fmt.Fprintf(&b, " %s |", c)
	}
	fmt.Fprint(&b, "\n|---|")
	for range ContextClasses {
		fmt.Fprint(&b, "---|")
	}
	fmt.Fprintln(&b)
	for _, st := range s.Strata {
		fmt.Fprintf(&b, "| %s |", st.Stratum)
		for _, c := range ContextClasses {
			fmt.Fprintf(&b, " %d |", st.ContextAvailability[c])
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Population and sampling (section 8.1)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| stratum | inventory digest | population | target | selected | status |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	for _, st := range m.Strata {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s |\n",
			st.Stratum, short(st.InventoryDigestSHA256), st.Population, st.Target, st.Selected, st.Status)
	}
	fmt.Fprintln(&b)
	if len(m.Exclusions) > 0 {
		fmt.Fprintln(&b, "Excluded from the candidate population, with reasons:")
		fmt.Fprintln(&b)
		for _, e := range m.Exclusions {
			fmt.Fprintf(&b, "- `%s` — %d\n", e.Reason, e.Count)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Macro summary (secondary)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- recall, mean over %s: %s\n", strata(s.Macro.RecallStrata), renderFloat(s.Macro.RecallMacroAverage))
	fmt.Fprintf(&b, "- primary nuisance, mean over %s: %s\n", strata(s.Macro.PrimaryNuisanceStrata), renderFloat(s.Macro.PrimaryNuisanceMacro))
	fmt.Fprintf(&b, "- conservative nuisance, mean over %s: %s\n", strata(s.Macro.ConservativeNuisanceStrata), renderFloat(s.Macro.ConservativeNuisanceMacro))
	if len(s.Macro.StrataWithoutRecall) > 0 {
		fmt.Fprintf(&b, "- strata with no applicable labels, and therefore no recall: %s\n", strata(s.Macro.StrataWithoutRecall))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Applicable items missed, per stratum")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Complete, not a selection. Section 12 freezes examples with the labels because choosing illustrative misses after seeing the scores is editing the answer key with extra steps; emitting all of them removes the choice.")
	fmt.Fprintln(&b)
	for _, st := range s.Strata {
		fmt.Fprintf(&b, "### %s — %d missed\n\n", st.Stratum, len(st.Misses))
		if len(st.Misses) == 0 {
			fmt.Fprintln(&b, "none")
			fmt.Fprintln(&b)
			continue
		}
		for _, miss := range st.Misses {
			fmt.Fprintf(&b, "- `%s` on `%s` (retrieval status: %s)\n", miss.CorpusItemID, miss.ItemKey, miss.RetrievalStatus)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Per-change detail")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| change | stratum | status | applicable | surfaced applicable | surfaced total |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	for _, st := range s.Strata {
		for _, c := range st.PerChange {
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d |\n",
				c.ItemKey, c.Stratum, c.RetrievalStatus, c.ApplicableTotal, c.ApplicableHit, c.SurfacedTotal)
		}
	}
	return b.String()
}

func secondAdjudicator(s Score) string {
	if strings.TrimSpace(s.SecondAdjudicator) != "" {
		return s.SecondAdjudicator + " (" + s.SecondAdjudicatorStatus + ")"
	}
	return s.SecondAdjudicatorStatus
}

func identity(b *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		value = "(absent)"
	}
	fmt.Fprintf(b, "- %s: `%s`\n", name, value)
}

func strata(names []string) string {
	if len(names) == 0 {
		return "no stratum"
	}
	return strings.Join(names, ", ")
}

// renderRate always shows the ratio beside the value, so a rate computed from
// three items cannot be read as one computed from three hundred.
func renderRate(r Rate) string {
	if r.Value == nil {
		return fmt.Sprintf("absent (0/%d)", r.Denominator)
	}
	return fmt.Sprintf("%.3f (%d/%d)", *r.Value, r.Numerator, r.Denominator)
}

func renderFloat(v *float64) string {
	if v == nil {
		return "absent"
	}
	return fmt.Sprintf("%.3f", *v)
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
