// SPDX-License-Identifier: AGPL-3.0-only

package derive

// What a derived fact may contribute to coverage, and the one collapse that
// would undo everything above it.
//
// The forbidden move:
//
//	stored recipe present  ->  coverage
//
// which silently turns "I know what question to ask here" into "I know the
// answer". A recipe is a question. Counting questions as knowledge is the same
// error as counting a non-empty evidence string as evidence, one layer higher,
// and it would let a fabricated record close a real coverage gap — dissolving
// every guarantee #300 through #303 established.
//
// The only legitimate path:
//
//	stored recipe -> Revalidate(current world) -> Established -> CoverageAnchor
//
// This file makes the wrong version hard to write rather than merely documented
// as wrong. AnchorFor takes an Established, which only Derive returns, so a
// caller holding a StoredFact has nothing to pass. There is no overload that
// accepts a recipe, and no field to set.

import (
	"fmt"
	"time"
)

// CoverageAnchor is a derived fact standing behind a coverage claim in one
// world.
//
// Unexported fields and no exported constructor, for the same reason Established
// has none: the guarantee should be a property of the type rather than a rule
// somebody remembers.
type CoverageAnchor struct {
	established Established
	world       string
}

// AnchorFor turns a fact established in a world into coverage for that world.
//
// It takes an Established deliberately. A caller holding only a StoredFact
// cannot reach this function without first revalidating, which is exactly the
// step that must not be skippable.
//
// The world is checked rather than assumed: an anchor built from a fact derived
// somewhere else would claim coverage for a world nobody looked at.
func AnchorFor(e Established, world string) (CoverageAnchor, error) {
	if e.receipt.Outcome != Derived {
		// Unreachable through Derive, which returns no Established otherwise.
		// Kept because "unreachable" is a claim about today's callers.
		return CoverageAnchor{}, fmt.Errorf("a %s receipt cannot anchor coverage", e.receipt.Outcome)
	}
	if e.receipt.Commit != world {
		return CoverageAnchor{}, fmt.Errorf(
			"this fact was derived in %s and cannot anchor coverage for %s; revalidate against the world being assessed",
			shortCommit(e.receipt.Commit), shortCommit(world))
	}
	return CoverageAnchor{established: e, world: world}, nil
}

// AnchorFromRecipe is the whole legitimate path in one call: revalidate the
// recipe in the world being assessed, and anchor only if it derives there.
//
// Offered so the safe sequence is the convenient one. Callers that want the
// steps separately still cannot skip revalidation, because AnchorFor will not
// take a recipe.
func AnchorFromRecipe(s StoredFact, src PinnedSource, now time.Time) (Receipt, *CoverageAnchor) {
	receipt, est := s.Revalidate(src, now)
	if est == nil {
		return receipt, nil
	}
	anchor, err := AnchorFor(*est, src.Commit())
	if err != nil {
		receipt.Detail = err.Error() + "; " + receipt.Detail
		return receipt, nil
	}
	return receipt, &anchor
}

// Proposition is what this anchor covers.
func (c CoverageAnchor) Proposition() Proposition { return c.established.Proposition() }

// World is the revision this anchor speaks for, and only that revision.
func (c CoverageAnchor) World() string { return c.world }

// Scope carries the derivation's envelope forward, so coverage never reads as
// stronger than the derivation that produced it (§8f).
func (c CoverageAnchor) Scope() string { return c.established.Scope() }

// Receipt is the reproducible basis, so a coverage claim can be re-run rather
// than trusted.
func (c CoverageAnchor) Receipt() Receipt { return c.established.Receipt() }
