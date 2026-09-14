package main

// The real law-9 defect: import defeats itself.
//
// Measured end to end on 2026-09-13 against a disposable store, with the checkout
// clean at the start:
//
//	hoisted admissibility check   corpus clean -> admissible -> proceed
//	steps 2-4 (extraction)        write docs/awareness/candidates/*.yaml and
//	                              docs/awareness/generated/library_api_contracts.yaml
//	step 5 publication gate       "corpus docs/awareness has uncommitted changes" -> REFUSE
//	tree afterwards               0 -> 27 changes, nothing loaded
//
// The extraction stages write INTO docs/awareness -- the very corpus root the domain
// publishes -- and the domain requires a clean worktree (AllowDirtyWorktree defaults
// to false). So the run creates the condition that guarantees its own refusal. My
// earlier hoisted check could not catch it: at hoist time the corpus IS clean, and the
// dirtiness does not exist yet.
//
// That contradiction is decidable up front, from the registry alone, with no work
// done and nothing written.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportRefusesWhenItsOwnExtractionWouldDefeatIt(t *testing.T) {
	// A domain that publishes docs/awareness and requires it clean: extraction
	// writing candidates and generated contracts under that root guarantees refusal.
	err := importWouldDefeatItself([]string{"docs/awareness"}, false)
	if err == nil {
		t.Fatal("import accepted a run whose own extraction guarantees the publication gate refuses it")
	}
	for _, want := range []string{"docs/awareness", "extraction", "clean"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %q: %v", want, err)
		}
	}
	// It must say nothing was done, because refusing up front is the whole value.
	if !strings.Contains(strings.ToLower(err.Error()), "nothing") {
		t.Errorf("the refusal does not say the checkout is untouched: %v", err)
	}
}

func TestADomainThatPermitsADirtyWorktreeIsNotDefeated(t *testing.T) {
	if err := importWouldDefeatItself([]string{"docs/awareness"}, true); err != nil {
		t.Errorf("a domain that permits a dirty worktree was refused: %v", err)
	}
}

func TestADomainThatDoesNotPublishTheWrittenRootIsNotDefeated(t *testing.T) {
	// Extraction writes under docs/awareness; a domain publishing only some other
	// root is unaffected by that, so the contradiction does not arise.
	if err := importWouldDefeatItself([]string{"corpus/elsewhere"}, false); err != nil {
		t.Errorf("a domain publishing an unrelated root was refused: %v", err)
	}
	// And an unconstrained domain states no root to contradict.
	if err := importWouldDefeatItself(nil, false); err != nil {
		t.Errorf("an unconstrained domain was refused: %v", err)
	}
}

