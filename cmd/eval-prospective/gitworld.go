// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// InventoryRuleID is the frozen identity of the enumeration rule below.
const InventoryRuleID = "sensei.prospective.inventory_rule.v1"

// InventoryRuleDescription is the mechanical rule, stated before the inventory
// is built (protocol section 8.1).
//
// The population is every commit reachable from the pinned revision — the
// complete set of proposed changes observable at that revision — not a curated
// range. Section 3.2 of the design settles this: scoping to the commits where
// the hypothesis was first observed selects for the phenomenon being measured,
// and a seed applied afterwards would be honest about the draw while the
// population underneath it was already biased.
const InventoryRuleDescription = "" +
	"Every commit reachable from the pinned revision is a candidate change. " +
	"A commit without exactly one parent (merge or root) is excluded as no_single_base_revision, " +
	"because it has no single pre-authoring state and so cannot say what the graph knew when it was authored. " +
	"A commit touching no path is excluded as no_paths_touched. " +
	"A commit whose diff cannot be reconstructed at the pinned revision is excluded as contents_not_reconstructable. " +
	"Every exclusion is counted and reported."

func git(ctx context.Context, repoRoot string, args ...string) (string, error) {
	// Direct argv, never a shell: a path or a revision that happens to contain
	// shell metacharacters must not become a second command.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// ResolveRevision returns the checkout's current commit.
func ResolveRevision(ctx context.Context, repoRoot, rev string) (string, error) {
	out, err := git(ctx, repoRoot, "rev-parse", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// TreeDigest returns the tree object a revision points at.
func TreeDigest(ctx context.Context, repoRoot, rev string) (string, error) {
	out, err := git(ctx, repoRoot, "rev-parse", rev+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// EnumerateChanges builds the complete candidate population at a pinned
// revision, together with everything that could not enter it.
//
// It walks git rather than taking a supplied list. A supplied list is a
// curated population wearing an enumeration's name, and whoever gets to decide
// what enters the denominator decides the score.
func EnumerateChanges(ctx context.Context, repoRoot, revision string) ([]prospective.Change, []prospective.Exclusion, error) {
	out, err := git(ctx, repoRoot, "rev-list", "--parents", revision)
	if err != nil {
		return nil, nil, err
	}
	var changes []prospective.Change
	var exclusions []prospective.Exclusion
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		commit, parents := fields[0], fields[1:]
		if len(parents) != 1 {
			reason := "merge commit with " + fmt.Sprint(len(parents)) + " parents"
			if len(parents) == 0 {
				reason = "root commit: no pre-authoring state exists"
			}
			exclusions = append(exclusions, prospective.Exclusion{
				ChangeID: changeID(commit), Reason: prospective.ExcludedNoSingleBase, Detail: reason,
			})
			continue
		}
		base := parents[0]
		paths, err := touchedPaths(ctx, repoRoot, base, commit)
		if err != nil {
			exclusions = append(exclusions, prospective.Exclusion{
				ChangeID: changeID(commit), Reason: prospective.ExcludedUnreconstructable, Detail: err.Error(),
			})
			continue
		}
		if len(paths) == 0 {
			exclusions = append(exclusions, prospective.Exclusion{
				ChangeID: changeID(commit), Reason: prospective.ExcludedNoPaths, Detail: "the commit touches no path",
			})
			continue
		}
		content, err := git(ctx, repoRoot, "diff", "--no-color", base, commit)
		if err != nil {
			exclusions = append(exclusions, prospective.Exclusion{
				ChangeID: changeID(commit), Reason: prospective.ExcludedUnreconstructable, Detail: err.Error(),
			})
			continue
		}
		baseTree, err := TreeDigest(ctx, repoRoot, base)
		if err != nil {
			exclusions = append(exclusions, prospective.Exclusion{
				ChangeID: changeID(commit), Reason: prospective.ExcludedUnreconstructable, Detail: err.Error(),
			})
			continue
		}
		digest, err := prospective.DigestOf(content)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, prospective.NormalizeChange(prospective.Change{
			ID:                   changeID(commit),
			Revision:             commit,
			BaseRevision:         base,
			BaseTreeDigestSHA256: baseTree,
			Paths:                paths,
			ContentDigestSHA256:  digest,
		}))
	}
	return changes, exclusions, nil
}

func changeID(commit string) string { return "change:" + commit }

// touchedPaths reports what a change touched and whether each path existed
// beforehand.
//
// Existence comes from git's status letter for the pair, not from looking the
// path up in the current tree: whether a file exists TODAY says nothing about
// whether it existed when the change was authored, and prospective recall is a
// claim about that earlier moment.
func touchedPaths(ctx context.Context, repoRoot, base, commit string) ([]prospective.PathChange, error) {
	out, err := git(ctx, repoRoot, "diff-tree", "-r", "--no-commit-id", "--name-status", "-M", base, commit)
	if err != nil {
		return nil, err
	}
	var paths []prospective.PathChange
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := fields[len(fields)-1]
		// A rename creates a path the graph has never seen, whatever git calls
		// the operation. Treating R as "existed before" would file a genuinely
		// new seam under stratum C and hide the case the experiment is for.
		existed := !strings.HasPrefix(status, "A") && !strings.HasPrefix(status, "R")
		paths = append(paths, prospective.PathChange{Path: path, ExistedBefore: existed, Status: status})
	}
	return paths, nil
}

// ContentLookup returns the exact diff for a change, for the adjudication
// package.
func ContentLookup(ctx context.Context, repoRoot string, changes []prospective.Change) prospective.ContentLookup {
	base := map[string]prospective.Change{}
	for _, c := range changes {
		base[c.ID] = c
	}
	return func(id string) (string, error) {
		c, ok := base[id]
		if !ok {
			return "", fmt.Errorf("no such change in the frozen inventory")
		}
		return git(ctx, repoRoot, "diff", "--no-color", c.BaseRevision, c.Revision)
	}
}
