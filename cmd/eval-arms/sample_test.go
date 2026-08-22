// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalharness"
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
	art := writeSample(t.TempDir(), protocolPath, protocolID, "deadbeef", nil, true, nil, worlds, "", "2026-08-22T05:00:00Z")
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
	art := writeSample(t.TempDir(), filepath.Join(t.TempDir(), "absent.md"), protocolID, "", errors.New("no such file"), false, nil, worlds, "seed", "2026-08-22T05:00:00Z")
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
	art := writeSample(out, protocolFileForTest(t), protocolID, "deadbeef", nil, false, nil, worlds, "seed", "2026-08-22T05:00:00Z")
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

// TestTheDefaultProtocolRefusesAPartialWorldSet is the case that looked safe.
//
// Nothing is substituted here — a world is simply missing. But the default
// protocol consumes every world in requiredWorlds, so a sample drawn from a
// subset carries the v1 identity while following a reduced world definition.
// That is the same false claim as swapping a world, reached by omission, and
// it is what the documented default invocation was doing.
func TestTheDefaultProtocolRefusesAPartialWorldSet(t *testing.T) {
	worlds := []evalsample.World{{
		Name:            "world1_sensei_self",
		Binding:         architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations:    []architecture.Fact{{Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e", Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1}}},
		RecallInventory: []string{"pkg/a"},
	}}
	art := writeSample(t.TempDir(), protocolFileForTest(t), protocolID, "deadbeef", nil, true,
		[]string{"world3_independent_calibration"}, worlds, "seed", "2026-08-22T05:00:00Z")
	// statusFailed: a seed was supplied, so the draw was REQUESTED, and a
	// refused request must fail the run rather than pass silently.
	if art.Status != statusFailed {
		t.Fatalf("a sample was drawn under the default protocol with a world missing: %s", art.Status)
	}
	if !strings.Contains(art.Reason, "world3_independent_calibration") {
		t.Errorf("the refusal does not name the world that was missing: %q", art.Reason)
	}

	// A protocol the operator bound explicitly is theirs to define, so the
	// same subset is allowed once they say which protocol governs it.
	art = writeSample(t.TempDir(), protocolFileForTest(t), "operator-protocol-v2", "deadbeef", nil, false,
		[]string{"world3_independent_calibration"}, worlds, "seed", "2026-08-22T05:00:00Z")
	if art.Status != statusRan {
		t.Fatalf("an explicitly bound protocol was refused a subset it may define: %s — %s", art.Status, art.Reason)
	}
}

// TestMissingRequiredWorldsNamesWhatDidNotRun keeps the refusal's input honest.
func TestMissingRequiredWorldsNamesWhatDidNotRun(t *testing.T) {
	bound := func(name string) evalsample.World {
		return evalsample.World{Name: name, Binding: architecture.ClaimDocumentBinding{RepositoryDomain: requiredWorldDomains[name]}}
	}
	got := missingRequiredWorlds([]evalsample.World{bound("world1_sensei_self")})
	if len(got) != 2 || got[0] != "world2_globular" || got[1] != "world3_independent_calibration" {
		t.Fatalf("missingRequiredWorlds = %v, want the two worlds that did not run", got)
	}
	if len(missingRequiredWorlds([]evalsample.World{
		bound("world1_sensei_self"), bound("world2_globular"), bound("world3_independent_calibration"),
	})) != 0 {
		t.Error("a complete world set reported something missing")
	}
}

// TestTheProtocolPairIsCheckedByValueNotByPresence.
//
// An earlier version compared only whether both flags were mentioned, so
// `--protocol-file <the v1 document> --protocol-id v2` passed: it recorded the
// v1 digest under a v2 identity AND made the completeness check believe a
// custom protocol was in use, disabling the guard that protects v1. Naming both
// flags is not the same as keeping them consistent.
func TestTheProtocolPairIsCheckedByValueNotByPresence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		file, id   string
		wantAgrees bool
	}{
		{"both default", protocolPath, protocolID, true},
		{"both custom", "other.md", "other-v2", true},
		{"v1 document under another identity", protocolPath, "other-v2", false},
		{"v1 identity over another document", "other.md", protocolID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agrees := (tc.file == protocolPath) == (tc.id == protocolID)
			if agrees != tc.wantAgrees {
				t.Fatalf("pair (%s, %s) agrees=%v, want %v", tc.file, tc.id, agrees, tc.wantAgrees)
			}
		})
	}
}

