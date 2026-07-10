// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// spineFakeStore models the demo spine:
//
//	contract.http.api_save_config --realizesContract--> config_mutation...
//	contract.http.api_cors_diagnostics --candidateRealizesContract--> config_mutation...
//	config_mutation... --constrainedByInvariant--> meta.authority... --requiresTest--> TestSaveConfig...
func spineFakeStore() fakeStore {
	implIRI := mintedIRI(rdf.ClassContract, "contract.http.api_save_config")
	candIRI := mintedIRI(rdf.ClassContract, "contract.http.api_cors_diagnostics")
	archIRI := mintedIRI(rdf.ClassContract, "contract.config_mutation_requires_valid_token")
	invIRI := mintedIRI(rdf.ClassInvariant, "meta.authority_must_express_uncertainty")
	testIRI := mintedIRI(rdf.ClassTest, "internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken")
	authEvidenceIRI := mintedIRI(rdf.ClassEvidence, "contract_realization.accepted.contract.http.api_save_config__contract.config_mutation_requires_valid_token")
	candEvidenceIRI := mintedIRI(rdf.ClassEvidence, "contract_realization.candidate.contract.http.api_cors_diagnostics__contract.config_mutation_requires_valid_token")
	return fakeStore{describe: func(_ context.Context, iri string) ([]store.Triple, error) {
		switch iri {
		case implIRI:
			return []store.Triple{
				{Predicate: rdf.PropLabel, Object: "HTTP /api/save-config"},
				{Predicate: rdf.PropRealizesContract, Object: archIRI, ObjectIsIRI: true},
				{Predicate: rdf.PropSupportedByEvidence, Object: authEvidenceIRI, ObjectIsIRI: true},
			}, nil
		case candIRI:
			return []store.Triple{
				{Predicate: rdf.PropLabel, Object: "HTTP /api/cors-diagnostics"},
				{Predicate: rdf.PropCandidateRealizesContract, Object: archIRI, ObjectIsIRI: true},
				{Predicate: rdf.PropSupportedByEvidence, Object: candEvidenceIRI, ObjectIsIRI: true},
			}, nil
		case archIRI:
			return []store.Triple{
				{Predicate: rdf.PropLabel, Object: "Config mutation requires a valid token"},
				{Predicate: rdf.PropConstrainedByInvariant, Object: invIRI, ObjectIsIRI: true},
				{Predicate: rdf.PropRequiresTest, Object: testIRI, ObjectIsIRI: true},
				{Predicate: rdf.PropSupportedByEvidence, Object: authEvidenceIRI, ObjectIsIRI: true},
			}, nil
		case authEvidenceIRI:
			return []store.Triple{
				{Predicate: rdf.PropSourceKind, Object: "promoted_candidate"},
				{Predicate: rdf.PropConfidence, Object: "high"},
				{Predicate: rdf.PropPromotionStatus, Object: "accepted"},
				{Predicate: rdf.PropComment, Object: "handler validates a token before persisting configuration"},
			}, nil
		case candEvidenceIRI:
			return []store.Triple{
				{Predicate: rdf.PropSourceKind, Object: "generated_evidence_scoring"},
				{Predicate: rdf.PropConfidence, Object: "low"},
				{Predicate: rdf.PropPromotionStatus, Object: "candidate"},
				{Predicate: rdf.PropComment, Object: "same directory internal/gateway/handlers/config"},
			}, nil
		}
		return nil, nil
	}}
}

func archImpact(iris ...string) *awarenesspb.ImpactResponse {
	imp := &awarenesspb.ImpactResponse{}
	for _, iri := range iris {
		imp.DirectArchitecture = append(imp.DirectArchitecture, &awarenesspb.KnowledgeNode{Iri: iri})
	}
	return imp
}

func TestBriefingSpine_AuthorityChainAppears(t *testing.T) {
	s := &server{store: spineFakeStore()}
	impl := mintedIRI(rdf.ClassContract, "contract.http.api_save_config")
	prose, refs := s.realizedContractSpineSection(context.Background(), archImpact(impl))

	for _, want := range []string{
		"Realized architectural contracts (AUTHORITY",
		"HTTP /api/save-config realizes contract.config_mutation_requires_valid_token",
		"The contract requires: Config mutation requires a valid token",
		"Constrained by: meta.authority_must_express_uncertainty",
		"Required proof: internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken",
		"Realization provenance: source=promoted_candidate; confidence=high; status=accepted; handler validates a token before persisting configuration",
		"Do not claim resolution if this contract is bypassed",
	} {
		if !strings.Contains(prose, want) {
			t.Errorf("prose missing %q\n---\n%s", want, prose)
		}
	}
	joined := strings.Join(refs, " ")
	for _, want := range []string{
		"contract:contract.config_mutation_requires_valid_token",
		"invariant:meta.authority_must_express_uncertainty",
		"test:internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken",
		"evidence:contract_realization.accepted.contract.http.api_save_config__contract.config_mutation_requires_valid_token",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("referenced_ids missing %q (got %v)", want, refs)
		}
	}
}

func TestBriefingSpine_CandidateIsReviewOnlyNeverAuthority(t *testing.T) {
	s := &server{store: spineFakeStore()}
	cand := mintedIRI(rdf.ClassContract, "contract.http.api_cors_diagnostics")
	prose, _ := s.realizedContractSpineSection(context.Background(), archImpact(cand))
	if strings.Contains(prose, "(AUTHORITY") {
		t.Errorf("a candidate must NOT appear under AUTHORITY:\n%s", prose)
	}
	if !strings.Contains(prose, "REVIEW-ONLY") || !strings.Contains(prose, "~candidate~>") {
		t.Errorf("candidate not surfaced as review-only:\n%s", prose)
	}
	if !strings.Contains(prose, "Candidate provenance: source=generated_evidence_scoring; confidence=low; status=candidate; same directory internal/gateway/handlers/config") {
		t.Errorf("candidate provenance missing from review-only section:\n%s", prose)
	}
}

func TestBriefingSpine_NoContractNoSection(t *testing.T) {
	s := &server{store: spineFakeStore()}
	prose, refs := s.realizedContractSpineSection(context.Background(),
		archImpact(mintedIRI(rdf.ClassComponent, "component.golang.foo")))
	if prose != "" || refs != nil {
		t.Errorf("expected no section when no contract is anchored; got prose=%q refs=%v", prose, refs)
	}
}

func TestBriefingSpine_NoDuplicateOutput(t *testing.T) {
	s := &server{store: spineFakeStore()}
	impl := mintedIRI(rdf.ClassContract, "contract.http.api_save_config")
	prose, _ := s.realizedContractSpineSection(context.Background(), archImpact(impl, impl))
	if n := strings.Count(prose, "HTTP /api/save-config realizes"); n != 1 {
		t.Errorf("expected exactly one authority line (deduped), got %d:\n%s", n, prose)
	}
}
