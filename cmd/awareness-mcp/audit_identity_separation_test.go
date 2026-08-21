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
