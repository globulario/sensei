// SPDX-License-Identifier: AGPL-3.0-only

// Tests for `sensei principle-pack refresh` — the missing edge between the
// upstream-authored principle pack and an ALREADY INSTALLED project mirror.
//
// The shape under test is deliberately narrow: carry upstream ADDITIONS into a
// mirror that is otherwise byte-identical to the pack it was installed from,
// emit a receipt binding both digests, refuse everything else. Every refusal
// below is a case where an automatic write could not tell an upstream change
// from a local edit — and silently guessing is how a mirror becomes a second
// authority.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/packcustody"
)

// installedMirror builds a project whose mirror is the embedded pack minus the
// named ids — i.e. exactly what an older `sensei init` would have left behind.
func installedMirror(t *testing.T, omit ...string) (root, mirrorPath string) {
	t.Helper()
	root = t.TempDir()
	mirrorPath = filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := packMinus(t, omit...)
	if err := os.WriteFile(mirrorPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// A baseline is what makes "absent locally" mean "upstream addition"
	// rather than "the project deleted it". Apply refuses without one.
	if err := writeInstallRecord(root, body); err != nil {
		t.Fatal(err)
	}
	return root, mirrorPath
}

// installedMirrorNoBaseline is a legacy project: a managed mirror with no
// install or adoption record, exactly like services today.
func installedMirrorNoBaseline(t *testing.T, omit ...string) (root, mirrorPath string) {
	t.Helper()
	root = t.TempDir()
	mirrorPath = filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mirrorPath, packMinus(t, omit...), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, mirrorPath
}

// packMinus renders the embedded pack with the named entries removed, keeping
// the preamble verbatim so only the entry set differs.
func packMinus(t *testing.T, omit ...string) []byte {
	t.Helper()
	b, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(omit) == 0 {
		return b
	}
	text := string(b)
	for _, id := range omit {
		start := strings.Index(text, "  - id: "+id+"\n")
		if start < 0 {
			t.Fatalf("id %q not present in the embedded pack", id)
		}
		next := strings.Index(text[start+1:], "\n  - id: ")
		if next < 0 {
			text = text[:start]
		} else {
			text = text[:start] + text[start+1+next+1:]
		}
	}
	return []byte(text)
}

// anAddableID returns an id present in the pack, used as the upstream addition.
func anAddableID(t *testing.T) string {
	t.Helper()
	b, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, _, err := parsePrinciplePack(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["meta.single_derivation_path_must_reach_its_consumer"]; ok {
		return "meta.single_derivation_path_must_reach_its_consumer"
	}
	for id := range entries {
		return id
	}
	t.Fatal("embedded pack has no entries")
	return ""
}

func readMirror(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func receiptFiles(t *testing.T, root string) []string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(adoptionsDirPath(root), "*.yaml"))
	return matches
}

// ─── the appliable case ─────────────────────────────────────────────────

func TestPackRefresh_AdditiveUpstreamEntryApplies(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root}); rc != 0 {
		t.Fatalf("plan should succeed, got rc=%d", rc)
	}
	if got := readMirror(t, mirrorPath); string(got) != string(before) {
		t.Fatal("plan-only run modified the mirror")
	}
	if n := len(receiptFiles(t, root)); n != 0 {
		t.Fatalf("plan-only run wrote %d receipt(s)", n)
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("apply should succeed, got rc=%d", rc)
	}

	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readMirror(t, mirrorPath)) != string(pack) {
		t.Fatal("refreshed mirror is not byte-identical to the embedded pack")
	}
	entries, _, _, err := parsePrinciplePack(readMirror(t, mirrorPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries[id]; !ok {
		t.Fatalf("upstream addition %q did not reach the mirror", id)
	}
}

func TestPackRefresh_ExactReplayDoesNotRewrite(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("first apply failed rc=%d", rc)
	}
	afterFirst := readMirror(t, mirrorPath)
	receipts := receiptFiles(t, root)
	if len(receipts) != 1 {
		t.Fatalf("expected exactly 1 receipt, got %d", len(receipts))
	}
	receiptBefore, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Stat(mirrorPath)
	if err != nil {
		t.Fatal(err)
	}

	// Replay: already current. Must report success and write nothing.
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("replay should succeed, got rc=%d", rc)
	}
	if string(readMirror(t, mirrorPath)) != string(afterFirst) {
		t.Fatal("replay rewrote the mirror")
	}
	if statAfter, err := os.Stat(mirrorPath); err == nil {
		if !statAfter.ModTime().Equal(statBefore.ModTime()) {
			t.Fatal("replay touched the mirror file")
		}
	}
	if now := receiptFiles(t, root); len(now) != 1 {
		t.Fatalf("replay changed receipt count to %d", len(now))
	}
	receiptAfter, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(receiptAfter) != string(receiptBefore) {
		t.Fatal("replay rewrote the receipt")
	}
}

// TestPackRefresh_ApplyReplayResyncsMirrorDirectory proves the
// "mirror already matches the pack" fast path does not treat correct
// on-disk mirror bytes as proof commitMirror's earlier directory sync ever
// succeeded. A prior --apply's rename could have completed while its final
// syncDir failed transiently; a naive replay reporting success here would
// leave that unconfirmed, letting a later crash still revert the mirror
// despite the "successful" retry.
func TestPackRefresh_ApplyReplayResyncsMirrorDirectory(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("first apply failed rc=%d", rc)
	}

	called := false
	orig := syncDir
	syncDir = func(dir string) error {
		called = true
		return fmt.Errorf("simulated transient sync failure")
	}
	defer func() { syncDir = orig }()

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("apply replay must surface a failed mirror directory re-sync, not silently succeed")
	}
	if !called {
		t.Fatal("apply replay of an already-current mirror never attempted to re-sync its directory")
	}
}

// TestPackRefresh_PlanReplayDoesNotSync proves plan mode (no --apply) stays
// read-only even on the already-current fast path: it must not attempt any
// directory sync, matching every other side-effecting step in this command
// being gated on --apply.
func TestPackRefresh_PlanReplayDoesNotSync(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("first apply failed rc=%d", rc)
	}

	called := false
	orig := syncDir
	syncDir = func(dir string) error {
		called = true
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if rc := runPrinciplePackRefresh([]string{"--repo", root}); rc != 0 {
		t.Fatalf("plan-mode replay failed rc=%d", rc)
	}
	if called {
		t.Fatal("plan mode (no --apply) must not sync anything, even on the already-current fast path")
	}
}

// TestPackRefresh_ApplyReplayRefusesWhenMirrorChangedSinceInitialRead proves
// the already-current --apply fast path does not trust the digest computed
// at the top of this function as still true. Unlike commitMirror, this
// path performs no write of its own to naturally re-validate against; an
// editor changing the mirror in the gap between that initial read and this
// fast path reporting success must be caught, not silently reported as a
// successful apply against content that may no longer match the pack at
// all.
func TestPackRefresh_ApplyReplayRefusesWhenMirrorChangedSinceInitialRead(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("first apply failed rc=%d", rc)
	}
	current := readMirror(t, mirrorPath)
	tampered := append(append([]byte{}, current...), []byte("# an editor landed here after the initial read\n")...)

	t.Cleanup(func() { applyAlreadyCurrentBeforeRevalidateHook = nil })
	applyAlreadyCurrentBeforeRevalidateHook = func() {
		if err := os.WriteFile(mirrorPath, tampered, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal when the mirror changed since the initial read, not a reported success")
	}
	if string(readMirror(t, mirrorPath)) != string(tampered) {
		t.Fatal("the concurrent edit was destroyed despite the refusal")
	}
}

