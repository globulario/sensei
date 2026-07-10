// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"fmt"
	"io"
	"sort"
)

// IntentReport is the dry-run scoring sheet for an intent-mining grounding pass.
// Like the cold-bootstrap report it WRITES NOTHING and promotes nothing — it
// surfaces grounded intents and divergence findings for the human gate.
type IntentReport struct {
	Repo       string
	Total      int
	Groundings []IntentGrounding
}

// classOrder fixes the display order: findings (the valuable divergences) first.
var classOrder = []IntentOutputClass{
	StaleIntent, AmbiguousOwner, MissingInvariant, HiddenIntent, UngroundedClaim, StrongIntent,
}

// RenderIntentReport prints the grounding result grouped by output class,
// findings first. Deterministic and bounded.
func RenderIntentReport(w io.Writer, r IntentReport) {
	fmt.Fprintf(w, "\nAWG intent-mine — GROUNDING report (dry-run, nothing written)\n")
	fmt.Fprintf(w, "============================================================\n")
	if r.Repo != "" {
		fmt.Fprintf(w, "repo:        %s\n", r.Repo)
	}
	fmt.Fprintf(w, "candidates:  %d\n\n", r.Total)

	counts := map[IntentOutputClass]int{}
	auto, human := 0, 0
	for _, g := range r.Groundings {
		counts[g.OutputClass]++
		if g.Route == RouteAutoMap {
			auto++
		} else {
			human++
		}
	}
	fmt.Fprintf(w, "Output classes\n")
	for _, c := range classOrder {
		tag := "  "
		if c.IsFinding() {
			tag = "! " // findings flagged
		}
		fmt.Fprintf(w, "%s%-18s %d\n", tag, c, counts[c])
	}
	fmt.Fprintf(w, "\nRouting (>80%% rule)\n")
	fmt.Fprintf(w, "  auto-map (existing):    %d   (advisory, audited, reversible — attaches to existing only)\n", auto)
	fmt.Fprintf(w, "  human required:         %d   (findings, <0.80, or NEW intent)\n", human)

	by := func(class IntentOutputClass) []IntentGrounding {
		var out []IntentGrounding
		for _, g := range r.Groundings {
			if g.OutputClass == class {
				out = append(out, g)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Certainty > out[j].Certainty })
		return out
	}

	fmt.Fprintf(w, "\nGroundings (findings first)\n")
	fmt.Fprintf(w, "%-16s %-16s %-18s %-18s %-6s %-8s %s\n",
		"class", "intent_id", "stated", "grounding", "cert", "route", "decided_by")
	fmt.Fprintf(w, "%s\n", repeat("-", 104))
	for _, class := range classOrder {
		for _, g := range by(class) {
			flag := ""
			if g.SymbolMismatch {
				flag = "!"
			}
			fmt.Fprintf(w, "%-16s %-16s %-18s %-18s %-6.2f %-8s %s\n",
				string(g.OutputClass), truncate(g.IntentID, 16), g.StatedTier.String(),
				g.GroundingTier.String()+flag, g.Certainty, string(g.Route), g.DecidedBy)
		}
	}

	fmt.Fprintf(w, "\nTrust tiers: executable_truth > landed_behavior > maintainer_intent > docs_only > weak_hint")
	fmt.Fprintf(w, "  (grounding bar = landed_behavior; ! = claimed symbol absent at a cited anchor)\n")
	fmt.Fprintf(w, "The LLM proposes intent; AWG grounds it; a human approves meaning. Docs alone are not authority.\n")
}

// RenderFinderHints prints the intent → coldsource hints: divergence sites where
// the next scar is likely. Nothing is written; this is advisory output.
func RenderFinderHints(w io.Writer, hints []FinderHint) {
	if len(hints) == 0 {
		return
	}
	fmt.Fprintf(w, "\nFinder hints for coldsource (where the next scar is likely)\n")
	fmt.Fprintf(w, "%-16s %-44s %s\n", "class", "file", "intent")
	fmt.Fprintf(w, "%s\n", repeat("-", 90))
	for _, h := range hints {
		fmt.Fprintf(w, "%-16s %-44s %s\n", string(h.Class), truncate(h.File, 44), truncate(h.IntentID, 26))
	}
	fmt.Fprintf(w, "(%d divergence site(s) — coldsource may weight these themes; advisory only)\n", len(hints))
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
