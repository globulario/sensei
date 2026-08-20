// SPDX-License-Identifier: AGPL-3.0-only

package main

// Per-domain slice verification (issue #221).
//
// The whole-graph marker proves the live store matches the last publication. It
// cannot notice a DOMAIN whose knowledge left the store, because a store that
// lost a slice and a store that never held it look identical from a triple
// count and a digest — and if the loss happened outside a publication, nothing
// in the store is left to disagree with.
//
// The proof set can notice, and it is the only thing that can: it records what
// each domain's slice held at publication, it is keyed by store rather than by
// repository (so every server reading that store reaches the same answer), and
// it lives outside both the store and every repository, so it survives the
// content it describes.
//
// This reports; it does not refuse. A domain whose slice is thin does not make
// the rest of the graph untrue, and a server that stopped answering because one
// domain went missing would take the whole graph down with it. What must not
// happen is what #221 recorded: the gap going unmentioned until a write is
// refused, and the operator writing the knowledge into a file beside the graph.

import (
	"context"
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/graphgeneration"
	"github.com/globulario/sensei/golang/store"
)

// domainSliceReport is one domain's live slice measured against its proof.
type domainSliceReport struct {
	Domain   string
	Expected int64
	Live     int64
	// Err is set when the live slice could not be counted. "I could not check"
	// is reported as itself; it is never rendered as agreement.
	Err error
}

func (r domainSliceReport) line() string {
	if r.Err != nil {
		return fmt.Sprintf("domain_slice_unverified: %s could not be measured against its published proof (%v)", r.Domain, r.Err)
	}
	return fmt.Sprintf(
		"domain_slice_shortfall: %s holds %d triple(s) live but its proof set recorded %d at publication — republish with: %s",
		r.Domain, r.Live, r.Expected, graphgeneration.RepairCommand(r.Domain))
}

// domainSliceReports measures the live store against the proof set, one entry
// per domain that needs mentioning. An empty result means every domain the
// proof set vouches for is present at roughly the size it was published at, or
// that there is no proof set to compare against — which the marker path already
// reports as its own kind of absence rather than as health.
//
// only restricts the check to one domain; empty checks every domain the proof
// set vouches for.
func domainSliceReports(ctx context.Context, s store.Store, storeURL, only string) []domainSliceReport {
	if s == nil || storeURL == "" {
		return nil
	}
	dir, err := graphgeneration.Dir(storeURL)
	if err != nil {
		return nil
	}
	set, err := graphgeneration.Load(dir)
	if err != nil || set == nil {
		return nil
	}
	// Deliberately NOT CountTriplesInDomain. That answers "what is visible in
	// this domain's scope", which also draws in shared and untagged subjects —
	// content the domain never owned, standing in for the content it lost. The
	// expectation counts subjects the domain OWNS, so the live count must too,
	// or a small domain can vanish entirely behind the store's shared triples.
	counter, ok := s.(interface {
		CountTriplesOwnedByDomain(ctx context.Context, domain string) (int64, error)
	})
	if !ok {
		return nil
	}
	var out []domainSliceReport
	for domain, proof := range set.Domains {
		if only != "" && domain != only {
			continue
		}
		// A proof that recorded no count is not a proof of a count of nothing.
		if proof.SliceTripleCount <= 0 {
			continue
		}
		live, err := counter.CountTriplesOwnedByDomain(ctx, domain)
		if err != nil {
			out = append(out, domainSliceReport{Domain: domain, Expected: proof.SliceTripleCount, Err: err})
			continue
		}
		if graphgeneration.MaterialShortfall(proof.SliceTripleCount, live) {
			out = append(out, domainSliceReport{Domain: domain, Expected: proof.SliceTripleCount, Live: live})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

// domainSliceBlindSpots renders the reports for one served domain as blind-spot
// lines, for the surfaces that carry them.
func (s *server) domainSliceBlindSpots(ctx context.Context, domain string) []string {
	if s == nil {
		return nil
	}
	reports := domainSliceReports(ctx, s.store, s.oxigraphQueryURL, domain)
	out := make([]string, 0, len(reports))
	for _, r := range reports {
		out = append(out, r.line())
	}
	return out
}
