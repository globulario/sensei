// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalharness"
	"github.com/globulario/sensei/golang/architecture/evalmodel"
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
	if len(got) != 3 {
		t.Fatalf("missingRequiredWorlds = %v, want the three worlds that did not run", got)
	}
	if len(missingRequiredWorlds([]evalsample.World{
		bound("world1_sensei_self"), bound("world2_globular"),
		bound("world3_independent_calibration"), bound(worldMutantSuite),
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
		{Name: worldMutantSuite},
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
		{Name: worldMutantSuite},
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

// TestNormalizeRemoteComparesTheRepositoryNotTheURLForm keeps the remote check
// from rejecting a legitimate checkout over how its URL happens to be spelled.
func TestNormalizeRemoteComparesTheRepositoryNotTheURLForm(t *testing.T) {
	for _, form := range []string{
		"https://github.com/globulario/sensei.git",
		"https://github.com/globulario/sensei",
		"git@github.com:globulario/sensei.git",
		"ssh://git@github.com/globulario/sensei.git",
		// git:// is a documented transport. Omitting it normalized a
		// legitimate checkout to "git///github.com/..." and rejected it.
		"git://github.com/globulario/sensei.git",
		"git://github.com/globulario/sensei",
		"git+ssh://git@github.com/globulario/sensei.git",
		"https://github.com/globulario/sensei.git/",
	} {
		if got := normalizeRemote(form); got != "github.com/globulario/sensei" {
			t.Errorf("normalizeRemote(%q) = %q, want the repository", form, got)
		}
	}
	if normalizeRemote("https://github.com/someone/else.git") == "github.com/globulario/sensei" {
		t.Error("a different repository normalized to the expected one")
	}
}

// TestAnImpostorCheckoutIsRefusedForANamedWorld.
//
// A world's name and its domain come from the same caller-supplied --world
// string, so neither is evidence about the tree. The remote is at least a
// property of the checkout. This defeats mislabelling rather than a determined
// forger — a local path has no unforgeable link upstream — and mislabelling is
// the failure an evaluation harness actually suffers.
func TestAnImpostorCheckoutIsRefusedForANamedWorld(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", "https://github.com/someone/not-sensei.git")

	if _, err := verifyRequiredWorldCheckout("world1_sensei_self", root); err == nil {
		t.Fatal("a checkout of an unrelated repository was accepted as world 1")
	} else if !strings.Contains(err.Error(), "not-sensei") {
		t.Errorf("the refusal does not say what the checkout actually is: %v", err)
	}

	// A world the protocol does not name is the operator's to bind.
	if _, err := verifyRequiredWorldCheckout("world3_operator_bound", root); err != nil {
		t.Errorf("an operator-bound world was subjected to the protocol's remote check: %v", err)
	}

	// A world the protocol DOES name but for which no upstream identity is
	// registered fails closed. Returning success would let an arbitrary tree
	// be reported as the SQLite calibration, and guessing a URL for the
	// repository whose identity is the open question would be worse.
	if _, err := verifyRequiredWorldCheckout("world3_independent_calibration", root); err == nil {
		t.Error("a protocol-named world with no registered upstream identity was accepted")
	}

	// A checkout with no origin is UNVERIFIED, not refused.
	//
	// "The remote says something else" is evidence of mislabelling; "there is
	// no remote to read" is the absence of evidence. Treating the second as
	// disproof made the advertised command fail in CI clones, source archives,
	// and any checkout whose remote metadata was stripped — the guard breaking
	// the very runs it exists to protect. The absence is typed onto the report
	// instead, so a reader sees the identity rests on the caller's word.
	bare := t.TempDir()
	if out, err := exec.Command("git", "-C", bare, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	note, err := verifyRequiredWorldCheckout("world2_globular", bare)
	if err != nil {
		t.Fatalf("a checkout with no origin was refused rather than reported unverified: %v", err)
	}
	if !strings.Contains(note, "unverified") {
		t.Errorf("the missing identity was not typed onto the report: %q", note)
	}
}

// TestResolveUpstreamFollowsACloneBackToItsRepository.
//
// The worlds are measured from clones, so a clone's origin is the local path it
// was made from rather than the upstream repository. The first version of this
// check compared that local path against the expected repository and refused
// every legitimate run — the guard rejected exactly the trees it was meant to
// admit.
func TestResolveUpstreamFollowsACloneBackToItsRepository(t *testing.T) {
	origin := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(origin, "init", "-q")
	git(origin, "remote", "add", "origin", "https://github.com/globulario/sensei.git")

	// A clone of the clone, so the chain is longer than one hop. The middle
	// hop uses a file:// URL, which git supports and which os.Stat rejects as
	// a directory unless the scheme is stripped before the walk decides.
	mid := t.TempDir()
	git(mid, "init", "-q")
	git(mid, "remote", "add", "origin", "file://"+origin)
	leaf := t.TempDir()
	git(leaf, "init", "-q")
	git(leaf, "remote", "add", "origin", mid)

	got, err := resolveUpstream(leaf)
	if err != nil {
		t.Fatalf("resolveUpstream: %v", err)
	}
	if got != "github.com/globulario/sensei" {
		t.Errorf("resolveUpstream followed the chain to %q, want the upstream repository", got)
	}
	if note, err := verifyRequiredWorldCheckout("world1_sensei_self", leaf); err != nil || note != "" {
		t.Errorf("a legitimate clone-of-a-clone was refused (note=%q): %v", note, err)
	}
}

// TestMutantObservationsCarryTheirSiteIdentity.
//
// Each mutant site is extracted from its own tree, so its anchors are
// repo-relative inside THAT tree: "a.go:1-2" in one mutant and "a.go:1-2" in
// another are different files sharing a name. Appended unchanged they
// collapsed to one identity under evalsample's content hash, so a precision
// label could attach to the wrong source and an adjudicator had no way to know
// which tree held the evidence.
func TestMutantObservationsCarryTheirSiteIdentity(t *testing.T) {
	same := []architecture.Fact{{
		Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e",
		Scope:    architecture.Scope{Files: []string{"a.go"}},
		Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 2},
	}}
	one := namespaceBySite(same, "authority_split")
	two := namespaceBySite(same, "forbidden_dependency")

	if one[0].Evidence.SourceFile == two[0].Evidence.SourceFile {
		t.Fatalf("two mutants share the anchor %q; their observations would collapse to one identity", one[0].Evidence.SourceFile)
	}
	if one[0].Evidence.SourceFile != "authority_split/a.go" {
		t.Errorf("anchor is %q, want the path the file has within the suite", one[0].Evidence.SourceFile)
	}
	if len(one[0].Scope.Files) != 1 || one[0].Scope.Files[0] != "authority_split/a.go" {
		t.Errorf("scope files were not namespaced: %v", one[0].Scope.Files)
	}
	// The caller's slice must not be mutated — the same document is also the
	// arm's own report.
	if same[0].Evidence.SourceFile != "a.go" {
		t.Errorf("namespacing rewrote the caller's observation in place: %q", same[0].Evidence.SourceFile)
	}
}

// TestTheMutantWorldNameCannotBeSuppliedExternally.
//
// World 4 is produced internally. Without reserving the name, a caller could
// pass --world for it: runWorlds would add an arbitrary checkout under that
// name while main added the real synthetic world under the same one.
func TestTheMutantWorldNameCannotBeSuppliedExternally(t *testing.T) {
	if !reservedArmNames[worldMutantSuite] {
		t.Fatal("the mutant suite's world name is not reserved, so --world can claim it")
	}
	dir := t.TempDir()
	arts := runWorlds(t.TempDir(), []string{worldMutantSuite + "=example.com/x=" + dir}, "2026-01-01T00:00:00Z", map[string]int64{}, nil)
	refused := false
	for _, a := range arts {
		if a.Arm == worldMutantSuite && a.Status == statusFailed {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("an external checkout was accepted as the mutant suite: %+v", arts)
	}
}

// TestTheMutantWorldNamespacesThroughItsBuilder covers the CALL SITE, not the
// helper. Removing the namespacing call leaves the helper's own test green —
// the same "verified the machinery, not the consumer" gap that this session
// hit twice already — so the property is asserted where it is actually used.
func TestTheMutantWorldNamespacesThroughItsBuilder(t *testing.T) {
	fact := func() architecture.Fact {
		return architecture.Fact{
			Kind: "k", Subject: "s", Predicate: "p", Object: "o", Extractor: "e",
			Scope:    architecture.Scope{Files: []string{"a.go"}},
			Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 2},
		}
	}
	report := evalharness.Report{
		// The clean control's Defect is EMPTY — evalmutant.Baseline sets it so
		// — while its tree is materialized at mutants/baseline. Namespacing by
		// Defect rewrote its anchors to "/a.go": unresolvable, and worse than
		// absent because it looks like evidence.
		Baseline: evalharness.SiteResult{DefectPaths: []string{"a.go"}},
		Results: []evalharness.SiteResult{
			{Defect: "one", DefectPaths: []string{"a.go"}},
			{Defect: "two", DefectPaths: []string{"a.go"}},
		},
	}
	report.Baseline.Document.Observations = []architecture.Fact{fact()}
	report.Results[0].Document.Observations = []architecture.Fact{fact()}
	report.Results[1].Document.Observations = []architecture.Fact{fact()}

	w := mutantSuiteWorld(report, "example.com/eval")
	anchors := map[string]bool{}
	for _, o := range w.Observations {
		anchors[o.Evidence.SourceFile] = true
	}
	if len(anchors) != 3 {
		t.Fatalf("three sites produced %d distinct anchors, want 3: %v", len(anchors), anchors)
	}
	if !anchors["one/a.go"] || !anchors["two/a.go"] {
		t.Errorf("anchors are not namespaced by site: %v", anchors)
	}
	// The clean control uses the name its tree is materialized under.
	if !anchors["baseline/a.go"] {
		t.Errorf("the clean control's anchor is not baseline/a.go: %v", anchors)
	}
	for a := range anchors {
		if strings.HasPrefix(a, "/") {
			t.Errorf("anchor %q has an empty site prefix; it resolves to nothing", a)
		}
	}
	// The recall inventory is the defect sites, independent of what was observed.
	if len(w.RecallInventory) != 3 {
		t.Errorf("recall inventory = %v, want one unit per defect site", w.RecallInventory)
	}
	if w.Binding.TreeDigestSHA256 == "" {
		t.Error("the suite world carries no identity")
	}
}

// TestTheSuiteBindingIsTheTreeNotTheRunTimestamp.
//
// The document digest covers receipt and evidence timestamps, so it changes
// with --captured-at even when the tree is byte-identical. evalsample's
// selection key hashes the world binding, so deriving the binding from
// document digests meant the same committed seed could draw different claims
// from an unchanged suite — which is the one property the frozen sample
// depends on.
func TestTheSuiteBindingIsTheTreeNotTheRunTimestamp(t *testing.T) {
	build := func(documentDigest string) evalharness.Report {
		site := evalharness.SiteResult{Defect: "one", DefectPaths: []string{"a.go"}, DocumentDigest: documentDigest}
		site.Document.Binding.Repository.TreeDigestSHA256 = "the-tree-is-unchanged"
		base := evalharness.SiteResult{DefectPaths: []string{"a.go"}, DocumentDigest: documentDigest}
		base.Document.Binding.Repository.TreeDigestSHA256 = "baseline-tree"
		return evalharness.Report{Baseline: base, Results: []evalharness.SiteResult{site}}
	}
	first := mutantSuiteWorld(build("run-one-document-digest"), "example.com/eval")
	second := mutantSuiteWorld(build("run-two-document-digest"), "example.com/eval")

	if first.Binding.TreeDigestSHA256 != second.Binding.TreeDigestSHA256 {
		t.Fatalf("two runs over an identical suite produced different bindings:\n  %s\n  %s\nthe same seed would draw different claims",
			first.Binding.TreeDigestSHA256, second.Binding.TreeDigestSHA256)
	}

	// A genuinely different tree must still change the binding.
	changed := build("run-one-document-digest")
	changed.Results[0].Document.Binding.Repository.TreeDigestSHA256 = "a-different-tree"
	if mutantSuiteWorld(changed, "example.com/eval").Binding.TreeDigestSHA256 == first.Binding.TreeDigestSHA256 {
		t.Error("a changed tree produced the same binding; the identity tracks nothing")
	}
}

// TestComposedClaimsReachTheSample.
//
// Without this the frozen manifest held no item key and no blinded payload for
// any composed candidate, so a reference set derived from it left every one
// unlabelled — and the protocol's unsupported-claim rate (§9) and model delta
// (§18) are computed over exactly those claims. The world carried the
// observations and silently dropped the propositions.
func TestComposedClaimsReachTheSample(t *testing.T) {
	var w evalsample.World
	site := evalharness.CompositionSiteResult{}
	site.Defect = "one"
	site.ModelAcquisition.Baseline.Candidates = []evalmodel.BaselineItem{
		{Kind: "claim", Text: "the boundary is crossed in the helper", CitedEvidenceIDs: []string{"ev-1"}, FilePaths: []string{"a.go"}},
	}
	site.ModelAcquisition.Items = []evalmodel.AcquiredItem{
		{Kind: "claim", Text: "a model-derived proposal", FilePaths: []string{"b.go"}},
	}
	addComposedClaims(&w, evalharness.CompositionReport{Results: []evalharness.CompositionSiteResult{site}})

	if len(w.Counterexamples) != 2 {
		t.Fatalf("composed claims produced %d sampleable items, want 2", len(w.Counterexamples))
	}
	var deterministic, model bool
	for _, c := range w.Counterexamples {
		if strings.Contains(c.Description, "[deterministic") {
			deterministic = true
		}
		if strings.Contains(c.Description, "[model") {
			model = true
		}
		if !strings.HasPrefix(c.ID, "one/") {
			t.Errorf("claim %q is not attributed to its site", c.ID)
		}
		// A claim without its evidence is not adjudicable, it is an opinion.
		if len(c.EvidenceRefIDs) == 0 {
			t.Errorf("claim %q carries no evidence anchor; an adjudicator cannot open the pinned source", c.ID)
		}
		for _, r := range c.EvidenceRefIDs {
			if r == "a.go" || r == "b.go" {
				t.Errorf("path %q is not namespaced by site; two mutants' a.go are different files", r)
			}
		}
	}
	if !deterministic || !model {
		t.Error("the two lanes are not distinguishable in the sampled claims; §9 forbids scoring them as one population")
	}
}
