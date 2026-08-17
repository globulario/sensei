// SPDX-License-Identifier: AGPL-3.0-only

// Package graphgeneration owns the proof set a graph publication leaves behind.
//
// # Why this package exists
//
// The proof set used to be scattered one-copy-per-repository: each server kept
// its own `<repo>/.sensei/graph-authority.json`, its own transaction stamp, and
// its own closure report. That arrangement inverted two bindings at once.
//
//   - The marker is a WHOLE-GRAPH fact stored PER REPOSITORY. Publishing any
//     domain recomputes the global marker, so every other repository's copy
//     immediately described a publication that no longer existed.
//   - The closure report is a PER-DOMAIN fact bound to WHOLE-GRAPH identity.
//     Rebuilding domain B invalidated domain A's proof even though nothing
//     about A's corpus, A's slice, or A's verdict had changed.
//
// The observable result was that no two registered domains could hold authority
// at the same time: building either one took authority away from the other.
//
// The repair is to store the proof set once per STORE, not once per repository,
// and to publish it as one immutable generation that readers select through a
// single atomically-replaced pointer. A reader therefore observes either the
// whole previous generation or the whole next one, never a half-updated set.
//
// # Layout
//
//	<root>/graph/<store-id>/
//	    store.json                         which store this proof set describes
//	    current.json                       pointer -> one generation (atomic swap)
//	    generations/<marker-digest>/
//	        generation.json                marker + published time
//	        transaction.tsv                runtime transaction stamp (optional)
//	        domains/<slug>.closure.json     one closure report per registered domain
//	        domains/<slug>.slice.json       that domain's slice digest at this generation
//
// Generation directories are immutable once the pointer names them. Writing a
// new generation never mutates the one currently being served, which is what
// makes the swap safe without holding a lock across readers.
package graphgeneration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/closure"
	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	// CurrentPointerFile names the generation a reader must use. It is the only
	// mutable file in the tree, and it is replaced by rename so that a reader
	// never observes a partially written proof set.
	CurrentPointerFile = "current.json"
	// StoreDescriptorFile records which store endpoint this proof set describes,
	// so an operator inspecting the directory can tell without guessing.
	StoreDescriptorFile = "store.json"
	// GenerationFileName holds the marker for one generation.
	GenerationFileName = "generation.json"
	// TransactionFileName holds the runtime transaction stamp for one generation.
	TransactionFileName = "transaction.tsv"

	generationsDirName = "generations"
	domainsDirName     = "domains"
	closureSuffix      = ".closure.json"
	sliceSuffix        = ".slice.json"
	refusalSuffix      = ".unproven.json"
)

// Pointer selects the generation readers must serve.
type Pointer struct {
	Generation    string `json:"generation"`
	PublishedUnix int64  `json:"published_unix"`
}

// Generation is the whole-graph fact for one publication.
type Generation struct {
	MarkerDigest  string `json:"marker_digest_sha256"`
	MarkerIRI     string `json:"marker_iri"`
	TripleCount   int64  `json:"triple_count"`
	PublishedUnix int64  `json:"published_unix"`
	// PublishedDomain is the domain whose build produced this generation. Every
	// other domain in the set was carried forward, not rebuilt.
	PublishedDomain string `json:"published_domain"`
}

// DomainProof is the per-domain half of the set: the closure verdict plus the
// digest of the slice that verdict was computed against.
//
// SliceDigest is what lets a later publication carry this proof forward
// honestly. If the domain's slice is byte-identical at the next generation, the
// corpus-to-slice proof still holds and only the whole-graph identity it cites
// has moved. If the slice changed, the proof must be recomputed, never
// re-stamped.
type DomainProof struct {
	Report      *closure.Report `json:"report"`
	SliceDigest string          `json:"slice_digest_sha256"`
	// CarryForwardRefusal is set when this publication could neither recompute
	// nor honestly carry forward the domain's proof.
	//
	// It exists so that "we do not know" is written down as itself. Emitting a
	// failed report would claim the domain is semantically wrong, and emitting
	// nothing at all would make the gap indistinguishable from a domain that was
	// never registered. Both are worse than a recorded refusal.
	CarryForwardRefusal string `json:"-"`
}

// Proven reports whether this proof affirmatively vouches for the domain.
func (p DomainProof) Proven() bool {
	return p.Report != nil && p.Report.ClosureProven
}

