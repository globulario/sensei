// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/benchmark"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "eval@example.test"},
		{"config", "user.name", "eval"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

// A clean checkout is identified by the commit it actually is.
func TestWorldBindingCleanTreeBindsRevision(t *testing.T) {
	root := initRepo(t)
	binding, err := worldBinding(root, "example.test/clean")
	if err != nil {
		t.Fatalf("worldBinding: %v", err)
	}
	if binding.RevisionStatus != architecture.RevisionResolved || binding.Revision == "" {
		t.Fatalf("clean tree must bind its revision, got status=%s revision=%q", binding.RevisionStatus, binding.Revision)
	}
	if binding.TreeDigestSHA256 != "" {
		t.Fatalf("clean tree must not need a tree digest, got %q", binding.TreeDigestSHA256)
	}
}

// A dirty checkout was not the commit HEAD names, so it must not claim to be:
// the measurement binds to the tree that was read (#216).
func TestWorldBindingDirtyTreeRefusesRevisionAndBindsTreeDigest(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binding, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatalf("worldBinding: %v", err)
	}
	if binding.Revision != "" {
		t.Fatalf("dirty tree must not claim a revision, got %q", binding.Revision)
	}
	if binding.RevisionStatus != architecture.RevisionUnavailable {
		t.Fatalf("dirty tree revision status = %s, want unavailable", binding.RevisionStatus)
	}
	if binding.TreeDigestSHA256 == "" {
		t.Fatal("dirty tree must be identified by its tree digest")
	}
}

// The digest must follow the content, or two different working trees would
// report the same identity.
func TestWorldBindingTreeDigestFollowsContent(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatal(err)
	}
	if first.TreeDigestSHA256 == second.TreeDigestSHA256 {
		t.Fatal("two different working trees produced the same tree digest")
	}
}

// Computing a dirty tree's identity must not disturb the repository it reads.
func TestWorldBindingLeavesRepositoryIndexAlone(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worldBinding(root, "example.test/dirty"); err != nil {
		t.Fatal(err)
	}
	after, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("worldBinding staged the caller's work: %q -> %q", before, after)
	}
}

// A report naming this machine's checkout is not comparable with the same
// world measured elsewhere.
func TestRelocateRootRemovesCheckoutLocation(t *testing.T) {
	got := relocateRoot("/home/somebody/checkout", "read /home/somebody/checkout/docs/generated: is a directory")
	if want := "read <repo>/docs/generated: is a directory"; got != want {
		t.Fatalf("relocateRoot = %q, want %q", got, want)
	}
	if got := relocateRoot("/home/somebody/checkout", "scanned /home/somebody/checkout"); got != "scanned <repo>" {
		t.Fatalf("bare root not relocated: %q", got)
	}
}

func TestParseWorldSpecRejectsIncompleteSpec(t *testing.T) {
	if _, _, _, err := parseWorldSpec("name=domain"); err == nil {
		t.Fatal("a spec without a path must be refused")
	}
	if _, _, _, err := parseWorldSpec("=domain=/path"); err == nil {
		t.Fatal("a spec without a name must be refused")
	}
	name, domain, path, err := parseWorldSpec("world2=github.com/x/y=/tmp/z")
	if err != nil || name != "world2" || domain != "github.com/x/y" || path != "/tmp/z" {
		t.Fatalf("parseWorldSpec = %q %q %q %v", name, domain, path, err)
	}
}

// A world may not claim the name of an arm this command writes itself, or of
// another world in the same run: the second report would overwrite the first
// after its digest was already recorded in the index.
func TestReservedArmNamesMatchTheArmsActuallyWritten(t *testing.T) {
	for _, name := range []string{
		"deterministic_extraction_without_composition",
		"phase10_composition_model_disabled",
		"phase10_composition_model_bound",
		"briefing_and_impact_surfaces",
	} {
		if !reservedArmNames[name] {
			t.Fatalf("arm %q is written by this command but not reserved against a world claiming it", name)
		}
	}
}

