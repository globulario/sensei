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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	matches, _ := filepath.Glob(filepath.Join(root, adoptionsRelDir, "*.yaml"))
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
	if got := r.derivedDisposition(resulting); got != "applied" {
		t.Errorf("derived disposition %q, want applied", got)
	}
	if got := r.derivedDisposition(previous); got != "intent_open" {
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
	b, err := os.ReadFile(filepath.Join(root, installRecordPath))
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

// ─── P1: partial apply must be recoverable ──────────────────────────────

func TestPackRefresh_UnwritableRecordDirLeavesMirrorUntouched(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	before := readMirror(t, mirrorPath)

	// Make the adoptions directory unwritable so the record cannot be created.
	dir := filepath.Join(root, adoptionsRelDir)
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

func TestPackRefresh_ConflictingExistingRecordRefuses(t *testing.T) {
	id := anAddableID(t)
	root, mirrorPath := installedMirror(t, id)
	pack, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	// A record already at this digest name, but with different content.
	dir := filepath.Join(root, adoptionsRelDir)
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
	_, mirrorPath := installedMirror(t, id)
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

	err = commitMirror(mirrorPath, validated, pack)
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
	m, _ := filepath.Glob(filepath.Join(root, adoptionsRelDir, "*.yaml"))
	return m
}
