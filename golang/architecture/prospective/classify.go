// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import (
	"fmt"
	"sort"
)

// ClassificationRuleID is the frozen identity of the stratum rule below.
//
// It is recorded in the manifest so a report states which rule cut its strata.
// Changing the rule changes this constant, which invalidates downstream scores
// rather than quietly re-cutting a frozen sample.
const ClassificationRuleID = "sensei.prospective.stratum_rule.v1"

// ClassificationRuleDescription is the stated rule, in the words a reader
// needs to check the code against.
const ClassificationRuleDescription = "" +
	"Applied to the change's pre-authoring state. Each touched path is one of: " +
	"new (did not exist at the base revision), anchored (existed and the frozen " +
	"anchor index resolves usable graph facts), or unanchored (existed and it does not). " +
	"A change with at least one anchored path AND at least one new-or-unanchored path is D. " +
	"Otherwise: all paths new is A; no anchored and at least one unanchored is B; " +
	"all paths anchored is C."

// Classification is one change's stratum and the raw evidence that put it
// there.
//
// The path partition is carried, not just the verdict. Classification consults
// the system under test, so a reader has to be able to see WHY a change landed
// in A rather than C without re-running a graph that may have moved since.
type Classification struct {
	ChangeID string `json:"change_id"`
	Change   Change `json:"change"`
	Stratum  string `json:"stratum"`

	NewPaths        []string `json:"new_paths,omitempty"`
	AnchoredPaths   []string `json:"anchored_paths,omitempty"`
	UnanchoredPaths []string `json:"unanchored_paths,omitempty"`
}

// Classify assigns exactly one stratum to one change.
//
// It is a total function over changes that touch at least one path: every
// input either returns a stratum from the closed vocabulary or an error. There
// is no default branch and no "unknown" stratum, because an unknown stratum is
// a change that would silently leave every denominator while still appearing
// in the population.
func Classify(c Change, idx AnchorIndex) (Classification, error) {
	c = NormalizeChange(c)
	if len(c.Paths) == 0 {
		return Classification{}, fmt.Errorf("change %s touches no path, so it has no seam to classify", c.ID)
	}
	out := Classification{ChangeID: c.ID, Change: c}
	for _, p := range c.Paths {
		switch {
		case !p.ExistedBefore:
			out.NewPaths = append(out.NewPaths, p.Path)
		case idx.Anchored(p.Path):
			out.AnchoredPaths = append(out.AnchoredPaths, p.Path)
		default:
			out.UnanchoredPaths = append(out.UnanchoredPaths, p.Path)
		}
	}
	sort.Strings(out.NewPaths)
	sort.Strings(out.AnchoredPaths)
	sort.Strings(out.UnanchoredPaths)

	hasNew := len(out.NewPaths) > 0
	hasUnanchored := len(out.UnanchoredPaths) > 0
	hasAnchored := len(out.AnchoredPaths) > 0

	switch {
	case hasAnchored && (hasNew || hasUnanchored):
		out.Stratum = StratumD
	case hasNew && !hasUnanchored:
		out.Stratum = StratumA
	case hasNew && hasUnanchored:
		// A change that creates one file and edits another unanchored one is
		// not "mixed" in the protocol's sense: mixed means it crosses the
		// anchored boundary. With nothing anchored, the new seam is the
		// stratum-relevant fact, and calling this D would move a pure new-seam
		// case out of the population the experiment exists to measure.
		out.Stratum = StratumA
	case hasUnanchored:
		out.Stratum = StratumB
	default:
		out.Stratum = StratumC
	}
	return out, nil
}

// BuildInventory classifies the whole population and content-addresses each
// stratum.
//
// Changes are classified in the order supplied and re-sorted by change ID, so
// two enumerations of the same world produce byte-identical stratum digests.
// A change that cannot be classified becomes a counted exclusion rather than a
// dropped row: the difference between "excluded for this reason" and "never
// appeared" is exactly the difference the protocol's exclusion counts exist to
// preserve.
func BuildInventory(wb WorldBinding, idx AnchorIndex, changes []Change, priorExclusions []Exclusion) (Inventory, error) {
	inv := Inventory{
		World:                   wb,
		AnchorIndexDigestSHA256: idx.DigestSHA256,
		ClassificationRuleID:    ClassificationRuleID,
		Exclusions:              append([]Exclusion(nil), priorExclusions...),
	}
	if idx.DigestSHA256 == "" {
		return Inventory{}, fmt.Errorf("anchor index is not content-addressed: normalize it before classifying, or the sample cannot state what evidence cut its strata")
	}
	for _, c := range changes {
		cl, err := Classify(c, idx)
		if err != nil {
			inv.Exclusions = append(inv.Exclusions, Exclusion{ChangeID: c.ID, Reason: ExcludedNoPaths, Detail: err.Error()})
			continue
		}
		inv.Classified = append(inv.Classified, cl)
	}
	sort.SliceStable(inv.Classified, func(i, j int) bool { return inv.Classified[i].ChangeID < inv.Classified[j].ChangeID })
	sort.SliceStable(inv.Exclusions, func(i, j int) bool {
		if inv.Exclusions[i].Reason != inv.Exclusions[j].Reason {
			return inv.Exclusions[i].Reason < inv.Exclusions[j].Reason
		}
		return inv.Exclusions[i].ChangeID < inv.Exclusions[j].ChangeID
	})

	inv.StratumDigests = map[string]string{}
	for _, s := range Strata {
		pop := inv.InStratum(s)
		d, err := DigestOf(struct {
			Stratum    string           `json:"stratum"`
			Rule       string           `json:"classification_rule_id"`
			AnchorIdx  string           `json:"anchor_index_digest_sha256"`
			World      string           `json:"world_binding_digest_sha256"`
			Population []Classification `json:"population"`
		}{s, ClassificationRuleID, idx.DigestSHA256, wb.DigestSHA256, pop})
		if err != nil {
			return Inventory{}, err
		}
		inv.StratumDigests[s] = d
	}
	return inv, nil
}
