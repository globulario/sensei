// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/closure"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// THE SAME SHAPE AS #342, ONE OWNER FARTHER UPSTREAM.
//
// #342 restored the transaction conjunct: the evidence was on the wire and the
// conclusion ignored it. This is that mechanism again. The server resolves a
// per-domain publication receipt, authenticates it by recomputing its identity,
// and binds it to the served generation -- and then composes the verdict from a
// weaker proxy that cannot describe a project domain at all.
//
// The v1 transaction stamp is authored as a CROSS-REPO certification: the proto
// names certified_awareness_graph_commit and certified_services_repo_commit,
// and buildTransactionTSV is hardwired around agRepo and svcRepo. Run for any
// other domain it emits both repository identities as "missing" while its seed
// digest still agrees, and evaluateTransactionForGraph -- which reads only the
// seed digest and triple count -- calls that certification.
//
// So "transactionMatchesSeed" does not mean "this domain publication is
// certified". These controls state what does.

const legacyHomeDomain = "github.com/globulario/sensei"

// certificationWorld is FRESH and CLOSED, so certification is the only variable
// left. Every fixture below differs from it in exactly one dimension.
type certificationWorld struct {
	s          *server
	generation string
}

func freshClosedServer(t *testing.T, st store.Store) *server {
	t.Helper()
	s := newServer(st)
	s.homeDomain = legacyHomeDomain
	s.closureEval = func(string) (closure.SemanticState, string) {
		return closure.SemanticClosureProven, "closure report vouches for this publication"
	}
	return s
}

// pinnedFreshness reports one CURRENT generation as both live and expected, so
// a transaction stamp naming that digest certifies these exact bytes.
func pinnedFreshness(generation string) func(context.Context) seedmeta.Verification {
	return func(context.Context) seedmeta.Verification {
		return seedmeta.Verification{
			State:           seedmeta.FreshnessCurrent,
			Live:            seedmeta.Marker{Digest: generation, TripleCount: 7},
			Expected:        seedmeta.Marker{Digest: generation, TripleCount: 7},
			LiveTripleCount: 7,
			Detail:          "live store matches the expected validated graph artifact",
		}
	}
}

// publishedWorld serves one receipt through the single-evaluation snapshot read.
func publishedWorld(t *testing.T, r publication.Receipt, generation string) certificationWorld {
	t.Helper()
	st := snapshotStore{
		fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)},
		receipt:   r,
		marker:    generation,
	}
	return certificationWorld{s: freshClosedServer(t, st), generation: generation}
}

// absentPublicationStore can be asked and answers that nothing was published.
// That is a different world from a store that cannot be asked at all, and the
// legacy path is admissible only in this one.
type absentPublicationStore struct {
	fakeStore
}

func (absentPublicationStore) DescribeAuthoritySnapshot(context.Context, string) (store.AuthoritySnapshot, error) {
	return store.AuthoritySnapshot{}, nil
}

func (absentPublicationStore) DescribeTerms(context.Context, string) ([]store.Statement, error) {
	return nil, nil
}

// writeStamp puts a v1 transaction beside the marker this server reads.
func writeStamp(t *testing.T, s *server, generation string, agCommit, svcCommit string) {
	t.Helper()
	s.graphMarkerFile = t.TempDir() + "/graph-authority.json"
	stamp := "format\tv1\n" +
		"seed\tdigest_sha256\t" + generation + "\n" +
		"seed\ttriple_count\t" + strconv.FormatInt(7, 10) + "\n" +
		"repo\tawareness-graph\t" + agCommit + "\n" +
		"repo\tservices\t" + svcCommit + "\n"
	if err := os.WriteFile(seedmeta.RuntimeTransactionPath(s.graphMarkerFile), []byte(stamp), 0o644); err != nil {
		t.Fatalf("write transaction stamp: %v", err)
	}
}

func verdictFor(t *testing.T, s *server, requestedDomain string) *awarenesspb.GraphAuthority {
	t.Helper()
	a := s.graphAuthorityFor(context.Background(), requestedDomain)
	if a == nil {
		t.Fatal("no authority verdict")
	}
	return a
}

// ---------------------------------------------------------------- positive --

