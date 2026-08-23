// SPDX-License-Identifier: AGPL-3.0-only

package phase10score

import (
	"fmt"
	"sort"
	"strings"
)

// Render writes the protocol section 20 report.
//
// Every denominator is printed beside its rate, and every metric this
// reference-set version cannot produce is named with its reason. Section 20
// exists to prevent flattering aggregation, and the cheapest flattery
// available to a report is leaving a metric out.
func Render(s Score) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Phase 10.8 score — %s\n\n", s.ProtocolID)
	fmt.Fprintf(&b, "%d of %d sampled items carry a label.\n\n", s.TotalLabelled, s.TotalItems)
	if s.TotalLabelled == 0 {
		fmt.Fprintln(&b, "> **No human label exists yet.** Every number below is therefore an empty")
		fmt.Fprintln(&b, "> denominator reported as absent, never as zero. This is the instrument")
		fmt.Fprintln(&b, "> answering honestly about a reference set nobody has adjudicated.")
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Identities")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- protocol digest: `%s`\n", s.ProtocolDigestSHA256)
	fmt.Fprintf(&b, "- sample manifest, declared identity: `%s`\n", s.SampleManifestDeclaredID)
	fmt.Fprintf(&b, "- sample manifest, file digest: `%s`\n", s.SampleManifestFileDigest)
	fmt.Fprintf(&b, "- adjudicator overlap: `%s`\n", or(s.AdjudicatorOverlapDigest, "(absent)"))
	fmt.Fprintf(&b, "- reference-set digest (section 17): `%s`\n", s.ReferenceSetDigestSHA256)
	fmt.Fprintf(&b, "- selection seed: `%s`\n", s.SelectionSeed)
	fmt.Fprintf(&b, "- score digest: `%s`\n", s.DigestSHA256)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The manifest's declared identity and its file digest are different facts. This report quotes both so neither can stand in for the other.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Per-world results")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| world | binding | macro precision | micro precision | unsupported rate | recall |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	for _, w := range s.Worlds {
		fmt.Fprintf(&b, "| %s | %s @ %s | %s | %s | %s | %s |\n",
			w.World, or(w.RepositoryDomain, "—"), shortRev(w.Revision),
			renderMetric(w.MacroPrecision), renderMetric(w.MicroPrecision),
			renderMetric(w.UnsupportedRate), renderMetric(w.Recall))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Headline macro precision, mean over %s: %s\n\n",
		or(strings.Join(s.HeadlineWorlds, ", "), "no world"), renderMetric(s.HeadlineMacroPrecision))
	fmt.Fprintln(&b, "The headline is a summary of the provider table below it, never a substitute for it: section 6.3 makes the macro average across provider strata the primary figure precisely so a high-volume provider cannot mathematically erase a weak thin one.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Provider strata")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| world | provider | population | sampled | supported | unsupported | ambiguous | outside_scope | cannot_adjudicate | unlabelled | precision |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|---|---|---|")
	for _, w := range s.Worlds {
		for _, p := range w.Providers {
			c := p.Labels
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %s |\n",
				w.World, p.ProviderID, p.Population, p.Sampled,
				c.Supported, c.Unsupported, c.Ambiguous, c.OutsideScope, c.CannotAdjudicate, c.Unlabelled,
				renderMetric(p.Precision))
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Recall lanes")
	fmt.Fprintln(&b)
	for _, w := range s.Worlds {
		fmt.Fprintf(&b, "- **%s** — %d recall units sampled; expected-state counts: %s; primary recall: %s\n",
			w.World, w.RecallUnits, renderCounts(w.RecallLabels), renderMetric(w.Recall))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Challenge usefulness (section 10)")
	fmt.Fprintln(&b)
	for _, w := range s.Worlds {
		u := w.ChallengeUsefulness
		fmt.Fprintf(&b, "- **%s** — rated %d; distribution %s; mean %s; median %s; action distribution: %s\n",
			w.World, u.Rated, renderCounts(u.Distribution), renderFloat(u.Mean), renderFloat(u.Median), u.Actions.Availability)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Operator burden (section 11)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| world | reviewed | evidence lookups /100 | ambiguous or cannot-adjudicate | corrections /100 | median active seconds |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	for _, w := range s.Worlds {
		bd := w.Burden
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s |\n",
			w.World, bd.ItemsReviewed, renderMetric(bd.EvidenceLookupsPer100), renderMetric(bd.AmbiguousRate),
			renderMetric(bd.CorrectionsPer100), renderMetric(bd.MedianActiveSecondsPerItem))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Second adjudicator (section 13)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- status: `%s`\n", s.SecondAdjudicator.Availability)
	if s.SecondAdjudicator.Reason != "" {
		fmt.Fprintf(&b, "- reason: %s\n", s.SecondAdjudicator.Reason)
	}
	fmt.Fprintf(&b, "- overlap items: %d; compared: %d; agreed: %d; raw agreement: %s\n",
		s.SecondAdjudicator.OverlapItems, s.SecondAdjudicator.Compared, s.SecondAdjudicator.Agreed,
		renderMetric(s.SecondAdjudicator.RawAgreement))
	if len(s.SecondAdjudicator.DisagreementsByPair) > 0 {
		fmt.Fprintln(&b, "- disagreements by label pair, both originals preserved:")
		for _, k := range sortedKeys(s.SecondAdjudicator.DisagreementsByPair) {
			fmt.Fprintf(&b, "  - `%s`: %d\n", k, s.SecondAdjudicator.DisagreementsByPair[k])
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Other required metrics")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- contradiction preservation (section 8): %s\n", renderMetric(s.ContradictionPreservation))
	fmt.Fprintf(&b, "- optional-model delta (section 18): %s\n", renderMetric(s.ModelDelta))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Metrics this reference-set version cannot produce")
	fmt.Fprintln(&b)
	if len(s.Uncomputable) == 0 {
		fmt.Fprintln(&b, "none")
	}
	for _, u := range s.Uncomputable {
		fmt.Fprintf(&b, "- %s\n", u)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "No single aggregate score in this report is sufficient evidence for #131 completion, and section 20 says so outright.")
	return b.String()
}

func renderMetric(m Metric) string {
	if m.Value == nil {
		return fmt.Sprintf("`%s`", m.Availability)
	}
	return fmt.Sprintf("%.3f (%d/%d)", *m.Value, m.Numerator, m.Denominator)
}

func renderFloat(v *float64) string {
	if v == nil {
		return "absent"
	}
	return fmt.Sprintf("%.2f", *v)
}

func renderCounts(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func or(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func shortRev(r string) string {
	if len(r) > 12 {
		return r[:12]
	}
	return or(r, "—")
}
