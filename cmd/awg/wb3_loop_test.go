// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

// TestWriteBackLoop_EndToEnd demonstrates the full write-back loop (WB-3) on a
// controlled corpus, using the REAL drafter (draftCandidateDoc, WB-2) and the
// REAL importer (extractor.ImportAwarenessDir):
//
//	scar/incident -> draft-candidate -> [review queue, QUARANTINED]
//	             -> approve+promote  -> [canonical YAML, IN THE GRAPH] -> validate
//
// The load-bearing property: a drafted candidate must NOT enter the graph until
// it is promoted. The loop closes only when promotion moves it into canonical
// YAML. This is the demonstrable proof that the incident->candidate->promote
// path is real and fail-safe (no candidate auto-enforces).
func TestWriteBackLoop_EndToEnd(t *testing.T) {
	const id = "wb3.loop.demo_convergence_rule"
	docsDir := t.TempDir()

	// Stage 0 — a baseline canonical corpus so the graph is non-empty.
	wb3WriteFile(t, filepath.Join(docsDir, "invariants.yaml"), `
invariants:
  - id: wb3.baseline.always_present
    title: Baseline invariant
    severity: high
    status: active
`)

	// Stage 1 — SCAR -> DRAFT CANDIDATE (real WB-2 drafter).
	relPath, content, err := draftCandidateDoc(draftCandidateInput{
		Class:          "invariant",
		ID:             id,
		Title:          "convergence comparison must key on build_id",
		Description:    "Synthetic scar: a reconcile compared version strings instead of build_id.",
		Severity:       "critical",
		SourceFiles:    []string{"golang/cluster_controller/cluster_controller_server/release_runtime_convergence.go"},
		DiscoveredFrom: "scar:wb3-e2e-demo",
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if !strings.Contains(string(content), "status: candidate") {
		t.Fatalf("drafted entry is not status:candidate:\n%s", content)
	}
	candidatePath := filepath.Join(docsDir, relPath)
	wb3WriteFile(t, candidatePath, string(content))

	// Stage 2 — the candidate sits in the REVIEW QUEUE and is QUARANTINED: an
	// import of the corpus must NOT contain the node. This is the fail-safe core.
	if got := importGraph(t, docsDir); strings.Contains(got, id) {
		t.Fatalf("candidate leaked into the graph before promotion — quarantine broken")
	}
	// ...while the baseline node IS present (proving the import actually ran).
	if got := importGraph(t, docsDir); !strings.Contains(got, "wb3.baseline.always_present") {
		t.Fatalf("baseline node missing — import did not run as expected")
	}

	// Stage 3 — APPROVE + PROMOTE: the reviewer moves the candidate into
	// canonical YAML (what `awg promote` does) and removes it from the queue.
	if err := os.Remove(candidatePath); err != nil {
		t.Fatalf("dequeue candidate: %v", err)
	}
	appendFile(t, filepath.Join(docsDir, "invariants.yaml"), `  - id: `+id+`
    title: convergence comparison must key on build_id
    severity: critical
    status: active
`)

	// Stage 4 — REBUILD + VALIDATE: re-import; the promoted node is now IN THE
	// GRAPH. The loop has closed.
	if got := importGraph(t, docsDir); !strings.Contains(got, id) {
		t.Fatalf("promoted invariant did not enter the graph — loop did not close")
	}
}

func importGraph(t *testing.T, docsDir string) string {
	t.Helper()
	var buf bytes.Buffer
	if _, _, err := extractor.ImportAwarenessDir(docsDir, &buf); err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String()
}

func wb3WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}