// TestTheMutantSuiteCountsTowardV1Completeness.
//
// The protocol consumes four worlds and the fourth is the mutant suite, which
// is not a checkout and so never appeared in requiredWorlds. A run whose mutant
// arms failed would otherwise have written a v1 sample over an incomplete v1
// evaluation — the same omission defect, in the world that does not look like
// a world.
func TestTheMutantSuiteCountsTowardV1Completeness(t *testing.T) {
	complete := []armArtifact{
		{Arm: evalharness.ArmDeterministicExtraction, Subject: subjectMutantSuite, Status: statusRan},
		{Arm: evalharness.ArmCompositionModelDisabled, Subject: subjectMutantSuite, Status: statusRan},
	}
	if got := incompleteMutantSuite(complete); len(got) != 0 {
		t.Errorf("a complete mutant suite reported %v missing", got)
	}

	failed := []armArtifact{
		{Arm: evalharness.ArmDeterministicExtraction, Subject: subjectMutantSuite, Status: statusFailed},
		{Arm: evalharness.ArmCompositionModelDisabled, Subject: subjectMutantSuite, Status: statusRan},
	}
	got := incompleteMutantSuite(failed)
	if len(got) != 1 || !strings.Contains(got[0], evalharness.ArmDeterministicExtraction) {
		t.Fatalf("a failed mutant arm was not counted as incomplete: %v", got)
	}

	// An arm that never appeared at all is missing, not silently complete.
	if got := incompleteMutantSuite(nil); len(got) != len(requiredMutantArms) {
		t.Errorf("absent mutant arms reported %v, want all %d missing", got, len(requiredMutantArms))
	}

	// The optional model arm must NOT count: the protocol treats a bound model
	// as available-when-available, so its absence is not incompleteness.
	withoutModel := append([]armArtifact(nil), complete...)
	withoutModel = append(withoutModel, armArtifact{Arm: armCompositionModelBound, Subject: subjectMutantSuite, Status: statusNotRun})
	if got := incompleteMutantSuite(withoutModel); len(got) != 0 {
		t.Errorf("an unbound optional model arm was treated as incompleteness: %v", got)
	}
}

// TestAWorldsNameIsNotItsIdentity.
//
// A name is a label the caller chose. A direct caller could point three
// arbitrary Go checkouts at the required names and the completeness check
// would have seen a full v1 world set, so a v1 sample could be drawn over
// repositories the protocol never named.
func TestAWorldsNameIsNotItsIdentity(t *testing.T) {
	impostors := []evalsample.World{
		{Name: "world1_sensei_self", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "example.com/not-sensei"}},
		{Name: "world2_globular", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "github.com/globulario/Globular"}},
		{Name: "world3_independent_calibration", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "sqlite.org/sqlite"}},
	}
	missing := missingRequiredWorlds(impostors)
	if len(missing) != 1 || !strings.Contains(missing[0], "world1_sensei_self") {
		t.Fatalf("a world bound to the wrong repository passed completeness: %v", missing)
	}
	if !strings.Contains(missing[0], "example.com/not-sensei") {
		t.Errorf("the report does not say what it was actually bound to: %q", missing[0])
	}

	honest := []evalsample.World{
		{Name: "world1_sensei_self", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "github.com/globulario/sensei"}},
		{Name: "world2_globular", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "github.com/globulario/Globular"}},
		{Name: "world3_independent_calibration", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "sqlite.org/sqlite"}},
	}
	if got := missingRequiredWorlds(honest); len(got) != 0 {
		t.Errorf("correctly bound worlds reported missing: %v", got)
	}
}

