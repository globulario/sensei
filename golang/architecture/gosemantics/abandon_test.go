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
