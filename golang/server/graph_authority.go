// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/closure"
	"github.com/globulario/sensei/golang/graphgeneration"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
)

func (s *server) graphAuthority(ctx context.Context) *awarenesspb.GraphAuthority {
	return s.graphAuthorityFor(ctx, "")
}

// graphAuthorityFor resolves the publication for an explicitly requested
// domain. An empty request falls back to the server's home domain and SAYS SO
// through requested_domain, so a caller that asked about something else refuses
// rather than reading the answer to a different question.
func (s *server) graphAuthorityFor(ctx context.Context, publicationDomain string) *awarenesspb.GraphAuthority {
	snap := snapshotGraphFreshness(ctx, s)
	a := graphAuthorityFromSnapshotFor(snap, s, publicationDomain)
	// RESOLVED ONLY WHEN ASKED.
	//
	// This projection exists for start gates. Attaching it to every RPC that
	// carries authority put two extra store reads on impact, query, briefing
	// and reference-site lookups -- paths that never consume it -- and made the
	// composition below run twice per call for nothing.
	//
	// There is also no home-domain fallback. Answering an unasked question with
	// the server's favourite domain produces a well-formed receipt for
	// something the caller did not ask about, and "a receipt came back" is
	// exactly the evidence a careless consumer would accept.
	if strings.TrimSpace(publicationDomain) == "" {
		a.CurrentPublication = &awarenesspb.DomainPublication{
			Resolution: awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNSPECIFIED,
			Detail:     "no publication_domain was requested, so no publication was resolved",
		}
		return a
	}
	pub := resolveCurrentPublication(ctx, s, publicationDomain)

	// ONE GENERATION, OR NEITHER CLAIM.
	//
	// Freshness and the publication are two separate reads, and an earlier
	// version of this function merely ASSERTED in a comment that they described
	// the same store. A publication landing between them yields a composite
	// whose graph digest describes generation A while its receipt describes
	// generation B -- individually accurate in both halves, false as a whole,
	// and precisely the composition a start gate would rely on.
	//
	// So the generation is re-read afterwards and required to be the one the
	// freshness verdict describes. A change is not resolved in favour of either
	// read: what this call observed was two worlds, so it reports that it
	// cannot attest to one.
	after := snapshotGraphFreshness(ctx, s)
	if before, now := snap.verification.Live.Digest, after.verification.Live.Digest; before != now {
		pub = &awarenesspb.DomainPublication{
			Resolution:      awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE,
			RequestedDomain: publicationDomain,
			Domain:          publicationDomain,
			Detail: fmt.Sprintf(
				"the served generation changed while this authority was being composed (%s -> %s), "+
					"so its freshness and its publication receipt would describe different worlds",
				shortDigest(before), shortDigest(now)),
		}
	}
	a.CurrentPublication = pub
	return a
}

func graphAuthorityFromSnapshot(snap graphFreshnessSnapshot, s *server) *awarenesspb.GraphAuthority {
	return graphAuthorityFromSnapshotFor(snap, s, "")
}

func graphAuthorityFromSnapshotFor(snap graphFreshnessSnapshot, s *server, closureDomain string) *awarenesspb.GraphAuthority {
	stamp, txMode, txPath, txReadErr := transactionStampForGraph(s)
	transactionMatchesSeed, transactionDetail := evaluateTransactionForGraph(
		snap.verification.Expected,
		stamp,
		txMode,
		txPath,
		txReadErr,
	)
	// Freshness and semantic validity are SEPARATE dimensions.
	//
	// This used to read `Authoritative: snap.verification.State == FreshnessCurrent`,
	// i.e. authority was defined as "the store matches the last publication".
	// On 2026-08-05 a `sensei build --repo globular` run from the wrong working
	// directory published the sensei corpus into the services domain. The store
	// matched its marker perfectly, so this returned authoritative=true and
	// freshness=CURRENT while certifying services commit d7c1a87c — and
	// resolve(four_layer.layer_has_single_writing_actor) returned found:false.
	// The publication was fresh; the knowledge was the wrong repository's.
	//
	// A fresh publication of the wrong corpus is still the wrong corpus, so
	// authority now additionally requires a closure proof bound to THIS
	// publication. The evaluator is the shared one in golang/closure that
	// `sensei domain-closure` uses — one canonical implementation, not two that
	// can drift apart.
	semanticState, semanticDetail := graphClosureStateFor(s, snap, closureDomain)
	freshnessCurrent := snap.verification.State == seedmeta.FreshnessCurrent

	detail := snap.verification.Detail
	if semanticState != closure.SemanticClosureProven {
		// Say WHY, and keep the freshness detail: "fresh but not closed" is the
		// distinction that was impossible to express before.
		detail = string(semanticState) + ": " + semanticDetail + " | freshness: " + snap.verification.Detail
	}

	// ONE predicate, two surfaces. `verdict` is the machine-readable field;
	// `authoritative` is retained for compatibility and derived from the same
	// value here so the two can never disagree. Deriving the bool from the
	// verdict (rather than computing it twice) is what makes that structural
	// instead of a convention someone must remember.
	authoritative := freshnessCurrent && semanticState == closure.SemanticClosureProven
	verdict := awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE
	if authoritative {
		verdict = awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE
	}

	return &awarenesspb.GraphAuthority{
		Verdict:                         verdict,
		Authoritative:                   authoritative,
		GraphFreshnessState:             graphFreshnessStateProto(snap.verification.State),
		GraphFreshnessDetail:            detail,
		BuildProvenanceState:            graphAuthorityBuildProvenance(),
		SeedState:                       graphFreshnessSeedState(snap.verification),
		GraphBuildCommit:                BuildCommit,
		GraphBuildTimeUnix:              parseUnixStamp(BuildTimeUnix),
		SourceRepoCommit:                SourceCommit,
		EmbeddedSeedDigestSha256:        snap.verification.Expected.Digest,
		LiveStoreGraphDigestSha256:      snap.verification.Live.Digest,
		LiveStoreGraphTripleCount:       snap.verification.LiveTripleCount,
		EmbeddedTransactionStampPresent: stamp.Present,
		CertifiedAwarenessGraphCommit:   stamp.AwarenessGraphCommit,
		CertifiedServicesRepoCommit:     stamp.ServicesCommit,
		EmbeddedTransactionMatchesSeed:  transactionMatchesSeed,
		EmbeddedTransactionDetail:       transactionDetail,
	}
}

