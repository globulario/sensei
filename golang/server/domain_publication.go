// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/store"
)

// describer is the bounded read this resolution needs: two subject lookups
// rather than a whole-graph dump.
type describer interface {
	Describe(context.Context, string) ([]store.Triple, error)
}

// resolveCurrentPublication answers "which governed revision produced the
// knowledge you are serving for this domain".
//
// IT VERIFIES AGAINST THE STORED POINTER TARGET, NOT AGAINST ITSELF. An earlier
// draft recomputed the receipt's identity and compared it with an identity
// recomputed from the same fields, which is a tautology that passes for any
// tampered receipt. The honest comparison is recomputed-vs-stored, so the
// stored target is carried through the lookup and compared explicitly.
//
// Every way this can end is DISTINGUISHED, because collapsing them fails open
// on the worst one:
//
//	ABSENT      no pointer exists -- nothing was ever published here
//	UNREADABLE  a pointer exists and its target is missing, unparseable, of an
//	            undefined version, or does not match its own recomputed identity
//	VERIFIED    the stored target and the recomputed identity agree
//
// A dangling pointer is UNREADABLE, never ABSENT: "never published" is a benign
// steady state and "the publication record is corrupt" is not.
func resolveCurrentPublication(ctx context.Context, s *server, domain string) *awarenesspb.DomainPublication {
	unreadable := func(format string, args ...any) *awarenesspb.DomainPublication {
		return &awarenesspb.DomainPublication{
			Resolution:      awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE,
			RequestedDomain: domain,
			Domain:          domain,
			Detail:          fmt.Sprintf(format, args...),
		}
	}
	if strings.TrimSpace(domain) == "" {
		return unreadable("no domain was named, so no per-domain publication can be resolved")
	}
	d, ok := s.store.(describer)
	if !ok || s.store == nil {
		return unreadable("this store cannot be described, so no receipt can be verified")
	}

	// 1. The pointer. Bounded: one subject.
	ptr, err := d.Describe(ctx, publication.PointerIRI(domain))
	if err != nil {
		return unreadable("the current-publication pointer could not be read: %v", err)
	}
	// EXACTLY ONE TARGET, or there is no current publication.
	//
	// Describe returns rows in no defined order, so keeping "the last one"
	// meant the same ambiguous graph could attest either receipt depending on
	// row order. Two targets do not mean one of them is current; they mean the
	// question has no single answer, and answering anyway is how a race becomes
	// an attestation.
	// A pointer edge that EXISTS but does not name exactly one IRI is
	// unreadable, never absent.
	//
	// Excluding a literal-valued edge and reporting the empty result as ABSENT
	// discards the only evidence that a pointer was ever written, and a start
	// gate allowed to bootstrap on absence would fail OPEN over malformed
	// stored state. Presence of the predicate is the fact; whether it is usable
	// is a separate question, and the two must not be answered together.
	edges := 0
	targets := map[string]struct{}{}
	for _, t := range ptr {
		if t.Predicate != publication.CurrentPublicationPredicate {
			continue
		}
		edges++
		if t.ObjectIsIRI {
			targets[t.Object] = struct{}{}
		}
	}
	if edges > 0 && len(targets) != 1 {
		return unreadable(
			"the pointer for %q has %d currentPublication edge(s) naming %d distinct IRI target(s); "+
				"exactly one is required for there to be a current publication",
			domain, edges, len(targets))
	}
	var storedTarget string
	for iri := range targets {
		storedTarget = iri
	}
	if storedTarget == "" {
		return &awarenesspb.DomainPublication{
			Resolution:      awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT,
			RequestedDomain: domain,
			Domain:          domain,
			Detail:          fmt.Sprintf("no current publication pointer exists for %q", domain),
		}
	}

	// 2. The receipt the pointer names. Bounded: one more subject.
	body, err := d.Describe(ctx, storedTarget)
	if err != nil {
		return unreadable("the receipt %s could not be read: %v", shortIRI(storedTarget), err)
	}
	preds := make([]string, 0, len(body))
	objs := make([]string, 0, len(body))
	for _, t := range body {
		preds = append(preds, t.Predicate)
		objs = append(objs, t.Object)
	}
	r, err := publication.ReceiptFromTriples(storedTarget, preds, objs)
	if err != nil {
		return unreadable(
			"the current-publication pointer for %q names %s: %v",
			domain, shortIRI(storedTarget), err)
	}
	if r.Domain == "" {
		return unreadable(
			"the current-publication pointer for %q names %s, which is missing or unparseable",
			domain, shortIRI(storedTarget))
	}
	if r.Version != "" && !r.Version.Valid() {
		return unreadable("receipt version %q is not one this server defines", r.Version)
	}
	// The source state is a CLOSED vocabulary and is read by membership. An
	// unrecognised state that happens to be self-consistent would otherwise be
	// projected as VERIFIED, presenting semantics this server cannot interpret
	// as an attestation it can.
	if !r.State.Valid() {
		return unreadable(
			"receipt source state %q is not one this schema defines", r.State)
	}
	if err := r.FieldsMatchVersion(); err != nil {
		return unreadable("%v", err)
	}

	// 3. The check that is not a tautology: recomputed against STORED.
	if !publication.VerifyIdentity(storedTarget, r) {
		return unreadable(
			"the receipt stored as %s recomputes to %s: its fields have changed since it was published",
			shortIRI(storedTarget), shortIRI(r.IRI()))
	}
	if r.Domain != domain {
		return unreadable(
			"the pointer for %q resolved to a receipt for %q", domain, r.Domain)
	}

	version := string(publication.ReceiptV1)
	if r.Version != "" {
		version = string(r.Version)
	}
	return &awarenesspb.DomainPublication{
		Resolution:         awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED,
		RequestedDomain:    domain,
		ReceiptIri:         storedTarget,
		ReceiptVersion:     version,
		Domain:             r.Domain,
		SourceRevision:     r.Revision,
		SourceTree:         r.Tree,
		SourceState:        string(r.State),
		SourcePath:         r.SourcePath,
		SourceDigestSha256: r.SourceDigest,
	}
}

func shortIRI(iri string) string {
	if i := strings.LastIndex(iri, "-"); i >= 0 && len(iri)-i > 12 {
		return iri[:i+13] + "..."
	}
	return iri
}
