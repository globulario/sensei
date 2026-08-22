// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import (
	"fmt"
	"sort"
	"strings"
)

// Change is one candidate proposed change, described from its PRE-AUTHORING
// state.
//
// Every field here answers "what did the world look like before this was
// written", because that is the only moment at which prospective recall is a
// meaningful claim. A change that cannot state what the graph knew when it was
// authored is excluded rather than scored (protocol section 4).
type Change struct {
	ID string `json:"change_id"`
	// Revision is the change's own identity in the world's history.
	Revision string `json:"revision"`
	// BaseRevision is the state the change was authored against.
	BaseRevision string `json:"base_revision"`
	// BaseTreeDigestSHA256 pins the pre-authoring tree.
	BaseTreeDigestSHA256 string `json:"base_tree_digest_sha256"`
	// Paths are the files the change touches, with whether each existed
	// beforehand. Sorted; see NormalizeChange.
	Paths []PathChange `json:"paths"`
	// ContentDigestSHA256 binds the exact diff or new-file contents the
	// adjudicator will be shown.
	ContentDigestSHA256 string `json:"content_digest_sha256"`
}

// PathChange is one file the change touches.
type PathChange struct {
	Path string `json:"path"`
	// ExistedBefore is the stratum-relevant fact, kept as a separate field
	// rather than derived from Status at classification time. A rename that
	// creates a path the graph has never seen is a new seam even though git
	// calls it a rename, and deriving existence from a status letter is where
	// that distinction gets lost.
	ExistedBefore bool `json:"existed_before"`
	// Status is git's own account, carried for the adjudicator's benefit.
	Status string `json:"status,omitempty"`
}

// Exclusion reasons. Each is a stated rule written before the inventory is
// built (protocol section 8.1), and each exclusion is counted so a shrinking
// population is visible rather than silent.
const (
	// ExcludedNoSingleBase covers merge commits and root commits: neither has
	// a single pre-authoring state, so neither can say what the graph knew
	// when it was authored.
	ExcludedNoSingleBase = "no_single_base_revision"
	// ExcludedNoPaths covers a change that touches no file.
	ExcludedNoPaths = "no_paths_touched"
	// ExcludedUnreconstructable covers a change whose contents could not be
	// reconstructed at the pinned revision.
	ExcludedUnreconstructable = "contents_not_reconstructable"
)

// Exclusion is one change that could not enter the population, and why.
type Exclusion struct {
	ChangeID string `json:"change_id"`
	Reason   string `json:"reason"`
	// Detail is free text for the operator. It is not a vocabulary and nothing
	// scores against it.
	Detail string `json:"detail,omitempty"`
}

// ExclusionCount is the reported shape: a reason and how many changes it took
// out of the population.
type ExclusionCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// AnchorIndex is the set of paths for which the graph resolves usable anchors,
// frozen with the provenance of the production command that produced it.
//
// It is an INPUT to this package rather than something the package fetches.
// Classification consults the system under test, so the evidence it consulted
// has to be frozen and publishable alongside the sample — otherwise a later
// production change would silently re-stratify a frozen sample, and the score
// it produced would move without anybody editing the sample.
type AnchorIndex struct {
	RepositoryDomain string `json:"repository_domain"`
	// GraphDigestSHA256 is the live store digest the index was read from.
	GraphDigestSHA256 string `json:"graph_digest_sha256"`
	// ProducedBy is the exact production invocation, recorded verbatim so the
	// index can be reproduced or disputed.
	ProducedBy string `json:"produced_by"`
	// AnchoredPaths is sorted and deduplicated by Normalize.
	AnchoredPaths []string `json:"anchored_paths"`
	DigestSHA256  string   `json:"digest_sha256"`

	set map[string]bool
}

// NormalizeAnchorIndex sorts, deduplicates and content-addresses the index.
func NormalizeAnchorIndex(idx AnchorIndex) (AnchorIndex, error) {
	if strings.TrimSpace(idx.ProducedBy) == "" {
		return AnchorIndex{}, fmt.Errorf("anchor index names no producing command: an index nobody can reproduce cannot be defended as the classification evidence")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(idx.AnchoredPaths))
	for _, p := range idx.AnchoredPaths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	idx.AnchoredPaths = out
	idx.set = seen
	idx.DigestSHA256 = ""
	d, err := DigestOf(idx)
	if err != nil {
		return AnchorIndex{}, err
	}
	idx.DigestSHA256 = d
	return idx, nil
}

// Anchored reports whether the graph holds usable facts for a path.
func (idx AnchorIndex) Anchored(path string) bool {
	if idx.set != nil {
		return idx.set[path]
	}
	for _, p := range idx.AnchoredPaths {
		if p == path {
			return true
		}
	}
	return false
}

// NormalizeChange sorts a change's paths so two enumerations of the same
// change produce the same bytes, and therefore the same digest.
func NormalizeChange(c Change) Change {
	paths := append([]PathChange(nil), c.Paths...)
	sort.SliceStable(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
	c.Paths = paths
	return c
}

// Inventory is the complete candidate population at the pinned revision,
// already classified, together with everything that could not enter it.
type Inventory struct {
	World                   WorldBinding `json:"world"`
	AnchorIndexDigestSHA256 string       `json:"anchor_index_digest_sha256"`
	// ClassificationRuleID names the frozen rule that assigned the strata.
	ClassificationRuleID string `json:"classification_rule_id"`

	// Classified holds every change that entered the population, in stable
	// order.
	Classified []Classification `json:"classified"`
	Exclusions []Exclusion      `json:"exclusions"`

	// StratumDigests content-addresses each stratum's population separately
	// (protocol section 8.1). Per stratum rather than one digest over
	// everything: a recall figure is computed per stratum, so each denominator
	// needs an identity a reader can check on its own.
	StratumDigests map[string]string `json:"stratum_digests"`
}

// ExclusionCounts reports exclusions as reason/count pairs in stable order.
func (inv Inventory) ExclusionCounts() []ExclusionCount {
	counts := map[string]int{}
	for _, e := range inv.Exclusions {
		counts[e.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	out := make([]ExclusionCount, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, ExclusionCount{Reason: r, Count: counts[r]})
	}
	return out
}

// InStratum returns the population of one stratum, in stable order.
func (inv Inventory) InStratum(stratum string) []Classification {
	var out []Classification
	for _, c := range inv.Classified {
		if c.Stratum == stratum {
			out = append(out, c)
		}
	}
	return out
}
