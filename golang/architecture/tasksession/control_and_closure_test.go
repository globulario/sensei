// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"gopkg.in/yaml.v3"
)

// TestResolveControlAndClosure_BeforePublishedGeneration covers a task
// that has never run advance-task: control/latest-generation.yaml does not
// exist yet, so both the control state and the closure report must come
// from the prepare-time base paths.
func TestResolveControlAndClosure_BeforePublishedGeneration(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)

	state, report, decision, err := ResolveControlAndClosure(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveControlAndClosure: %v", err)
	}
	if state.TaskID == "" {
		t.Fatal("expected a non-empty task control state")
	}
	if report.SchemaVersion == "" {
		t.Fatal("expected a real closure report, got a zero value")
	}
	if decision.SchemaVersion == "" {
		t.Fatal("expected a real admission decision, got a zero value")
	}
}

// TestResolveControlAndClosure_AfterPublishedGeneration covers the
// generation-scoped case: once advance-task has published a generation,
// both the returned control state and closure report must come from that
// generation, not the stale prepare-time snapshot.
func TestResolveControlAndClosure_AfterPublishedGeneration(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}

	state, report, decision, err := ResolveControlAndClosure(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveControlAndClosure: %v", err)
	}
	if state.TaskID == "" {
		t.Fatal("expected a non-empty task control state")
	}
	if report.SchemaVersion == "" {
		t.Fatal("expected a real closure report, got a zero value")
	}
	if decision.SchemaVersion == "" {
		t.Fatal("expected a real admission decision, got a zero value")
	}
}

// cloneGenerationAs copies taskDir/control/generations/<from> to
// taskDir/control/generations/<to> wholesale, giving the test a second,
// structurally real (if content-identical) generation directory it can
// point control/latest-generation.yaml at, without needing to force a
// genuinely different task-content change through the full advance-task
// pipeline (repeated advance-task calls on an unchanged task are
// idempotent replays that reuse the same generation digest -- see
// AdvanceTaskResult's own replay_no_new_iteration semantics -- so a real
// second, distinct generation cannot be produced just by calling it
// twice). currentControlPaths only checks that the generation directory
// name matches what latest-generation.yaml claims (control.go's
// `filepath.Base(root) != ptr.DigestSHA256` check); it never re-verifies
// the directory's content actually hashes to that name, so a renamed copy
// is accepted exactly like a real second generation would be.
func cloneGenerationAs(t *testing.T, taskDir, from, to string) {
	t.Helper()
	src := filepath.Join(taskDir, "control", "generations", from)
	dst := filepath.Join(taskDir, "control", "generations", to)
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	}); err != nil {
		t.Fatalf("clone generation %s -> %s: %v", from, to, err)
	}
}

// writeLatestGenerationPointer uses the same writeFileAtomic helper
// production code does (temp file + rename), not a plain os.WriteFile --
// a real concurrent advance-task's own pointer write is atomic (a reader
// never observes a partially-written file), and a test helper simulating
// it must preserve that property or it manufactures a synthetic failure
// mode (a torn/incomplete read) that production concurrency never
// actually exhibits.
func writeLatestGenerationPointer(t *testing.T, taskDir, digest string) {
	t.Helper()
	data, err := yaml.Marshal(controlGenerationPointerEnvelope{TaskControlGeneration: controlGenerationPointer{
		SchemaVersion: SchemaVersion,
		Generation:    filepath.ToSlash(filepath.Join("generations", digest)),
		DigestSHA256:  digest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(taskDir, "control", "latest-generation.yaml"), data); err != nil {
		t.Fatal(err)
	}
}

// TestOldTwoCallPatternCanObserveDifferentGenerations is the direct
// regression demonstration for the bug this fix closes: two independent
// currentControlPaths resolutions -- exactly what the removed
// ControlStatus-then-separate-ResolveClosureReportPath pattern performed
// -- can observe control/latest-generation.yaml pointing at two different
// generations if the pointer is rewritten in between, exactly what a
// concurrent sensei advance-task's second, independent writeFileAtomic
// call does. This reproduces that torn-write window deterministically
// (rewriting the pointer directly, not racing a real advance-task, so the
// test is not timing-dependent/flaky) to prove the two-call pattern really
// was vulnerable, not just theoretically.
func TestOldTwoCallPatternCanObserveDifferentGenerations(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}
	_, digestA, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	const digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cloneGenerationAs(t, taskDir, digestA, digestB)

	// Simulate the OLD pattern's first, independent resolution -- e.g. what
	// the removed ControlStatus call would have resolved.
	_, firstCallDigest, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatal(err)
	}

	// A concurrent advance-task lands strictly between the two independent
	// calls, publishing a new generation.
	writeLatestGenerationPointer(t, taskDir, digestB)

	// Simulate the OLD pattern's second, independent resolution -- e.g.
	// what the removed, separate ResolveClosureReportPath call would have
	// resolved moments later.
	_, secondCallDigest, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatal(err)
	}

	if firstCallDigest != digestA {
		t.Fatalf("first call digest = %q, want %q", firstCallDigest, digestA)
	}
	if secondCallDigest != digestB {
		t.Fatalf("second call digest = %q, want %q", secondCallDigest, digestB)
	}
	if firstCallDigest == secondCallDigest {
		t.Fatal("expected the two independent resolutions to observe different generations -- this is exactly the bug fixed by resolving control and closure through one call")
	}
}