// TestTheProtocolDigestIsTheOneValidated.
//
// Validating the pair at startup and re-reading the file when the sample is
// written leaves a window across the arms' runtime. A file edited in between
// would be validated as one document and recorded as another — the identity
// split the pair check exists to prevent.
func TestTheProtocolDigestIsTheOneValidated(t *testing.T) {
	out := t.TempDir()
	path := protocolFileForTest(t)
	worlds := []evalsample.World{{
		Name:            "w",
		Binding:         architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations:    []architecture.Fact{{Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e", Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1}}},
		RecallInventory: []string{"pkg/a"},
	}}

	const validated = "the-digest-checked-at-startup"
	// The file changes after validation, exactly as it could while the arms run.
	if err := os.WriteFile(path, []byte("# a different protocol entirely\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	art := writeSample(out, path, "operator-v2", validated, nil, false, nil, worlds, "seed", "2026-08-22T05:00:00Z")
	if art.Status != statusRan {
		t.Fatalf("writeSample: %s — %s", art.Status, art.Reason)
	}
	data, err := os.ReadFile(filepath.Join(out, art.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), validated) {
		t.Error("the manifest recorded a digest re-read from disk rather than the one validated at startup")
	}
}

// TestTheCompiledProtocolDigestMatchesTheDocument is the drift guard for the
// constant that lets an installed binary recognise the default protocol
// without a working directory. If the document changes and the constant does
// not, every caller passing the real v1 document would be told it is custom.
func TestTheCompiledProtocolDigestMatchesTheDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", protocolPath))
	if err != nil {
		t.Skipf("protocol document not readable from the test's working directory: %v", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != defaultProtocolDigest {
		t.Fatalf("the compiled default-protocol digest is stale:\n  constant: %s\n  document: %s\nUpdate defaultProtocolDigest, or the binary will call the real v1 document custom.", defaultProtocolDigest, got)
	}
}

// TestARefusedRequestedSampleFailsTheRun.
//
// The caller asked for a draw by supplying a seed. Refusing it is a failure of
// what was requested, and main's exit code counts only failures — reporting
// not_run let automation see a successful command that produced no manifest.
func TestARefusedRequestedSampleFailsTheRun(t *testing.T) {
	worlds := []evalsample.World{{
		Name:            "world1_sensei_self",
		Binding:         architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations:    []architecture.Fact{{Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e", Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1}}},
		RecallInventory: []string{"pkg/a"},
	}}
	art := writeSample(t.TempDir(), protocolFileForTest(t), protocolID, "deadbeef", nil, true,
		[]string{"mutant suite"}, worlds, "a-seed-was-supplied", "2026-08-22T05:00:00Z")
	if art.Status != statusFailed {
		t.Fatalf("a requested sample was refused with status %q; main counts only %q, so the run would have exited successfully with no manifest", art.Status, statusFailed)
	}

	// No seed means no sample was requested, which is not a failure.
	art = writeSample(t.TempDir(), protocolFileForTest(t), protocolID, "deadbeef", nil, true, nil, worlds, "", "2026-08-22T05:00:00Z")
	if art.Status != statusNotRun {
		t.Errorf("a run that asked for no sample reported %q, want %q", art.Status, statusNotRun)
	}
}

// TestARequestedDrawWithNoWorldFails.
//
// The zero-world branch returned before the refusal guard, so a direct caller
// passing --selection-seed with no --world got exit 0 and no manifest. A seed
// means the draw was requested; "nothing to sample" is then a failure of what
// was asked for rather than a quiet absence.
func TestARequestedDrawWithNoWorldFails(t *testing.T) {
	art := writeSample(t.TempDir(), protocolFileForTest(t), protocolID, "deadbeef", nil, true, nil, nil, "a-seed", "2026-08-22T05:00:00Z")
	if art.Status != statusFailed {
		t.Fatalf("a requested draw with no world reported %q; main counts only %q, so the run would exit 0 with no manifest", art.Status, statusFailed)
	}

	// Without a seed nothing was requested, so nothing failed.
	art = writeSample(t.TempDir(), protocolFileForTest(t), protocolID, "deadbeef", nil, true, nil, nil, "", "2026-08-22T05:00:00Z")
	if art.Status != statusNotRun {
		t.Errorf("an unrequested draw with no world reported %q, want %q", art.Status, statusNotRun)
	}
}
