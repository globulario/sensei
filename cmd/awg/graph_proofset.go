// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/globulario/sensei/golang/closure"
	"github.com/globulario/sensei/golang/graphgeneration"
	"github.com/globulario/sensei/golang/seedmeta"
)

// publishProofSet writes the complete, store-scoped proof set for this
// publication: the whole-graph marker plus one closure proof per registered
// domain, swapped in as a single generation.
//
// The domain being built is proven by the report this build just computed.
// Every other registered domain keeps the verdict it already had, re-stamped
// onto this publication — but only after checking that its slice is
// byte-identical, so a re-stamp is a statement about unchanged content rather
// than an assumption about it. A domain that cannot be verified unchanged gets
// a recorded refusal, never a carried-forward claim.
func publishProofSet(storeEndpoint, builtDomain string, marker seedmeta.Marker,
	transaction []byte, builtReport *closure.Report, postUpdateNT []byte) int {

	dir, err := graphgeneration.Dir(storeEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: resolve graph proof set: %v\n", err)
		return 1
	}

	// A missing or unreadable previous set is not an error: this may be the
	// first publication under the new layout. It does mean no domain can be
	// carried forward, which Compose records as a refusal per domain rather
	// than passing off as proven.
	prev, loadErr := graphgeneration.Load(dir)
	if loadErr != nil {
		prev = nil
	}

	next := graphgeneration.Compose(
		prev,
		graphgeneration.Generation{
			MarkerDigest:    marker.Digest,
			MarkerIRI:       marker.IRI,
			TripleCount:     marker.TripleCount,
			PublishedUnix:   time.Now().Unix(),
			PublishedDomain: builtDomain,
		},
		marker,
		transaction,
		builtDomain,
		builtReport,
		postUpdateNT,
		registeredDomains(),
	)

	if err := graphgeneration.Write(dir, storeEndpoint, next); err != nil {
		// A SET THAT CANNOT BE PUBLISHED MUST NOT LEAVE THE PREVIOUS ONE STANDING.
		//
		// Write refuses a set with no domain proofs, correctly: publishing one
		// would drop every domain's proof while claiming to be a publication.
		// But the store's content has already been replaced by the time we get
		// here, so the previous generation is no longer a true statement about
		// it -- and expectedGraphMarker prefers that record over the marker
		// file. Leaving it is how a fresh build inherits a stale identity.
		//
		// Dropping the pointer reports "this store has published nothing",
		// which is true, and falls through to the marker file this build wrote.
		if invErr := graphgeneration.Invalidate(dir); invErr != nil {
			fmt.Fprintf(os.Stderr, "sensei build: publish graph proof set: %v (and the stale pointer could not be dropped: %v)\n", err, invErr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "  proof set: not published (%v)\n", err)
		fmt.Fprintf(os.Stderr, "    the store's previous generation pointer was dropped: it described content this build replaced\n")
		return 0
	}

	proven, refused := summarizeProofSet(next)
	fmt.Fprintf(os.Stderr, "  proof set: %s (%d domain(s) proven)\n", dir, len(proven))
	for _, domain := range proven {
		fmt.Fprintf(os.Stderr, "    proven   %s\n", domain)
	}
	// Refusals are printed, not swallowed. A domain silently missing from the
	// set is how "we could not check" turns back into "it was fine".
	for _, r := range refused {
		fmt.Fprintf(os.Stderr, "    UNPROVEN %s — %s\n", r.domain, r.reason)
	}
	return 0
}

type proofRefusal struct {
	domain string
	reason string
}

func summarizeProofSet(s *graphgeneration.Set) ([]string, []proofRefusal) {
	var proven []string
	var refused []proofRefusal
	for domain, proof := range s.Domains {
		if proof.Proven() {
			proven = append(proven, domain)
			continue
		}
		reason := proof.CarryForwardRefusal
		if reason == "" && proof.Report != nil {
			reason = "closure report is present but not proven"
		}
		if reason == "" {
			reason = "no closure proof"
		}
		refused = append(refused, proofRefusal{domain: domain, reason: reason})
	}
	sort.Strings(proven)
	sort.Slice(refused, func(i, j int) bool { return refused[i].domain < refused[j].domain })
	return proven, refused
}

// registeredDomains lists the operator-registered domains.
//
// The registry is advisory input here, not a gate: domains actually present in
// the published graph are covered whether or not the registry mentions them.
// An unreadable registry therefore degrades to "cover what is in the store"
// rather than failing the publication.
func registeredDomains() []string {
	path := DefaultDomainRegistryPath()
	if path == "" {
		return nil
	}
	reg, err := LoadDomainRegistry(path)
	if err != nil || reg == nil {
		return nil
	}
	out := make([]string, 0, len(reg.Domains))
	for domain := range reg.Domains {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}
