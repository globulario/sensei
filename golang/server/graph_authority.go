// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"

	"github.com/globulario/sensei/golang/closure"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
)

func (s *server) graphAuthority(ctx context.Context) *awarenesspb.GraphAuthority {
	snap := snapshotGraphFreshness(ctx, s)
	return graphAuthorityFromSnapshot(snap, s)
}

func graphAuthorityFromSnapshot(snap graphFreshnessSnapshot, s *server) *awarenesspb.GraphAuthority {
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
	semanticState, semanticDetail := graphClosureState(s, snap)
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
func graphClosureState(s *server, snap graphFreshnessSnapshot) (closure.SemanticState, string) {
	// Injectable at the same seam the fake store injects freshness: a fixture
	// that declares its synthetic publication current must be able to declare it
	// closed too. The DEFAULT (nil) is the real file-based evaluator, so
	// production never gains a bypass — omitting the hook fails closed.
	if s != nil && s.closureEval != nil {
		return s.closureEval()
	}
	if s == nil || s.graphMarkerFile == "" {
		return closure.SemanticClosureUnproven,
			"no graph marker path configured, so no closure report can be located for this publication"
	}
	state, detail, _ := closure.Evaluate(s.graphMarkerFile, snap.verification.Live.Digest)
	return state, detail
}

func graphAuthorityBuildProvenance() awarenesspb.BuildProvenanceState {
	return classifyBuildProvenance(&awarenesspb.MetadataResponse{
		ServerVersion:      Version,
		GraphBuildCommit:   BuildCommit,
		SourceRepoCommit:   SourceCommit,
		GraphBuildTimeUnix: parseUnixStamp(BuildTimeUnix),
	})
}
