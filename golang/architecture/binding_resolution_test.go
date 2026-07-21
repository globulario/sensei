// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import "testing"

const testTreeDigest = "1111111111111111111111111111111111111111111111111111111111111111"

func committedBinding() ClaimDocumentBinding {
	return ClaimDocumentBinding{
		RepositoryDomain:  "github.com/example/project",
		Revision:          "0123456789abcdef",
		RevisionStatus:    RevisionResolved,
		GraphDigestSHA256: "abcdef0123456789",
		GraphDigestStatus: GraphDigestResolved,
	}
}

func worktreeBinding() ClaimDocumentBinding {
	return ClaimDocumentBinding{
		RepositoryDomain:  "github.com/example/project",
		RevisionStatus:    RevisionUnavailable,
		TreeDigestSHA256:  testTreeDigest,
		GraphDigestSHA256: "abcdef0123456789",
		GraphDigestStatus: GraphDigestResolved,
	}
}

// Test 1 / Test 6: a committed result resolves through its revision, exactly as
// before the tree-digest field existed.
func TestRepositorySnapshotResolvedCommitted(t *testing.T) {
	b := committedBinding()
	if !RepositorySnapshotResolved(b) {
		t.Fatal("committed binding must be snapshot-resolved")
	}
	if b.TreeDigestSHA256 != "" {
		t.Fatal("committed binding needs no tree digest to resolve")
	}
}

// Test 2 / Test 19: an uncommitted result tree resolves through its canonical
// tree digest, with no fabricated revision.
func TestRepositorySnapshotResolvedWorktreeTreeOnly(t *testing.T) {
	b := worktreeBinding()
	if !RepositorySnapshotResolved(b) {
		t.Fatal("worktree binding with tree digest must be snapshot-resolved")
	}
	if b.Revision != "" || RepositoryRevisionResolved(b) {
		t.Fatal("worktree binding must not carry or claim a resolved revision")
	}
}

// Test 3: a snapshot is not resolved when neither an exact revision nor an exact
// tree digest is present, so the base revision alone cannot impersonate a result
// whose tree is unknown.
func TestRepositorySnapshotUnresolvedWhenNeitherRevisionNorTree(t *testing.T) {
	b := worktreeBinding()
	b.TreeDigestSHA256 = ""
	if RepositorySnapshotResolved(b) {
		t.Fatal("binding without revision or tree digest must not resolve")
	}
	// Graph alone is never enough.
	if RepositorySnapshotResolved(ClaimDocumentBinding{GraphDigestSHA256: "x", GraphDigestStatus: GraphDigestResolved}) {
		t.Fatal("graph-only binding must not resolve")
	}
}

// Test 7: a native Git object id (40-hex SHA-1) is never accepted as a canonical
// tree digest; only a 64-hex SHA-256 is a resolved tree.
func TestRepositoryTreeResolvedRejectsGitOID(t *testing.T) {
	oid := "0123456789abcdef0123456789abcdef01234567" // 40-hex SHA-1
	if RepositoryTreeResolved(ClaimDocumentBinding{TreeDigestSHA256: oid}) {
		t.Fatal("a 40-hex Git OID must not count as a resolved tree digest")
	}
	if !RepositoryTreeResolved(ClaimDocumentBinding{TreeDigestSHA256: testTreeDigest}) {
		t.Fatal("a 64-hex sha256 must count as a resolved tree digest")
	}
	if RepositoryTreeResolved(ClaimDocumentBinding{TreeDigestSHA256: "GGGG" + testTreeDigest[4:]}) {
		t.Fatal("non-hex must not count as a resolved tree digest")
	}
}

// Test 1: a committed result supports exact (supported) claims.
func TestCommittedResultSupportsExactClaims(t *testing.T) {
	if _, err := NormalizeClaimDocument(validClaimDocument()); err != nil {
		t.Fatalf("committed claim document must validate: %v", err)
	}
}

// Test 2 / Test 19: an uncommitted result tree supports exact (supported) claims
// without a fake revision.
func TestWorktreeResultSupportsExactClaims(t *testing.T) {
	doc := validClaimDocument()
	doc.Binding = worktreeBinding()
	// The premise fact is bound to the exact file content (source digest), not a
	// revision — mirroring a worktree extraction.
	for i := range doc.FactReceipts {
		doc.FactReceipts[i].Provenance.Revision = ""
		doc.FactReceipts[i].Provenance.RevisionStatus = RevisionUnavailable
	}
	got, err := NormalizeClaimDocument(doc)
	if err != nil {
		t.Fatalf("worktree claim document must validate without a revision: %v", err)
	}
	if got.Binding.Revision != "" {
		t.Fatal("normalized worktree binding must not invent a revision")
	}
	if got.Binding.TreeDigestSHA256 != testTreeDigest {
		t.Fatalf("normalized worktree binding lost its tree digest: %q", got.Binding.TreeDigestSHA256)
	}
}

// Test 3: without the exact result tree digest, a supported claim cannot validate
// against a merely domain-tagged binding — a missing tree is never treated as a
// resolved result.
func TestSupportedClaimRejectedWhenSnapshotUnresolved(t *testing.T) {
	doc := validClaimDocument()
	doc.Binding = worktreeBinding()
	doc.Binding.TreeDigestSHA256 = "" // drop the result tree identity
	doc.Binding.RevisionStatus = RevisionUnavailable
	for i := range doc.FactReceipts {
		doc.FactReceipts[i].Provenance.Revision = ""
		doc.FactReceipts[i].Provenance.RevisionStatus = RevisionUnavailable
	}
	if _, err := NormalizeClaimDocument(doc); err == nil {
		t.Fatal("a supported claim must not validate against an unresolved snapshot")
	}
}

// Test 8: snapshot resolution is a pure function of binding field values, so a
// relocated checkout with the same tree digest resolves identically.
func TestRelocatedCheckoutIdenticalResolution(t *testing.T) {
	a := worktreeBinding()
	b := worktreeBinding() // same values, "different checkout"
	if RepositorySnapshotResolved(a) != RepositorySnapshotResolved(b) {
		t.Fatal("identical bindings must resolve identically regardless of checkout path")
	}
	b.TreeDigestSHA256 = "2222222222222222222222222222222222222222222222222222222222222222"
	if RepositorySnapshotResolved(a) != RepositorySnapshotResolved(b) {
		t.Fatal("both remain resolved, but they now identify different result trees")
	}
	if a.TreeDigestSHA256 == b.TreeDigestSHA256 {
		t.Fatal("expected distinct tree digests to model distinct result trees")
	}
}
