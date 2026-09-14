// SPDX-License-Identifier: AGPL-3.0-only

package main

// RE-REVIEW FINDING (P1, cmd_import.go:247): "Recompute publication inputs after bootstrap."
//
// A REGRESSION INTRODUCED BY MY OWN REPAIR at 49ae8399. That repair stopped offering
// `docs/awareness/generated` to AdmitPublication before anything created it -- correct in
// itself -- by snapshotting which planned roots EXIST. The snapshot is taken once, before
// stage 2, and the same frozen list is then reused by the pre-stage admission, the
// post-stage admission, and runBuild.
//
// On an ordinary FRESH repository none of the three roots exists yet, so the snapshot is
// empty, admission refuses "no corpus root resolved", and stage 2 -- the stage whose whole
// job is to create those roots -- never runs. The command refuses to bootstrap a repository
// because the thing it was about to create did not exist yet.
//
// THE QUANTIFIED DOMAIN OF THE OLD CLAIM, stated because getting this wrong is what caused
// the regression: my witnesses covered a checkout that ALREADY had docs/awareness. They
// said nothing about a fresh one, and I certified "a root a later stage has not written yet
// is not an input for this run" universally. Two facts were collapsed:
//
//	declared/admissible-before   != exists-and-publishable-after
//
// The first is knowable before mutation. The second cannot be, for output this command
// creates. They are separate questions asked at separate times.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// freshRepo is an ordinary repository with NO awareness corpus: the bootstrap case.
// generated/ and .sensei/project do not exist either. Committed clean, with a remote, so
// the extraction stages can actually run.
func freshRepo(t *testing.T, domain string) string {
	t.Helper()
	checkout := t.TempDir()
	// Real source, so structural extraction has something to represent.
	if err := os.MkdirAll(filepath.Join(checkout, "golang"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "golang", "thing.go"),
		[]byte("package thing\n\n// Thing does a thing.\nfunc Thing() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "sensei-repository.yaml"),
		[]byte("repository_identity: acme/fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"remote", "add", "origin", "https://example.com/acme/fresh.git"},
		{"add", "-A"}, {"-c", "commit.gpgsign=false", "commit", "-qm", "fresh"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	return checkout
}

// disposableDomainRegistry redirects HOME so DefaultDomainRegistryPath resolves to a
// throwaway registry, rather than inventing a production override for a test.
func disposableDomainRegistry(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const freshDomain = "example.com/acme/fresh"

func freshRegistry(t *testing.T) {
	disposableDomainRegistry(t, "domains:\n  "+freshDomain+":\n    repository_identity: acme/fresh\n"+
		"    allowed_corpus_roots:\n      - docs/awareness\n    allow_dirty_worktree: true\n")
}

// 1. THE REGRESSION. A fresh repository must reach the stage that creates its corpus.
// Before the repair this refused with "no corpus root resolved" and docs/awareness was
// never created.
func TestAFreshRepositoryReachesTheStageThatCreatesItsCorpus(t *testing.T) {
	checkout := freshRepo(t, freshDomain)
	freshRegistry(t)
	awareness := filepath.Join(checkout, "docs", "awareness")
	if _, err := os.Stat(awareness); !os.IsNotExist(err) {
		t.Fatalf("the fixture is not a fresh repository: %s already exists", awareness)
	}

	_, so, se := captureStdoutStderr(t, func() int {
		// A store URL that cannot serve, so the run ends at the LOAD step. The claim under
		// test is what happens before that: the corpus must be built and admitted.
		return runImport([]string{"--refresh", checkout, "--domain", freshDomain,
			"-depth", "basic", "--store-url", "http://127.0.0.1:1/store?default"})
	})
	out := so + se

	// The specific regression: refusing because the corpus this command creates was
	// absent when a pre-stage snapshot was taken.
	if strings.Contains(out, "no corpus root resolved") {
		t.Errorf("the command refused to bootstrap a fresh repository because its own output did not exist yet:\n%s", out)
	}
	// And the proof it is not merely a nicer message: the stage actually ran.
	if _, err := os.Stat(awareness); err != nil {
		t.Errorf("structural extraction never ran, so %s was never created: %v\n%s", awareness, err, out)
	}
}

// 2. A path a stage creates that the domain does NOT allow is still refused. The
// recomputation must not become a way for generated output to publish itself.
func TestAStageCreatedRootOutsideTheAllowlistIsStillRefused(t *testing.T) {
	checkout := freshRepo(t, freshDomain)
	// The domain publishes ONLY docs/awareness, so .sensei/project stays inadmissible
	// however it comes to exist.
	freshRegistry(t)
	if err := os.MkdirAll(filepath.Join(checkout, ".sensei", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".sensei", "project", "x.nt"), []byte("# generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	present, _ := existingCorpusInputs([]string{
		filepath.Join(checkout, "docs", "awareness"),
		filepath.Join(checkout, ".sensei", "project"),
	})
	kept, dropped := admissibleCorpusInputs(checkout, present, []string{"docs/awareness"})
	for _, k := range kept {
		if strings.Contains(k, ".sensei/project") {
			t.Errorf("an inadmissible root was kept because it exists: %s", k)
		}
	}
	joined := strings.Join(dropped, " ")
	if !strings.Contains(joined, ".sensei/project") {
		t.Errorf(".sensei/project was neither kept nor reported as dropped: %v", dropped)
	}
}

// 3. THE WIRING CLAIM, behaviourally. The real command must reach the LOAD step on a fresh
// repository, which is only possible if the admission it performs there was recomputed from
// the post-bootstrap filesystem. A source-text assertion about statement order proves the
// text, not the run -- the previous "wiring witness" for this filter was exactly that, and
// it is why the regression shipped.
func TestTheImportCommandAdmitsTheRecomputedInputsAtTheLoadStep(t *testing.T) {
	checkout := freshRepo(t, freshDomain)
	freshRegistry(t)
	_, so, se := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", checkout, "--domain", freshDomain,
			"-depth", "basic", "--store-url", "http://127.0.0.1:1/store?default"})
	})
	out := so + se
	if strings.Contains(out, "PUBLICATION_REFUSED") {
		t.Fatalf("the load step refused the recomputed input set on a fresh repository:\n%s", out)
	}
	// Reaching the load means admission passed over a NON-EMPTY set built after the
	// stages ran; the load then fails only because no store is listening.
	if !strings.Contains(out, "== [5/5] load domain-scoped slice ==") {
		t.Errorf("the run never reached the load step, so the recomputed admission was never exercised:\n%s", out)
	}
	if !strings.Contains(out, "load failed") {
		t.Errorf("expected the load itself to fail against an unreachable store, not an earlier refusal:\n%s", out)
	}
}

// 4. Every admission fact knowable before extraction refuses before extraction, with the
// checkout untouched. This is review finding cmd_import.go:185, proven per cause rather
// than argued: an unreadable registry, an unregistered domain, and a checkout belonging to
// another repository were each discovered only after stage 1 had written.
func TestAdmissionFactsKnowableBeforeExtractionRefuseBeforeExtraction(t *testing.T) {
	cases := map[string]struct {
		registry string
		expect   string
	}{
		"unreadable registry": {registry: "domains:\n  - this is not a mapping\n    broken: [\n", expect: "domain registry"},
		"unregistered domain": {registry: "domains:\n  example.com/other/repo:\n    repository_identity: other/repo\n", expect: "not registered"},
		// allow_dirty_worktree keeps the SELF-DEFEAT gate from firing first: both are
		// legitimate pre-mutation refusals, and this case is about the identity one.
		"checkout belongs to another repository": {
			registry: "domains:\n  " + freshDomain + ":\n    repository_identity: someone-else/thing\n" +
				"    allowed_corpus_roots:\n      - docs/awareness\n    allow_dirty_worktree: true\n",
			expect: "identity mismatch"},
		"domain publishes none of the roots this run would write": {
			registry: "domains:\n  " + freshDomain + ":\n    repository_identity: acme/fresh\n" +
				"    allowed_corpus_roots:\n      - docs/somewhere-else\n",
			expect: "allowed roots"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			checkout := freshRepo(t, freshDomain)
			disposableDomainRegistry(t, tc.registry)
			before := porcelain(t, checkout)

			_, so, se := captureStdoutStderr(t, func() int {
				return runImport([]string{"--refresh", checkout, "--domain", freshDomain,
					"-depth", "full", "--store-url", "http://127.0.0.1:1/store?default"})
			})
			out := so + se
			if !strings.Contains(out, tc.expect) {
				t.Errorf("the refusal does not name the cause %q:\n%s", tc.expect, out)
			}
			// --depth full runs stage 1 (intent-mine --adopt), which writes under
			// docs/awareness. The refusal must precede it.
			if after := porcelain(t, checkout); after != before {
				t.Errorf("extraction wrote before the refusal:\nbefore=%q\nafter=%q", before, after)
			}
			if _, err := os.Stat(filepath.Join(checkout, "docs", "awareness")); err == nil {
				t.Errorf("docs/awareness was created before a refusal whose cause was knowable beforehand")
			}
		})
	}
}

