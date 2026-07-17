// SPDX-License-Identifier: Apache-2.0

package architecture

// Exact repository-snapshot resolution.
//
// A ClaimDocumentBinding identifies the exact repository state its claims,
// dialogue, and evidence were derived against. Historically that state was a
// native Git revision (a committed commit id). An admitted result can also be an
// exact but *uncommitted* working tree that has no revision; such a result is
// identified by its canonical Sensei repository-tree digest
// (TreeDigestSHA256, a 64-hex SHA-256 over deterministic Git tree material, never
// a native Git object id). These predicates are the single shared definition of
// "resolved" so every stage (inference, claim-document validation, maintenance,
// plane, closure, dialogue, question generation) agrees, and so a worktree result
// is never forced to invent a revision to look resolved.

// RepositoryRevisionResolved reports whether the binding names an exact resolved
// committed revision.
func RepositoryRevisionResolved(b ClaimDocumentBinding) bool {
	return b.RevisionStatus == RevisionResolved && b.Revision != ""
}

// RepositoryTreeResolved reports whether the binding names an exact canonical
// repository-tree digest (64 lowercase hex). This is the Sensei tree identity,
// not a native Git object id.
func RepositoryTreeResolved(b ClaimDocumentBinding) bool {
	return isHexSHA256(b.TreeDigestSHA256)
}

// RepositoryGraphResolved reports whether the binding names an exact resolved
// architecture-graph digest.
func RepositoryGraphResolved(b ClaimDocumentBinding) bool {
	return b.GraphDigestStatus == GraphDigestResolved && b.GraphDigestSHA256 != ""
}

// RepositorySnapshotResolved reports whether the binding identifies an exact
// repository snapshot: the graph digest is resolved AND either an exact resolved
// revision or an exact canonical repository-tree digest is present. A committed
// result satisfies it through the revision; an uncommitted admitted result
// satisfies it through the tree digest, with no fabricated revision.
func RepositorySnapshotResolved(b ClaimDocumentBinding) bool {
	return RepositoryGraphResolved(b) && (RepositoryRevisionResolved(b) || RepositoryTreeResolved(b))
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
