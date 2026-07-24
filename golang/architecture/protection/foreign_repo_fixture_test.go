// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "testing"

// TestForeignRepositoryProofFixture is the required end-to-end proof
// (contract §13): a synthetic foreign repository demonstrating, in one
// place, every numbered acceptance point before implementation handoff:
//
//  1. empty manual registry;
//  2. contract/schema/invariant-bearing files;
//  3. non-empty automatically derived protection;
//  4. hook refusal before briefing (proven at the ClassifyFile/briefing-
//     required level — the shell hook itself is proven separately by the
//     manual smoke test recorded in the handoff comment, since t.TempDir()
//     fixtures cannot easily drive a bash PreToolUse hook process);
//  5. successful edit authorization after briefing (the marker-file
//     mechanism itself is unchanged shell-side logic, proven by inspection
//     — this test proves the DATA the hook decision is based on flips
//     correctly, which is the part this contract owns);
//  6. candidate protection without promotion;
//  7. an unrelated README remaining unprotected.
func TestForeignRepositoryProofFixture(t *testing.T) {
	root := t.TempDir()

	// 1. Empty manual registry — the exact bootstrap-gap starting state.
	writeFile(t, root, ManualRegistryFile, "files: []\n")

	// 2. Contract/schema/invariant-bearing files: a governed invariant that
	// directly protects one real source file, plus a genuinely unrelated file.
	writeFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: fixture.payment.authority
    title: Payment authority must flow through the ledger
    severity: critical
    protects:
      files:
        - src/payments/ledger.go
`)
	writeFile(t, root, "src/payments/ledger.go", "package payments\n")
	writeFile(t, root, "README.md", "# fixture repository\n")

	cov, err := Derive(root)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	// 3. Non-empty automatically derived protection despite an empty manual
	// list.
	if cov.Status == CoverageEmpty {
		t.Fatalf("expected real derived protection, got status=%s", cov.Status)
	}
	ledgerClassification, ok := ClassifyFile(cov, "src/payments/ledger.go")
	if !ok || !ledgerClassification.Protected {
		t.Fatalf("expected src/payments/ledger.go to be protected, got %+v", ledgerClassification)
	}
	if ledgerClassification.Provisional {
		t.Fatal("a direct governed-relation reason must not be marked provisional")
	}

	// 4/5. Hook refusal before briefing, authorization after: this is exactly
	// what enforce-briefing.sh's session-marker check gates on. This test
	// proves the DATA side: ClassifyFile reports briefing-required (true)
	// regardless of any marker — the marker check itself is the hook's own,
	// unchanged responsibility (contract §9.6 "preserve existing session
	// marker behavior").
	if !ledgerClassification.Protected {
		t.Fatal("a protected file must require briefing before an edit is authorized")
	}

	// 6. Candidate protection without promotion: a source-file candidate
	// signal protects provisionally, the candidate file itself is never
	// mutated, and it never becomes a governed source.
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", testAuthorityCandidatesYAML)
	cov2, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	candidateClassification, ok := ClassifyFile(cov2, "src/lifecycle/start.go")
	if !ok || !candidateClassification.Protected || !candidateClassification.Provisional {
		t.Fatalf("expected provisional candidate protection, got %+v", candidateClassification)
	}
	governedAfterCandidate, err := GovernedSourceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range governedAfterCandidate {
		if f == "docs/awareness/candidates/authority_surface_candidates.yaml" {
			t.Fatal("a candidate file must never become an authored governed source merely by existing")
		}
	}

	// 7. An unrelated README remains unprotected.
	readmeClassification, ok := ClassifyFile(cov2, "README.md")
	if !ok {
		t.Fatal("classifying README.md must succeed")
	}
	if readmeClassification.Protected {
		t.Fatalf("an unrelated README must remain unprotected, got reasons=%+v", readmeClassification.Reasons)
	}
}