// Set is a complete proof set: one whole-graph generation plus one proof for
// every registered domain present in it.
type Set struct {
	Generation  Generation
	Marker      seedmeta.Marker
	Transaction []byte
	Domains     map[string]DomainProof
}

// Root is the parent directory for every store's proof set. It lives outside
// any published repository for the same reason the domain registry does: state
// that vouches for a repository must not be writable by that repository's own
// build.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for graph proof set: %w", err)
	}
	return filepath.Join(home, ".sensei", "graph"), nil
}

// Dir returns the proof-set directory for one store endpoint.
//
// Keyed by the store rather than by a repository, because the marker it holds
// is a property of the store's contents and every server reading that store
// must reach the same answer.
func Dir(storeURL string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, StoreID(storeURL)), nil
}

// StoreID is a stable, filesystem-safe identity for a store endpoint.
func StoreID(storeURL string) string {
	sum := sha256.Sum256([]byte(normalizeStoreKey(storeURL)))
	return hex.EncodeToString(sum[:])[:16]
}

// normalizeStoreKey reduces an endpoint to the store it belongs to.
//
// Path and query are deliberately dropped. The builder holds a Graph Store
// endpoint (".../store?default") and the server holds a SPARQL query endpoint
// (".../query"); both address the same Oxigraph instance and must therefore
// resolve to the same proof set. Keying on the full URL would give the writer
// and the reader two different directories, which is the same split-brain this
// package exists to remove.
func normalizeStoreKey(storeURL string) string {
	s := strings.TrimSpace(strings.ToLower(storeURL))
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		scheme := u.Scheme
		if scheme == "" {
			scheme = "http"
		}
		return scheme + "://" + u.Host
	}
	s = strings.TrimSuffix(s, "/")
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return s
}

// GenerationDir is where one immutable generation lives.
func GenerationDir(dir, generation string) string {
	return filepath.Join(dir, generationsDirName, generation)
}

// Write publishes a complete proof set and then makes it current.
//
// Every file is written into the generation directory first. The pointer is
// replaced last, by rename, so the swap from the previous generation to this
// one is a single filesystem operation. A crash before the rename leaves an
// unreferenced generation directory and a still-valid previous generation; it
// cannot produce the half-updated state this package exists to prevent.
func Write(dir, storeURL string, s *Set) error {
	if s == nil || s.Marker.Digest == "" {
		return fmt.Errorf("graph generation: refusing to publish an incomplete proof set")
	}
	if len(s.Domains) == 0 {
		return fmt.Errorf("graph generation: refusing to publish a proof set with no domain proofs")
	}
	genDir := GenerationDir(dir, s.Marker.Digest)
	if err := os.MkdirAll(filepath.Join(genDir, domainsDirName), 0o755); err != nil {
		return fmt.Errorf("graph generation: create generation directory: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(dir, StoreDescriptorFile), map[string]string{
		"store_url": storeURL,
		"store_id":  StoreID(storeURL),
	}); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(genDir, GenerationFileName), s.Generation); err != nil {
		return err
	}
	if len(s.Transaction) > 0 {
		if err := writeFileAtomic(filepath.Join(genDir, TransactionFileName), s.Transaction); err != nil {
			return err
		}
	}
	for domain, proof := range s.Domains {
		slug := DomainSlug(domain)
		if proof.Report != nil {
			if err := writeJSONAtomic(filepath.Join(genDir, domainsDirName, slug+closureSuffix), proof.Report); err != nil {
				return err
			}
		} else {
			// No report, and the reason is recorded rather than left as silence.
			if err := writeJSONAtomic(filepath.Join(genDir, domainsDirName, slug+refusalSuffix), map[string]string{
				"domain": domain,
				"reason": proof.CarryForwardRefusal,
			}); err != nil {
				return err
			}
		}
		if err := writeJSONAtomic(filepath.Join(genDir, domainsDirName, slug+sliceSuffix), map[string]string{
			"domain":              domain,
			"slice_digest_sha256": proof.SliceDigest,
		}); err != nil {
			return err
		}
	}

	// Last, and only now: make this generation the one readers serve.
	return writeJSONAtomic(filepath.Join(dir, CurrentPointerFile), Pointer{
		Generation:    s.Marker.Digest,
		PublishedUnix: s.Generation.PublishedUnix,
	})
}

