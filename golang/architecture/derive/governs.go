// SPDX-License-Identifier: AGPL-3.0-only

package derive

// Whether a fact established in one world still governs another.
//
// A derivation runs against a pinned commit. The moment a candidate changes the
// files it read, the receipt describes a world that no longer exists — and a
// fact that silently keeps governing after its inputs move is the same defect
// as a stale graph reporting itself current, which is where this whole line of
// work started.
//
//	A derived fact may govern only worlds compatible with the world and the
//	dependencies from which it was derived.
//
// Compatibility is decided by the inputs the derivation ACTUALLY READ, recorded
// in the receipt, rather than by the commit alone. Most candidates touch none
// of them, so most facts survive; the ones that do not are exactly the ones
// whose evidence moved.
//
// This is deliberately not general supersession. It answers one question about
// one receipt against one candidate world, and it answers conservatively:
// anything it cannot determine is stale, because re-deriving is cheap and a
// wrong answer here is authority granted on evidence that no longer holds.

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Applicability is why a receipt does or does not govern a candidate world.
type Applicability struct {
	Governs bool
	Reason  string
	// Changed are the receipt's own inputs that differ in the candidate world.
	// Empty when the fact still governs.
	Changed []string
}

// GovernsWorld reports whether this receipt still establishes its proposition
// in a candidate world.
//
// dir is a checkout of the repository able to read both revisions.
func (r Receipt) GovernsWorld(ctx context.Context, dir, candidateRevision string) Applicability {
	if r.Outcome != Derived {
		return Applicability{Reason: fmt.Sprintf(
			"this receipt established nothing (%s); there is no fact to carry into another world", r.Outcome)}
	}
	if strings.TrimSpace(r.Commit) == "" {
		return Applicability{Reason: "the receipt pins no commit, so no world can be compared to it"}
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse",
		strings.TrimSpace(candidateRevision)+"^{commit}").Output()
	if err != nil {
		return Applicability{Reason: fmt.Sprintf("cannot resolve the candidate world %q: %v", candidateRevision, err)}
	}
	candidate := strings.TrimSpace(string(out))
	if candidate == r.Commit {
		return Applicability{Governs: true, Reason: "the candidate world is the world this was derived in"}
	}
	if len(r.Inputs) == 0 {
		return Applicability{Reason: "the receipt records no inputs, so nothing can be shown to be unchanged"}
	}

	args := append([]string{"-C", dir, "diff", "--name-only", r.Commit, candidate, "--"}, r.Inputs...)
	diff, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		// Conservative: a comparison that cannot be made is not a comparison
		// that succeeded.
		return Applicability{Reason: fmt.Sprintf(
			"cannot compare %s with %s over the derivation's inputs: %v", shortCommit(r.Commit), shortCommit(candidate), err)}
	}
	var changed []string
	for _, line := range strings.Split(strings.TrimSpace(string(diff)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			changed = append(changed, line)
		}
	}
	if len(changed) == 0 {
		return Applicability{Governs: true, Reason: fmt.Sprintf(
			"none of the %d input(s) this derivation read differ between %s and %s",
			len(r.Inputs), shortCommit(r.Commit), shortCommit(candidate))}
	}
	sort.Strings(changed)
	return Applicability{Changed: changed, Reason: fmt.Sprintf(
		"%d of the %d input(s) this derivation read changed between %s and %s (%s); "+
			"the fact must be re-derived against the candidate rather than carried into it",
		len(changed), len(r.Inputs), shortCommit(r.Commit), shortCommit(candidate), strings.Join(changed, ", "))}
}