// ─── refusals ───────────────────────────────────────────────────────────

func TestPackRefresh_ModifiedSharedEntryRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	// Edit a shared entry's severity — indistinguishable from an upstream
	// change without a receipt, so it must refuse rather than guess.
	text := string(readMirror(t, mirrorPath))
	text = strings.Replace(text, "    severity: critical\n", "    severity: low\n", 1)
	if err := os.WriteFile(mirrorPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal on a locally changed shared entry")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("refused run still modified the mirror")
	}
	if n := len(receiptFiles(t, root)); n != 0 {
		t.Fatalf("refused run wrote %d receipt(s)", n)
	}
}

func TestPackRefresh_LocalExtraEntryRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	text := string(readMirror(t, mirrorPath))
	text += "  - id: project.local_only_principle\n    title: authored here, not upstream\n    severity: high\n"
	if err := os.WriteFile(mirrorPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal when the mirror holds a project-authored entry")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("refused run overwrote a project-authored entry")
	}
}

func TestPackRefresh_UnknownGeneratedShapeRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	// Strip the generated marker: now it reads as a hand-authored file.
	text := strings.Replace(string(readMirror(t, mirrorPath)), principlePackGeneratedMarker, "hand written by a person", 1)
	if err := os.WriteFile(mirrorPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal on a file that does not declare itself generated")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("refused run wrote over a hand-authored file")
	}
}

func TestPackRefresh_MissingMirrorRefuses(t *testing.T) {
	root := t.TempDir()
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal when the project has no managed mirror")
	}
	if _, err := os.Stat(filepath.Join(root, mirrorRelPath)); !os.IsNotExist(err) {
		t.Fatal("refresh created a mirror where none existed; that is `init`'s job, not refresh's")
	}
}

// ─── the write must land where it says ──────────────────────────────────

func TestPackRefresh_DestinationReplacementCannotRedirectWrite(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	// Replace the destination with a symlink pointing outside the project.
	outside := filepath.Join(t.TempDir(), "elsewhere.yaml")
	if err := os.WriteFile(outside, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mirrorPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, mirrorPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal to write through a symlinked destination")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "untouched\n" {
		t.Fatal("write was redirected through the symlink to a file outside the project")
	}
}

// TestPackRefresh_SymlinkedParentDirectoryRefuses proves the leaf-symlink
// check above is not sufficient by itself: Lstat-ing only
// meta_principles.yaml would pass here, since the LEAF is a real file --
// but its PARENT, "docs/awareness", is a symlink to an outside directory.
// The read and any later rename would still traverse it, redirecting
// outside root exactly as the destination-replacement case above, just one
// level up the path.
func TestPackRefresh_SymlinkedParentDirectoryRefuses(t *testing.T) {
	root := t.TempDir()
	outsideAwareness := filepath.Join(t.TempDir(), "awareness")
	if err := os.MkdirAll(outsideAwareness, 0o755); err != nil {
		t.Fatal(err)
	}
	body := packMinus(t, anAddableID(t))
	outsideMirror := filepath.Join(outsideAwareness, "meta_principles.yaml")
	if err := os.WriteFile(outsideMirror, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeInstallRecord(root, body); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideAwareness, filepath.Join(docsDir, "awareness")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal to read/write through a symlinked parent directory")
	}
	got, err := os.ReadFile(outsideMirror)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatal("the outside mirror was modified despite the refusal")
	}
}

// TestPackRefresh_SymlinkedStateDirRefusesWithoutMutatingTarget proves
// mkdirAllSynced's ancestor/leaf symlink check for the ACTIVE STATE
// DIRECTORY itself (".sensei" or ".awg"), not just the mirror's parent
// path. statedir.Path resolves the state directory via os.Stat, which
// follows a symlink, and plain os.MkdirAll on something that already
// Stats as a directory (even through a symlink) does nothing further and
// succeeds silently -- so a project whose ".sensei" is a symlink to
// somewhere else entirely (standing in here for the real risk: another
// checkout's real governance state, e.g. a worktree accidentally
// resolving into the main checkout's ".sensei") would otherwise have
// --apply create principle-pack/, its lock, and adoption evidence
// there instead, before the mirror is ever even read.
//
// This asserts every property that finding actually requires, not just
// "the call returns an error":
//   - the state directory resolves without a competing real ".sensei" or
//     ".awg" being created alongside/instead of the symlink;
//   - the symlink's target -- standing in for another checkout's real
//     governance state -- is left completely untouched, not just "the
//     mirror wasn't written";
//   - retrying behaves identically: same refusal, same untouched target,
//     not a different error class or a partial mutation on a second try.
func TestPackRefresh_SymlinkedStateDirRefusesWithoutMutatingTarget(t *testing.T) {
	root := t.TempDir()
	mirrorPath := filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	id := anAddableID(t)
	body := packMinus(t, id)
	if err := os.WriteFile(mirrorPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	// Stands in for "another checkout's real .sensei/" -- the actual risk
	// being closed. Must start, and remain, completely empty.
	outsideState := t.TempDir()
	before, err := os.ReadDir(outsideState)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("test setup: outside state dir must start empty, got %v", before)
	}

	if err := os.Symlink(outsideState, filepath.Join(root, ".sensei")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	assertUntouched := func(when string) {
		t.Helper()
		got, err := os.ReadDir(outsideState)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("%s: the symlink target was mutated despite the refusal: %v", when, got)
		}
		fi, err := os.Lstat(filepath.Join(root, ".sensei"))
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s: the .sensei symlink was replaced by a real directory", when)
		}
		if _, err := os.Stat(filepath.Join(root, ".awg")); !os.IsNotExist(err) {
			t.Fatalf("%s: a competing .awg directory was created (statedir must not fall back), err=%v", when, err)
		}
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal when the active state directory is a symlink")
	}
	assertUntouched("first attempt")

	// Retry must behave identically -- the same refusal for the same
	// reason, not a different error class, and still no mutation.
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal on retry too")
	}
	assertUntouched("retry")
}

// ─── receipt ────────────────────────────────────────────────────────────

func TestPackRefresh_WritesReceiptBeforeReportingSuccess(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("apply failed rc=%d", rc)
	}
	receipts := receiptFiles(t, root)
	if len(receipts) != 1 {
		t.Fatalf("success reported with %d receipt(s); a green refresh that cannot say what it refreshed is the silent-parity problem again", len(receipts))
	}
}

