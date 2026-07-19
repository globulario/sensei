// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestArchitectureClaimReferenceValidatorAcceptsDefinedEvidence(t *testing.T) {
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(validClaimNT(true)))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs=%v", errs)
	}
}

func TestArchitectureClaimReferenceValidatorRejectsFabricatedSupportingEvidence(t *testing.T) {
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(validClaimNT(false)))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected fabricated evidence error")
	}
}

func TestArchitectureClaimReferenceValidatorRejectsFabricatedRefutingEvidence(t *testing.T) {
	nt := strings.Replace(validClaimNT(true), rdf.IRI(rdf.PropSupportedByEvidence), rdf.IRI(rdf.PropRefutedByEvidence), 1)
	nt = strings.Replace(nt, rdf.MintIRI(rdf.ClassEvidence, "ev.ok"), rdf.MintIRI(rdf.ClassEvidence, "ev.missing"), 1)
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected fabricated refuting evidence error")
	}
}

func TestArchitectureClaimReferenceValidatorRejectsMissingDependentClaim(t *testing.T) {
	nt := validClaimNT(true) + rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.one") + " " + rdf.IRI(rdf.PropDependsOnClaim) + " " + rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.missing") + " .\n"
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected missing dependent claim")
	}
}

func TestArchitectureClaimReferenceValidatorRejectsMissingSupersedingClaim(t *testing.T) {
	nt := validClaimNT(true) + rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.one") + " " + rdf.IRI(rdf.PropSupersededBy) + " " + rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.missing") + " .\n"
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected missing superseding claim")
	}
}

func TestArchitectureClaimReferenceValidatorRejectsMissingRequiredClaimProperties(t *testing.T) {
	nt := rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.one") + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassArchitectureClaim) + " .\n"
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected missing property errors")
	}
}

func TestArchitectureClaimReferenceValidatorDoesNotChangeLegacyReferenceResultsWithoutClaims(t *testing.T) {
	errs, err := ValidateArchitectureClaimReferences(strings.NewReader(`<x> <y> "z" .`))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no claim errors, got %v", errs)
	}
}

func validClaimNT(includeEvidenceDefinition bool) string {
	claim := rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.one")
	ev := rdf.MintIRI(rdf.ClassEvidence, "ev.ok")
	nt := claim + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassArchitectureClaim) + " .\n" +
		claim + " " + rdf.IRI(rdf.PropClaimSubject) + " " + rdf.Lit("s") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropClaimPredicate) + " " + rdf.Lit("p") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropClaimObject) + " " + rdf.Lit("o") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropArchitecturalPlane) + " " + rdf.Lit("observed") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropAssertionOrigin) + " " + rdf.Lit("derived") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropEpistemicStatus) + " " + rdf.Lit("supported") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropPromotionStatus) + " " + rdf.Lit("candidate") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropSourceKind) + " " + rdf.Lit("generated_candidate") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropHumanReviewRequired) + " " + rdf.Lit("true") + " .\n" +
		claim + " " + rdf.IRI(rdf.PropSupportedByEvidence) + " " + ev + " .\n"
	if includeEvidenceDefinition {
		nt += ev + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassEvidence) + " .\n" +
			ev + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("evidence.yaml") + " .\n"
	}
	return nt
}
