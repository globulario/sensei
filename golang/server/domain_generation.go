// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.domain_generation
// @awareness file_role=authority_resolution
// @awareness risk=high
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/graphgeneration"
)

// domain_generation.go answers one question, and answers "no" to all of it today:
//
//	What is the DOMAIN-SCOPED generation of domain D's published graph?
//
// It is a different question from the one this server has always been able to
// answer — "what generation does this store serve?" — and the difference is
// invisible on a store serving a single domain, because there the two digests
// are equal. Every reader that consumed the store's answer as though it were the
// domain's answer was correct by coincidence, and stops being correct the moment
// a second domain exists.
//
// WHY THE ANSWER IS CURRENTLY "NO", structurally rather than incidentally.
// There are three digests in this system that a careless reader could offer as
// "domain D's identity", and none of them is one:
//
//   - the seed marker's digest, which certifies the WHOLE STORE. It is a true
//     statement, and it is not about D.
//   - the proof set's per-domain SliceDigest, which is computed by partitioning
//     the whole graph on `<aw#repo>` subject tags. It answers "has this domain's
//     content changed", which is what it was built for and is good at. It is NOT
//     the identity of the bytes served from D's graph, because it is derived from
//     the default graph rather than from anything published under D's authority —
//     and subject tagging is precisely the attribution model this repository has
//     already found defective, when publishing sensei moved the services slice
//     and cost services its closure proof because 173 identifiers are legitimately
//     co-authored.
//   - the digest of whatever bytes happen to sit in D's named graph slot, which
//     nothing publishes into: LoadGraph, the domain-scoped writer, has no
//     production caller. Publication promotes staging into the DEFAULT graph.
//
// So a domain identity cannot be represented today without changing publication
// mechanics, and changing publication mechanics is deliberately NOT part of this
// change. The resolver therefore reports the absence as an absence. That is the
// honest state of the system, and it is fail-closed: a reader that needs a domain
// identity refuses rather than accepting a digest that answers a different
// question.

// domainGenerationResolver reports the domain-scoped generation of one domain,
// whether one is established at all, and — when it is not — why not.
//
// The reason is a return value rather than a log line because it is the operator's
// only account of why an otherwise-healthy graph will not be served.
type domainGenerationResolver func(ctx context.Context, domain string) (graphgeneration.Identity, bool, string)

// resolveDomainGeneration is the one entry point handlers use.
func (s *server) resolveDomainGeneration(ctx context.Context, domain string) (graphgeneration.Identity, bool, string) {
	if s != nil && s.domainGeneration != nil {
		return s.domainGeneration(ctx, domain)
	}
	return publishedDomainGeneration(s, domain)
}

// publishedDomainGeneration reads the store's publication record and reports
// what it establishes about one domain's own graph identity.
//
// It deliberately walks all the way to the per-domain record before refusing,
// rather than short-circuiting on "this is not implemented". The distinction
// between "this store has published nothing", "this publication does not mention
// that domain" and "this publication mentions it but records no domain-scoped
// identity" is exactly what an operator needs to tell a misconfiguration from the
// migration's current state, and collapsing them would make the three
// indistinguishable.
func publishedDomainGeneration(s *server, domain string) (graphgeneration.Identity, bool, string) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return graphgeneration.Identity{}, false, "no domain was named, and an unnamed domain has no identity to establish"
	}
	if s == nil || strings.TrimSpace(s.oxigraphQueryURL) == "" {
		return graphgeneration.Identity{}, false,
			"this server is not bound to a store, so there is no publication record in which a domain identity could be established"
	}
	dir, err := graphgeneration.Dir(s.oxigraphQueryURL)
	if err != nil {
		return graphgeneration.Identity{}, false,
			fmt.Sprintf("the store's publication record could not be located: %v", err)
	}
	set, err := graphgeneration.Load(dir)
	if err != nil || set == nil {
		return graphgeneration.Identity{}, false,
			"this store has published no proof set, so nothing records an identity for any domain in it"
	}
	proof, ok := set.ProofFor(domain)
	if !ok {
		return graphgeneration.Identity{}, false, fmt.Sprintf(
			"the live publication %s carries no record at all for domain %s",
			shortDigest(set.Marker.Digest), domain)
	}

	// THE REFUSAL THIS FILE EXISTS FOR.
	//
	// A slice digest is present, it is per-domain, and it is the obvious thing to
	// return. Returning it is the defect: it is derived by tagging subjects inside
	// the whole graph, so it identifies a VIEW OVER THE DEFAULT GRAPH rather than
	// the bytes published under this domain's authority. Handing it back as the
	// domain's generation would let this server declare an identity no publication
	// ever proved — and it would do so with a digest that looks specific enough to
	// be believed.
	//
	// The remedy is in the writer, not here. Until publication writes a domain's
	// graph under that domain's authority, "we do not know" is the only true answer,
	// and it is recorded as itself.
	return graphgeneration.Identity{}, false, fmt.Sprintf(
		"the live publication %s records only a tag-derived slice digest (%s) for %s, which identifies a view over the "+
			"default graph rather than bytes published under that domain's authority; no domain-scoped generation is "+
			"established for it",
		shortDigest(set.Marker.Digest), shortDigest(proof.SliceDigest), domain)
}