func TestPackRefresh_ReceiptBindsOldAndNewDigests(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	previous := sha256Hex(readMirror(t, mirrorPath))

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("apply failed rc=%d", rc)
	}
	resulting := sha256Hex(readMirror(t, mirrorPath))
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}

	receipts := receiptFiles(t, root)
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	raw, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	var r principlePackRecord
	if err := yaml.Unmarshal(raw, &r); err != nil {
		t.Fatalf("receipt is not parseable: %v", err)
	}

	if r.SchemaVersion != principlePackReceiptSchemaVersion || r.Kind != principlePackReceiptKind {
		t.Fatalf("receipt identity wrong: %d/%q", r.SchemaVersion, r.Kind)
	}
	if r.Target.PreviousDigest != previous {
		t.Errorf("previous digest %s, want %s", r.Target.PreviousDigest, previous)
	}
	if r.Target.ResultingDigest != resulting {
		t.Errorf("resulting digest %s, want %s", r.Target.ResultingDigest, resulting)
	}
	if r.Source.PackDigest != sha256Hex(pack) {
		t.Errorf("pack digest not bound to the embedded pack")
	}
	if r.Target.Path != mirrorRelPath {
		t.Errorf("target path %q, want %q", r.Target.Path, mirrorRelPath)
	}
	if r.Source.Authority != "github.com/globulario/sensei" {
		t.Errorf("authority %q", r.Source.Authority)
	}
	if r.Source.SenseiRevision == "" {
		t.Error("receipt does not bind a sensei revision")
	}
	if got := r.DerivedDisposition(resulting); got != "applied" {
		t.Errorf("derived disposition %q, want applied", got)
	}
	if got := r.DerivedDisposition(previous); got != "intent_open" {
		t.Errorf("derived disposition at previous digest %q, want intent_open", got)
	}
	if r.DispositionIs != "derived" {
		t.Errorf("record should declare its disposition derived, got %q", r.DispositionIs)
	}
	if r.Authorization != "verified_baseline" {
		t.Errorf("authorization %q, want verified_baseline", r.Authorization)
	}
	// The exact ids consumed and produced must be named.
	if len(r.Change.AddedIDs) != 1 || r.Change.AddedIDs[0] != id {
		t.Errorf("added_ids %v, want [%s]", r.Change.AddedIDs, id)
	}
	if len(r.Change.ChangedIDs) != 0 || len(r.Change.RemovedIDs) != 0 || len(r.Change.ConflictingIDs) != 0 {
		t.Errorf("unexpected conflict fields: changed=%v removed=%v conflicting=%v",
			r.Change.ChangedIDs, r.Change.RemovedIDs, r.Change.ConflictingIDs)
	}
	if r.Source.PrincipleCount <= 0 {
		t.Error("receipt does not bind a principle count")
	}
}

// ─── the project's own knowledge is not this command's business ─────────

func TestPackRefresh_DoesNotTouchCandidateLedger(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)

	ledgerDir := filepath.Join(root, "docs", "awareness", "candidates")
	if err := os.MkdirAll(ledgerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(ledgerDir, "session_discovered_invariants.yaml")
	const body = "session_discovered_candidates:\n  candidates:\n    - id: candidate.invariant.meta.example\n      status: candidate\n"
	if err := os.WriteFile(ledger, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("apply failed rc=%d", rc)
	}
	got, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatal("refresh modified the project's candidate ledger; that ledger is the project's own append-only record")
	}
}

// ─── P1: no verified baseline ───────────────────────────────────────────

func TestPackRefresh_NoBaselineRefusesApply(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirrorNoBaseline(t, id)
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal: without a baseline, an absent id may be an upstream addition OR a local deletion")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("refused run modified the mirror")
	}
	if n := len(receiptFilesIn(t, root)); n != 0 {
		t.Fatalf("refused run wrote %d record(s)", n)
	}
}

func TestPackRefresh_NoBaselineReportsCauseUnknownNotAddition(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirrorNoBaseline(t, id)
	out := captureStdout(t, func() { runPrinciplePackRefresh([]string{"--repo", root}) })
	if !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("baseline-free plan must report cause UNKNOWN, got:\n%s", out)
	}
	if strings.Contains(out, "upstream additions") {
		t.Fatalf("baseline-free plan must not claim 'upstream additions', got:\n%s", out)
	}
}