// 5. A root that exists but is DIRTY is still refused, and that refusal belongs to the
// post-generation admission: dirtiness is a property of content, so it is not knowable for
// a directory the run has not created yet. The domain here forbids a dirty worktree.
func TestADirtyGovernedRootIsStillRefusedAtPublication(t *testing.T) {
	checkout := freshRepo(t, freshDomain)
	// Committed corpus, then an uncommitted edit to it.
	awareness := filepath.Join(checkout, "docs", "awareness")
	if err := os.MkdirAll(awareness, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awareness, "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "commit.gpgsign=false", "commit", "-qm", "corpus"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(awareness, "invariants.yaml"), []byte("invariants: [{id: x}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// allow_dirty_worktree is absent, so a dirty corpus must refuse.
	disposableDomainRegistry(t, "domains:\n  "+freshDomain+":\n    repository_identity: acme/fresh\n"+
		"    allowed_corpus_roots:\n      - docs/awareness\n")

	_, so, se := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", checkout, "--domain", freshDomain,
			"-depth", "basic", "--store-url", "http://127.0.0.1:1/store?default"})
	})
	out := so + se
	if !strings.Contains(out, "uncommitted changes") {
		t.Errorf("a dirty governed corpus was not refused:\n%s", out)
	}
	// A root that ALREADY EXISTS is fully judgeable before mutation, cleanliness included,
	// so this refusal must precede extraction rather than follow four stages of writing.
	if !strings.Contains(out, "== [2/5] structural extraction ==") {
		return // refused before extraction, which is the requirement
	}
	t.Errorf("extraction ran before a refusal whose cause (a dirty existing corpus) was true before the command started:\n%s", out)
}