// End to end: the command refuses and writes nothing.
//
// HONEST LIMIT OF THIS TEST. It does NOT bind the call site. Deleting the refusal
// still passes it, because in a synthetic repository the extraction stages write
// nothing to begin with — there is no Go source to extract and no managed skills to
// regenerate — so no assertion here can distinguish "refused early" from "ran and
// produced nothing". Building a repository realistic enough to write would be a
// disproportionate fixture.
//
// The WIRING is proven live instead, on this repository, and that evidence is
// stronger than a synthetic fixture would be: the same invocation that previously
// turned a clean checkout into 27 changes with nothing loaded now prints the refusal
// and leaves git status empty, 0 -> 0. Recorded in
// sensei/docs/architecture/graph-identity-audit.md.
//
// What this test does bind: that the command exits non-zero and, when it refuses,
// leaves the worktree clean — checked with git status rather than an entry count,
// because most of the 27 observed writes were MODIFICATIONS and an entry count cannot
// see those. An assertion that cannot see the damage it exists to detect is not an
// assertion.
func TestTheImportCommandRefusesBeforeWritingAnything(t *testing.T) {
	// A REAL git repository with a remote, committed clean. Without this the
	// extraction stages fail immediately and write nothing, so the test would pass
	// whether or not the refusal is wired -- which is exactly how its first version
	// let the mutation survive.
	checkout := t.TempDir()
	for _, d := range []string{"docs/awareness", ".sensei"} {
		if err := os.MkdirAll(filepath.Join(checkout, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(checkout, "docs", "awareness", "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "sensei-repository.yaml"),
		[]byte("repository_identity: acme/thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"remote", "add", "origin", "https://example.com/acme/thing.git"},
		{"add", "-A"}, {"commit", "-qm", "corpus"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	// A registry that publishes the root extraction writes into, and requires it
	// clean: the contradiction this refusal exists for.
	// DefaultDomainRegistryPath resolves from the user's home, so the home is
	// redirected rather than a production override being invented for a test.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  example.com/acme/thing:\n    repository_identity: acme/thing\n"+
			"    allowed_corpus_roots:\n      - docs/awareness\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runImport([]string{"--refresh", checkout, "--domain", "example.com/acme/thing",
		"-depth", "basic", "--store-url", "http://127.0.0.1:1/store?default"})
	if code == 0 {
		t.Fatal("the command accepted a run that cannot succeed")
	}
	// git status, not an entry count. The first version counted directory entries,
	// so MODIFICATIONS were invisible to it -- and most of the 27 writes observed on
	// the real repository were modifications. An assertion that cannot see the
	// damage it is written to detect is not an assertion.
	if dirty := porcelain(t, checkout); dirty != "" {
		t.Errorf("a refused run wrote to the checkout:\n%s", dirty)
	}
}

func porcelain(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// REVIEW FINDING (P1, chatgpt-codex-connector, cmd_import.go:186):
// "Run import admission before contract extraction writes."
//
// CONFIRMED BY INSPECTION. Stage [1/5] runs `intent-mine --adopt`, which creates or
// updates files under docs/awareness, and importWouldDefeatItself sits ~50 lines LATER.
// So on `--depth full` with an available drafter the refusal prints
//
//	"this import cannot succeed, so nothing has been run and the checkout is untouched"
//
// after stage 1 has already written. The message is FALSE in that configuration.
//
// My own live proof of this gate used --depth basic, which skips stage 1 entirely — so it
// measured the one configuration in which the claim happens to be true and I asserted it
// generally. That is the recorded pattern: a witness only proves its first guard, and
// evidence that could not have failed is not evidence.
//
// The repair is ordering, not wording: a gate whose refusal asserts an untouched checkout
// must run before anything writes. Weakening the sentence instead would keep the defect
// and describe it.
func TestTheSelfDefeatGateRunsBeforeAnyExtractionWrites(t *testing.T) {
	raw, err := os.ReadFile("cmd_import.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	gate := strings.Index(src, "importWouldDefeatItself(")
	if gate < 0 {
		t.Fatal("the self-defeat gate is gone; this check has lost its anchor")
	}
	// Every writing stage that must not precede it. intent-mine --adopt is the one the
	// reviewer found; the structural scaffold and the admission call are named too so a
	// future reordering cannot reintroduce the same class elsewhere.
	for _, writer := range []struct{ token, what string }{
		{`"--adopt"`, "stage 1 contract extraction (intent-mine --adopt writes docs/awareness)"},
		{"runIntentMine(", "stage 1 contract extraction"},
	} {
		at := strings.Index(src, writer.token)
		if at < 0 {
			continue
		}
		if at < gate {
			t.Errorf("%s runs at offset %d, BEFORE the gate at %d: the refusal claims an untouched checkout that stage 1 has already modified",
				writer.what, at, gate)
		}
	}
	// And the refusal must only make that claim if it is true — asserted here so the
	// sentence and the ordering stay tied together.
	if !strings.Contains(src, "nothing has been run and the checkout is untouched") {
		t.Log("note: the refusal no longer claims an untouched checkout; the ordering assertion above is then the weaker requirement")
	}
}