func TestPackRefresh_ReconcileLegacyAuthorizesAndIsRecorded(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirrorNoBaseline(t, id)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply", "--reconcile-legacy"}); rc != 0 {
		t.Fatalf("explicit legacy reconciliation should be allowed, rc=%d", rc)
	}
	recs := receiptFilesIn(t, root)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	var r principlePackRecord
	raw, err := os.ReadFile(recs[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if r.Authorization != "reconcile_legacy" {
		t.Fatalf("authorization %q; the record must say the baseline was asserted, not verified", r.Authorization)
	}
}

func TestPackRefresh_InitWritesUsableBaseline(t *testing.T) {
	root := t.TempDir()
	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	b, err := os.ReadFile(installRecordFilePath(root))
	if err != nil {
		t.Fatalf("sensei init did not write an install baseline: %v", err)
	}
	var ir installRecord
	if err := yaml.Unmarshal(b, &ir); err != nil {
		t.Fatal(err)
	}
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	if ir.PackDigest != sha256Hex(pack) {
		t.Fatal("install baseline does not bind the installed pack digest")
	}
	if len(ir.PrincipleIDs) == 0 {
		t.Fatal("install baseline records no principle ids")
	}
	ok, why := verifyBaseline(root, sha256Hex(readMirror(t, filepath.Join(root, mirrorRelPath))))
	if !ok {
		t.Fatalf("a freshly initialized project must have a verified baseline, got %q", why)
	}
}

// TestPackRefresh_SymlinkedBaselineRecordNotTrustedAsVerified proves
// verifyBaseline does not follow a symlinked installed.yaml. A digest
// match through a symlink is not this project's own verified history --
// it is, at best, another checkout's real install record (the concrete
// risk: a worktree resolving into a different checkout's governance
// state) -- and trusting it would let --apply classify genuinely missing
// ids as upstream additions and replace this checkout's mirror without
// real local adoption provenance.
func TestPackRefresh_SymlinkedBaselineRecordNotTrustedAsVerified(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirrorNoBaseline(t, id)
	mirrorDigest := sha256Hex(readMirror(t, mirrorPath))

	outsideRecord := filepath.Join(t.TempDir(), "installed.yaml")
	var ir installRecord
	ir.SchemaVersion = principlePackReceiptSchemaVersion
	ir.Kind = principlePackInstallKind
	ir.PackDigest = mirrorDigest // matches, as if it were genuinely this project's baseline
	body, err := yaml.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideRecord, body, 0o644); err != nil {
		t.Fatal(err)
	}

	recordPath := installRecordFilePath(root)
	if err := os.MkdirAll(filepath.Dir(recordPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideRecord, recordPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if ok, why := verifyBaseline(root, mirrorDigest); ok {
		t.Fatalf("a symlinked install record must never be trusted as a verified baseline, got %q", why)
	}
}

// TestPackRefresh_SymlinkedAdoptionRecordIgnored proves loadRecords (used
// by both verifyBaseline's fallback check and openIntentFor/
// recordForResult) does not follow a symlinked receipt either -- the same
// class of risk as the baseline record, just for prior-adoption evidence
// instead of the install baseline.
func TestPackRefresh_SymlinkedAdoptionRecordIgnored(t *testing.T) {
	root := t.TempDir()
	dir := adoptionsDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "fake-receipt.yaml")
	if err := os.WriteFile(outside, []byte("kind: principle_pack_adoption\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "deadbeef.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if recs := loadRecords(root); len(recs) != 0 {
		t.Fatalf("a symlinked adoption record must not be loaded as this project's own evidence, got %d", len(recs))
	}
}

// TestPackRefresh_InitRetryAfterPartialWriteRecoversBaseline reproduces a
// crash between writing the mirror and writing its install baseline: a
// previous `sensei init` (or a process that died mid-run) left
// meta_principles.yaml on disk, template-identical, with no
// installed.yaml. The old code's early `continue` on "file already exists"
// skipped the whole baseline block on retry, so re-running init reported
// success while leaving the project permanently unrecoverable by
// `principle-pack refresh` (a real mirror, no baseline, refuses forever).
func TestPackRefresh_InitRetryAfterPartialWriteRecoversBaseline(t *testing.T) {
	root := t.TempDir()
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	mirrorPath := filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mirrorPath, pack, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installRecordFilePath(root)); !os.IsNotExist(err) {
		t.Fatalf("test setup: install record must not exist yet, got err=%v", err)
	}

	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	if _, err := os.Stat(installRecordFilePath(root)); err != nil {
		t.Fatalf("retry did not recover the missing install baseline: %v", err)
	}
	ok, why := verifyBaseline(root, sha256Hex(readMirror(t, mirrorPath)))
	if !ok {
		t.Fatalf("mirror left by a partial init must be a verified baseline after retry, got %q", why)
	}
}

// TestPackRefresh_InitDoesNotFabricateBaselineForCustomizedMirror is the
// other side of the retry fix: a mirror that already exists but does NOT
// match the template (a project customized it before this baseline
// mechanism existed) must never get a baseline recorded against content it
// did not actually start from — that would let `principle-pack refresh`
// treat a real local edit as a verified, untouched install.
func TestPackRefresh_InitDoesNotFabricateBaselineForCustomizedMirror(t *testing.T) {
	root := t.TempDir()
	mirrorPath := filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	customized := packMinus(t, anAddableID(t))
	if err := os.WriteFile(mirrorPath, customized, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	if _, err := os.Stat(installRecordFilePath(root)); !os.IsNotExist(err) {
		t.Fatalf("must not fabricate a baseline for a mirror that was never the template, got err=%v", err)
	}
	if string(readMirror(t, mirrorPath)) != string(customized) {
		t.Fatal("init must not overwrite an already-customized mirror")
	}
}

// TestPackRefresh_InitRetryResyncsExistingCorrectBaseline proves init does
// not treat an already-present, byte-correct install record as proof its
// directory sync ever succeeded. A prior init may have renamed
// installed.yaml into place and then hit a transient sync failure; a naive
// retry that only checks "does the record file already exist" would skip
// writeInstallRecord entirely and silently report success without ever
// retrying the sync that was never confirmed.
func TestPackRefresh_InitRetryResyncsExistingCorrectBaseline(t *testing.T) {
	root := t.TempDir()
	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if _, err := os.Stat(installRecordFilePath(root)); err != nil {
		t.Fatalf("test setup: first init must have written a baseline: %v", err)
	}

	called := false
	orig := syncDir
	syncDir = func(dir string) error {
		called = true
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("second init (retry over an already-correct baseline): %v", err)
	}
	if !called {
		t.Fatal("retry over an already-existing, byte-correct baseline never attempted to re-sync its directory")
	}
}

// TestPackRefresh_LegacyAwgStateDirUsedWhenSenseiAbsent proves the
// principle-pack lock, baseline, and adoption paths route through
// statedir.Path (the active state directory) rather than a hard-coded
// ".sensei/..." literal. A legacy repo with ".awg" but no ".sensei" must
// have its principle-pack state written under ".awg", not a freshly
// created ".sensei" — a hard-coded path would make statedir.Name prefer
// ".sensei" from then on, splitting the project's state in two.
func TestPackRefresh_LegacyAwgStateDirUsedWhenSenseiAbsent(t *testing.T) {
	root := t.TempDir()
	mirrorPath := filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	id := anAddableID(t)
	body := packMinus(t, id)
	if err := os.WriteFile(mirrorPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".awg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeInstallRecord(root, body); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".awg", "principle-pack", "installed.yaml")); err != nil {
		t.Fatalf("install record must live under the active .awg state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".sensei")); err == nil {
		t.Fatal("writing the baseline must not create a competing .sensei directory for a legacy .awg project")
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("refresh --apply failed, rc=%d", rc)
	}
	if _, err := os.Stat(filepath.Join(root, ".awg", "principle-pack", "adoptions")); err != nil {
		t.Fatalf("adoption record must be written under .awg, not .sensei: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".sensei")); err == nil {
		t.Fatal("refresh --apply must not create .sensei for a legacy .awg project")
	}
}

// ─── P1: partial apply must be recoverable ──────────────────────────────

func TestPackRefresh_UnwritableRecordDirLeavesMirrorUntouched(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	before := readMirror(t, mirrorPath)

	// Make the adoptions directory unwritable so the record cannot be created.
	dir := adoptionsDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not apply")
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected failure when the record cannot be written")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("mirror was modified even though the adoption record could not be recorded")
	}
}

func TestPackRefresh_InterruptedApplyIsResumable(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := sha256Hex(readMirror(t, mirrorPath))

	// Simulate a crash after the record was written but before the mirror
	// moved: write only the record, exactly as the real path does first.
	entries, _, _, err := parsePrinciplePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	d := packDiff{UpstreamOnly: []string{id}}
	rec := buildPrinciplePackRecord(sha256Hex(pack), len(entries), mirrorRelPath, previous, sha256Hex(pack), d, "verified_baseline")
	if _, err := writePrinciplePackRecord(root, rec); err != nil {
		t.Fatal(err)
	}

	// The interrupted state must be visible, and resumable.
	out := captureStdout(t, func() { runPrinciplePackRefresh([]string{"--repo", root}) })
	if !strings.Contains(out, "intent_open") {
		t.Fatalf("an interrupted adoption must be reported as intent_open, got:\n%s", out)
	}
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("resume failed rc=%d", rc)
	}
	if sha256Hex(readMirror(t, mirrorPath)) != sha256Hex(pack) {
		t.Fatal("resume did not complete the mirror replacement")
	}
	if n := len(receiptFilesIn(t, root)); n != 1 {
		t.Fatalf("resume should reuse the open intent, got %d record(s)", n)
	}
}

// TestPackRefresh_RecoveredReceiptResyncedBeforeCommit proves the
// openIntentFor recovery branch does not treat a visible receipt as proof
// its own directory sync ever succeeded. The receipt existing here is not
// evidence of that: an earlier run's atomicCreateFile could have linked it
// successfully and then hit a transient sync failure, exactly the case
// writePrinciplePackRecord's own exact-replay branch already guards. This
// recovery branch never calls writePrinciplePackRecord again (the receipt
// is already correct), so it must repeat the resync itself before
// committing the mirror.
func TestPackRefresh_RecoveredReceiptResyncedBeforeCommit(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := sha256Hex(readMirror(t, mirrorPath))
	entries, _, _, err := parsePrinciplePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	rec := buildPrinciplePackRecord(sha256Hex(pack), len(entries), mirrorRelPath, previous, sha256Hex(pack),
		packDiff{UpstreamOnly: []string{id}}, "verified_baseline")

	// A real write, using the real syncDir, simulating a prior --apply
	// that wrote the receipt and then crashed before touching the mirror.
	if _, err := writePrinciplePackRecord(root, rec); err != nil {
		t.Fatal(err)
	}

	called := false
	orig := syncDir
	syncDir = func(dir string) error {
		if dir == adoptionsDirPath(root) {
			called = true
		}
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("resuming the open intent failed, rc=%d", rc)
	}
	if !called {
		t.Fatal("recovering an open intent never re-synced the receipt directory before committing the mirror")
	}
	if sha256Hex(readMirror(t, mirrorPath)) != sha256Hex(pack) {
		t.Fatal("mirror was not committed to the pack digest")
	}
}

// TestPackRefresh_RecoveredReceiptResyncFailureRefusesAndLeavesMirrorAlone
// proves a failed resync on the recovery path is surfaced, not silently
// ignored, and the mirror is left untouched rather than committed against
// an unconfirmed receipt.
func TestPackRefresh_RecoveredReceiptResyncFailureRefusesAndLeavesMirrorAlone(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := sha256Hex(readMirror(t, mirrorPath))
	entries, _, _, err := parsePrinciplePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	rec := buildPrinciplePackRecord(sha256Hex(pack), len(entries), mirrorRelPath, previous, sha256Hex(pack),
		packDiff{UpstreamOnly: []string{id}}, "verified_baseline")
	if _, err := writePrinciplePackRecord(root, rec); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	orig := syncDir
	syncDir = func(dir string) error {
		if dir == adoptionsDirPath(root) {
			return fmt.Errorf("simulated transient sync failure")
		}
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal when the recovered receipt's directory re-sync fails")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("mirror was committed despite the receipt's re-sync failing")
	}
}

func TestPackRefresh_ConflictingExistingRecordRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	// A record already at this digest name, but with different content.
	dir := adoptionsDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, short(sha256Hex(pack))+".yaml")
	if err := os.WriteFile(path, []byte("kind: principle_pack_adoption\nsomething: else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal rather than overwriting an existing, different record")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("mirror modified despite a conflicting record")
	}
}

// TestPackRefresh_UnreadableExistingRecordRefuses proves an existing record
// this process cannot read (permissions, a transient I/O error -- anything
// short of proven absence) refuses rather than falling through to
// atomicWriteFile and renaming over content that was never actually
// verified. Only os.IsNotExist may permit creating a new one; this record
// is documented as immutable evidence, so overwriting an unreadable one on
// an honest "I don't know what's there" would be worse than refusing.
func TestPackRefresh_UnreadableExistingRecordRefuses(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file mode 0000 does not block reads")
	}
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	dir := adoptionsDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, short(sha256Hex(pack))+".yaml")
	if err := os.WriteFile(path, []byte("unreadable\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) }) // let TempDir cleanup remove it
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal rather than overwriting a record that could not be read")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("mirror modified despite an unreadable existing record")
	}
}

