// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prereview"
)

// The pre-review binding records the canonical Sensei SHA-256 tree digest, not
// the native Git tree object id. This pins that the digest equals an independent
// recomputation of the exact canonical algorithm and that the native object id
// is preserved separately as a distinct identity. Cross-package equality with
// architecture/binding is proven at PR-2 (binding is not on this branch's base).
func TestGitDiffCanonicalTreeIdentity(t *testing.T) {
	dir, base := mkPreReviewRepo(t)
	diff, err := gitDiffSource{}.ResolveReviewDiff(context.Background(), prereview.DiffRequest{RepoRoot: dir, Base: base, Head: "HEAD"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Independent recomputation of the canonical algorithm: SHA-256 over
	// `git ls-tree -r -z --full-tree HEAD`.
	lsTree, err := exec.Command("git", "-C", dir, "ls-tree", "-r", "-z", "--full-tree", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(lsTree)
	want := hex.EncodeToString(sum[:])
	if diff.HeadTreeDigestSHA256 != want {
		t.Fatalf("head tree digest %s != canonical recomputation %s", diff.HeadTreeDigestSHA256, want)
	}
	if len(diff.HeadTreeDigestSHA256) != 64 || len(diff.BaseTreeDigestSHA256) != 64 {
		t.Fatalf("canonical digests must be 64 hex: base=%s head=%s", diff.BaseTreeDigestSHA256, diff.HeadTreeDigestSHA256)
	}

	// Native object id: SHA-1 (40 hex), preserved, and distinct from the digest.
	nativeOID := trimNL(mustGit(t, dir, "rev-parse", "--verify", "HEAD^{tree}"))
	if diff.HeadTreeObjectID != nativeOID {
		t.Fatalf("native tree object id %s not preserved (got %s)", nativeOID, diff.HeadTreeObjectID)
	}
	if len(diff.HeadTreeObjectID) != 40 {
		t.Fatalf("expected a SHA-1 (40 hex) object id, got %q", diff.HeadTreeObjectID)
	}
	if diff.HeadTreeObjectID == diff.HeadTreeDigestSHA256 {
		t.Fatal("native object id and canonical digest must be distinct identities")
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