// POSITIVE CONTROL. A verified, CLEAN_EXACT receipt for the domain being asked
// about, read from the generation being served, certifies the graph.
//
// Without this every control below would pass for a server that refuses
// everything, which is the failure mode a certification predicate invites.
func TestAVerifiedCleanExactPublicationCertifiesTheGraph(t *testing.T) {
	generation := strings.Repeat("a", 64)
	w := publishedWorld(t, healthyReceipt(), generation)

	a := verdictFor(t, w.s, pubTestDomain)

	if a.GetCurrentPublication().GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("fixture premise broken: resolution = %v (%s)",
			a.GetCurrentPublication().GetResolution(), a.GetCurrentPublication().GetDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("verdict = %s with a verified CLEAN_EXACT publication for this generation: %s",
			a.GetVerdict(), a.GetGraphFreshnessDetail())
	}
	// And no v1 stamp was involved: this world has none.
	if a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatal("the fixture wrote no transaction stamp and one was reported as certifying")
	}
}

// ------------------------------------------------- the receipt's own limits --

// A DIRTY publication records its revision because that is useful, and the
// receipt contract says it must never be read as "produced from that commit".
// Certifying it would turn an accurately recorded limitation into authority.
func TestADirtyPublicationDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	dirty := healthyReceipt()
	dirty.State = publication.Dirty
	w := publishedWorld(t, dirty, generation)

	a := verdictFor(t, w.s, pubTestDomain)

	if a.GetCurrentPublication().GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("fixture premise broken: the receipt must authenticate, only its STATE is dirty: %v",
			a.GetCurrentPublication().GetResolution())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a DIRTY publication was certified; what was compiled is not what that revision holds")
	}
	if !strings.Contains(a.GetGraphFreshnessDetail(), "DIRTY") {
		t.Fatalf("the refusal does not name the state that caused it: %q", a.GetGraphFreshnessDetail())
	}
}

// UNKNOWN is what the publisher DOWNGRADES to when the checkout moved between
// compilation and publication, or when the consumed bytes could not be proven
// against the revision. Certifying it would defeat that downgrade.
func TestAnUnknownStatePublicationDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	unknown := healthyReceipt()
	unknown.State = publication.Unknown
	unknown.Revision, unknown.Tree = "", ""
	w := publishedWorld(t, unknown, generation)

	a := verdictFor(t, w.s, pubTestDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("an UNKNOWN publication was certified; it names no revision at all")
	}
}

// The receipt authenticates and answers for a DIFFERENT domain. A compound
// verdict whose parts describe different referents is not an attestation.
func TestAReceiptForAnotherDomainDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	foreign := healthyReceipt()
	foreign.Domain = "github.com/globulario/some-other-repo"
	w := publishedWorld(t, foreign, generation)

	a := verdictFor(t, w.s, pubTestDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a receipt for another domain certified this one")
	}
}

// The receipt authenticates and was read from a different generation than the
// one being served. Individually accurate halves, false as a whole.
func TestAReceiptFromAnotherGenerationDoesNotCertify(t *testing.T) {
	served := strings.Repeat("a", 64)
	st := snapshotStore{
		fakeStore: fakeStore{graphFreshness: pinnedFreshness(served)},
		receipt:   healthyReceipt(),
		marker:    strings.Repeat("b", 64), // the publication was read from B
	}
	s := freshClosedServer(t, st)

	a := verdictFor(t, s, pubTestDomain)

	if a.GetVerdict() == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatal("a receipt from another generation certified the served one")
	}
}

// --------------------------------------------- no weaker route around a NO --