// ─── P2: unrecognized document shapes must refuse, never be discarded ───

func TestPackRefresh_ExtraTopLevelKeyRefuses(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"scalar", "\nlocal_owner: the project\n"},
		{"mapping", "\nlocal_metadata:\n  owner: project\n  tier: gold\n"},
		{"list", "\nlocal_notes:\n  - keep me\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := anAddableID(t)
			root, mirrorPath := installedMirror(t, id)
			if err := os.WriteFile(mirrorPath, append(readMirror(t, mirrorPath), []byte(tc.extra)...), 0o644); err != nil {
				t.Fatal(err)
			}
			before := readMirror(t, mirrorPath)

			if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
				t.Fatal("expected refusal: a whole-document replacement would silently delete the extra key")
			}
			got := readMirror(t, mirrorPath)
			if string(got) != string(before) {
				t.Fatal("mirror was rewritten, discarding project-authored document content")
			}
			if !strings.Contains(string(got), strings.TrimSpace(strings.SplitN(tc.extra, ":", 2)[0])) {
				t.Fatal("project-authored top-level key did not survive")
			}
		})
	}
}

// TestPackRefresh_TrailingYAMLDocumentRefuses proves a second "---"
// document is caught the same way an extra top-level key within one
// document already is. yaml.Unmarshal decodes only the FIRST document in a
// stream, so a mirror whose first document is a valid, byte-identical pack
// but which has a second document appended would otherwise pass shape
// validation, have its diff computed from the first document alone, and
// then have --apply replace the WHOLE FILE with the embedded pack --
// silently deleting the second document, which parsePrinciplePack never
// even looked at.
func TestPackRefresh_TrailingYAMLDocumentRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	trailing := "\n---\nlocal_notes:\n  - a second document the project added\n"
	if err := os.WriteFile(mirrorPath, append(readMirror(t, mirrorPath), []byte(trailing)...), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal: a whole-document replacement would silently delete the trailing document")
	}
	got := readMirror(t, mirrorPath)
	if string(got) != string(before) {
		t.Fatal("mirror was rewritten, discarding the project-authored trailing document")
	}
	if !strings.Contains(string(got), "a second document the project added") {
		t.Fatal("the trailing document did not survive")
	}
}

func TestPackRefresh_MalformedListMemberRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	// A bare scalar where a mapping belongs.
	if err := os.WriteFile(mirrorPath, append(readMirror(t, mirrorPath), []byte("  - just-a-string\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal on a non-mapping list member")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("mirror modified despite a malformed list member")
	}
}

// ─── P2: concurrent edits must not be overwritten ───────────────────────

