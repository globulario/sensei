// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalsample"
)

// TestRecallInventoryComesFromTheTreeNotFromSensei is the property section 7
// depends on.
//
// A recall denominator built from Sensei's own extraction can only contain
// units Sensei already had something to say about, so a unit it missed
// entirely never enters the denominator and its omission is unmeasurable by
// construction — recall would be a measurement of the output against itself.
//
// Here a package exists on disk that no observation mentions. It must appear
// in the inventory anyway.
func TestRecallInventoryComesFromTheTreeNotFromSensei(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"seen", "never/mentioned/anywhere", "vendor/foreign", "testdata/fixture", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "x.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	units, err := recallUnitInventory(root)
	if err != nil {
		t.Fatalf("recallUnitInventory: %v", err)
	}
	got := map[string]bool{}
	for _, u := range units {
		got[u] = true
	}

	if !got["never/mentioned/anywhere"] {
		t.Error("a package no observation mentions is absent from the recall inventory; an omission there could never be measured")
	}
	if !got["seen"] {
		t.Error("an ordinary package is missing from the inventory")
	}
	for _, excluded := range []string{"vendor/foreign", "testdata/fixture", ".hidden"} {
		if got[excluded] {
			t.Errorf("%s entered the unit inventory; it is not a unit of this repository's architecture", excluded)
		}
	}
}

// TestAnEmptyTreeIsRefusedRatherThanSampledEmpty. A world sampled with a
// silently empty inventory reports a recall lane that is honestly absent for a
// dishonest reason, and no later reader can tell the two apart.
func TestAnEmptyTreeIsRefusedRatherThanSampledEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# no go here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recallUnitInventory(root); err == nil {
		t.Fatal("a tree with no Go file produced an inventory instead of refusing")
	}
}

// TestASeedlessRunIsNotRunRatherThanFailed. A run that only wanted the arms is
// legitimate; what it did not do was draw a sample, and that is a different
// fact from a draw that was attempted and broke.
func TestASeedlessRunIsNotRunRatherThanFailed(t *testing.T) {
	worlds := []evalsample.World{{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
	}}
	art := writeSample(t.TempDir(), protocolPath, protocolID, worlds, "", "2026-08-22T05:00:00Z")
	if art.Status != statusNotRun {
		t.Errorf("a seedless run reported status %q, want %q", art.Status, statusNotRun)
	}
	if !strings.Contains(art.Reason, "selection-seed") {
		t.Errorf("the refusal does not say what was missing: %q", art.Reason)
	}
}

// TestAMissingProtocolIsAnActionableRefusal. The manifest records the
// protocol's digest so a sample drawn under one version can never be read as
// though it obeyed another; if the protocol cannot be read, nothing may be
// drawn.
func TestAMissingProtocolIsAnActionableRefusal(t *testing.T) {
	worlds := []evalsample.World{{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
	}}
	art := writeSample(t.TempDir(), filepath.Join(t.TempDir(), "absent.md"), protocolID, worlds, "seed", "2026-08-22T05:00:00Z")
	if art.Status != statusFailed {
		t.Fatalf("an unreadable protocol produced status %q, want %q", art.Status, statusFailed)
	}
	if !strings.Contains(art.Reason, "--protocol-file") {
		t.Errorf("the refusal does not tell the operator how to fix it: %q", art.Reason)
	}
}

// TestTheSampleIsWrittenWhereTheIndexSaysItIs. An index naming a file that is
// not there, or naming a digest that does not describe the bytes on disk, is
// worse than no index.
func TestTheSampleIsWrittenWhereTheIndexSaysItIs(t *testing.T) {
	out := t.TempDir()
	worlds := []evalsample.World{{
		Name:            "w",
		Binding:         architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations:    []architecture.Fact{{Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e", Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1}}},
		RecallInventory: []string{"pkg/a"},
	}}
	art := writeSample(out, protocolFileForTest(t), protocolID, worlds, "seed", "2026-08-22T05:00:00Z")
	if art.Status != statusRan {
		t.Fatalf("writeSample: %s — %s", art.Status, art.Reason)
	}
	if _, err := os.Stat(filepath.Join(out, art.ReportFile)); err != nil {
		t.Fatalf("the index names %s but it is not there: %v", art.ReportFile, err)
	}
	if art.ReportDigest == "" {
		t.Error("the sample was written without an identity, so no release can name it")
	}

	// report_digest must verify the way every other arm's does: re-hash the
	// bytes on disk. Reporting the manifest's self-excluding identity here
	// would make any verifier that re-hashes the file declare this artifact
	// altered on every single run, because the file contains that value.
	onDisk, err := os.ReadFile(filepath.Join(out, art.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(onDisk)
	if got := hex.EncodeToString(sum[:]); got != art.ReportDigest {
		t.Errorf("report_digest is %s but the bytes on disk hash to %s; a verifier would call this artifact altered every run", art.ReportDigest, got)
	}

	// The self-excluding identity a reference-set release names still has to
	// be carried, separately, or step 11 has nothing to bind to.
	if art.SampleManifestDigest == "" {
		t.Error("the sample manifest's own identity was dropped; a reference-set release names it as sample_manifest_digest_sha256")
	}
	if art.SampleManifestDigest == art.ReportDigest {
		t.Error("the two digests are equal, which is impossible if one hashes bytes that contain the other")
	}
}

// protocolFileForTest writes a stand-in protocol so the test does not depend
// on the working directory the test binary happens to run in.
func protocolFileForTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "protocol.md")
	if err := os.WriteFile(path, []byte("# protocol under test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
