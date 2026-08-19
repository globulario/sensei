// SPDX-License-Identifier: AGPL-3.0-only

package rdf

import (
	"fmt"
	"strings"
)

// SourceFile identity, issue #197.
//
// A SourceFile IRI used to be keyed by repo-relative path alone, with no
// repository scope, so every registered repository's README.md resolved to
// the SAME RDF subject. Observed live with two domains publishing into one
// store, the graph asserted that one repository's README implements the
// other repository's decision -- it does not; two different files had been
// collapsed onto one identity. README.md and docs/awareness/invariants.yaml
// are paths EVERY Sensei-onboarded repository has, so any two registered
// repositories collided by default. That is the ordinary case, not an edge
// case.
//
// The architect decision on #197 fixes the identity itself rather than
// describing the collision more precisely with ownership metadata:
//
//	SourceFileID = f(canonical_repository_identity, canonical_repo_relative_path)
//
// with these hard consequences:
//
//  1. two repositories' README.md files MUST mint different subjects;
//  2. the same repository/path MUST mint the same subject across
//     checkouts, machines, and publication domains;
//  3. aw:repo remains provenance/metadata; it is never the identity
//     discriminator;
//  4. the RDF subject itself is re-identified -- not kept while a
//     composite key is applied at query time, because the subject is
//     already the thing relations point at.
//
// The repository identity is the durable one Sensei's repository/admission
// machinery establishes (repodomain: the checkout's configured
// repository.domain), NOT the selectable publication domain a build names
// with --repo and NOT the checkout path. A file belongs to a repository; a
// graph domain is a publication scope and may be aliased or rebound.
const (
	// SourceFileGenerationV1 is the original unscoped identity family,
	// sourceFile/<encoded-path>. It is intrinsically ambiguous: one IRI
	// can mean N repositories.
	SourceFileGenerationV1 = "v1"
	// SourceFileGenerationV2 is the repository-scoped identity family,
	// sourceFile/<encoded-repository-identity>/<encoded-path>.
	SourceFileGenerationV2 = "v2"
)

// MintSourceFileIRI composes the repository-scoped identity of one source
// file: sourceFile/<encoded repository identity>/<encoded repo-relative
// path>, rendered as an IRI reference token.
//
// Both segments go through EncodeIRIPath, which percent-encodes '/', so
// the single unescaped '/' between them is an unambiguous separator no
// identity or path content can forge. Neither segment is derived from the
// checkout location, so the same repository and path mint the same subject
// on every machine.
//
// repositoryIdentity must be a canonical repository identity (e.g.
// "github.com/globulario/sensei"). An empty repositoryIdentity is a
// programming error at the call site -- there is no unscoped fallback,
// because falling back is exactly the collapse this function exists to
// prevent -- so callers resolve identity first and refuse the build when it
// is unresolved.
func MintSourceFileIRI(repositoryIdentity, repoRelativePath string) string {
	return IRI(sourceFileClassPrefix() +
		EncodeIRIPath(repositoryIdentity) + "/" + EncodeIRIPath(repoRelativePath))
}

// sourceFileClassPrefix is ".../awareness#sourceFile/", the prefix every
// SourceFile identity of either generation starts with.
func sourceFileClassPrefix() string {
	hashIdx := strings.LastIndex(ClassSourceFile, "#")
	prefix := ClassSourceFile[:hashIdx+1]
	className := ClassSourceFile[hashIdx+1:]
	return prefix + lowerFirst(className) + "/"
}

// SourceFileIdentity is a parsed SourceFile IRI. Generation is
// SourceFileGenerationV1 or SourceFileGenerationV2; RepositoryIdentity is
// empty for v1, which carries none.
type SourceFileIdentity struct {
	Generation         string
	RepositoryIdentity string
	Path               string
}

// ParseSourceFileIRI decodes a SourceFile IRI of either generation. iri may
// be given with or without its surrounding angle brackets.
//
// A v2 IRI has exactly two unescaped-'/'-separated segments and yields both
// the repository identity and the path. A v1 IRI has one and yields the
// path with RepositoryIdentity empty -- it is reported as v1, never
// guessed into a repository, because a v1 IRI is intrinsically ambiguous:
// sourceFile/README.md can mean any of N repositories. Callers that need a
// repository decide what to do with an ambiguous v1 identity; this function
// never invents one.
//
// ok is false when iri is not a SourceFile IRI at all.
func ParseSourceFileIRI(iri string) (SourceFileIdentity, bool) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(iri), "<"), ">")
	rest, found := strings.CutPrefix(trimmed, sourceFileClassPrefix())
	if !found || rest == "" {
		return SourceFileIdentity{}, false
	}
	identity, path, split := strings.Cut(rest, "/")
	if !split {
		return SourceFileIdentity{
			Generation: SourceFileGenerationV1,
			Path:       DecodeIRIPath(rest),
		}, true
	}
	// A third segment is not a shape this package ever mints. Refusing it
	// keeps "two segments" the only v2 reading, so no future family can be
	// silently mistaken for one.
	if strings.Contains(path, "/") {
		return SourceFileIdentity{}, false
	}
	if identity == "" || path == "" {
		return SourceFileIdentity{}, false
	}
	return SourceFileIdentity{
		Generation:         SourceFileGenerationV2,
		RepositoryIdentity: DecodeIRIPath(identity),
		Path:               DecodeIRIPath(path),
	}, true
}

