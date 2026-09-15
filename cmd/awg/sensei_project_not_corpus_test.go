// SPDX-License-Identifier: AGPL-3.0-only

package main

// The architectural decision, turned into an enforced invariant.
//
// `.sensei/project` is an IGNORED LOCAL WORKING / CACHE / STAGING AREA. It is not
// source corpus, not an allowed corpus root, and its contents must never define
// published graph identity.
//
// That was already true by convention — no registered domain lists it — but nothing
// enforced it. `importWouldDefeatItself` refuses a run whose own extraction dirties a
// root the domain publishes, and it consulted `extractionWriteRoots`, which named only
// `docs/awareness`. Import demonstrably writes `.sensei/project` too: graph.nt,
// claims.yaml, knowledge/adoption-report.yaml, protection-coverage.yaml, the
// `project-invalid-<txID>` quarantine directory and project.lock.
//
// So an operator who added `.sensei/project` to a domain's allowed_corpus_roots would
// have gotten no refusal at all: the import would proceed to publish a directory it
// rewrites as it runs. That is the precise failure the self-defeat guard exists to
// prevent, in the one root the guard could not see.
//
// Measured while deciding this (2026-09-13): sensei's `.sensei/project` is 963 MB
// across 34 files, of which nine are near-duplicate intermediate claim sets at ~79 MB
// each. Publishing that tree would put pipeline intermediates into the graph.

import (
	"strings"
	"testing"
)

// `.sensei/project` must be recognised as a place extraction writes, so admitting it as
// corpus is refused rather than silently accepted.
func TestTheProjectWorkingAreaIsAnExtractionWriteRoot(t *testing.T) {
	var found bool
	for _, r := range extractionWriteRoots {
		if r == ".sensei/project" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extractionWriteRoots = %v; .sensei/project is written by every import (graph.nt, claims.yaml, the quarantine dir) and must be named here, or admitting it as corpus is accepted in silence", extractionWriteRoots)
	}
}

// The teeth: a domain that admits the working area AND requires a clean tree — the
// default, and what every registered domain does — cannot run an import at all.
func TestAdmittingTheProjectWorkingAreaAsCorpusIsRefused(t *testing.T) {
	err := importWouldDefeatItself([]string{".sensei/project"}, false)
	if err == nil {
		t.Fatal("a domain admitting .sensei/project as corpus was allowed to import; the run would publish a directory it rewrites as it runs")
	}
	if !strings.Contains(err.Error(), ".sensei/project") {
		t.Errorf("the refusal does not name the root it refuses: %v", err)
	}
	// It must say nothing was run, because the whole point is refusing before the
	// extraction dirties anything.
	if !strings.Contains(err.Error(), "nothing has been run") {
		t.Errorf("the refusal does not state that nothing was run: %v", err)
	}
}

// The docs/awareness case must keep working exactly as before: this adds a root, it
// does not change the existing contradiction.
func TestTheAwarenessCorpusRefusalIsUnchanged(t *testing.T) {
	if err := importWouldDefeatItself([]string{"docs/awareness"}, false); err == nil {
		t.Error("the original self-defeat refusal regressed")
	}
}

// A domain that admits NEITHER root is unaffected — the guard must not become a blanket
// refusal of every import.
func TestADomainAdmittingNeitherRootStillImports(t *testing.T) {
	if err := importWouldDefeatItself([]string{"some/other/corpus"}, false); err != nil {
		t.Errorf("an unrelated corpus root was refused: %v", err)
	}
}

// And the escape remains exactly where the registry puts it: a domain that explicitly
// permits a dirty worktree is not contradicting itself. This test exists to prove the
// new root inherits that rule rather than becoming an unconditional block.
func TestADirtyPermittingDomainIsStillNotDefeated(t *testing.T) {
	if err := importWouldDefeatItself([]string{".sensei/project"}, true); err != nil {
		t.Errorf("a domain permitting a dirty worktree was refused: %v", err)
	}
}
