// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The rule this file enforces:
//
//	Code created to test a hypothesis must not become governing architecture
//	merely because it exists. It remains mutable under its established
//	architectural envelope until an explicit evidence-backed adoption promotes
//	it.
//
// and its mirror:
//
//	Promotion to architecture is an epistemic event, not a side effect of
//	implementation.
//
// Without them the loop closes on itself. An agent guesses B, implements B,
// extraction observes that B exists, B is recorded as architectural intent, and
// the agent can no longer replace its own guess. Architecture by sediment: the
// system defends experiments before anyone knows whether they were any good,
// and every guess becomes permanent by the act of having been made.
//
// This is the same principle the design document already applies to prose --
// an uncertain claim must not masquerade as established knowledge -- extended
// to code.

// SedimentKind is what a finding says went wrong.
type SedimentKind string

const (
	// KindSediment: a path exists to test an OPEN hypothesis, and canonical
	// architecture already cites it as established. The guess is being defended
	// before the question it was meant to answer has been settled.
	KindSediment SedimentKind = "ARCHITECTURE_BY_SEDIMENT"

	// KindOrphaned: the hypothesis was refuted, and the code written to test it
	// is still declared as its scope. Not automatically wrong -- the code may
	// have been kept deliberately -- but the reason it existed is gone, and
	// nothing else will notice that.
	KindOrphaned SedimentKind = "ORPHANED_EXPERIMENT"
)

// SedimentFinding is one experimental path with a governance problem.
type SedimentFinding struct {
	Kind       SedimentKind `json:"kind"`
	Hypothesis string       `json:"hypothesis"`
	State      State        `json:"hypothesis_state"`
	Path       string       `json:"path"`
	// CitedBy are the canonical entries that already treat the path as
	// established. Empty for KindOrphaned.
	CitedBy []string `json:"cited_by,omitempty"`
	Detail  string   `json:"detail"`
}

// CheckSediment reports experimental scope that established architecture has
// begun to defend, and experimental scope whose hypothesis is already refuted.
//
// established maps a repository path to the canonical entries citing it -- an
// invariant that protects it, a high-risk listing, and so on. The caller reads
// the corpus; this stays pure so the rule can be tested without a repository.
//
// Matching is by prefix in BOTH directions. A canonical entry naming a
// directory covers an experimental file inside it, and an experimental scope
// naming a directory covers a canonical entry pointing at one file within.
// Exact-match-only would be trivially defeated by the ordinary way this corpus
// is written, since it anchors directories as often as files.
//
// A settled hypothesis produces no sediment finding: once a belief is SUPPORTED
// or REFUTED the question is no longer open, and adoption -- which this slice
// does not implement -- is what should follow.
func CheckSediment(hs []Hypothesis, obs []Observation, established map[string][]string, now time.Time) []SedimentFinding {
	var out []SedimentFinding
	for _, h := range hs {
		state := StateOf(h, obs, now)
		for _, raw := range h.ExperimentalScope {
			path := normalizePath(raw)
			if path == "" {
				continue
			}
			if state == StateRefuted {
				out = append(out, SedimentFinding{
					Kind: KindOrphaned, Hypothesis: h.ID, State: state, Path: raw,
					Detail: "the hypothesis this code was written to test was refuted; keeping it may be deliberate, but the reason it exists is gone",
				})
				continue
			}
			if state == StateSupported {
				// Settled the other way. Adoption is the event that would make
				// this code architecture, and it is not this slice's to invent.
				continue
			}
			if citedBy := citations(established, path); len(citedBy) > 0 {
				out = append(out, SedimentFinding{
					Kind: KindSediment, Hypothesis: h.ID, State: state, Path: raw, CitedBy: citedBy,
					Detail: fmt.Sprintf("canonical architecture already defends this path while %s is still open; promotion to architecture is an epistemic event, not a side effect of implementation", h.ID),
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hypothesis != out[j].Hypothesis {
			return out[i].Hypothesis < out[j].Hypothesis
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func citations(established map[string][]string, path string) []string {
	var hits []string
	seen := map[string]bool{}
	for anchor, ids := range established {
		a := normalizePath(anchor)
		if a == "" {
			continue
		}
		if !pathOverlaps(a, path) {
			continue
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				hits = append(hits, id)
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// pathOverlaps reports whether two repo-relative paths refer to overlapping
// material, in either direction.
func pathOverlaps(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasPrefix(b, a+"/") || strings.HasPrefix(a, b+"/")
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	return strings.TrimSuffix(p, "/")
}