// SourceFilePathFromIRI returns just the repo-relative path of a SourceFile
// IRI of either generation, for readers that only need the path and already
// know the repository from context.
func SourceFilePathFromIRI(iri string) (string, bool) {
	id, ok := ParseSourceFileIRI(iri)
	if !ok {
		return "", false
	}
	return id.Path, true
}

// --- the v1 -> v2 generation boundary (issue #197 migration decision) ---
//
// The migration is an identity-schema GENERATION BOUNDARY, not an in-place
// rewrite of history:
//
//   - existing receipts and proofs remain immutable; nothing here rewrites
//     an old document to pretend it was issued against a v2 identity;
//   - there is NO global owl:sameAs and no unconditional alias from an old
//     identity to a new one. The old identity is intrinsically ambiguous --
//     sourceFile/README.md can mean any of N repositories -- so a blanket
//     alias would assert a continuity that is not true;
//   - where an old receipt ALSO binds an unambiguous canonical repository
//     identity, a migration record may state
//     (old_iri, repository_identity) -> new_iri. That record is provenance
//     for translation, never a retroactive mutation of the receipt;
//   - an old receipt that does not carry enough repository context to
//     disambiguate cannot authorize or re-prove the v2 identity. It fails
//     closed, and the current proof is rebuilt/reissued instead;
//   - a v2 publication never exposes a mixed v1/v2 authoritative
//     generation.

// SourceFileMigrationRecord states that one ambiguous v1 identity, read
// together with a repository identity the same document independently
// bound, denotes exactly one v2 identity. It is provenance for a
// translation that was performed, not an assertion that the two IRIs are
// the same subject.
type SourceFileMigrationRecord struct {
	OldIRI             string
	RepositoryIdentity string
	NewIRI             string
	Path               string
}

// TranslateV1SourceFileIdentity resolves one v1 SourceFile identity against
// the repository identity the SAME document independently binds, and
// returns the migration record for it.
//
// repositoryIdentity must come from the old document itself. Supplying one
// from anywhere else -- the current checkout, the build being run, a guess
// -- is what "no unconditional alias" forbids: it would manufacture a
// continuity the old document never asserted.
//
// It fails closed rather than inventing continuity:
//   - an empty repositoryIdentity means the old document carries no
//     repository binding, so its identity stays ambiguous and cannot be
//     translated at all;
//   - an IRI that is already v2 is not translated; v2 identities are not
//     re-scoped.
func TranslateV1SourceFileIdentity(oldIRI, repositoryIdentity string) (SourceFileMigrationRecord, error) {
	parsed, ok := ParseSourceFileIRI(oldIRI)
	if !ok {
		return SourceFileMigrationRecord{}, fmt.Errorf("translate source file identity: %q is not a SourceFile IRI", oldIRI)
	}
	if parsed.Generation != SourceFileGenerationV1 {
		return SourceFileMigrationRecord{}, fmt.Errorf(
			"translate source file identity: %q is already repository-scoped (%s); a v2 identity is never re-scoped",
			oldIRI, parsed.RepositoryIdentity)
	}
	identity := strings.TrimSpace(repositoryIdentity)
	if identity == "" {
		return SourceFileMigrationRecord{}, fmt.Errorf(
			"translate source file identity: %q carries no repository binding, so which repository's %q it names cannot be determined.\n"+
				"An unscoped identity is ambiguous by construction -- every repository has that path -- so it is historical-only for this surface: "+
				"rebuild and reissue the current proof rather than translating it",
			oldIRI, parsed.Path)
	}
	return SourceFileMigrationRecord{
		OldIRI:             strings.Trim(strings.TrimSpace(oldIRI), "<>"),
		RepositoryIdentity: identity,
		NewIRI:             strings.Trim(MintSourceFileIRI(identity, parsed.Path), "<>"),
		Path:               parsed.Path,
	}, nil
}

// CheckSourceFileGeneration refuses a publication that would expose a mixed
// v1/v2 authoritative generation.
//
// A reader of one graph must never have to ask which identity generation a
// given SourceFile subject belongs to: with both present, the same file can
// appear as two subjects, edges divide between them, and a proof computed
// over one says nothing about the other. Every SourceFile subject a
// publication exposes therefore belongs to one generation.
//
// ntriples is scanned for IRI tokens rather than parsed as RDF, so it costs
// one pass and needs no graph in memory.
func CheckSourceFileGeneration(ntriples []byte) error {
	var firstV1, firstV2 string
	for _, iri := range sourceFileIRIsIn(string(ntriples)) {
		parsed, ok := ParseSourceFileIRI(iri)
		if !ok {
			continue
		}
		switch parsed.Generation {
		case SourceFileGenerationV1:
			if firstV1 == "" {
				firstV1 = iri
			}
		case SourceFileGenerationV2:
			if firstV2 == "" {
				firstV2 = iri
			}
		}
		if firstV1 != "" && firstV2 != "" {
			return fmt.Errorf(
				"mixed SourceFile identity generations in one publication: %s is unscoped (v1) while %s is repository-scoped (v2).\n"+
					"A reader cannot tell which subject a file is, so the publication is refused. Rebuild every input of this graph "+
					"under one generation rather than publishing across the boundary",
				firstV1, firstV2)
		}
	}
	return nil
}

// sourceFileIRIsIn returns every SourceFile IRI token in an N-Triples
// document, in order of appearance.
func sourceFileIRIsIn(doc string) []string {
	var out []string
	prefix := sourceFileClassPrefix()
	for rest := doc; ; {
		start := strings.Index(rest, "<"+prefix)
		if start < 0 {
			return out
		}
		rest = rest[start+1:]
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end+1:]
	}
}
