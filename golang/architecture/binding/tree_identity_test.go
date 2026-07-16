// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"path/filepath"
	"testing"
)

// A native Git object ID and Sensei's canonical SHA-256 tree digest are
// distinct identities. These tests pin that only the SHA-256 digest is the
// canonical *_sha256 value, that the native object ID is preserved separately,
// and that the digest is stable across checkouts and sensitive to content — the
// exact algorithm Phase 1 repository snapshots use.

func TestTreeIdentityDigestIs64HexChars(t *testing.T) {
	root := testRepo(t)
	id, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.DigestSHA256) != 64 {
		t.Fatalf("canonical digest is not 64 hex chars: %q (len %d)", id.DigestSHA256, len(id.DigestSHA256))
	}
	if !isHex(id.DigestSHA256) {
		t.Fatalf("canonical digest is not hexadecimal: %q", id.DigestSHA256)
	}
}

func TestTreeIdentityPreservesNativeObjectIDSeparately(t *testing.T) {
	root := testRepo(t)
	id, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if id.GitTreeObjectID == "" {
		t.Fatal("native git tree object id was dropped")
	}
	if id.GitTreeObjectID == id.DigestSHA256 {
		t.Fatal("native object id and canonical digest must be distinct identities")
	}
}

func TestTreeIdentitySHA1RepositoryStillProducesSHA256Digest(t *testing.T) {
	// testRepo is an ordinary SHA-1 object-format repository. Its native tree
	// object id is 40 hex chars; the Sensei digest must still be a 64-hex SHA-256.
	root := testRepo(t)
	id, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.GitTreeObjectID) != 40 {
		t.Fatalf("expected a SHA-1 (40 hex) native object id, got %q", id.GitTreeObjectID)
	}
	if len(id.DigestSHA256) != 64 {
		t.Fatalf("SHA-1 repository did not yield a SHA-256 Sensei digest: %q", id.DigestSHA256)
	}
}

func TestTreeIdentitySameContentAcrossCheckoutsMatches(t *testing.T) {
	root := testRepo(t)
	other := filepath.Join(t.TempDir(), "clone")
	git(t, "", "clone", root, other)
	a, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ResolveTreeIdentity(context.Background(), other, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if a.DigestSHA256 != b.DigestSHA256 {
		t.Fatalf("digest differs across checkouts at different paths: %s vs %s", a.DigestSHA256, b.DigestSHA256)
	}
}

func TestTreeIdentityMatchesPhase1BaseBindingDigest(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	base, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := ResolveTreeIdentity(context.Background(), root, base.Repository.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// The Phase 3 observed base tree digest must equal the Phase 1 base-binding
	// tree digest for the same revision — one algorithm, one identity.
	if id.DigestSHA256 != base.Repository.TreeDigestSHA256 {
		t.Fatalf("phase-3 observed base digest %s != phase-1 base-binding digest %s",
			id.DigestSHA256, base.Repository.TreeDigestSHA256)
	}
}

func TestTreeIdentityChangesWhenTrackedFileChanges(t *testing.T) {
	root := testRepo(t)
	before, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "change tracked file")
	after, err := ResolveTreeIdentity(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if before.DigestSHA256 == after.DigestSHA256 {
		t.Fatal("canonical digest did not change when a tracked file changed")
	}
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}
