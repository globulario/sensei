// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.export_graph
// @awareness file_role=grpc_rpc_handler
// @awareness risk=high
package main

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
)

// GetDomainGraph materializes, read-only, the exact graph this service serves for
// one domain.
//
// WHY THIS EXISTS. Every other RPC answers a scoped question, so a consumer that
// needed the graph AS BYTES -- a task session pinning the world it was planned
// against, for instance -- had to query the backing store itself and then argue
// that what it read equalled what this service serves. That argument is where
// the recurring "wrong store, wrong named graph, stale generation" family of
// defects lives: the reconstruction is plausible, the equivalence is asserted
// afterwards, and nothing structurally prevents the two from diverging.
//
// So the authority that says "this is the graph I serve" also emits the bytes,
// and emits them WITH their identity from ONE resolution. A caller transports a
// snapshot; it does not choose that snapshot's authority.
//
// WHAT IT DOES NOT ASSERT, stated because the alternative is a reader assuming
// otherwise: this does not say the exported graph is the ACTIVE generation. The
// registry that declares ACTIVE is operator-owned and deliberately outside any
// published repository -- a repository must not be able to declare its own graph
// active -- and this server does not read it. The response states the SERVED
// identity; comparing that with the declared ACTIVE generation is the caller's
// step, against the registry. Two records, neither vouching for itself.
//
// Read-only in the strong sense: no promotion, no marker write, no generation
// change, no lock. It observes authority and never creates it.
func (s *server) GetDomainGraph(ctx context.Context, req *awarenesspb.GetDomainGraphRequest) (*awarenesspb.GetDomainGraphResponse, error) {
	if s == nil || s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	domain := strings.TrimSpace(req.GetDomain())
	if domain == "" {
		// No home-domain fallback. Answering an unasked question with this
		// server's favourite domain produces a well-formed snapshot of something
		// the caller did not ask for, and "a snapshot came back" is exactly the
		// evidence a careless consumer would accept.
		return nil, status.Error(codes.InvalidArgument,
			"export requires a domain; exporting whatever this server calls home would snapshot a repository the caller did not name")
	}

	exporter, ok := s.store.(interface {
		ExportDomainGraph(context.Context, string) ([]byte, error)
	})
	if !ok {
		return nil, status.Error(codes.Unavailable,
			"this store cannot materialize one domain's graph, so nothing here can certify an export of it")
	}

	// ONE RESOLUTION for the served identity, taken BEFORE the bytes are read
	// and compared against them afterwards. The verdict and the artifact must
	// describe the same world; resolving twice would let a publication landing
	// between them produce a snapshot whose identity names a different graph.
	snap := snapshotGraphFreshness(ctx, s)
	served := strings.TrimSpace(servedGraphDigest(ctx, s, snap.verification))
	if served == "" {
		// A graph that cannot state its own identity has none to export. This is
		// the "declared but not served" case seen from the other side: a triple
		// count is evidence, not identity.
		return nil, status.Errorf(codes.FailedPrecondition,
			"this server serves no coherent graph identity for %s, so there is nothing whose export could be certified: %s",
			domain, snap.verification.Detail)
	}
	if snap.verification.State != seedmeta.FreshnessCurrent {
		// Exporting a store that does not match its own marker would hand out
		// bytes whose identity is contested. Refuse rather than choose one.
		return nil, status.Errorf(codes.FailedPrecondition,
			"the served graph and its authority marker disagree (%s), so no export of it can be certified: %s",
			snap.verification.State, snap.verification.Detail)
	}

	nt, err := exporter.ExportDomainGraph(ctx, domain)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "the graph for %s could not be materialized: %v", domain, err)
	}

	// The identity is RECOMPUTED from the bytes being returned, with the same
	// rules that produced the served digest: canonicalize, strip the marker,
	// hash. A digest copied from the freshness snapshot would describe what the
	// server believes rather than what the caller is about to receive.
	_, recomputed := seedmeta.AppendMarker(nt)
	if !strings.EqualFold(strings.TrimSpace(recomputed.Digest), served) {
		// The store's whole-graph identity and this domain's slice legitimately
		// differ on a multi-domain store. Saying so is the honest answer: this
		// server cannot certify that the slice it just read IS the graph whose
		// identity it reports, and inventing a per-slice generation here would be
		// this service declaring an identity nobody published.
		return nil, status.Errorf(codes.FailedPrecondition,
			"the exported graph for %s digests to %s and this server serves generation %s; "+
				"an export is certified only when they are the same graph",
			domain, recomputed.Digest, served)
	}

	return &awarenesspb.GetDomainGraphResponse{
		Domain:      domain,
		Generation:  served,
		GraphDigest: recomputed.Digest,
		TripleCount: recomputed.TripleCount,
		Format:      "ntriples",
		Ntriples:    nt,
	}, nil
}