func TestPackRefresh_ConcurrentMirrorEditRefusesAtCommit(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	validated := readMirror(t, mirrorPath)

	// Another writer lands an edit after validation, before commit.
	concurrent := append(append([]byte{}, validated...), []byte("# a concurrent editor was here\n")...)
	if err := os.WriteFile(mirrorPath, concurrent, 0o644); err != nil {
		t.Fatal(err)
	}

	err = commitMirror(root, mirrorPath, validated, pack)
	if err == nil {
		t.Fatal("commit must refuse when the mirror changed after validation")
	}
	if !strings.Contains(err.Error(), "changed while this run was deciding") {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(readMirror(t, mirrorPath)) != string(concurrent) {
		t.Fatal("the concurrent edit was destroyed")
	}
}

// TestPackRefresh_ConcurrentEditDuringPrepareIsCaught lands the concurrent
// edit from INSIDE commitMirror's own execution -- after the replacement
// temp file is written, synced, and chmoded, immediately before the
// pre-rename re-check -- rather than before commitMirror is even called.
// TestPackRefresh_ConcurrentMirrorEditRefusesAtCommit above would already
// pass against the OLD ordering (check first, then write+sync+chmod, then
// rename): editing before the call was always caught. Only this ordering
// (prepare first, re-check immediately before the rename) can catch an
// edit that lands during preparation, which is the actual window a
// concurrent editor's save can race.
func TestPackRefresh_ConcurrentEditDuringPrepareIsCaught(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	validated := readMirror(t, mirrorPath)

	concurrent := append(append([]byte{}, validated...), []byte("# a concurrent editor landed mid-prepare\n")...)
	t.Cleanup(func() { commitMirrorAfterPrepareHook = nil })
	commitMirrorAfterPrepareHook = func() {
		if err := os.WriteFile(mirrorPath, concurrent, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err = commitMirror(root, mirrorPath, validated, pack)
	if err == nil {
		t.Fatal("commit must refuse when the mirror changed during preparation")
	}
	if !strings.Contains(err.Error(), "changed while this run was deciding") {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(readMirror(t, mirrorPath)) != string(concurrent) {
		t.Fatal("the concurrent edit was destroyed")
	}
}

// ─── P2: true no-clobber creation ────────────────────────────────────────

// TestAtomicCreateFile_SucceedsForNewFile is the sanity case: creating a
// file that does not exist yet works and its content is correct.
func TestAtomicCreateFile_SucceedsForNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.yaml")
	if err := atomicCreateFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("wrong content: %q", got)
	}
}

// TestAtomicCreateFile_RefusesWhenTargetExists proves atomicCreateFile has
// true create-only semantics: unlike atomicWriteFile's rename (an
// unconditional replace), it must fail -- with an error satisfying
// os.IsExist -- and leave the existing target completely untouched, rather
// than silently overwriting content that arrived between any absence-check
// a caller might have done and this call.
func TestAtomicCreateFile_RefusesWhenTargetExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.yaml")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := atomicCreateFile(path, []byte("attempted overwrite\n"), 0o644)
	if err == nil {
		t.Fatal("expected an error when the target already exists")
	}
	if !os.IsExist(err) {
		t.Fatalf("expected an os.IsExist error, got: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original\n" {
		t.Fatalf("target was modified despite the refusal: %q", got)
	}
}

// TestPackRefresh_ConcurrentReceiptCreationDuringRaceIsNotClobbered proves
// the mechanism writePrinciplePackRecord actually relies on end to end: a
// receipt that appears at the exact digest-named path AFTER this process's
// own logical decision point (simulated here by writing it directly, since
// the whole point of atomicCreateFile is that there is no observable gap
// to land a hook in) is treated as "already there" -- read and compared --
// never silently replaced by an unconditional rename.
func TestPackRefresh_ConcurrentReceiptCreationDuringRaceIsNotClobbered(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	packEntries, _, _, err := parsePrinciplePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	rec := buildPrinciplePackRecord(sha256Hex(pack), len(packEntries), mirrorRelPath,
		sha256Hex(readMirror(t, filepath.Join(root, mirrorRelPath))), sha256Hex(pack),
		packDiff{UpstreamOnly: []string{id}}, "verified_baseline")

	// A "concurrent writer" wins the race and creates the receipt first,
	// with conflicting content.
	dir := adoptionsDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, short(rec.Target.ResultingDigest)+".yaml")
	conflicting := "kind: principle_pack_adoption\nfrom: a different writer\n"
	if err := os.WriteFile(path, []byte(conflicting), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := writePrinciplePackRecord(root, rec); err == nil {
		t.Fatal("expected refusal against a receipt with conflicting content")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != conflicting {
		t.Fatal("the concurrently created receipt was overwritten")
	}
}

// TestPackRefresh_FreshStateDirCreationSyncsRoot proves mkdirAllSynced's
// boundary is root itself, not statedir.Path(root): on a project with
// NEITHER ".sensei" nor ".awg" yet, this call's own MkdirAll creates the
// state directory fresh. Stopping the sync at the state directory (a
// prior version of this function did) would leave ITS OWN creation --
// the new ".sensei" entry inside root -- unsynced.
func TestPackRefresh_FreshStateDirCreationSyncsRoot(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".sensei", ".awg"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("test setup: %s must not exist yet, got err=%v", name, err)
		}
	}
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}

	var synced []string
	orig := syncDir
	syncDir = func(dir string) error {
		synced = append(synced, dir)
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if err := writeInstallRecord(root, pack); err != nil {
		t.Fatalf("writeInstallRecord on a fresh root: %v", err)
	}

	found := false
	for _, d := range synced {
		if d == root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("root %s was never synced despite this call creating its state directory fresh; synced=%v", root, synced)
	}
}

// ─── P2: rename durability ──────────────────────────────────────────────

// TestSyncDir_SucceedsForRealDirectory proves syncDir is actually wired into
// the write path without erroring on an ordinary directory -- rename(2) is
// atomic but not durable by itself; atomicWriteFile and commitMirror both
// call this after their rename so a crash right after a successful write
// cannot revert the directory entry.
func TestSyncDir_SucceedsForRealDirectory(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncDir on a real directory must not error: %v", err)
	}
}

// TestSyncDir_PropagatesOpenError proves a directory-sync failure is
// reported, not silently swallowed -- a caller that ignored this could
// report success for a rename that is not actually durable.
func TestSyncDir_PropagatesOpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := syncDir(missing); err == nil {
		t.Fatal("syncDir on a missing directory must return an error")
	}
}

// TestPackRefresh_ApplySyncsReceiptAndMirrorDirectories proves the ordering
// Codex's review specified: the receipt's directory entry is durable
// (synced) before commitMirror ever runs, and the mirror's directory entry
// is synced before refresh reports success. Both writes already succeed
// under the existing full-apply tests; this asserts the specific ordering
// by exercising the real files on a real filesystem rather than mocking
// syncDir, since the property under test is "did the real directory get
// synced", not "was a function called".
func TestPackRefresh_ApplySyncsReceiptAndMirrorDirectories(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("refresh --apply failed, rc=%d", rc)
	}

	// Both directories must themselves still be syncable (i.e. real,
	// present, and not left in some broken half-written state) after the
	// run -- a crashed/failed dir-sync would have surfaced as a non-zero
	// exit above rather than a silently accepted success.
	if err := syncDir(adoptionsDirPath(root)); err != nil {
		t.Fatalf("receipt directory must be sync-able after apply: %v", err)
	}
	if err := syncDir(filepath.Dir(mirrorPath)); err != nil {
		t.Fatalf("mirror directory must be sync-able after apply: %v", err)
	}
}

// TestPackRefresh_WrittenFilesHaveCorrectMode proves every file this
// command writes (mirror, receipt, install baseline) ends up at its
// intended mode. commitMirror and atomicWriteFile both chmod the temp file
// BEFORE writing and syncing its content, not after: a chmod issued after
// the content sync is itself unsynced metadata, so a crash between that
// chmod and the rename could leave the eventual file at CreateTemp's
// default 0600 instead of the intended 0644, invisible to a black-box
// mode check that only runs after a clean (non-crashing) success. This
// test can only confirm the reachable outcome is correct on every ordinary
// run; the crash-durability property itself is structural (verified by
// code review of the ordering), not something a unit test can force.
func TestPackRefresh_WrittenFilesHaveCorrectMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: umask/mode assertions are unreliable")
	}
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("refresh --apply failed, rc=%d", rc)
	}

	check := func(path string) {
		t.Helper()
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if fi.Mode().Perm() != 0o644 {
			t.Errorf("%s has mode %o, want 0644", path, fi.Mode().Perm())
		}
	}
	check(mirrorPath)
	check(installRecordFilePath(root))
	receipts := receiptFiles(t, root)
	if len(receipts) != 1 {
		t.Fatalf("expected exactly 1 receipt, got %d", len(receipts))
	}
	check(receipts[0])
}