// TestResolveControlAndClosure_UsesGenerationDecisionNotPrepareTimeDecision
// is the direct regression test for a live review finding: once
// advance-task has published a generation, ResolveControlAndClosure must
// resolve the admission decision from that generation's own
// admission-decision.yaml, never the fixed prepare-time
// taskDir/admission/decision.yaml -- admission.projectProof derives
// obligations from the CURRENT closure's RelevantNodes, so the two
// decisions can genuinely diverge (e.g. an initial decision declaring zero
// proof obligations, superseded by a current one that declares real
// ones). This test forces that divergence directly (rather than trying to
// engineer a real claims/closure change that would newly surface an
// obligation) by mutating the on-disk generation decision after
// advance-task publishes it, and asserts ResolveControlAndClosure returns
// THAT mutated decision, not the untouched prepare-time one.
func TestResolveControlAndClosure_UsesGenerationDecisionNotPrepareTimeDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}

	prepareTimeDecision, err := admission.LoadDecision(filepath.Join(taskDir, "admission", "decision.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prepareTimeDecision.ProofObligations) != 0 {
		t.Fatalf("test fixture assumption violated: prepare-time decision already declares %d proof obligations", len(prepareTimeDecision.ProofObligations))
	}

	paths, _, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	generationDecisionPath := filepath.Join(filepath.Dir(paths.Results), "admission-decision.yaml")
	generationDecision, err := admission.LoadDecision(generationDecisionPath)
	if err != nil {
		t.Fatal(err)
	}
	generationDecision.ProofObligations = []admission.ProofReceipt{{ID: "obligation.forced-by-test"}}
	mutatedBytes, err := admission.MarshalCanonicalDecisionYAML(generationDecision)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(generationDecisionPath, mutatedBytes); err != nil {
		t.Fatal(err)
	}

	_, _, decision, err := ResolveControlAndClosure(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveControlAndClosure: %v", err)
	}
	if len(decision.ProofObligations) != 1 || decision.ProofObligations[0].ID != "obligation.forced-by-test" {
		t.Fatalf("ResolveControlAndClosure returned decision.ProofObligations = %+v, want the mutated generation decision's obligation -- it read the stale prepare-time decision.yaml instead of the current generation's admission-decision.yaml", decision.ProofObligations)
	}
}

// TestResolveControlAndClosure_ConcurrentPointerFlipNeverErrors is the
// concurrency stress test for the actual fix: many goroutines repeatedly
// call ResolveControlAndClosure (the single-resolution replacement) while
// a separate goroutine repeatedly flips control/latest-generation.yaml
// between two real, valid generation directories -- simulating a
// continuous stream of concurrent advance-task publications. Every call
// must either succeed with a complete, coherent state+report pair or fail
// cleanly; it must never panic, deadlock, or (verified separately by
// running this test under -race) exhibit a data race, regardless of how
// the pointer flips land relative to any single call's one-time
// currentControlPaths resolution.
func TestResolveControlAndClosure_ConcurrentPointerFlipNeverErrors(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}
	_, digestA, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	const digestB = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	cloneGenerationAs(t, taskDir, digestA, digestB)

	const flips = 200
	const readers = 8
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < flips; i++ {
			if i%2 == 0 {
				writeLatestGenerationPointer(t, taskDir, digestA)
			} else {
				writeLatestGenerationPointer(t, taskDir, digestB)
			}
		}
		close(stop)
	}()

	errCh := make(chan error, readers)
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				state, report, decision, err := ResolveControlAndClosure(repo, taskDir, false)
				if err != nil {
					errCh <- err
					return
				}
				if state.TaskID == "" || report.SchemaVersion == "" || decision.SchemaVersion == "" {
					errCh <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ResolveControlAndClosure under concurrent pointer flips: %v", err)
		} else {
			t.Fatal("ResolveControlAndClosure returned an incomplete state/report pair under concurrent pointer flips")
		}
	}
}
