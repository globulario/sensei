package main

import (
	"context"
	"fmt"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/publication"
)

// resolveCurrentPublication answers "which governed revision produced the
// knowledge you are serving for this domain".
//
// It RECOMPUTES the receipt's identity rather than echoing the IRI it was
// stored under. A stored pointer proves only that something once wrote it; a
// recomputed digest proves the fields have not moved since. A start gate
// reading this to decide whether to run governed work is exactly the consumer
// that must not be told an unverified answer.
//
// Every way this can fail is DISTINGUISHED. ABSENT means no publication was
// ever recorded for the domain; UNREADABLE means one exists and this server
// cannot vouch for it. Collapsing those two fails open on the second, which is
// the shape this projection was added to prevent rather than repeat.
func resolveCurrentPublication(ctx context.Context, s *server, domain string) *awarenesspb.DomainPublication {
	unreadable := func(format string, args ...any) *awarenesspb.DomainPublication {
		return &awarenesspb.DomainPublication{
			Resolution: awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE,
			Domain:     domain,
			Detail:     fmt.Sprintf(format, args...),
		}
	}
	if domain == "" {
		return unreadable("no domain was named, so no per-domain publication can be resolved")
	}
	dumper, ok := s.store.(interface {
		DumpNTriples(context.Context) ([]byte, error)
	})
	if !ok || s.store == nil {
		return unreadable("this store cannot be dumped, so no receipt can be verified")
	}
	nt, err := dumper.DumpNTriples(ctx)
	if err != nil {
		return unreadable("the live store could not be read: %v", err)
	}
	r, found := publication.Current(nt, domain)
	if !found {
		return &awarenesspb.DomainPublication{
			Resolution: awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT,
			Domain:     domain,
			Detail: fmt.Sprintf(
				"no current publication pointer resolves to a receipt for %q", domain),
		}
	}
	if !r.Version.Valid() && r.Version != "" {
		return unreadable("receipt version %q is not one this server defines", r.Version)
	}
	iri := r.IRI()
	if !publication.VerifyIdentity(iri, r) {
		return unreadable("the receipt for %q does not hash to its own identity", domain)
	}
	version := string(publication.ReceiptV1)
	if r.Version != "" {
		version = string(r.Version)
	}
	return &awarenesspb.DomainPublication{
		Resolution:         awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED,
		ReceiptIri:         iri,
		ReceiptVersion:     version,
		Domain:             r.Domain,
		SourceRevision:     r.Revision,
		SourceTree:         r.Tree,
		SourceState:        string(r.State),
		SourcePath:         r.SourcePath,
		SourceDigestSha256: r.SourceDigest,
	}
}