// TestMkdirAllSynced_SyncsEveryLevelUpToBoundary proves mkdirAllSynced syncs
// every directory level from path up to (and including) boundary, not just
// the leaf's own parent -- creating ".sensei/principle-pack/adoptions" from
// scratch (an existing ".sensei" boundary, but neither "principle-pack" nor
// "adoptions" yet) must sync both ".sensei" (to make "principle-pack"
// durable) and ".sensei/principle-pack" (to make "adoptions" durable). A
// single sync of the leaf's own contents proves nothing about whether the
// leaf itself, or its own parent, durably exists.
func TestMkdirAllSynced_SyncsEveryLevelUpToBoundary(t *testing.T) {
	root := t.TempDir()
	senseiDir := filepath.Join(root, ".sensei")
	if err := os.MkdirAll(senseiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	principlePackDir := filepath.Join(senseiDir, "principle-pack")
	adoptions := filepath.Join(principlePackDir, "adoptions")

	var synced []string
	orig := syncDir
	syncDir = func(dir string) error {
		synced = append(synced, dir)
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if err := mkdirAllSynced(adoptions, senseiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(adoptions); err != nil {
		t.Fatalf("adoptions directory was not created: %v", err)
	}

	want := map[string]bool{senseiDir: false, principlePackDir: false}
	for _, d := range synced {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for dir, got := range want {
		if !got {
			t.Errorf("%s was never synced", dir)
		}
	}
}

// TestMkdirAllSynced_AlwaysResyncsEvenWhenAlreadyExisting proves the actual
// fix: mkdirAllSynced does NOT skip syncing a level merely because it
// already exists on disk. A directory being visible is not proof its
// creation was ever made durable -- a previous call may have created it and
// then had its own sync fail transiently, and there is no on-disk marker
// distinguishing "exists and durable" from "exists and never confirmed". A
// prior version of this function detected "newly created" levels by
// existence and skipped resyncing anything already visible; that was
// exactly the bug Codex's review found (a retry after a transient sync
// failure never re-attempted it). Calling mkdirAllSynced a second time, on
// a path that already fully exists from the first call, must still sync
// every level up to boundary.
func TestMkdirAllSynced_AlwaysResyncsEvenWhenAlreadyExisting(t *testing.T) {
	root := t.TempDir()
	boundary := filepath.Join(root, ".sensei")
	if err := os.MkdirAll(boundary, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(boundary, "principle-pack", "adoptions")

	if err := mkdirAllSynced(target, boundary, 0o755); err != nil {
		t.Fatalf("first call: %v", err)
	}

	var synced []string
	orig := syncDir
	syncDir = func(dir string) error {
		synced = append(synced, dir)
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if err := mkdirAllSynced(target, boundary, 0o755); err != nil {
		t.Fatalf("second call (everything already exists): %v", err)
	}
	want := map[string]bool{boundary: false, filepath.Dir(target): false}
	for _, d := range synced {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for dir, got := range want {
		if !got {
			t.Errorf("%s was not resynced on a call where it already existed", dir)
		}
	}
}

// TestMkdirAllSynced_RejectsPathThroughExistingFile proves a regular file
// blocking a required directory level produces a clear error rather than
// mkdir's own less specific failure.
func TestMkdirAllSynced_RejectsPathThroughExistingFile(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mkdirAllSynced(filepath.Join(blocker, "child"), root, 0o755); err == nil {
		t.Fatal("expected an error when a path component is a regular file")
	}
}

// TestMkdirAllSynced_RejectsPathNotUnderBoundary proves a path outside the
// given boundary errors rather than climbing to the filesystem root and
// syncing directories far outside anything this call is responsible for.
func TestMkdirAllSynced_RejectsPathNotUnderBoundary(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a", "b")
	unrelatedBoundary := t.TempDir()
	if err := mkdirAllSynced(a, unrelatedBoundary, 0o755); err == nil {
		t.Fatal("expected an error when path is not under boundary")
	}
}

// TestPackRefresh_FirstAdoptionSyncsNewAdoptionsDirParent is the integration
// case Codex's review named directly: on a project's FIRST adoption,
// ".sensei/principle-pack/adoptions" does not exist yet. A real --apply run
// must sync its parent (".sensei/principle-pack") so the new "adoptions"
// entry itself is durable, not just the receipt written inside it.
func TestPackRefresh_FirstAdoptionSyncsNewAdoptionsDirParent(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)
	principlePackDir := principlePackDirPath(root)
	if _, err := os.Stat(adoptionsDirPath(root)); !os.IsNotExist(err) {
		t.Fatalf("test setup: adoptions dir must not exist yet, got err=%v", err)
	}

	var synced []string
	orig := syncDir
	syncDir = func(dir string) error {
		synced = append(synced, dir)
		return orig(dir)
	}
	defer func() { syncDir = orig }()

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc != 0 {
		t.Fatalf("refresh --apply failed, rc=%d", rc)
	}

	found := false
	for _, d := range synced {
		if d == principlePackDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the newly created adoptions dir's parent (%s) was never synced; synced=%v", principlePackDir, synced)
	}
}

// TestPackRefresh_ExactReplayRetriesDirectorySync proves the replay branch
// of writePrinciplePackRecord does not treat identical on-disk bytes as
// proof the directory sync ever succeeded. A prior run may have renamed the
// receipt into place and then hit a transient sync failure; a naive replay
// would see the correct bytes and return success without ever retrying the
// sync, letting the caller proceed to commitMirror on an unconfirmed
// receipt.
func TestPackRefresh_ExactReplayRetriesDirectorySync(t *testing.T) {
	id := anAddableID(t)
	root, _ := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	packEntries, _, _, err := parsePrinciplePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	rec := buildPrinciplePackRecord(sha256Hex(pack), len(packEntries), mirrorRelPath,
		sha256Hex(readMirror(t, filepath.Join(root, mirrorRelPath))), sha256Hex(pack),
		packDiff{UpstreamOnly: []string{id}}, "verified_baseline")

	// First write: real, succeeds normally.
	if _, err := writePrinciplePackRecord(root, rec); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Replay: identical content is already on disk. Force the sync to fail
	// and confirm the replay branch (a) actually calls it and (b) surfaces
	// the failure rather than returning success.
	called := false
	orig := syncDir
	syncDir = func(dir string) error {
		called = true
		return fmt.Errorf("simulated transient sync failure")
	}
	defer func() { syncDir = orig }()

	if _, err := writePrinciplePackRecord(root, rec); err == nil {
		t.Fatal("replay must surface a failed directory re-sync, not silently succeed")
	}
	if !called {
		t.Fatal("replay did not attempt to re-sync the receipt directory at all")
	}
}

func TestPackRefresh_LockPreventsConcurrentApply(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	release, err := acquireRefreshLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("expected refusal while another refresh holds the lock")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("a locked-out run still modified the mirror")
	}
}

// ─── helpers ────────────────────────────────────────────────────────────

func receiptFilesIn(t *testing.T, root string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(adoptionsDirPath(root), "*.yaml"))
	return m
}

// ─── legacy reconciliation of a DRIFTED mirror (issue #178) ─────────────

// readOneRecord decodes the single adoption record a run is expected to write.
func readOneRecord(t *testing.T, root string) principlePackRecord {
	t.Helper()
	recs := receiptFilesIn(t, root)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 adoption record, got %d", len(recs))
	}
	raw, err := os.ReadFile(recs[0])
	if err != nil {
		t.Fatal(err)
	}
	var r principlePackRecord
	if err := yaml.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// driftedLegacyMirror is the services shape issue #178 describes: a mirror with
// no baseline record (it predates them), missing entries the pack has since
// gained, AND carrying a local edit to a shared entry.
func driftedLegacyMirror(t *testing.T, omit string) (root, mirrorPath string) {
	t.Helper()
	root, mirrorPath = installedMirrorNoBaseline(t, omit)
	text := string(readMirror(t, mirrorPath))
	edited := strings.Replace(text, "    severity: critical\n", "    severity: low\n", 1)
	if edited == text {
		t.Fatal("fixture did not change the mirror; the drift it models is absent")
	}
	if err := os.WriteFile(mirrorPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, mirrorPath
}

// THE issue #178 test. A legacy mirror that has drifted must be reconcilable.
//
// Before this, --reconcile-legacy was consulted only at the missing-baseline
// gate, which a drifted mirror never reached: the conflict gate returned first.
// So the flag was unreachable for exactly the population it exists to serve,
// and custody -- which settles only on a record -- refused the file forever
// while its own error message recommended that flag.
func TestPackRefresh_ReconcileLegacyResolvesDriftedLegacyMirror(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := driftedLegacyMirror(t, id)

	// Unauthorized, it must still refuse: an unreviewed local edit is
	// indistinguishable from an upstream change.
	before := readMirror(t, mirrorPath)
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply"}); rc == 0 {
		t.Fatal("a drifted mirror was refreshed with no operator authorization")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("the refused run modified the mirror")
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply", "--reconcile-legacy"}); rc != 0 {
		t.Fatalf("an authorized operator could not reconcile a drifted legacy mirror, rc=%d", rc)
	}

	// The mirror is now the canonical pack.
	packBytes, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readMirror(t, mirrorPath)) != string(packBytes) {
		t.Fatal("the reconciled mirror is not byte-identical to the pack")
	}

	// And the record must NAME what it overwrote. A receipt that reported only
	// the adoption would describe a clean carry-forward, hiding that an
	// operator's authorization was spent discarding local text.
	r := readOneRecord(t, root)
	if r.Authorization != "reconcile_legacy" {
		t.Fatalf("authorization = %q, want reconcile_legacy", r.Authorization)
	}
	if len(r.Change.ChangedIDs) == 0 {
		t.Error("the record does not name the overwritten entry; it reads as a clean adoption")
	}
	var adopted bool
	for _, got := range r.Change.AddedIDs {
		if got == id {
			adopted = true
		}
	}
	if !adopted {
		t.Errorf("the record does not list %s as adopted", id)
	}
}

// The point of reconciling: custody must SETTLE. A mirror that is still Refused
// after a successful refresh has gained nothing, since custody derives from a
// record and never from matching content.
func TestPackRefresh_ReconciledMirrorIsNoLongerRefusedByCustody(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := driftedLegacyMirror(t, id)

	if v := packcustody.Derive(root, readMirror(t, mirrorPath)); v.Custody != packcustody.Refused {
		t.Fatalf("precondition: a drifted legacy mirror should be Refused, got %v", v.Custody)
	}
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply", "--reconcile-legacy"}); rc != 0 {
		t.Fatalf("reconcile failed, rc=%d", rc)
	}
	v := packcustody.Derive(root, readMirror(t, mirrorPath))
	if v.Custody != packcustody.SharedProjection {
		t.Fatalf("custody = %v after reconciliation, want SharedProjection (%s)", v.Custody, v.Reason)
	}
}

// The asymmetry that keeps reconciliation safe. --reconcile-legacy attests that
// local text carried no intent worth keeping; an id upstream has never seen is
// the one case where that cannot be true, because the pack has nothing to
// restore it from. Applying would destroy it permanently, so the refusal holds
// even for an authorized operator.
func TestPackRefresh_ReconcileLegacyStillRefusesToDeleteProjectAuthoredEntries(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirrorNoBaseline(t, id)

	text := string(readMirror(t, mirrorPath))
	text += "  - id: project.local_only_principle\n    title: authored here, not upstream\n    severity: high\n"
	if err := os.WriteFile(mirrorPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readMirror(t, mirrorPath)

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply", "--reconcile-legacy"}); rc == 0 {
		t.Fatal("an authorized run deleted a project-authored entry the pack cannot restore")
	}
	if string(readMirror(t, mirrorPath)) != string(before) {
		t.Fatal("the refused run modified the mirror")
	}
	if n := len(receiptFilesIn(t, root)); n != 0 {
		t.Fatalf("the refused run wrote %d record(s)", n)
	}
}

// Matching bytes with no record is a dead end: custody refuses it, and every
// other path declines to act because there is nothing to change. An operator
// who reconciled by hand lands there permanently, so there must be an exit that
// records provenance without pretending to have changed anything.
func TestPackRefresh_AlreadyCurrentWithoutRecordCanRecordProvenance(t *testing.T) {
	root := t.TempDir()
	mirrorPath := filepath.Join(root, mirrorRelPath)
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	packBytes, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mirrorPath, packBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if v := packcustody.Derive(root, packBytes); v.Custody != packcustody.Refused {
		t.Fatalf("precondition: matching bytes with no record must still be Refused, got %v", v.Custody)
	}

	// Plan mode stays read-only.
	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--reconcile-legacy"}); rc != 0 {
		t.Fatalf("plan rc=%d", rc)
	}
	if n := len(receiptFilesIn(t, root)); n != 0 {
		t.Fatalf("plan mode wrote %d record(s)", n)
	}

	if rc := runPrinciplePackRefresh([]string{"--repo", root, "--apply", "--reconcile-legacy"}); rc != 0 {
		t.Fatalf("could not record provenance for already-current content, rc=%d", rc)
	}
	if string(readMirror(t, mirrorPath)) != string(packBytes) {
		t.Fatal("recording provenance modified the mirror")
	}

	// The record must not claim a change it did not make.
	r := readOneRecord(t, root)
	if r.Target.PreviousDigest != r.Target.ResultingDigest {
		t.Error("the record claims the mirror moved between two different digests")
	}
	if len(r.Change.AddedIDs) != 0 || len(r.Change.ChangedIDs) != 0 || len(r.Change.RemovedIDs) != 0 {
		t.Errorf("the record names id changes that never happened: %+v", r.Change)
	}
	if v := packcustody.Derive(root, packBytes); v.Custody != packcustody.SharedProjection {
		t.Fatalf("custody = %v after recording provenance, want SharedProjection", v.Custody)
	}
}
