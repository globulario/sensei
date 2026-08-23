// SPDX-License-Identifier: AGPL-3.0-only

package gosemantics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/extractbudget"
)

// packages.Load holds the context and returns when the Go toolchain finishes,
// not when the deadline passes: measured on Sensei's own repository, a
// 20-second ceiling produced a 122-second run. So the ceiling has to bind the
// answer, not the loader.
func TestExtractBoundedOrAbandonReturnsAtTheCeiling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	res, err, abandoned := ExtractBoundedOrAbandon(ctx, "../../..", extractbudget.Budget{})
	elapsed := time.Since(start)

	if !abandoned {
		t.Fatal("an already-cancelled ceiling must abandon the load rather than wait for it")
	}
	if err != nil {
		t.Fatalf("abandonment is not an error: %v", err)
	}
	if len(res.Observations) != 0 {
		t.Fatalf("an abandoned load returned %d observation(s)", len(res.Observations))
	}
	if elapsed > 10*time.Second {
		t.Fatalf("abandonment took %s — the ceiling did not bind", elapsed)
	}
}

// A caller with no ceiling runs inline, so nothing about an unbounded
// extraction changes: no goroutine, no abandonment.
func TestExtractBoundedOrAbandonRunsInlineWithoutACeiling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/abandon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package abandon\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err, abandoned := ExtractBoundedOrAbandon(context.Background(), root, extractbudget.Budget{})
	if err != nil {
		t.Fatalf("unbounded extraction failed: %v", err)
	}
	if abandoned {
		t.Fatal("an unbounded run reported abandonment")
	}
}

// The test above passes whichever way the race falls, because on an idle
// machine the load never finishes before the ceiling is consulted. On a loaded
// two-core CI runner it did, both select arms were ready, and Go chose between
// them at random — so the same cancelled run reported abandonment or completion
// depending on the scheduler.
//
// The two answers are not interchangeable. Abandonment is a BLOCKING limitation
// naming a document with no semantic observations; a load that "completed"
// returns its stop as a NON-blocking note instead (factextract/invariants.go
// records the two differently). Deciding between them by scheduler tells a
// caller its document is whole at a moment the clock says it cannot be.
//
// beforeCeilingForTest puts the run in exactly that state — load delivered,
// ceiling expired, both arms ready — on every iteration. Repeated, because a
// bare select would take the wrong arm about half the time and once is not
// evidence.
func TestExtractBoundedOrAbandonAbandonsWhenBothArmsAreReady(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/abandon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package abandon\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	beforeCeilingForTest = func(loadDelivered <-chan struct{}) { <-loadDelivered }
	t.Cleanup(func() { beforeCeilingForTest = nil })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const runs = 40
	for i := 0; i < runs; i++ {
		res, err, abandoned := ExtractBoundedOrAbandon(ctx, root, extractbudget.Budget{})
		if !abandoned {
			t.Fatalf("run %d: a cancelled ceiling reported a completed load (err=%v, observations=%d, limitations=%+v)",
				i, err, len(res.Observations), res.Limitations)
		}
		if err != nil {
			t.Fatalf("run %d: abandonment is not an error: %v", i, err)
		}
		if len(res.Observations) != 0 || len(res.Limitations) != 0 {
			t.Fatalf("run %d: an abandoned load carried %d observation(s) and %d limitation(s)",
				i, len(res.Observations), len(res.Limitations))
		}
	}
}