// graphClosureState resolves the semantic verdict for the live publication.
//
// Fail-closed in every direction. When no marker path is configured the server
// cannot locate a closure report at all, and "I could not check" must never be
// reported as "it passed" — that is the precise shape of the defect this whole
// change exists to remove.
// closureDomain is the domain the closure verdict is ABOUT. Empty means the
// server's own home domain.
func graphClosureState(s *server, snap graphFreshnessSnapshot) (closure.SemanticState, string) {
	return graphClosureStateFor(s, snap, "")
}

func graphClosureStateFor(s *server, snap graphFreshnessSnapshot, closureDomain string) (closure.SemanticState, string) {
	// Injectable at the same seam the fake store injects freshness: a fixture
	// that declares its synthetic publication current must be able to declare it
	// closed too. The DEFAULT (nil) is the real file-based evaluator, so
	// production never gains a bypass — omitting the hook fails closed.
	if s != nil && s.closureEval != nil {
		// The hook carries the SAME referent the verdict is about, so a test
		// can prove the requested domain reached the closure evaluation.
		return s.closureEval(closureDomain)
	}
	if s == nil {
		return closure.SemanticClosureUnproven, "no server context, so no closure report can be located"
	}
	// The store-scoped proof set is authoritative when it describes the
	// publication that is actually live. It holds one proof per registered
	// domain, so a rebuild of some OTHER domain no longer takes authority away
	// from this one.
	if state, detail, ok := storeScopedClosureState(s, snap.verification.Live.Digest, closureDomain); ok {
		return state, detail
	}
	// Fall back to this repository's own copy. Reached when the proof set has
	// not been published yet, or when it describes a different generation than
	// the store currently holds — a `--all` build, for instance, which still
	// writes only the legacy per-repository files. Falling back is safe because
	// the legacy evaluator is itself fail-closed.
	if s.graphMarkerFile == "" {
		return closure.SemanticClosureUnproven,
			"no graph marker path configured, so no closure report can be located for this publication"
	}
	state, detail, _ := closure.Evaluate(s.graphMarkerFile, snap.verification.Live.Digest)
	return state, detail
}

// storeScopedClosureState resolves this server's domain against the store's
// published proof set.
//
// Returns ok=false — meaning "use the legacy path" — only when no proof set
// applies to the live publication. Once a set does apply it is the answer,
// including when the answer is that this domain is unproven: a set that covers
// the live generation and does not vouch for this domain is a real negative,
// not a reason to go looking for a more agreeable file elsewhere.
func storeScopedClosureState(s *server, liveDigest, closureDomain string) (closure.SemanticState, string, bool) {
	if s.oxigraphQueryURL == "" || strings.TrimSpace(liveDigest) == "" {
		return "", "", false
	}
	dir, err := graphgeneration.Dir(s.oxigraphQueryURL)
	if err != nil {
		return "", "", false
	}
	set, err := graphgeneration.Load(dir)
	if err != nil || set == nil {
		return "", "", false
	}
	if !strings.EqualFold(set.Marker.Digest, liveDigest) {
		return "", "", false
	}

	// THE CLOSURE MUST ANSWER FOR THE DOMAIN BEING ASKED ABOUT.
	//
	// This read s.homeDomain unconditionally while the publication receipt was
	// resolved for the REQUESTED domain, so one response could pair an
	// AUTHORITATIVE verdict earned by the home domain's proof with a VERIFIED
	// receipt for a foreign domain that has no proof at all. A compound
	// attestation whose parts describe different referents is not an
	// attestation.
	domain := strings.TrimSpace(closureDomain)
	if domain == "" {
		domain = strings.TrimSpace(s.homeDomain)
	}
	if domain == "" {
		return closure.SemanticClosureUnproven,
			"the store's proof set covers this publication but this server declares no domain, so none of its proofs can be claimed", true
	}
	proof, ok := set.ProofFor(domain)
	if !ok {
		return closure.SemanticClosureUnproven, fmt.Sprintf(
			"the proof set for publication %s carries no proof for domain %q — rebuild that domain to publish one",
			shortDigest(liveDigest), domain), true
	}
	if proof.Report == nil {
		detail := proof.CarryForwardRefusal
		if detail == "" {
			detail = "no closure report was recorded for this domain in the live publication"
		}
		return closure.SemanticClosureUnproven, detail, true
	}
	// One evaluator, not two. The report is handed to the same verdict logic the
	// legacy path uses so the two surfaces cannot drift into disagreeing about
	// what a report means.
	state, detail := closure.EvaluateReport(proof.Report, liveDigest)
	return state, detail, true
}

func shortDigest(d string) string {
	if len(d) <= 12 {
		return d
	}
	return d[:12]
}

func graphAuthorityBuildProvenance() awarenesspb.BuildProvenanceState {
	return classifyBuildProvenance(&awarenesspb.MetadataResponse{
		ServerVersion:      Version,
		GraphBuildCommit:   BuildCommit,
		SourceRepoCommit:   SourceCommit,
		GraphBuildTimeUnix: parseUnixStamp(BuildTimeUnix),
	})
}
