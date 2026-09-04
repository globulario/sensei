// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// The world this exists for, preserved as a fixture rather than described.
//
// The specimen is github.com/globulario/sensei on :10121, observed 2026-09-03:
// the live graph matched its validated artifact, the seed was current, a
// runtime transaction CERTIFIED that publication -- and build provenance was
// still INCOMPLETE, because the serving binary carried no link-time
// source-repo commit.
//
// That is the legitimate independent-propositions case, and it is coherent:
// every field agrees, and two different questions get two different answers.
//
// It deliberately replaces an earlier fixture built from the sensei-code
// reading of the same day, which had authoritative=true beside
// transaction_matches_seed=false. Once the canonical verdict regained its
// transaction conjunct that combination stopped being a world at all -- it was
// a response disagreeing with itself, and preserving it would have meant
// keeping the defect alive as a fixture.
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
		TripleCount:                         141543,
		// Certified, as :10121 is. The graph's publication is signed; the
		// SERVER's build stamp is what is missing.
		EmbeddedTransactionStampPresent: true,
		EmbeddedTransactionMatchesSeed:  true,
		CertifiedAwarenessGraphCommit:   "58c055fbd9d65ad2f5a8c965f728897012e75f09",
		CertifiedServicesRepoCommit:     "b98c91eb540a3e5aa9d97e7b3c08005e08cb1897",
	}
}

// fullyProvenanced is the same world with the provenance chain stated.
func fullyProvenanced() *awarenesspb.MetadataResponse {
	m := divergentWorld()
	// A SERVICES commit, semantically distinct from the awareness-graph SHA:
	// they are different repositories, and a fixture that reuses one value for
	// both would pass while proving nothing about which field carries which.
	m.SourceRepoCommit = "b98c91eb540a3e5aa9d97e7b3c08005e08cb1897"
	m.BuildProvenanceState = awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED
	m.Authority.BuildProvenanceState = awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED
	return m
}

// The divergent state is COHERENT, not contradictory. Asserting both halves in
// one test is the point: a reader who sees only one of them learns the wrong
// thing.
func TestAGraphCanAnswerAuthoritativelyWithAnUnstampedBinary(t *testing.T) {
	scoped := InterpretMetadataScoped(divergentWorld())

	if !scoped.AnswerAuthority {
		t.Fatalf("a graph whose canonical verdict is AUTHORITATIVE was reported as unable to answer: %q",
			scoped.Reason)
	}
	// The fixture is coherent: the publication IS certified. What is missing is
	// the serving binary's own build stamp.
	if !divergentWorld().GetEmbeddedTransactionMatchesSeed() {
		t.Fatal("the fixture is back to a response that disagrees with itself")
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

// The verdict and its explanation must come from the same evaluation.
//
// InterpretMetadataAuthority took the BOOLEAN from the repaired path and then
// recomputed the WHY from the old top-level metadata fields. Those fields carry
// neither the closure proof nor the transaction certification, so a graph the
// canonical verdict refuses for a missing transaction was explained as "graph
// metadata is not authoritative (current)" -- a sentence that names the state
// the reader can already see and none of the reason they need.
//
// This type's own doc calls it the single source of truth for "can I trust this
// answer, and if not why". Half of that was true.
func TestTheRefusalCarriesTheCanonicalReasonNotARecomputedOne(t *testing.T) {
	m := fullyProvenanced()
	m.Authority.Verdict = awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE
	m.Authority.Authoritative = false
	m.Authority.EmbeddedTransactionMatchesSeed = false
	m.Authority.GraphFreshnessDetail = "transaction certification: runtime transaction stamp missing | freshness: live store matches expected validated graph artifact"
	// Every top-level field a recomputed explanation would read still looks
	// healthy, which is what makes the recomputation produce the wrong reason
	// rather than no reason.
	if m.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT ||
		m.GetSeedState() != awarenesspb.SeedState_SEED_STATE_CURRENT ||
		m.GetBuildProvenanceState() != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED {
		t.Fatal("this fixture is only meaningful while the top-level fields look healthy")
	}

	v := InterpretMetadataAuthority(m)
	if v.Authoritative {
		t.Fatal("a canonical refusal was reported as authoritative")
	}
	if !strings.Contains(v.Warning, "transaction") {
		t.Fatalf("the refusal does not carry the canonical reason: %q", v.Warning)
	}
	if strings.Contains(v.Warning, "graph metadata is not authoritative") {
		t.Fatalf("the refusal recomputed a reason from the top-level fields: %q", v.Warning)
	}
	// The verdict and the scoped reading must be the same evaluation, not two
	// that happen to agree.
	if scoped := InterpretMetadataScoped(m); v.Warning != scoped.Reason {
		t.Fatalf("the combined verdict explains itself differently from the scoped reading:\n got %q\nwant %q",
			v.Warning, scoped.Reason)
	}
}

// A refusal on the binary build stamp is explained as that, not as a graph
// problem. Without this the test above would pass for a version that always
// reports the answer-authority reason.
func TestAStampRefusalIsExplainedAsTheStamp(t *testing.T) {
	v := InterpretMetadataAuthority(divergentWorld())

	if v.Authoritative {
		t.Fatal("an incomplete build stamp was reported as authoritative")
	}
	if !strings.Contains(v.Warning, "build stamp") {
		t.Fatalf("the refusal does not name the build stamp: %q", v.Warning)
	}
	if !strings.Contains(v.Warning, "says nothing about which commits produced the graph") {
		t.Fatalf("the refusal overclaims what a build stamp proves: %q", v.Warning)
	}
}

// And a healthy world is still authoritative, with no warning at all.
func TestAHealthyWorldCarriesNoWarning(t *testing.T) {
	v := InterpretMetadataAuthority(fullyProvenanced())

	if !v.Authoritative || v.Verdict != "authoritative" {
		t.Fatalf("authoritative=%v verdict=%q warning=%q", v.Authoritative, v.Verdict, v.Warning)
	}
	if v.Warning != "" {
		t.Fatalf("a healthy world carried a warning: %q", v.Warning)
	}
}