// Load reads the current proof set. A missing pointer is reported as such so
// callers can fall back to the legacy per-repository layout rather than
// treating absence as a passing verdict.
func Load(dir string) (*Set, error) {
	var ptr Pointer
	if err := readJSON(filepath.Join(dir, CurrentPointerFile), &ptr); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ptr.Generation) == "" {
		return nil, fmt.Errorf("graph generation: pointer names no generation")
	}
	return LoadGeneration(dir, ptr.Generation)
}

// LoadGeneration reads one exact generation by name.
func LoadGeneration(dir, generation string) (*Set, error) {
	genDir := GenerationDir(dir, generation)
	set := &Set{Domains: map[string]DomainProof{}}
	if err := readJSON(filepath.Join(genDir, GenerationFileName), &set.Generation); err != nil {
		return nil, err
	}
	set.Marker = seedmeta.Marker{
		Digest:      set.Generation.MarkerDigest,
		IRI:         set.Generation.MarkerIRI,
		TripleCount: set.Generation.TripleCount,
	}
	if tx, err := os.ReadFile(filepath.Join(genDir, TransactionFileName)); err == nil {
		set.Transaction = tx
	}
	entries, err := os.ReadDir(filepath.Join(genDir, domainsDirName))
	if err != nil {
		return nil, fmt.Errorf("graph generation: read domain proofs: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		var (
			proof DomainProof
			slug  string
			label string
		)
		switch {
		case strings.HasSuffix(name, closureSuffix):
			var rep closure.Report
			if err := readJSON(filepath.Join(genDir, domainsDirName, name), &rep); err != nil {
				return nil, err
			}
			proof.Report = &rep
			slug = strings.TrimSuffix(name, closureSuffix)
			label = rep.Domain
		case strings.HasSuffix(name, refusalSuffix):
			var refusal struct {
				Domain string `json:"domain"`
				Reason string `json:"reason"`
			}
			if err := readJSON(filepath.Join(genDir, domainsDirName, name), &refusal); err != nil {
				return nil, err
			}
			proof.CarryForwardRefusal = refusal.Reason
			slug = strings.TrimSuffix(name, refusalSuffix)
			label = refusal.Domain
		default:
			continue
		}
		var slice struct {
			Domain      string `json:"domain"`
			SliceDigest string `json:"slice_digest_sha256"`
		}
		// A missing slice digest is not fatal on read: the proof is still a real
		// verdict for this generation. It only means the next publication must
		// recompute this domain instead of carrying it forward.
		_ = readJSON(filepath.Join(genDir, domainsDirName, slug+sliceSuffix), &slice)
		proof.SliceDigest = slice.SliceDigest
		if label == "" {
			label = slice.Domain
		}
		if label == "" {
			continue
		}
		set.Domains[label] = proof
	}
	return set, nil
}

