// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"strings"
	"testing"
)

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

// contract §4/§6 correction (second review round): an invalid candidate
// target (escaping the repository) must be reported as malformed, not
// silently dropped.
func TestCandidateSignalReasons_InvalidTargetIsReportedMalformedNotSilentlyDropped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", `
authority_surface_candidates:
  repo_root: /fixture
  generated_by: awg extract-authority
  candidates:
    - id: candidate.authority.example.escaping
      class: AuthoritySurface
      status: candidate
      source_files:
        - ../escapes/repo.go
      symbols:
        - Escaping
`)

	reasons, malformed, err := CandidateSignalReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reasons) != 0 {
		t.Fatalf("an escaping target must never resolve to a protected path, got %v", reasons)
	}
	if len(malformed) != 1 {
		t.Fatalf("expected exactly one malformed entry for the escaping target, got %v", malformed)
	}
	if !strings.Contains(malformed[0], "../escapes/repo.go") {
		t.Fatalf("expected the malformed entry to name the invalid target, got %q", malformed[0])
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

// contract §4 correction: JSON Schema is now an implemented structural
// source (golang/extractor/jsonschemascan), not a permanent gap.
func TestStructuralContractReasons_ProtectsJSONSchemaFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "schemas/config.schema.json", `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object"
}`)

	reasons, malformed, err := StructuralContractReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	rs, ok := reasons["schemas/config.schema.json"]
	if !ok || len(rs) == 0 {
		t.Fatal("expected a structural reason for the JSON Schema file")
	}
	if rs[0].Kind != "json_schema_contract" {
		t.Fatalf("expected kind=json_schema_contract, got %q", rs[0].Kind)
	}
	if rs[0].Provisional {
		t.Fatal("a JSON Schema structural signal must be definite, not provisional")
	}
}

// contract §4 correction: COMPLETE must be genuinely reachable now that
// every named structural source is implemented — a clean repository with
// real protection and zero gaps must reach COMPLETE, not be permanently
// capped at PARTIAL by an admitted-but-unimplemented scanner.
func TestDerive_CompleteIsReachableWithNoGaps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Status != CoverageComplete {
		t.Fatalf("expected COMPLETE for a clean repository with real protection and no gaps, got %s (gaps=%v)", cov.Status, cov.Gaps)
	}
	if len(cov.Gaps) != 0 {
		t.Fatalf("expected zero gaps, got %v", cov.Gaps)
	}
}
