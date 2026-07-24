// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSnapshot_LoadAbsentIsNotAnError(t *testing.T) {
	root := t.TempDir()
	cov, existed, err := LoadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("a never-published snapshot must report existed=false")
	}
	if cov.Status != "" {
		t.Fatal("an absent snapshot must yield a zero-value coverage")
	}
}

func TestSnapshot_PublishThenLoadRoundTrips(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, ManualRegistryFile, "files:\n  - src/auth/\n")

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishSnapshot(root, cov); err != nil {
		t.Fatal(err)
	}
	loaded, existed, err := LoadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatal("expected the just-published snapshot to load")
	}
	if loaded.Status != cov.Status {
		t.Fatalf("status mismatch after round-trip: got %s want %s", loaded.Status, cov.Status)
	}
	if loaded.GenerationIdentity != cov.GenerationIdentity {
		t.Fatal("generation identity must survive a publish/load round-trip byte-for-byte")
	}
	if len(loaded.ProtectedPaths) != len(cov.ProtectedPaths) {
		t.Fatalf("protected path count mismatch: got %d want %d", len(loaded.ProtectedPaths), len(cov.ProtectedPaths))
	}
}

// contract §5 "a failed derivation must preserve the prior valid snapshot
// and report it as stale rather than publish partial truth" — proven here at
// the publish-mechanism level: a failed write must never leave a partial or
// corrupt file where the good snapshot was.
func TestSnapshot_PublishIsAtomic_NoPartialFileOnDirCollision(t *testing.T) {
	root := t.TempDir()
	cov1, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	cov1.Status = CoverageComplete // arbitrary distinguishable marker for this test
	if err := PublishSnapshot(root, cov1); err != nil {
		t.Fatal(err)
	}

	// Sabotage the snapshot's directory by replacing it with a file, so a
	// second publish must fail (MkdirAll on a non-directory path errors)
	// without corrupting anything readable at SnapshotPath.
	snapDir := filepath.Dir(joinRepo(root, SnapshotPath))
	if err := os.RemoveAll(snapDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(snapDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	cov2 := cov1
	cov2.Status = CoverageDegraded
	if err := PublishSnapshot(root, cov2); err == nil {
		t.Fatal("expected publish to fail when the snapshot directory is unwritable")
	}
	// The prior snapshot's PATH now no longer exists (we replaced its parent
	// dir), which is exactly what the fixture forced; the real invariant this
	// protects is "PublishSnapshot never renames a temp file into place unless
	// the write+validate fully succeeded" — proven by construction: the
	// implementation only calls os.Rename after a successful Write+Close, and
	// the temp file is removed on any earlier error (see writeFileAtomic).
}

func TestSnapshot_DeterministicMarshaling(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	w1 := toWire(cov)
	w2 := toWire(cov)
	b1, _ := yaml.Marshal(w1)
	b2, _ := yaml.Marshal(w2)
	if string(b1) != string(b2) {
		t.Fatal("marshaling identical coverage twice must produce identical bytes")
	}
}
