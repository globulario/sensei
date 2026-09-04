// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// The world this exists for, preserved as a fixture rather than described.
//
// Observed on 2026-09-03 against github.com/globulario/sensei-code: the live
// graph matched its validated artifact exactly, the seed was current, and the
// serving binary carried no link-time source-repo commit -- so build provenance
// was INCOMPLETE and the runtime transaction stamp was absent.
//
// Two Sensei surfaces then published a field called `authoritative` about that
// one graph, one saying true and one saying false, from identical evidence.
// Both were right about their own question. Nothing said there were two.
func divergentWorld() *awarenesspb.MetadataResponse {
	return &awarenesspb.MetadataResponse{
		// The canonical verdict, which is what AnswerAuthority consumes. The
		// top-level fields below are evidence beside it, not a second opinion.
		Authority: &awarenesspb.GraphAuthority{
			Verdict:                        awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE,
			Authoritative:                  true,
			GraphFreshnessState:            awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			SeedState:                      awarenesspb.SeedState_SEED_STATE_CURRENT,
			BuildProvenanceState:           awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE,
			EmbeddedTransactionMatchesSeed: true,
		},
		ServerVersion:                       "reflex-v2-frozen",
		GraphBuildCommit:                    "58c055fbd9d6",
		GraphBuildTimeUnix:                  1788407139,
		SourceRepoCommit:                    "", // the binding constraint: unstamped binary
		GraphFreshnessState:                 awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		SeedState:                           awarenesspb.SeedState_SEED_STATE_CURRENT,
		BuildProvenanceState:                awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE,
		LiveStoreContainsEmbeddedSeedMarker: true,
		TripleCount:                         34619,
		EmbeddedTransactionStampPresent:     false,
		EmbeddedTransactionMatchesSeed:      false,
	}
}

// fullyProvenanced is the same world with the provenance chain stated.
func fullyProvenanced() *awarenesspb.MetadataResponse {
	m := divergentWorld()
	// A SERVICES commit, semantically distinct from the awareness-graph SHA
	// above: they are different repositories, and a fixture that reuses one
	// value for both would pass while proving nothing about which field
	// carries which.
	m.SourceRepoCommit = "b98c91eb540a3e5aa9d97e7b3c08005e08cb1897"
	m.BuildProvenanceState = awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED
	m.EmbeddedTransactionStampPresent = true
	m.EmbeddedTransactionMatchesSeed = true
	m.CertifiedAwarenessGraphCommit = "58c055fbd9d65ad2f5a8c965f728897012e75f09"
	m.CertifiedServicesRepoCommit = "b98c91eb540a3e5aa9d97e7b3c08005e08cb1897"
	m.Authority.BuildProvenanceState = awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED
	return m
}

// The divergent state is COHERENT, not contradictory. Asserting both halves in
// one test is the point: a reader who sees only one of them learns the wrong
// thing.
func TestAGraphCanAnswerWellAndStateNoProvenance(t *testing.T) {
	scoped := InterpretMetadataScoped(divergentWorld())

	if !scoped.AnswerAuthority {
		t.Fatal("a graph matching its validated artifact, seed current, marker present, non-empty " +
			"was reported as unable to answer")
	}
	if scoped.BinaryBuildStampComplete {
		t.Fatal("a serving binary with no source-repo commit reported that it can state its provenance")
	}
	if scoped.Reason == "" {
		t.Fatal("the divergent state was reported without saying which proposition failed")
	}
	if !strings.Contains(scoped.Reason, "build stamp") {
		t.Fatalf("the reason does not name the failing proposition: %q", scoped.Reason)
	}
	// And it must not overclaim: a build stamp says nothing about which
	// commits produced the graph.
	if !strings.Contains(scoped.Reason, "says nothing about which commits produced the graph") {
		t.Fatalf("the reason overclaims what a build stamp proves: %q", scoped.Reason)
	}
	// And the compatibility surface still says what it always said, so nothing
	// downstream silently changes verdict.
	if InterpretMetadataAuthority(divergentWorld()).Authoritative {
		t.Fatal("the combined reading changed meaning; consumers gate on it")
	}
}

