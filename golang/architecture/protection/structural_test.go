// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "testing"

const testAuthorityCandidatesYAML = `
authority_surface_candidates:
  repo_root: /fixture
  generated_by: awg extract-authority
  candidates:
    - id: candidate.authority.example.startservice
      class: AuthoritySurface
      status: candidate
      source_files:
        - src/lifecycle/start.go
      symbols:
        - StartService
`

func TestCandidateSignalReasons_ProvisionalOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", testAuthorityCandidatesYAML)

	reasons, _, err := CandidateSignalReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	rs, ok := reasons["src/lifecycle/start.go"]
	if !ok || len(rs) == 0 {
		t.Fatal("candidate source_files entry must produce a protection reason")
	}
	if !rs[0].Provisional {
		t.Fatal("a candidate-derived reason must be marked Provisional")
	}
	if rs[0].KnowledgeRef != "candidate.authority.example.startservice" {
		t.Fatalf("expected knowledge ref to trace to the candidate id, got %q", rs[0].KnowledgeRef)
	}
}

// contract §12 "deleting/rejecting the candidate signal removes provisional
// protection on the next derivation unless another reason remains" — proven
// here at the Derive level (stateless: re-deriving after removing the
// candidate file removes the reason with it).
func TestCandidateSignalReasons_RemovedOnNextDerivationAfterDeletion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", testAuthorityCandidatesYAML)

	cov1, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	fc1, _ := ClassifyFile(root, cov1, "src/lifecycle/start.go")
	if !fc1.Protected || !fc1.Provisional {
		t.Fatalf("expected provisional protection before candidate removal, got %+v", fc1)
	}

	if err := removeFile(root, "docs/awareness/candidates/authority_surface_candidates.yaml"); err != nil {
		t.Fatal(err)
	}
	cov2, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	fc2, _ := ClassifyFile(root, cov2, "src/lifecycle/start.go")
	if fc2.Protected {
		t.Fatalf("expected protection to be gone after the only candidate signal was removed, got %+v", fc2)
	}
}
