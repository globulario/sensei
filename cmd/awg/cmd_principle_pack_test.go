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
	matches, _ := filepath.Glob(filepath.Join(root, receiptRelDir, "*.yaml"))
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
	var r principlePackReceipt
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
	if r.Disposition != "applied" {
		t.Errorf("disposition %q, want applied", r.Disposition)
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
