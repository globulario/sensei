// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"

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
	return &awarenesspb.GraphAuthority{
		Authoritative:                   snap.verification.State == seedmeta.FreshnessCurrent,
		GraphFreshnessState:             graphFreshnessStateProto(snap.verification.State),
		GraphFreshnessDetail:            snap.verification.Detail,
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

func graphAuthorityBuildProvenance() awarenesspb.BuildProvenanceState {
	return classifyBuildProvenance(&awarenesspb.MetadataResponse{
		ServerVersion:      Version,
		GraphBuildCommit:   BuildCommit,
		SourceRepoCommit:   SourceCommit,
		GraphBuildTimeUnix: parseUnixStamp(BuildTimeUnix),
	})
}
