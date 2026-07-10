// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// TestContractSpineV1_GoldenBriefing is the certification of Contract Spine v1:
// for the /api/save-config demo, the briefing must render BOTH the authoritative
// realized-contract chain (impl → realizesContract → arch → invariant → required
// test) AND the candidate as a clearly-marked REVIEW-ONLY entry that is never
// presented as authority. See docs/contract-spine-v1.md.
//
// It reuses spineFakeStore()/archImpact() from realized_contracts_test.go, which
// model the exact committed spine (api_save_config realizes
// config_mutation_requires_valid_token; api_cors_diagnostics is a candidate).
func TestContractSpineV1_GoldenBriefing(t *testing.T) {
	s := &server{store: spineFakeStore()}
	impl := mintedIRI(rdf.ClassContract, "contract.http.api_save_config")
	cand := mintedIRI(rdf.ClassContract, "contract.http.api_cors_diagnostics")

	prose, refs := s.realizedContractSpineSection(context.Background(), archImpact(impl, cand))

	mustContain := []string{
		// authority section + the realized chain
		"Realized architectural contracts",
		"HTTP /api/save-config realizes contract.config_mutation_requires_valid_token",
		"Constrained by: meta.authority_must_express_uncertainty",                                           // constrained invariant
		"Required proof: internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken", // required test
		"Do not claim resolution if this contract is bypassed, weakened, or left untested.",
		// candidate stays review-only
		"REVIEW-ONLY",
		"~candidate~>",
		"do not treat as a guarantee",
	}
	for _, w := range mustContain {
		if !strings.Contains(prose, w) {
			t.Errorf("golden briefing missing %q\n--- prose ---\n%s", w, prose)
		}
	}

	// The candidate must NOT be promoted into the authority section.
	authority, _, found := strings.Cut(prose, "Candidate realized contracts")
	if !found {
		t.Fatal("expected a separate candidate section")
	}
	if strings.Contains(authority, "api-cors-diagnostics") || strings.Contains(authority, "cors-diagnostics realizes") {
		t.Error("a candidate leaked into the AUTHORITY section")
	}

	// referenced_ids carry the contract, invariant, and test for follow-up.
	joined := strings.Join(refs, " ")
	for _, w := range []string{
		"contract:contract.config_mutation_requires_valid_token",
		"invariant:meta.authority_must_express_uncertainty",
		"test:internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken",
	} {
		if !strings.Contains(joined, w) {
			t.Errorf("referenced_ids missing %q", w)
		}
	}
}
