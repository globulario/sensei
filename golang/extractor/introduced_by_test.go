// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func importScars(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "failure_modes.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	e := rdf.NewEmitter(&buf)
	if err := importFailureModes(e, path); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The attribution becomes a traversable relation, not prose. A Change node is
// keyed on repository AND commit, because the same short hash occurs in more
// than one repository and an edge that cannot say which one is not evidence.
func TestAnAttributionBecomesAFirstClassRelation(t *testing.T) {
	nt := importScars(t, `
failure_modes:
  - id: example.failure
    title: something broke
    introduced_by:
      - repo: github.com/globulario/sensei
        commit: 7fbd15a4
`)
	if !strings.Contains(nt, "introducedBy") {
		t.Fatalf("no introducedBy relation was emitted:\n%s", nt)
	}
	if !strings.Contains(nt, "github.com/globulario/sensei@7fbd15a4") {
		t.Fatalf("the change identity does not carry repository and commit:\n%s", nt)
	}
	if !strings.Contains(nt, "#Change") {
		t.Fatalf("the change was not typed as a Change node:\n%s", nt)
	}
}

// Compound ancestry survives: one change introduced unsafe state, another
// removed the check that compensated for it.
func TestCompoundAncestryEmitsEveryAttribution(t *testing.T) {
	nt := importScars(t, `
failure_modes:
  - id: example.failure
    title: something broke
    introduced_by:
      - repo: r
        commit: aaaaaaa
      - repo: r
        commit: bbbbbbb
`)
	if strings.Count(nt, "introducedBy") != 2 {
		t.Fatalf("expected two attributions, got:\n%s", nt)
	}
}

// Half an identity is not one. Emitting a Change keyed on a bare SHA would
// collide across repositories and quietly merge two histories.
func TestAHalfIdentityMintsNothing(t *testing.T) {
	for _, entry := range []string{
		"      - commit: 7fbd15a4",
		"      - repo: github.com/globulario/sensei",
	} {
		nt := importScars(t, "\nfailure_modes:\n  - id: example.failure\n    title: t\n    introduced_by:\n"+entry+"\n")
		if strings.Contains(nt, "introducedBy") {
			t.Fatalf("a half identity minted a relation:\n%s", nt)
		}
	}
}

// Nothing infers this relation. A scar that names files and tests, with no
// attribution, produces no Change node however suggestive its contents are.
func TestNothingIsInferredFromFilesOrTests(t *testing.T) {
	nt := importScars(t, `
failure_modes:
  - id: example.failure
    title: something broke
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/query_test.go:TestSomething
    related_invariants:
      - inv.example
`)
	if strings.Contains(nt, "introducedBy") || strings.Contains(nt, "#Change") {
		t.Fatalf("an attribution was inferred from files or tests:\n%s", nt)
	}
}
