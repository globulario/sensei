// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestValidateRefusesMissingMandatoryReceipt(t *testing.T) {
	err := Validate(Result{}, GroundingSnapshot{})
	if err == nil || !strings.Contains(err.Error(), "receipt exact result digest is required") {
		t.Fatalf("missing receipt must be refused, got %v", err)
	}
}

func TestCandidateIdentityIsDeterministicAndIgnoresRankingMetadata(t *testing.T) {
	binding := Binding{Repository: architecture.ClaimDocumentBinding{RepositoryDomain: "example/repo", Revision: "abc"}, GraphDigestSHA256: "graph", EvidenceSnapshotDigestSHA256: "evidence"}
	scope := architecture.ClaimScope{Files: []string{"b.go", "a.go"}, Symbols: []string{"b", "a"}}
	id1, digest1, err := ComputeCandidateID("v1", binding, " proposition ", scope, KindInvariant, "generator.v1")
	if err != nil {
		t.Fatal(err)
	}
	scope.Files = []string{"a.go", "b.go"}
	scope.Symbols = []string{"a", "b"}
	id2, digest2, err := ComputeCandidateID("v1", binding, "proposition", scope, KindInvariant, "generator.v1")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 || digest1 != digest2 {
		t.Fatalf("identity changed with ordering: %s/%s != %s/%s", id1, digest1, id2, digest2)
	}
}

func TestGroundingSnapshotDigestIsDeterministic(t *testing.T) {
	a, err := GroundingSnapshotDigest(GroundingSnapshot{Files: []string{"b.go", "a.go"}, Symbols: []string{"b", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := GroundingSnapshotDigest(GroundingSnapshot{Files: []string{"a.go", "b.go"}, Symbols: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("snapshot digest must be order independent: %s != %s", a, b)
	}
}

func TestClosedVocabulariesRefuseUnknownValues(t *testing.T) {
	if IsValidCandidateKind(CandidateKind("invented")) {
		t.Fatal("unknown candidate kind accepted")
	}
	if IsValidEvidenceRequestReason("invented") {
		t.Fatal("unknown evidence request reason accepted")
	}
}