// The full-provenance case makes both propositions healthy, and only then does
// the combined reading turn.
func TestStatingTheProvenanceChainMakesBothPropositionsHealthy(t *testing.T) {
	scoped := InterpretMetadataScoped(fullyProvenanced())

	if !scoped.AnswerAuthority || !scoped.BinaryBuildStampComplete {
		t.Fatalf("answer=%v provenance=%v; both should hold", scoped.AnswerAuthority, scoped.BinaryBuildStampComplete)
	}
	if scoped.Reason != "" {
		t.Fatalf("a healthy world reported a reason: %q", scoped.Reason)
	}
	if !InterpretMetadataAuthority(fullyProvenanced()).Authoritative {
		t.Fatal("the combined reading did not turn when both propositions hold")
	}
}

// A graph that cannot answer fails the answer proposition first, whatever its
// provenance says. The order matters: reporting "provenance incomplete" about a
// stale graph would send a reader to repair the wrong thing.
func TestAnUnusableGraphFailsTheAnswerPropositionFirst(t *testing.T) {
	stale := fullyProvenanced()
	stale.Authority.Verdict = awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE
	stale.Authority.Authoritative = false
	stale.Authority.GraphFreshnessState = awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE
	stale.GraphFreshnessState = awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE
	stale.SeedState = awarenesspb.SeedState_SEED_STATE_STALE

	scoped := InterpretMetadataScoped(stale)
	if scoped.AnswerAuthority {
		t.Fatal("a stale graph reported that its answers can be trusted")
	}
	if strings.Contains(scoped.Reason, "build stamp") {
		t.Fatalf("a stale graph was reported as a build-stamp problem: %q", scoped.Reason)
	}
}

// The false-green case this repair exists to remove: the canonical verdict
// refuses, and every top-level field a private reconstruction would have read
// looks healthy.
//
// A scoped reading that rebuilt the answer from freshness/seed/marker/count
// would say "usable" here, because those fields say nothing about the closure
// proof or the transaction certification the canonical verdict weighs.
func TestACanonicalRefusalIsNotOverriddenByHealthyLookingFields(t *testing.T) {
	m := fullyProvenanced()
	m.Authority.Verdict = awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE
	m.Authority.Authoritative = false
	m.Authority.EmbeddedTransactionMatchesSeed = false
	m.Authority.GraphFreshnessDetail = "transaction certification: runtime transaction stamp missing"
	// Everything a reconstruction would have looked at still reads healthy.
	if m.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT ||
		m.GetSeedState() != awarenesspb.SeedState_SEED_STATE_CURRENT ||
		!m.GetLiveStoreContainsEmbeddedSeedMarker() || m.GetTripleCount() == 0 {
		t.Fatal("this fixture is only meaningful while the top-level fields look healthy")
	}

	scoped := InterpretMetadataScoped(m)
	if scoped.AnswerAuthority {
		t.Fatal("a canonical refusal was overridden by healthy-looking top-level fields; " +
			"the scoped reading is reconstructing authority instead of consuming it")
	}
	if !strings.Contains(scoped.Reason, "transaction") {
		t.Fatalf("the reason does not carry the canonical refusal: %q", scoped.Reason)
	}
}

// The combined reading is DERIVED from the two, never computed again. A third
// answer is exactly what this whole repair exists to prevent.
func TestTheCombinedReadingIsTheConjunctionAndNotAThirdAnswer(t *testing.T) {
	for name, m := range map[string]*awarenesspb.MetadataResponse{
		"divergent": divergentWorld(),
		"healthy":   fullyProvenanced(),
		"nil":       nil,
	} {
		scoped := InterpretMetadataScoped(m)
		want := scoped.AnswerAuthority && scoped.BinaryBuildStampComplete
		if got := InterpretMetadataAuthority(m).Authoritative; got != want {
			t.Fatalf("%s: combined reading %v, conjunction of the two %v", name, got, want)
		}
	}
}
