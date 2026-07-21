// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"bytes"
	"testing"
)

func TestClaimDocumentRequiresExplicitRevisionStatus(t *testing.T) {
	doc := validClaimDocument()
	doc.Binding.RevisionStatus = ""
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing revision status error")
	}
}

func TestClaimDocumentRequiresExplicitGraphDigestStatus(t *testing.T) {
	doc := validClaimDocument()
	doc.Binding.GraphDigestStatus = ""
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing graph digest status error")
	}
}

func TestClaimDocumentRejectsUnknownPremiseFact(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].PremiseFacts = []string{"fact.missing"}
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected unknown premise fact error")
	}
}

func TestClaimDocumentRejectsFactRevisionMismatch(t *testing.T) {
	doc := validClaimDocument()
	doc.FactReceipts[0].Provenance.Revision = "other"
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected revision mismatch")
	}
}

func TestClaimDocumentRejectsFactRepositoryMismatch(t *testing.T) {
	doc := validClaimDocument()
	doc.FactReceipts[0].Provenance.RepositoryDomain = "github.com/other/repo"
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected repo mismatch")
	}
}

func TestClaimDocumentRejectsInventedFileAnchor(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].Scope.Files = []string{"other.go"}
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected invented file anchor error")
	}
}

func TestClaimDocumentRejectsInventedSymbolAnchor(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].Scope.Symbols = []string{"other.Symbol"}
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected invented symbol anchor error")
	}
}

func TestClaimDocumentAcceptsScopeUnionAcrossPremiseFacts(t *testing.T) {
	doc := validClaimDocument()
	second := doc.FactReceipts[0]
	second.Fact.ID = "fact.456"
	second.Fact.Scope.Files = []string{"writer.go"}
	second.Fact.Scope.Symbols = []string{"repository.Write"}
	doc.FactReceipts = append(doc.FactReceipts, second)
	doc.Claims[0].PremiseFacts = []string{"fact.123", "fact.456"}
	doc.Claims[0].Scope.Files = []string{"repository.go", "writer.go"}
	doc.Claims[0].Scope.Symbols = []string{"repository.Publish", "repository.Write"}
	if err := ValidateClaimDocument(doc); err != nil {
		t.Fatalf("multi-premise scope union rejected: %v", err)
	}
}

func TestUnboundClaimMayOnlyBeUnknownOrStale(t *testing.T) {
	doc := validClaimDocument()
	doc.Binding.Revision = ""
	doc.Binding.RevisionStatus = RevisionNotRequested
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected supported unbound claim rejection")
	}
	doc.Claims[0].EpistemicStatus = StatusUnknown
	doc.Claims[0].Unknowns = []string{"revision unavailable"}
	doc.Claims[0].InvalidationConditions = nil
	if err := ValidateClaimDocument(doc); err != nil {
		t.Fatalf("unknown unbound claim rejected: %v", err)
	}
}

func TestSupportedClaimRequiresSupportAndInvalidationCondition(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].PremiseFacts = nil
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing support")
	}
	doc = validClaimDocument()
	doc.Claims[0].InvalidationConditions = nil
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing invalidation")
	}
}

func TestContestedClaimRequiresSupportAndRefutation(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusContested
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing refutation")
	}
	doc.Claims[0].RefutingEvidence = []string{"evidence:ev.refutes"}
	if err := ValidateClaimDocument(doc); err != nil {
		t.Fatalf("contested claim rejected: %v", err)
	}
}

func TestRefutedClaimRequiresRefutingEvidence(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusRefuted
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing refuting evidence")
	}
}

func TestStaleClaimRequiresStaleReason(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusStale
	doc.Claims[0].Freshness = "current"
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected stale freshness error")
	}
}

func TestSupersededClaimRequiresResolvableReplacement(t *testing.T) {
	doc := validClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusSuperseded
	doc.Claims[0].SupersededBy = "claim.missing"
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected missing replacement error")
	}
}

func TestClaimDocumentOutputIsDeterministic(t *testing.T) {
	a, err := MarshalCanonicalClaimDocument(validClaimDocument())
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalCanonicalClaimDocument(validClaimDocument())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("claim document render is not deterministic")
	}
}

func validClaimDocument() ClaimDocument {
	f := validFact()
	f.ID = "fact.123"
	f.Scope.Repository = "github.com/example/project"
	f.Scope.Files = []string{"repository.go"}
	f.Scope.Symbols = []string{"repository.Publish"}
	f.Kind = "authority_observation"
	f.Subject = "repository.Publish"
	f.Predicate = "mutates_state"
	f.Object = "package_identity"
	f.Extractor = "go_authority_extractor"
	c := validClaim()
	c.ID = "claim.valid"
	c.PremiseFacts = []string{f.ID}
	return ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "sensei architecture inference",
		Binding: ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          "0123456789abcdef",
			RevisionStatus:    RevisionResolved,
			GraphDigestSHA256: "abcdef0123456789",
			GraphDigestStatus: GraphDigestResolved,
		},
		FactReceipts: []ClaimFactReceipt{{
			Fact: f,
			Provenance: Provenance{
				RepositoryDomain:       "github.com/example/project",
				RepositoryDomainStatus: RepositoryDomainResolved,
				Revision:               "0123456789abcdef",
				RevisionStatus:         RevisionResolved,
				SourceDigest:           "feedface",
				SourceDigestStatus:     SourceDigestResolved,
				SourceKind:             "source_file",
			},
		}},
		Claims: []Claim{c},
	}
}