// Every world #131 defines appears in the index whether it ran or not.
func TestEveryRequiredWorldIsAccountedFor(t *testing.T) {
	arts := runWorlds(t.TempDir(), nil, "2026-01-01T00:00:00Z", map[string]int64{}, nil)
	seen := map[string]string{}
	for _, a := range arts {
		seen[a.Arm] = a.Status
	}
	for _, name := range requiredWorlds {
		// The mutant suite is a required world that is not a CHECKOUT: it is
		// materialized by the harness, so runWorlds neither runs nor reports
		// it, and main records its status from the arm that extracts over it.
		// Listing it here would tell an operator to supply a --world for a
		// repository that does not exist.
		if name == worldMutantSuite {
			if _, listed := seen[name]; listed {
				t.Fatalf("runWorlds reported %q, which it does not own", name)
			}
			continue
		}
		if seen[name] != statusNotRun {
			t.Fatalf("world %q status = %q, want %q", name, seen[name], statusNotRun)
		}
	}
}

func TestDuplicateWorldNameIsRefused(t *testing.T) {
	dir := t.TempDir()
	specs := []string{
		"world3_independent_calibration=a/b=" + dir,
		"world3_independent_calibration=c/d=" + dir,
	}
	arts := runWorlds(dir, specs, "2026-01-01T00:00:00Z", map[string]int64{}, nil)
	failed := 0
	for _, a := range arts {
		if a.Status == statusFailed && strings.Contains(a.Reason, "collides") {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("duplicate world name was not refused exactly once: %+v", arts)
	}
}

// An ignored .go file is still compiled into the semantic input. `git status`
// does not list it and `git add -A` skips it, so neither the revision nor the
// tree digest covers it: an identity that excludes part of its own input would
// let the report change while claiming to be the same measurement.
func TestWorldBindingRefusesAnIgnoredSemanticInput(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("hidden.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "go"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := worldBinding(root, "example.test/clean"); err != nil {
		t.Fatalf("a clean tree with no ignored input must bind: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hidden.go"), []byte("package p\n\nconst X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := worldBinding(root, "example.test/ignored")
	if err == nil {
		t.Fatal("an ignored semantic input was bound to an identity that does not cover it")
	}
	if !strings.Contains(err.Error(), "git-ignored") {
		t.Fatalf("refusal does not name the reason: %v", err)
	}
}

// The four unknown-ish statuses are different worlds, and each carries its own
// obligation: an absence must say why, and a claim to have searched and found
// nothing must show the tree it searched. A metric that cannot detect a
// violation would report integrity it never checked.
func TestAbsenceIntegrityDetectsAnUnexplainedAbsence(t *testing.T) {
	got := checkAbsenceIntegrity([]investigation.CoverageEntry{
		{ProviderID: "p.silent", Status: investigation.CoverageUnavailable},
		{ProviderID: "p.explained", Status: investigation.CoverageUnavailable, Reason: "the provider is not installed"},
	})
	if got.AbsenceClaims != 2 {
		t.Fatalf("absence claims = %d, want 2", got.AbsenceClaims)
	}
	if got.Unexplained != 1 {
		t.Fatalf("unexplained = %d, want 1", got.Unexplained)
	}
	if len(got.Examples) != 1 || !strings.Contains(got.Examples[0], "p.silent") {
		t.Fatalf("the unexplained absence is not named: %v", got.Examples)
	}
}

// searched_no_result asserts that a search happened. Without a snapshot digest
// there is nothing showing it did, and "we looked and found nothing" is a much
// stronger claim than "we did not look".
func TestSearchedNoResultWithoutASnapshotIsNotProof(t *testing.T) {
	got := checkAbsenceIntegrity([]investigation.CoverageEntry{
		{ProviderID: "p.unproven", Status: investigation.CoverageNoResult},
		{ProviderID: "p.proven", Status: investigation.CoverageNoResult, SourceSnapshotDigestSHA256: strings.Repeat("a", 64)},
	})
	if got.SearchedWithoutProof != 1 {
		t.Fatalf("searched-without-proof = %d, want 1", got.SearchedWithoutProof)
	}
	if len(got.Examples) != 1 || !strings.Contains(got.Examples[0], "p.unproven") {
		t.Fatalf("the unproven search is not named: %v", got.Examples)
	}
}

// A positive result is not an absence claim and must not be audited as one.
func TestPositiveCoverageIsNotCountedAsAnAbsence(t *testing.T) {
	got := checkAbsenceIntegrity([]investigation.CoverageEntry{
		{ProviderID: "p.found", Status: investigation.CoverageSupporting},
		{ProviderID: "p.refuted", Status: investigation.CoverageRefuting},
		{ProviderID: "p.mixed", Status: investigation.CoverageMixed},
	})
	if got.AbsenceClaims != 0 || got.Unexplained != 0 || len(got.Examples) != 0 {
		t.Fatalf("a positive result was audited as an absence: %+v", got)
	}
}

// skipped_with_reason and not_configured carry the same obligation as
// unavailable: the status names a kind of absence, not an excuse from stating
// one.
func TestEveryAbsenceStatusMustCarryAReason(t *testing.T) {
	for _, status := range []investigation.CoverageStatus{
		investigation.CoverageUnavailable,
		investigation.CoverageNotConfigured,
		investigation.CoverageSkipped,
	} {
		got := checkAbsenceIntegrity([]investigation.CoverageEntry{{ProviderID: "p", Status: status}})
		if got.Unexplained != 1 {
			t.Fatalf("status %s without a reason went unreported: %+v", status, got)
		}
	}
}

// TestIndexBindsTheEvaluatingAuthority pins the Sensei-side half of a run's
// identity into the index.
//
// index.Revision already binds the evaluator's checkout (#216), but a checkout
// is a different claim from the authority that answered: it can be advanced
// without rebuilding, and it names neither the compiled seed nor the authored
// corpus consulted. #131 requires every run to bind "revision/tree, graph
// digest/status, policy/profile, provider versions"; without this the eval
// path recorded only the first.
//
// The block is asserted to live in the index, which carries no digest, so it
// cannot perturb the per-arm replay identity CI compares.
func TestIndexBindsTheEvaluatingAuthority(t *testing.T) {
	idx := newIndex("2026-01-01T00:00:00Z", "example.com/eval")
	if idx.Authority == nil {
		t.Fatal("the index records no evaluating authority; a run bound only to a target repository cannot be certified against another")
	}
	switch idx.Authority.CaptureState {
	case benchmark.AuthorityCaptureBound:
		// A bound authority must actually carry the identities, not just the state.
		for name, got := range map[string]string{
			"sensei_revision":        idx.Authority.SenseiRevision,
			"seed_digest":            idx.Authority.SeedDigestSHA256,
			"authored_corpus_digest": idx.Authority.AuthoredCorpusDigestSHA256,
			"transaction_stamp":      idx.Authority.TransactionStampSHA256,
		} {
			if strings.TrimSpace(got) == "" {
				t.Errorf("authority reports %q but carries no %s", benchmark.AuthorityCaptureBound, name)
			}
		}
	case benchmark.AuthorityCaptureUnavailable:
		// Unavailable is a legitimate outcome, but never a silent one.
		if strings.TrimSpace(idx.Authority.CaptureReason) == "" {
			t.Error("an unavailable authority carries no typed reason")
		}
	default:
		t.Errorf("authority capture state %q is outside the closed vocabulary", idx.Authority.CaptureState)
	}
}

// TestPublishedArmRecordsTheAnsweringServersAuthority: arm 4's measurements
// come from the server at --addr, not from the local checkout, so binding only
// the local evaluator would let two runs against DIFFERENT remote authorities
// carry identical authority blocks and read as comparable.
//
// The unobserved case is what this pins hardest: when no impact response is
// obtained, the report must say so rather than omit the block, because a
// missing block would read as a server whose authority happened to match.
func TestPublishedArmRecordsTheAnsweringServersAuthority(t *testing.T) {
	// No server is listening. gRPC dials lazily, so the arm proceeds and every
	// call fails — which is exactly the case that must not silently omit the
	// remote authority block.
	out := t.TempDir()
	runPublishedSurfaces(out, "127.0.0.1:1", "example.com/published", []string{"a.go"}, map[string]int64{})

	data, err := os.ReadFile(filepath.Join(out, armBriefingImpactSurfaces+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var report publishedSurfaceReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.RemoteAuthority == nil {
		t.Fatal("the published arm recorded no remote authority; its measurements come from --addr, not from the local checkout")
	}
	if report.RemoteAuthority.Observed {
		t.Error("no server answered, yet the remote authority is recorded as observed")
	}
	if strings.TrimSpace(report.RemoteAuthority.Reason) == "" {
		t.Error("an unobserved remote authority carries no typed reason")
	}

	// And the typed-absence contract at the unit that builds the block.
	if got := observedRemoteAuthority(nil); got == nil || got.Observed {
		t.Fatal("a response carrying no authority stamp must be recorded as unobserved, never omitted")
	} else if strings.TrimSpace(got.Reason) == "" {
		t.Error("an unobserved remote authority carries no typed reason")
	}
}

// TestModelArmIsImplementedButNotRunWithoutAProvider is the status change #258
// asks for, and the reason it is not cosmetic.
//
// `not_implemented_in_evaluated_path` and `not_run` say different things: one
// is "no such behaviour exists to measure", the other is "this run did not
// ask". The first stopped being true when the execution path and a real
// adapter landed. Continuing to report it would understate the system, and
// reporting it while the capability exists would be false.
func TestModelArmIsImplementedButNotRunWithoutAProvider(t *testing.T) {
	art := runModelBoundArm(t.TempDir(), "2026-01-01T00:00:00Z", modelArmConfig{}, map[string]int64{})
	if art.Status == statusNotImplemented {
		t.Fatalf("arm 3 still reports %q, but the evaluated path can now invoke the capability", statusNotImplemented)
	}
	if art.Status != statusNotRun {
		t.Fatalf("status = %q, want %q", art.Status, statusNotRun)
	}
	if !strings.Contains(art.Reason, "did not bind a provider") {
		t.Errorf("reason %q does not say why the arm did not run", art.Reason)
	}
}

// A partially configured provider is still not a provider. Half a binding must
// not launch anything, and must not look like a measurement.
func TestModelArmRefusesAPartialProviderBinding(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  modelArmConfig
	}{
		{"no path", modelArmConfig{ProviderID: "bridge", ModelName: "m"}},
		{"no model", modelArmConfig{ProviderID: "bridge", ProviderPath: "/bin/true"}},
		{"no provider id", modelArmConfig{ProviderPath: "/bin/true", ModelName: "m"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if art := runModelBoundArm(t.TempDir(), "2026-01-01T00:00:00Z", tc.cfg, map[string]int64{}); art.Status != statusNotRun {
				t.Errorf("status = %q, want %q", art.Status, statusNotRun)
			}
		})
	}
}

// The evaluator must not be able to state a terminal model outcome. The
// strongest form of the proof is structural: there is no field for it.
func TestEvaluationConfigCannotStateATerminalModelOutcome(t *testing.T) {
	blob, err := json.Marshal(modelArmConfig{ProviderID: "bridge", ProviderPath: "/bin/true", ModelName: "m"})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"resolved", "status", "artifact_digest", "request_digest"} {
		if strings.Contains(strings.ToLower(string(blob)), forbidden) {
			t.Errorf("the evaluation arm's configuration carries %q; a terminal outcome is execution evidence, not a setting", forbidden)
		}
	}
}

// TestAPathLikeWorldNameIsRefusedBeforeAnythingIsWritten.
//
// A world name becomes a filename — its report, and its blinded views — so a
// name carrying a separator or a parent reference writes outside the output
// directory the run was given. The guard in evalsample.Build is too late: the
// report is written first, and on a seedless run Build is never reached at
// all. This is the parse seam every caller passes.
func TestAPathLikeWorldNameIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape", "a/b", "..", "ok/../bad"} {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			arts := runWorlds(out, []string{name + "=example.com/x=" + dir}, "2026-01-01T00:00:00Z", map[string]int64{}, nil)
			refused := false
			for _, a := range arts {
				if a.Status == statusFailed && strings.Contains(a.Reason, "is a path, not a name") {
					refused = true
				}
			}
			if !refused {
				t.Fatalf("world name %q was not refused: %+v", name, arts)
			}
			// Nothing may have been written for it.
			if entries, err := os.ReadDir(out); err == nil {
				for _, e := range entries {
					if strings.Contains(e.Name(), "escape") || strings.Contains(e.Name(), "bad") {
						t.Errorf("a refused world still wrote %s", e.Name())
					}
				}
			}
		})
	}
}
