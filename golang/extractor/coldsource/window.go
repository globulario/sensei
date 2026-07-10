// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"fmt"
	"os/exec"
	"strings"
)

// DefaultWindowLadder is the conservative, bounded set of commit-window depths
// auto-window planning will try, smallest first. The largest entry is the hard
// safe-max: planning NEVER scans more history than this, so it can never run
// unbounded over a huge repo's full history.
var DefaultWindowLadder = []int{500, 1000, 2000, 4000, 8000}

// DefaultWindowTargetReverts is how many revert/regression commits the planner
// wants inside the window before it stops widening. Enough to give a handful of
// themes a chance to triangulate; small enough that scar-dense repos stay tight.
const DefaultWindowTargetReverts = 8

// WindowPlan is the outcome of auto-window planning: the git range to scan, plus
// the scar evidence behind the choice, for transparent reporting.
type WindowPlan struct {
	Range       string // e.g. "HEAD~2000..HEAD"
	Depth       int    // commits in the chosen window
	Reverts     int    // revert/regression commits found in the window
	Scanned     int    // commits actually examined (<= safe max)
	HitTarget   bool   // true if Reverts >= target; false = scar-sparse, capped
	LadderMaxed bool   // true if we used the largest resolvable depth
}

// chooseWindowDepth is the PURE selection rule, factored out for testing.
// Given the ladder, the cumulative revert count at each ladder boundary, and a
// target, it returns the index of the smallest window that meets the target —
// or the last index (largest window) if none do. counts[i] is the number of
// reverts within the first ladder[i] commits (cumulative, non-decreasing).
func chooseWindowDepth(ladder []int, counts []int, target int) int {
	if len(ladder) == 0 {
		return -1
	}
	for i := range ladder {
		if i < len(counts) && counts[i] >= target {
			return i
		}
	}
	// None met the target → use the largest window we actually scanned.
	last := len(counts) - 1
	if last < 0 || last >= len(ladder) {
		last = len(ladder) - 1
	}
	return last
}

// PlanWindow chooses a commit window for the revert scan by widening along the
// ladder until it finds `target` revert/regression commits, capped at the
// largest ladder depth that resolves in this repo. It is BOUNDED: it scans at
// most the largest resolvable ladder depth, never full history.
//
// It does exactly one `git log` (at the largest resolvable depth) and counts
// reverts cumulatively at each ladder boundary — no unbounded walk, no
// PR-comment fetching. Returns the plan for the caller to report and apply.
func PlanWindow(repo string, ladder []int, target int) (WindowPlan, error) {
	if len(ladder) == 0 {
		ladder = DefaultWindowLadder
	}
	if target <= 0 {
		target = DefaultWindowTargetReverts
	}

	// Find the largest ladder depth that actually resolves (repo may be smaller
	// than the safe max). This keeps the single git log bounded by real history.
	maxDepth := 0
	for _, d := range ladder {
		if revParseResolves(repo, fmt.Sprintf("HEAD~%d", d)) {
			maxDepth = d
		}
	}
	if maxDepth == 0 {
		// History shorter than the smallest window — let the caller fall back to
		// the default range rather than inventing one.
		return WindowPlan{Range: "", Depth: 0, Reverts: 0, Scanned: 0}, nil
	}

	commits, err := loadCommitMetadata(repo, fmt.Sprintf("HEAD~%d..HEAD", maxDepth))
	if err != nil {
		return WindowPlan{}, err
	}
	scanned := len(commits)

	// Cumulative revert counts at each ladder boundary <= maxDepth.
	var usable []int
	counts := make([]int, 0, len(ladder))
	revertSoFar := 0
	cursor := 0
	for _, d := range ladder {
		if d > maxDepth {
			break
		}
		usable = append(usable, d)
		for cursor < d && cursor < scanned {
			if ok, _ := matchCommit(commits[cursor]); ok {
				revertSoFar++
			}
			cursor++
		}
		counts = append(counts, revertSoFar)
	}

	idx := chooseWindowDepth(usable, counts, target)
	if idx < 0 {
		return WindowPlan{Range: "", Depth: 0, Reverts: 0, Scanned: scanned}, nil
	}
	depth := usable[idx]
	return WindowPlan{
		Range:       fmt.Sprintf("HEAD~%d..HEAD", depth),
		Depth:       depth,
		Reverts:     counts[idx],
		Scanned:     scanned,
		HitTarget:   counts[idx] >= target,
		LadderMaxed: depth == maxDepth,
	}, nil
}

// revParseResolves reports whether a revision (e.g. "HEAD~5000") exists.
func revParseResolves(repo, rev string) bool {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", rev)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}
