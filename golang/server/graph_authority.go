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
	// ONE RESOLUTION. The verdict and the projection describe the same read of
	// the same publication: resolving twice would let a publication landing
	// between them produce a response whose conclusion and whose evidence
	// describe different worlds.
	a, pub := graphAuthorityFromSnapshotFor(ctx, snap, s, publicationDomain)
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
	// The generation binding and the mid-composition stability re-read used to
	// live HERE, after the conclusion, and could only decorate the projection
	// with a warning the verdict did not share. Both are authority evidence, so
	// both now participate in the one conclusion -- see certifyPublication.
	a.CurrentPublication = pub
	return a
}

func graphAuthorityFromSnapshot(ctx context.Context, snap graphFreshnessSnapshot, s *server) *awarenesspb.GraphAuthority {
	a, _ := graphAuthorityFromSnapshotFor(ctx, snap, s, "")
	return a
}

// effectiveAuthorityDomain is the ONE referent an authority verdict concerns.
//
// It answers "what domain does this server's authority verdict concern", which
// is a different question from "what publication did the caller ask me to
// show". Both conjuncts that have a referent -- the closure proof and the
// publication certification -- take this exact value, and neither carries a
// fallback of its own. Two evaluators each resolving their own domain is how a
// verdict becomes a compound attestation whose halves describe different
// repositories: the defect storeScopedClosureState was repaired to remove, made
// structurally impossible here rather than warned about there.
func effectiveAuthorityDomain(requested, home string) string {
	if d := strings.TrimSpace(requested); d != "" {
		return d
	}
	return strings.TrimSpace(home)
}

// graphAuthorityFromSnapshotFor composes the verdict and returns the
// publication it was certified against, so a caller that also projects the
// publication shows the same read the conclusion rests on.
func graphAuthorityFromSnapshotFor(ctx context.Context, snap graphFreshnessSnapshot, s *server, requestedDomain string) (*awarenesspb.GraphAuthority, *awarenesspb.DomainPublication) {
	effectiveDomain := effectiveAuthorityDomain(requestedDomain, serverHomeDomain(s))
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
	semanticState, semanticDetail := graphClosureStateFor(s, snap, effectiveDomain)
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
	//
	// THREE conjuncts, and the third was missing for three weeks.
	//
	// transactionMatchesSeed was computed above, returned as an evidence field,
	// and left out of the conclusion. The wire contract had said otherwise the
	// whole time -- MetadataResponse.authority then read "freshness AND the
	// closure proof bound to this publication AND the transaction certification
	// -- not freshness alone" -- and so did the commit that authored it
	// (aa0e757d, #176) and the test fixture comment beside it. Three authored
	// statements, one implementation, and they disagreed.
	//
	// That proto text is quoted in the PAST TENSE because it no longer stands:
	// the third conjunct is now publication certification, of which the legacy
	// transaction is one ordered case. When the implementation changed owners
	// the contract was restated to match, rather than being left to describe a
	// predicate the server had stopped computing -- which is the same defect
	// this comment is about, and would have been a poor way to close it.
	//
	// The gap permitted exactly this, observed live on 2026-09-03 against
	// github.com/globulario/sensei-code:
	//
	//	authoritative                      true
	//	embedded_transaction_matches_seed  false
	//
	// A publication no transaction certifies is one nobody signed. The evidence
	// was on the wire and the conclusion ignored it, which is the same shape as
	// the #176 defect this function was written to close: a cheap surface
	// reading green while the fact it rests on says otherwise.
	// AND THE CERTIFICATION IS THE PUBLICATION'S, NOT A PROXY FOR IT.
	//
	// transactionMatchesSeed does not mean "this domain publication is
	// certified". The v1 stamp is authored as a CROSS-REPO certification --
	// certified_awareness_graph_commit, certified_services_repo_commit, and a
	// writer hardwired around agRepo/svcRepo -- and evaluateTransactionForGraph
	// reads only the seed digest and triple count. Run for a project domain the
	// writer emits both repository identities as "missing" while the seed still
	// agrees, so the conjunct would be satisfied by a stamp certifying nothing.
	// Measured on 2026-09-04 against github.com/globulario/sensei-code.
	//
	// The stronger record already existed and the conclusion did not consult
	// it: a per-domain receipt, authenticated by recomputing its identity, and
	// bound to the served generation by being CONTAINED IN it. That is the same
	// shape as the conjunct #342 restored, one owner farther upstream.
	certified, certificationDetail, pub := certifyPublication(
		ctx, s, effectiveDomain, snap, stamp, transactionMatchesSeed, transactionDetail)
	if freshnessCurrent && semanticState == closure.SemanticClosureProven && !certified {
		// Otherwise the refusal renders as the freshness detail, which says the
		// graph is fine.
		detail = "publication certification: " + certificationDetail + " | freshness: " + snap.verification.Detail
	}

	authoritative := freshnessCurrent &&
		semanticState == closure.SemanticClosureProven &&
		certified
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
	}, pub
}

// graphClosureState resolves the semantic verdict for the live publication.
//
// Fail-closed in every direction. When no marker path is configured the server
// cannot locate a closure report at all, and "I could not check" must never be
// reported as "it passed" — that is the precise shape of the defect this whole
// change exists to remove.
// authorityDomain is the referent the verdict is ABOUT, already resolved by
// effectiveAuthorityDomain. It is no longer merely a closure parameter: the
// certification conjunct takes the same value, so this evaluator must not
// resolve a domain of its own. An empty value here means the server declares no
// domain at all, which is a refusal rather than an invitation to pick one.
func graphClosureState(s *server, snap graphFreshnessSnapshot) (closure.SemanticState, string) {
	return graphClosureStateFor(s, snap, serverHomeDomain(s))
}

func graphClosureStateFor(s *server, snap graphFreshnessSnapshot, authorityDomain string) (closure.SemanticState, string) {
	// Injectable at the same seam the fake store injects freshness: a fixture
	// that declares its synthetic publication current must be able to declare it
	// closed too. The DEFAULT (nil) is the real file-based evaluator, so
	// production never gains a bypass — omitting the hook fails closed.
	if s != nil && s.closureEval != nil {
		// The hook carries the SAME referent the verdict is about, so a test
		// can prove the requested domain reached the closure evaluation.
		return s.closureEval(authorityDomain)
	}
	if s == nil {
		return closure.SemanticClosureUnproven, "no server context, so no closure report can be located"
	}
	// The store-scoped proof set is authoritative when it describes the
	// publication that is actually live. It holds one proof per registered
	// domain, so a rebuild of some OTHER domain no longer takes authority away
	// from this one.
	if state, detail, ok := storeScopedClosureState(s, snap.verification.Live.Digest, authorityDomain); ok {
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
func storeScopedClosureState(s *server, liveDigest, authorityDomain string) (closure.SemanticState, string, bool) {
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
	// NO INDEPENDENT FALLBACK. The referent arrives already resolved; an
	// evaluator that reached for the home domain on its own is how the two
	// conjuncts came to be able to describe different repositories.
	domain := strings.TrimSpace(authorityDomain)
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