// ORDERED ADMISSIBILITY, NOT INTERCHANGEABLE EVIDENCE.
//
// The publication evidence exists and is broken. A legacy stamp that happens to
// match must not route around it: that is the weaker mechanism overruling the
// stronger one precisely when the stronger one is reporting a problem.
//
// UNREADABLE and ABSENT are different worlds, and resolveCurrentPublication
// takes pains not to collapse them. This is that distinction one layer up.
func TestAnUnreadablePublicationIsNotRescuedByAValidLegacyStamp(t *testing.T) {
	generation := strings.Repeat("a", 64)
	// A pointer naming a receipt whose stored fields no longer hash to the IRI
	// they sit at: authenticated evidence, failing authentication.
	healthy := healthyReceipt()
	tampered := healthy
	tampered.Revision = "0000000000000000000000000000000000000000"
	st := tamperedPointerStore{
		fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)},
		pointerTo: healthy.IRI(),
		body:      tampered,
		marker:    generation,
	}
	s := freshClosedServer(t, st)
	// A legacy stamp that certifies this exact generation, in the legacy
	// topology, naming both repositories. On its own it would certify.
	s.homeDomain = legacyHomeDomain
	writeStamp(t, s, generation,
		"58c055fbd9d65ad2f5a8c965f728897012e75f09",
		"b98c91eb540a3e5aa9d97e7b3c08005e08cb1897")

	a := verdictFor(t, s, legacyHomeDomain)

	if a.GetCurrentPublication().GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("fixture premise broken: resolution = %v", a.GetCurrentPublication().GetResolution())
	}
	if !a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("fixture premise broken: the legacy stamp must certify this generation: %s",
			a.GetEmbeddedTransactionDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("broken publication evidence was routed around by a legacy stamp; " +
			"the weaker mechanism must not overrule the stronger one that is reporting a problem")
	}
}

// tamperedPointerStore returns a pointer to one receipt and a body that no
// longer hashes to it.
type tamperedPointerStore struct {
	fakeStore
	pointerTo string
	body      publication.Receipt
	marker    string
}

func (p tamperedPointerStore) DescribeAuthoritySnapshot(context.Context, string) (store.AuthoritySnapshot, error) {
	return store.AuthoritySnapshot{
		Pointer: asStatements(pointerTriples(p.body, p.pointerTo)),
		Receipt: asStatements(receiptTriples(p.body)),
		Marker: []store.Statement{{
			Predicate: "https://globular.io/awareness#seedDigestSha256",
			Object:    store.Term{Kind: store.TermLiteral, Value: p.marker},
		}},
	}, nil
}

func (tamperedPointerStore) DescribeTerms(context.Context, string) ([]store.Statement, error) {
	return nil, nil
}

// ------------------------------------------------------------ one referent --

// CLOSURE AND CERTIFICATION MUST SHARE ONE REFERENT.
//
// A verdict whose conjuncts describe different repositories is a compound
// attestation whose parts answer different questions -- the defect
// storeScopedClosureState was repaired to remove, one layer up.
//
// This asserts the referent IDENTITY rather than a downstream behaviour. A
// behavioural proxy here is worth very little: the first version of this test
// refused for want of a transaction stamp and would have passed with the
// referents wired to two different domains.
func TestClosureAndCertificationShareOneReferent(t *testing.T) {
	generation := strings.Repeat("a", 64)

	for _, requested := range []string{"", pubTestDomain} {
		w := publishedWorld(t, healthyReceipt(), generation)
		asked := &[]string{}
		w.s.closureEval = func(domain string) (closure.SemanticState, string) {
			*asked = append(*asked, domain)
			return closure.SemanticClosureProven, "proven"
		}

		ctx := context.Background()
		_, pub := graphAuthorityFromSnapshotFor(ctx, snapshotGraphFreshness(ctx, w.s), w.s, requested)

		if len(*asked) != 1 {
			t.Fatalf("requested %q: closure was evaluated %d times", requested, len(*asked))
		}
		if got := pub.GetRequestedDomain(); got != (*asked)[0] {
			t.Fatalf("requested %q: closure answered for %q while certification resolved %q -- "+
				"the two halves of this verdict describe different repositories",
				requested, (*asked)[0], got)
		}
		// And the shared value is the effective domain, not whatever each
		// evaluator would have picked for itself.
		want := effectiveAuthorityDomain(requested, w.s.homeDomain)
		if (*asked)[0] != want {
			t.Fatalf("requested %q: the referent was %q, want the effective domain %q",
				requested, (*asked)[0], want)
		}
	}
}