// Compose derives the next complete proof set from the previous one plus the
// domain this build actually republished.
//
// This is the whole repair expressed as one pure function. Every registered
// domain present in the new generation gets a proof:
//
//   - the built domain gets the freshly computed report;
//   - a domain whose slice is byte-identical to the previous generation keeps
//     its verdict, re-stamped onto this publication — the corpus-to-slice proof
//     did not change, only the whole-graph identity it cites;
//   - a domain whose slice changed, or that has no comparable prior proof,
//     gets a recorded refusal rather than a carried-forward claim.
//
// The third case is the one that must never be softened. Re-stamping a verdict
// onto content it was not computed against is precisely the "fresh marker over
// a stale proof" shape the closure report exists to catch.
func Compose(prev *Set, gen Generation, marker seedmeta.Marker, transaction []byte,
	builtDomain string, builtReport *closure.Report, postUpdateNT []byte, registered []string) *Set {

	next := &Set{
		Generation:  gen,
		Marker:      marker,
		Transaction: transaction,
		Domains:     map[string]DomainProof{},
	}

	wanted := map[string]bool{}
	// A registered domain earns a proof only once it has actually published
	// something. Registered-but-absent is absent, not unproven.
	for _, d := range registered {
		if strings.TrimSpace(d) != "" && HasContent(postUpdateNT, d) {
			wanted[d] = true
		}
	}
	for _, d := range DomainsPresent(postUpdateNT) {
		wanted[d] = true
	}
	wanted[builtDomain] = true

	for domain := range wanted {
		digest := SliceDigest(postUpdateNT, domain)
		if domain == builtDomain {
			next.Domains[domain] = DomainProof{Report: builtReport, SliceDigest: digest}
			if builtReport == nil {
				next.Domains[domain] = DomainProof{
					SliceDigest:         digest,
					CarryForwardRefusal: "this build produced no closure report for the domain it published",
				}
			}
			continue
		}
		prior, ok := prev.ProofFor(domain)
		switch {
		case !ok || prior.Report == nil:
			next.Domains[domain] = DomainProof{
				SliceDigest: digest,
				CarryForwardRefusal: fmt.Sprintf(
					"no prior closure proof for %q was available to carry forward; rebuild that domain to restore its authority", domain),
			}
		case prior.SliceDigest == "":
			next.Domains[domain] = DomainProof{
				SliceDigest: digest,
				CarryForwardRefusal: fmt.Sprintf(
					"the prior proof for %q recorded no slice digest, so this publication cannot verify that its content is unchanged", domain),
			}
		case prior.SliceDigest != digest:
			next.Domains[domain] = DomainProof{
				SliceDigest: digest,
				CarryForwardRefusal: fmt.Sprintf(
					"the slice for %q changed during a publication of %q (%s -> %s), so its proof cannot be carried forward",
					domain, builtDomain, shortDigest(prior.SliceDigest), shortDigest(digest)),
			}
		default:
			carried := *prior.Report
			carried.MarkerDigest = marker.Digest
			next.Domains[domain] = DomainProof{Report: &carried, SliceDigest: digest}
		}
	}
	return next
}

func shortDigest(d string) string {
	if len(d) <= 12 {
		return d
	}
	return d[:12]
}

// ProofFor returns the proof recorded for one domain in this set.
func (s *Set) ProofFor(domain string) (DomainProof, bool) {
	if s == nil {
		return DomainProof{}, false
	}
	p, ok := s.Domains[domain]
	return p, ok
}

