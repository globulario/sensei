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
	// The pointer is read through the SAME lossless path as the receipt. The
	// simplified transport drops empty lexical objects, so a currentPublication
	// "" would vanish and the missing edge be reported as ABSENT -- corrupt
	// state becoming "never published", the one answer a start gate may treat
	// as benign.
	terms, ok := s.store.(interface {
		DescribeTerms(context.Context, string) ([]store.Statement, error)
	})
	if !ok || s.store == nil {
		return unreadable(
			"this store cannot return RDF terms losslessly, so no receipt can be verified against what it holds")
	}
	ptr, err := terms.DescribeTerms(ctx, publication.PointerIRI(domain))
	if err != nil {
		return unreadable("the current-publication pointer could not be read: %v", err)
	}
	storedTarget, outcome, perr := publication.DecodePointer(domain, asPublicationTerms(ptr))
	switch outcome {
	case publication.PointerNone:
		return &awarenesspb.DomainPublication{
			Resolution:      awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT,
			RequestedDomain: domain,
			Domain:          domain,
			Detail:          fmt.Sprintf("no current publication pointer exists for %q", domain),
		}
	case publication.PointerBroken:
		return unreadable("%v", perr)
	}

	body, err := terms.DescribeTerms(ctx, storedTarget)
	if err != nil {
		return unreadable("the receipt %s could not be read: %v", shortIRI(storedTarget), err)
	}
	stmts := asPublicationTerms(body)
	// ONE OPERATION: schema selection, cardinality, term kind, datatype,
	// lexical validity, cross-field rules and the stored-IRI comparison all
	// happen inside DecodeStoredReceipt, in that order, before a Receipt this
	// server will project exists at all.
	r, err := publication.DecodeStoredReceipt(storedTarget, stmts)
	if err != nil {
		return unreadable(
			"the current-publication pointer for %q names %s: %v",
			domain, shortIRI(storedTarget), err)
	}
	// The source state is a CLOSED vocabulary and is read by membership. An
	// unrecognised state that happens to be self-consistent would otherwise be
	// projected as VERIFIED, presenting semantics this server cannot interpret
	// as an attestation it can.

	// 3. The check that is not a tautology: recomputed against STORED.
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

func versionOrV1(v publication.ReceiptVersion) publication.ReceiptVersion {
	if v == "" {
		return publication.ReceiptV1
	}
	return v
}

// asPublicationTerms lifts lossless store statements into the publication
// package's term type without simplifying any of them.
func asPublicationTerms(in []store.Statement) []publication.RDFStatement {
	out := make([]publication.RDFStatement, 0, len(in))
	for _, st := range in {
		out = append(out, publication.RDFStatement{
			Predicate: st.Predicate,
			Object: publication.Term{
				Kind:     publication.TermKind(st.Object.Kind),
				Value:    st.Object.Value,
				Datatype: st.Object.Datatype,
				Language: st.Object.Language,
			},
		})
	}
	return out
}
