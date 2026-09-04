// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// Classification happens before information loss.
//
// The v1 receipt carries freshness, seed and build_provenance beside a combined
// authority bool. It does NOT carry the closure proof or the transaction
// certification. So after the projection, a false combined bool with CURRENT
// freshness, CURRENT seed and INCOMPLETE provenance is produced by at least
// three different worlds, and a classifier reading only those fields reports
// "the graph answers authoritatively" for two of them -- about a graph the
// canonical verdict had refused.
//
// These drive the real metadata -> workspace-identity path, because the defect
// lives in the seam rather than in either end.

// authorityWorld is currentMetadataResponse with a stated canonical verdict.
func authorityWorld(verdict awarenesspb.AuthorityVerdict, txMatches bool, provenance awarenesspb.BuildProvenanceState,
	detail string) *awarenesspb.MetadataResponse {
	m := currentMetadataResponse()
	m.BuildProvenanceState = provenance
	m.EmbeddedTransactionMatchesSeed = txMatches
	m.EmbeddedTransactionStampPresent = txMatches
	m.Authority = &awarenesspb.GraphAuthority{
		Verdict:                        verdict,
		Authoritative:                  verdict == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE,
		GraphFreshnessState:            awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		GraphFreshnessDetail:           detail,
		SeedState:                      awarenesspb.SeedState_SEED_STATE_CURRENT,
		BuildProvenanceState:           provenance,
		EmbeddedTransactionMatchesSeed: txMatches,
	}
	return m
}

func classifiedLimitation(t *testing.T, m *awarenesspb.MetadataResponse) (scope, reason string) {
	t.Helper()
	root := t.TempDir()
	initTestGitRepo(t, root)
	writeSenseiConfigDomain(t, root, "github.com/globulario/sensei")
	b := testBridge(fakeClient{
		metadata: func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return m, nil
		},
	})
	res, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root})
	if err != nil {
		t.Fatalf("workspace_status: %v", err)
	}
	structured := res.Structured.(map[string]interface{})
	limitations, _ := structured["limitations"].([]interface{})
	for _, raw := range limitations {
		l := raw.(map[string]interface{})
		s, _ := l["scope"].(string)
		switch s {
		case "graph_answer_authority", "binary_build_stamp", "graph_authority":
			r, _ := l["reason"].(string)
			if scope != "" {
				t.Fatalf("two authority limitations, %q and %q: a reader is left two answers to one question", scope, s)
			}
			scope, reason = s, r
		}
	}
	if scope == "" {
		t.Fatalf("no authority limitation: %+v", structured["limitations"])
	}
	return scope, reason
}

//  1. The canonical verdict refuses on the TRANSACTION, and the binary stamp is
//     also incomplete. The receipt must not say the graph answers.
func TestATransactionRefusalIsNotReportedAsABuildStampProblem(t *testing.T) {
	scope, reason := classifiedLimitation(t, authorityWorld(
		awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE, false,
		awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE,
		"transaction certification: runtime transaction stamp missing"))

	if scope != "graph_answer_authority" {
		t.Fatalf("scope = %q; the canonical verdict refused this graph", scope)
	}
	if strings.Contains(reason, "answers authoritatively") {
		t.Fatalf("the receipt says the graph answers authoritatively after the verdict refused it: %q", reason)
	}
	if !strings.Contains(reason, "transaction") {
		t.Fatalf("the reason does not carry the canonical refusal: %q", reason)
	}
}

//  2. The canonical verdict refuses on CLOSURE, the transaction is fine, and the
//     binary stamp is incomplete. Same requirement, different refused conjunct:
//     the projected fields are identical to case 1 and the answer is not.
func TestAClosureRefusalIsNotReportedAsABuildStampProblem(t *testing.T) {
	scope, reason := classifiedLimitation(t, authorityWorld(
		awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE, true,
		awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE,
		"GRAPH_DOMAIN_CLOSURE_UNPROVEN: the proof set carries no proof for this domain"))

	if scope != "graph_answer_authority" {
		t.Fatalf("scope = %q; the canonical verdict refused this graph", scope)
	}
	if strings.Contains(reason, "answers authoritatively") {
		t.Fatalf("the receipt says the graph answers authoritatively after the verdict refused it: %q", reason)
	}
}

//  3. The positive control, and the one that proves the two propositions really
//     are independent: the canonical verdict is AUTHORITATIVE with a certifying
//     transaction, and only the serving binary's stamp is missing.
func TestAnAuthoritativeGraphWithAnUnstampedBinaryIsNamedAsSuch(t *testing.T) {
	scope, reason := classifiedLimitation(t, authorityWorld(
		awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE, true,
		awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE, ""))

	if scope != "binary_build_stamp" {
		t.Fatalf("scope = %q; this graph answers and its binary is unstamped", scope)
	}
	if !strings.Contains(reason, "SERVING BINARY") && !strings.Contains(reason, "serving binary") {
		t.Fatalf("the reason does not say where the repair is: %q", reason)
	}
	if !strings.Contains(reason, "says nothing about which commits produced the graph") {
		t.Fatalf("the reason overclaims what a build stamp proves: %q", reason)
	}
}

// The three worlds are indistinguishable in the projected v1 fields. This is
// what makes classification-after-projection unsound, asserted rather than
// argued.
func TestTheThreeWorldsAreIndistinguishableAfterProjection(t *testing.T) {
	worlds := []*awarenesspb.MetadataResponse{
		authorityWorld(awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE, false,
			awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE, "transaction"),
		authorityWorld(awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE, true,
			awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE, "closure"),
		authorityWorld(awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE, true,
			awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE, ""),
	}
	for _, m := range worlds {
		// The fields the v1 receipt keeps are identical across all three.
		if m.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT ||
			m.GetSeedState() != awarenesspb.SeedState_SEED_STATE_CURRENT ||
			m.GetBuildProvenanceState() != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE {
			t.Fatal("the fixtures no longer share the projected fields, so they prove nothing about the loss")
		}
	}
	// And they classify differently, which is only possible before the loss.
	first, _ := classifiedLimitation(t, worlds[0])
	third, _ := classifiedLimitation(t, worlds[2])
	if first == third {
		t.Fatalf("a refused graph and an authoritative one classified the same (%q): "+
			"the classification is reading the projection", first)
	}
}