// The projection rule is unchanged by any of this: a caller who asked no
// publication question is not shown a home-domain receipt as though they had.
func TestAnUnscopedCallCertifiesItsHomeDomainAndProjectsNothing(t *testing.T) {
	generation := strings.Repeat("a", 64)
	w := publishedWorld(t, healthyReceipt(), generation)

	a := verdictFor(t, w.s, "")

	if got := a.GetCurrentPublication().GetResolution(); got != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNSPECIFIED {
		t.Fatalf("an unscoped call projected a publication (%v); the caller asked for none", got)
	}
	// The receipt is for sensei-code and the home domain is sensei, so the
	// verdict must not borrow it.
	if a.GetVerdict() == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatal("an unscoped verdict was certified by a receipt for another domain")
	}
}

// ------------------------------------------------------------------ legacy --

// THE WITNESS FROM THE FIELD, not an invented malformed case.
//
// This is what buildTransactionTSV actually emits when run for a project repo:
// resolveAGRepo finds no golang/server/embeddata, both repository slots become
// "missing", and the seed digest still agrees. Measured on 2026-09-04 against
// github.com/globulario/sensei-code.
//
// evaluateTransactionForGraph accepts it, because it reads only the seed digest
// and triple count. It must not certify a project publication.
func TestASeedOnlyStampWithNoRepositoryIdentitiesDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	s.homeDomain = pubTestDomain // a project domain, not the legacy topology
	writeStamp(t, s, generation, "missing", "missing")

	a := verdictFor(t, s, pubTestDomain)

	if !a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("fixture premise broken: the stamp's seed must agree, which is what makes it a lie: %s",
			a.GetEmbeddedTransactionDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a stamp naming no repository identity certified a project publication")
	}
}

// THE SAME LIE INSIDE THE LEGACY TOPOLOGY.
//
// The domain here IS one the v1 format can speak about, so nothing refuses on
// applicability grounds. What refuses is the stamp itself: it names no
// repository identity at all, which is what buildTransactionTSV emits whenever
// it cannot resolve its repositories -- including from the wrong working
// directory in the very topology the format was written for.
//
// Seed digest and triple count still agree, so evaluateTransactionForGraph
// calls it certification. It certifies nothing.
func TestALegacyDomainStampNamingNoRepositoriesDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st) // homeDomain is the legacy sensei domain
	writeStamp(t, s, generation, "missing", "missing")

	a := verdictFor(t, s, legacyHomeDomain)

	if !a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("fixture premise broken: the seed must agree, which is what makes the stamp a lie: %s",
			a.GetEmbeddedTransactionDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a stamp naming no repository identity certified a publication in the legacy topology")
	}
	if !strings.Contains(a.GetGraphFreshnessDetail(), "awareness-graph repository identity") {
		t.Fatalf("the refusal does not name what was missing: %q", a.GetGraphFreshnessDetail())
	}
}

// And the services half is checked too, so a stamp cannot pass by naming only
// the repository the check happens to look at first.
func TestALegacyStampNamingNoServicesIdentityDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	writeStamp(t, s, generation, "58c055fbd9d65ad2f5a8c965f728897012e75f09", "missing")

	a := verdictFor(t, s, legacyHomeDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a stamp naming no services identity certified the cross-repo topology")
	}
	if !strings.Contains(a.GetGraphFreshnessDetail(), "services repository identity") {
		t.Fatalf("the refusal does not name what was missing: %q", a.GetGraphFreshnessDetail())
	}
}

// A stamp from the legacy topology presented for a project domain does not
// certify it either. The commits are real; they are about other repositories.
func TestALegacyStampDoesNotCertifyAProjectDomain(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	s.homeDomain = pubTestDomain
	writeStamp(t, s, generation,
		"58c055fbd9d65ad2f5a8c965f728897012e75f09",
		"b98c91eb540a3e5aa9d97e7b3c08005e08cb1897")

	a := verdictFor(t, s, pubTestDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a sensei/services stamp certified a project domain publication")
	}
}

