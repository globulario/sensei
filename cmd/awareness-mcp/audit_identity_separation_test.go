// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// A diff audit has two independent identities:
//
//  1. the commit in the repository being audited, used to reconstruct the
//     pre-change bytes for modify hunks; and
//  2. the commit carried by graph authority, which identifies the rule
//     snapshot that supplied invariants/tests/contracts.
//
// They are not required to be commits in the same repository. In the
// multi-domain deployment used by sensei-code, the graph authority snapshot is
// produced by the awareness service while expected_head names the governed
// repository's candidate base. Requiring equality makes every modified-file
// audit impossible: omitting expected_head prevents base reconstruction, while
// supplying it is rejected as a graph-commit mismatch.
//
// This is intentionally a red regression test on the branch that introduced
// it. The implementation must remove only the cross-identity equality check.
// It must NOT weaken either independent fail-closed rule: modified files still
// require a caller-pinned repository base, and authoritative graph evidence
// still requires an exact source/build commit so the rule snapshot enters the
// audit digest.
func TestAuditTargetBaseIsIndependentFromGraphSnapshotCommit(t *testing.T) {
	head := testGitHEAD(t)
	graphCommit := strings.Repeat("a", len(head))
	if graphCommit == strings.ToLower(head) {
		graphCommit = strings.Repeat("b", len(head))
	}

	fake := fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(graphCommit)}, nil
		},
	}
	checker := &mcpSingleFileChecker{
		bridge:       testBridge(fake),
		root:         ".",
		expectedHead: head,
	}

	_, _, _, gotGraphCommit, err := checker.GetFileImpact(
		context.Background(),
		"internal/example.go",
		"github.com/globulario/sensei-code",
	)
	if err != nil {
		t.Fatalf("candidate base %s and graph snapshot %s are independent identities; audit refused: %v", head, graphCommit, err)
	}
	if gotGraphCommit != graphCommit {
		t.Fatalf("graph snapshot identity = %q, want %q", gotGraphCommit, graphCommit)
	}
}

// A unified diff's final line may be a single space: the context line for a
// blank source line. A context line counts toward BOTH sides of its hunk, so
// trimming it makes a valid diff miscount by exactly one in each direction.
//
// This asserts the BOUNDARY, not the parser. ParseDiff handles the blank line
// correctly and always did; the payload was being damaged before it arrived,
// by the shared trimming arg reader. Measured 2026-09-26: a 49 KB candidate was
// refused as malformed_diff and the refusal was reported as a structural
// failure of the candidate, which was intact.
func TestADiffPayloadKeepsItsTrailingBlankContextLine(t *testing.T) {
	const withBlankFinalContext = "diff --git a/x.go b/x.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/x.go\n+++ b/x.go\n" +
		"@@ -1,3 +1,4 @@\n package x\n \n+var Added = 1\n \n"

	args := map[string]interface{}{"diff": withBlankFinalContext}

	if got := argStringExact(args, "diff"); got != withBlankFinalContext {
		t.Fatalf("the diff payload was altered on the way in:\n got %q\nwant %q", got, withBlankFinalContext)
	}

	// The shared reader is still allowed to trim: that is correct for a path or
	// a SHA. This pins WHY a separate reader exists, so the two cannot be merged
	// back together without this failing.
	if trimmed := argString(args, "diff"); trimmed == withBlankFinalContext {
		t.Fatal("argString no longer trims; this test no longer distinguishes the two readers")
	} else if strings.HasSuffix(trimmed, " \n") {
		t.Fatalf("argString kept the trailing context line, so the readers are no longer distinct: %q", trimmed)
	}
}