// DomainSlug makes a domain safe to use as a filename without losing which
// domain it names.
func DomainSlug(domain string) string {
	var b strings.Builder
	for _, r := range domain {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// PartitionByDomain splits an N-Triples graph into the subjects owned solely by
// one domain and everything else.
//
// "Solely owned" is the same predicate the scoped promotion DELETE uses: a
// subject attributed to this domain and to no other. Whole-graph marker
// subjects belong to neither side and are dropped from both, because they
// describe the publication rather than any domain's content.
//
// One implementation, used by both the builder's scoped rebuild and the slice
// digest. Two copies of this predicate that drift apart would silently mean
// "the slice I proved" and "the slice I replaced" are different sets.
func PartitionByDomain(nt []byte, domain string) (owned, rest []byte) {
	const rdfType = "<http://www.w3.org/1999/02/22-rdf-syntax-ns#type>"
	repoPredicate := "<" + seedmeta.NamespaceIRI + "repo>"
	seedClass := "<" + seedmeta.NamespaceIRI + "SeedBuild>"

	type subjectInfo struct {
		repos  map[string]bool
		marker bool
	}
	infos := map[string]*subjectInfo{}
	lines := strings.Split(string(nt), "\n")
	for _, line := range lines {
		subject, predicate, tail, ok := splitTriple(line)
		if !ok {
			continue
		}
		info := infos[subject]
		if info == nil {
			info = &subjectInfo{repos: map[string]bool{}}
			infos[subject] = info
		}
		if predicate == repoPredicate {
			if value, ok := literalValue(tail); ok {
				info.repos[value] = true
			}
		}
		if predicate == rdfType && strings.TrimSpace(tail) == seedClass+" ." {
			info.marker = true
		}
	}

	var ownedBuf, restBuf strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		subject, _, _, ok := splitTriple(line)
		if !ok {
			continue
		}
		info := infos[subject]
		switch {
		case info != nil && info.marker:
			// Publication metadata: not content, and not any domain's to own.
		case info != nil && info.repos[domain] && len(info.repos) == 1:
			ownedBuf.WriteString(trimmed)
			ownedBuf.WriteByte('\n')
		default:
			restBuf.WriteString(trimmed)
			restBuf.WriteByte('\n')
		}
	}
	return []byte(ownedBuf.String()), []byte(restBuf.String())
}

// SliceDigest is the identity of one domain's published content.
//
// Ownership here is INCLUSIVE — every subject carrying this domain's repo tag,
// whether or not another domain also claims it — and it deliberately differs
// from the sole-ownership predicate PartitionByDomain uses for replacement.
//
// Sole ownership is correct for deciding what a rebuild may delete. It is wrong
// for identity, and using it here reintroduced the exact defect this package
// exists to remove. A live two-domain run found 143 subjects co-owned by
// services and sensei-code: publishing sensei-code added its repo tag to those
// shared subjects, which pushed them out of services' solely-owned set, which
// moved services' digest even though not one byte of services' content had
// changed. A per-domain fact must not be a function of what other domains
// publish.
//
// Foreign repo tags are therefore excluded from the digest: another domain
// declaring co-ownership of a shared subject is that domain's content, not
// this one's. Everything else about a shared subject IS included, because if a
// co-owned subject's properties really do change, this domain's closure proof
// was computed against different content and must not be carried forward.
//
// Order-independent and duplicate-independent, so it answers "is this domain's
// content the same?" rather than "were these bytes produced the same way?".
func SliceDigest(nt []byte, domain string) string {
	repoPredicate := "<" + seedmeta.NamespaceIRI + "repo>"
	tagged := taggedSubjects(nt, domain)

	var b strings.Builder
	for _, line := range strings.Split(string(nt), "\n") {
		subject, predicate, tail, ok := splitTriple(line)
		if !ok || !tagged[subject] {
			continue
		}
		if predicate == repoPredicate {
			// Keep only this domain's own attribution.
			if value, ok := literalValue(tail); !ok || value != domain {
				continue
			}
		}
		b.WriteString(strings.TrimSpace(line))
		b.WriteByte('\n')
	}
	return DigestLines([]byte(b.String()))
}

// taggedSubjects collects every subject attributed to a domain, shared or not.
func taggedSubjects(nt []byte, domain string) map[string]bool {
	repoPredicate := "<" + seedmeta.NamespaceIRI + "repo>"
	out := map[string]bool{}
	for _, line := range strings.Split(string(nt), "\n") {
		subject, predicate, tail, ok := splitTriple(line)
		if !ok || predicate != repoPredicate {
			continue
		}
		if value, ok := literalValue(tail); ok && value == domain {
			out[subject] = true
		}
	}
	return out
}

// HasContent reports whether a domain owns anything in this graph.
//
// A registered domain that was never published is ABSENT, not unproven. Listing
// it as unproven would fill the proof set with permanent refusals for domains
// nobody published, and drown the refusals that mean something.
func HasContent(nt []byte, domain string) bool {
	return len(taggedSubjects(nt, domain)) > 0
}

// DigestLines canonicalizes and digests a set of N-Triples lines.
func DigestLines(nt []byte) string {
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(string(nt), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		lines = append(lines, trimmed)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// DomainsPresent lists every domain that owns content in a graph.
func DomainsPresent(nt []byte) []string {
	repoPredicate := "<" + seedmeta.NamespaceIRI + "repo>"
	seen := map[string]bool{}
	for _, line := range strings.Split(string(nt), "\n") {
		_, predicate, tail, ok := splitTriple(line)
		if !ok || predicate != repoPredicate {
			continue
		}
		if value, ok := literalValue(tail); ok && strings.TrimSpace(value) != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func splitTriple(line string) (subject, predicate, tail string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", "", false
	}
	first := strings.IndexByte(trimmed, ' ')
	if first <= 0 {
		return "", "", "", false
	}
	rest := strings.TrimSpace(trimmed[first+1:])
	second := strings.IndexByte(rest, ' ')
	if second <= 0 {
		return "", "", "", false
	}
	return trimmed[:first], rest[:second], strings.TrimSpace(rest[second+1:]), true
}

func literalValue(tail string) (string, bool) {
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tail), "."))
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return "", false
	}
	return parsed, true
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("graph generation: encode %s: %w", filepath.Base(path), err)
	}
	return writeFileAtomic(path, append(b, '\n'))
}

// writeFileAtomic replaces a file in one rename so a reader sees the old
// contents or the new contents, never a partial write.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("graph generation: create %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("graph generation: stage %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("graph generation: write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("graph generation: sync %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("graph generation: close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("graph generation: publish %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("graph generation: decode %s: %w", filepath.Base(path), err)
	}
	return nil
}