// THE LEGACY WORLD MUST KEEP WORKING. globulario/sensei publishes no per-domain
// receipt today, so its publication resolves ABSENT and the v1 stamp is the
// certification it has. Refusing it would take authority away from a graph
// whose certification model is the one v1 was authored for.
func TestTheLegacyCrossRepoTopologyStillCertifiesWhenNoPublicationExists(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	writeStamp(t, s, generation,
		"58c055fbd9d65ad2f5a8c965f728897012e75f09",
		"b98c91eb540a3e5aa9d97e7b3c08005e08cb1897")

	a := verdictFor(t, s, legacyHomeDomain)

	if a.GetCurrentPublication().GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT {
		t.Fatalf("fixture premise broken: resolution = %v", a.GetCurrentPublication().GetResolution())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("the legacy cross-repo topology lost its authority: %s", a.GetGraphFreshnessDetail())
	}
}

// The standalone self-only build is a legacy topology too: the build script
// writes "standalone" deliberately, and it is a stated fact rather than the
// "missing" a writer emits when it could not resolve a repository at all.
func TestTheStandaloneLegacyTopologyStillCertifies(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	writeStamp(t, s, generation, "61605175f42aa178b1fd256c240faa53b07d3a5e", "standalone")

	a := verdictFor(t, s, legacyHomeDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("the standalone legacy topology lost its authority: %s", a.GetGraphFreshnessDetail())
	}
}

// ------------------------------------------------- the generation witness --

// VERIFIED ANSWERS ONE QUESTION, AND CERTIFICATION ASKS TWO.
//
//	receipt identity VERIFIED   !=   receipt certifies the served generation
//
// resolveCurrentPublication returns VERIFIED through its DescribeTerms
// compatibility path, which carries no snapshot marker at all. Treating a
// missing generation witness as agreement is reading silence as proof -- and it
// is the one shape where "bound to the generation" was never established.
func TestAVerifiedReceiptWithNoGenerationWitnessDoesNotCertify(t *testing.T) {
	generation := strings.Repeat("a", 64)
	healthy := healthyReceipt()
	// The two-read path: this store offers DescribeTerms and no authority
	// snapshot, so nothing reports which generation the receipt was read from.
	st := publicationStore{
		fakeStore:    fakeStore{graphFreshness: pinnedFreshness(generation)},
		storedTarget: healthy.IRI(),
		body:         receiptTriples(healthy),
	}
	s := freshClosedServer(t, st)

	a := verdictFor(t, s, pubTestDomain)

	// The receipt really did authenticate. That is not downgraded, because it
	// is true: what is missing is the binding, not the identity.
	if got := a.GetCurrentPublication().GetResolution(); got != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("the receipt resolution was downgraded to %v; its identity verified", got)
	}
	if a.GetCurrentPublication().GetSnapshotGeneration() != "" {
		t.Fatalf("fixture premise broken: this path must carry no generation witness, got %q",
			a.GetCurrentPublication().GetSnapshotGeneration())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("a receipt with no generation witness certified the served generation; " +
			"nothing bound the two together")
	}
	if !strings.Contains(a.GetGraphFreshnessDetail(), "no generation witness") {
		t.Fatalf("the refusal does not name what was missing: %q", a.GetGraphFreshnessDetail())
	}
}

// ------------------------------------------------------ generation races --

// racingFreshness reports one generation on the first read and another on every
// read after it: a publication landing mid-composition.
func racingFreshness(first, rest string) func(context.Context) seedmeta.Verification {
	var calls int
	return func(ctx context.Context) seedmeta.Verification {
		calls++
		if calls == 1 {
			return pinnedFreshness(first)(ctx)
		}
		return pinnedFreshness(rest)(ctx)
	}
}

// A GENERATION CHANGE MID-COMPOSITION REVOKES THE VERDICT, not just the
// projection.
//
// The stability re-read used to happen after the conclusion and could only
// replace the projected publication. A world that was fresh, closed and
// certified on the first read therefore returned AUTHORITATIVE alongside an
// UNREADABLE publication saying the generation had moved -- a composite that
// contradicts itself, and the half a start gate reads is the confident one.
func TestAGenerationChangeMidCompositionRevokesTheAuthorityVerdict(t *testing.T) {
	a1, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	st := snapshotStore{
		fakeStore: fakeStore{graphFreshness: racingFreshness(a1, b)},
		receipt:   healthyReceipt(),
		marker:    a1, // the publication was read from the world the verdict started in
	}
	s := freshClosedServer(t, st)

	a := verdictFor(t, s, pubTestDomain)

	if got := a.GetCurrentPublication().GetResolution(); got != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("the publication resolution is %v; the world moved underneath it", got)
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("AUTHORITATIVE was returned beside an UNREADABLE publication reporting a " +
			"generation change: the two halves of this response contradict each other")
	}
	if a.GetAuthoritative() {
		t.Fatal("the compatibility bool disagreed with the verdict")
	}
	if !strings.Contains(a.GetGraphFreshnessDetail(), "changed while this authority was being composed") {
		t.Fatalf("the refusal does not say the world moved: %q", a.GetGraphFreshnessDetail())
	}
}

// POSITIVE CONTROL for the race: a world that holds still still certifies.
// Without this the test above passes for a server that refuses every fixture
// whose freshness is read more than once.
func TestAStableGenerationStillCertifies(t *testing.T) {
	a1 := strings.Repeat("a", 64)
	st := snapshotStore{
		fakeStore: fakeStore{graphFreshness: racingFreshness(a1, a1)}, // A -> A
		receipt:   healthyReceipt(),
		marker:    a1,
	}
	s := freshClosedServer(t, st)

	a := verdictFor(t, s, pubTestDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("a world that held still was refused: %s", a.GetGraphFreshnessDetail())
	}
}

// ------------------------------------------- what counts as an identity --

// A SENTINEL BLACKLIST IS NOT A VALIDATOR.
//
// Rejecting "", "missing" and "standalone" while accepting everything else
// makes "potato" a repository identity, and the legacy admissibility claim --
// that the stamp names the repositories it certifies -- untrue for any value
// nobody thought to exclude.
func TestArbitraryProseIsNotARepositoryIdentity(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st) // the legacy domain: nothing else refuses
	writeStamp(t, s, generation, "potato", "banana")

	a := verdictFor(t, s, legacyHomeDomain)

	if !a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("fixture premise broken: the seed must agree: %s", a.GetEmbeddedTransactionDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("arbitrary prose was accepted as two repository identities")
	}
}

// The sentinel the self-only build writes when git cannot be read is not an
// identity either. It was accepted before, because it is not one of the three
// values the old blacklist named -- which is exactly why a blacklist fails.
func TestTheUnknownSentinelIsNotARepositoryIdentity(t *testing.T) {
	generation := strings.Repeat("a", 64)
	st := absentPublicationStore{fakeStore: fakeStore{graphFreshness: pinnedFreshness(generation)}}
	s := freshClosedServer(t, st)
	writeStamp(t, s, generation, "unknown", "standalone")

	a := verdictFor(t, s, legacyHomeDomain)

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatal("the 'unknown' git fallback was accepted as an awareness-graph identity")
	}
}

// The identity shape itself, over the forms the writers actually emit. Both
// call `git rev-parse HEAD`, so a full object ID is the authored shape -- 40
// hex under SHA-1, 64 under SHA-256 -- and an abbreviation is not one.
func TestARepositoryIdentityIsAFullGitObjectID(t *testing.T) {
	for value, want := range map[string]bool{
		"58c055fbd9d65ad2f5a8c965f728897012e75f09":  true,  // real SHA-1 head
		"61605175f42aa178b1fd256c240faa53b07d3a5e":  true,  // real SHA-1 head
		strings.Repeat("a", 64):                     true,  // SHA-256 repositories
		"58c055fbd9d6":                              false, // abbreviated
		"58c055fbd9d65ad2f5a8c965f728897012e75f0":   false, // 39
		"58c055fbd9d65ad2f5a8c965f728897012e75f090": false, // 41
		"58c055fbd9d65ad2f5a8c965f728897012e75zzz":  false, // not hex
		"potato":     false,
		"unknown":    false,
		"missing":    false,
		"standalone": false,
		"":           false,
		"  58c055fbd9d65ad2f5a8c965f728897012e75f09  ": true, // surrounding space is not content
		"HEAD":            false,
		"refs/heads/main": false,
	} {
		if got := namesRepository(value); got != want {
			t.Fatalf("namesRepository(%q) = %v, want %v", value, got, want)
		}
	}
}
