// Package publication binds a published graph generation to the repository
// revision that produced it.
//
// WHY THIS EXISTS. The graph marker proves CONTENT identity: the bytes a server
// answers with are the bytes a publication produced. It never promised to prove
// REVISION identity, and nothing else did either. So the proof chain read:
//
//	serving   --[digest]--> published    cryptographic
//	published --[say-so]--> revision     testimony
//
// A self-governing system cannot close that gap by assertion. A graph that
// cannot state which revision produced it cannot later show that a governed run
// inherited a particular world -- only that it inherited some generation.
//
// WHY NOT ON THE GRAPH MARKER. The store is multi-domain. One generation's
// digest covers every domain present, so a single revision stamped on the
// whole-store marker would be a lie about all but one of them. Revision
// provenance is therefore per-domain, and lives on a receipt.
//
// CONTENT IDENTITY AND REVISION IDENTITY ARE SEPARATE, ON PURPOSE. Two distinct
// commits can carry byte-identical sources. Their SourceDigest is then equal
// and their Revision is not, and a receipt must keep saying which commit it
// came from. Collapsing them because the content matched would rebuild exactly
// the defect this package was written to remove, so Revision is an independent
// input to the receipt's identity rather than a value derived from content.
package publication

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
)

// ReceiptVersion is the receipt shape a publication speaks.
//
// It exists because v1 receipts are IMMUTABLE HISTORICAL EVIDENCE. A1's
// succession proof rests on one, and changing the identity algorithm in place
// would make that receipt fail to verify under the very code that improved it --
// rewriting history in the costume of a repair. So the algorithm is versioned
// and v1 stays exactly as it was.
type ReceiptVersion string

const (
	// ReceiptV1 is the shape A1 published. Its identity covers domain,
	// revision, tree, state and source_digest. It also carries an operational
	// SourceRoot -- an absolute filesystem path -- which is NOT identity
	// bearing, and that is the defect v2 exists to remove.
	ReceiptV1 ReceiptVersion = "v1"
	// ReceiptV2 replaces the operational SourceRoot with a durable
	// repository-relative SourcePath, and every field it carries participates
	// in identity.
	ReceiptV2 ReceiptVersion = "v2"
)

// CurrentReceiptVersion is what new publications emit.
const CurrentReceiptVersion = ReceiptV2

// Valid reads membership by enumeration.
func (v ReceiptVersion) Valid() bool {
	switch v {
	case ReceiptV1, ReceiptV2:
		return true
	}
	return false
}

// SourceState says how much the revision is allowed to claim.
//
// It is a closed vocabulary read by membership. An unrecognised value is not
// silently treated as acceptable, because a state nobody can name must not
// inherit the privileges of one that is proven.
type SourceState string

const (
	// CleanExact: the source tree is a git checkout with no uncommitted change
	// affecting the compiled inputs. Only this state may claim the generation
	// was produced from exactly that revision.
	CleanExact SourceState = "CLEAN_EXACT"
	// Dirty: HEAD is known, but the working tree differs from it. The revision
	// is recorded because it is useful, and it must never be read as "produced
	// from that commit" -- what was compiled is not what that commit contains.
	Dirty SourceState = "DIRTY"
	// Unknown: the source root is not a resolvable git checkout. The revision is
	// EMPTY and is never guessed. Publishing may still be allowed; claiming a
	// revision is not.
	Unknown SourceState = "UNKNOWN"
)

// Valid reports whether s is a member of the closed vocabulary.
func (s SourceState) Valid() bool {
	switch s {
	case CleanExact, Dirty, Unknown:
		return true
	}
	return false
}

// ClaimsExactRevision reports whether this state permits the claim that the
// generation was produced from exactly Receipt.Revision.
//
// Callers MUST route through this rather than testing Revision != "", which is
// the shape that would let a DIRTY publication pass as an exact one.
func (s SourceState) ClaimsExactRevision() bool { return s == CleanExact }

// Receipt is the per-domain publication identity.
type Receipt struct {
	Domain string
	// Revision is the commit the sources came from, or "" when Unknown.
	Revision string
	// Tree is the git tree object of the compiled source root. It distinguishes
	// two commits whose awareness corpus is identical, without being the
	// content digest of the compiled output.
	Tree  string
	State SourceState
	// SourceRoot is the absolute filesystem path a publication ran from. It is
	// OPERATIONAL, it is v1-only, and it is deliberately not identity bearing:
	// /tmp/build-7f2/docs/awareness says nothing durable about which knowledge
	// was published, and two machines publishing the same corpus would disagree
	// on it. v2 records SourcePath instead and drops this field entirely.
	SourceRoot string
	// SourcePath is the source root RELATIVE TO THE REPOSITORY the domain names
	// -- "docs/awareness". It is durable across machines and checkouts, it is
	// what a knowledge contract can be written against, and in v2 it
	// participates in identity.
	SourcePath   string
	SourceDigest string
	// Version selects the identity algorithm. The zero value resolves to
	// ReceiptV1, because that is what every receipt published before versioning
	// existed actually is -- a migration fact, not a guess.
	Version ReceiptVersion
}

// version resolves the zero value to v1 without pretending it was stated.
func (r Receipt) version() ReceiptVersion {
	if r.Version == "" {
		return ReceiptV1
	}
	return r.Version
}

// CLOSURE IS DELIBERATELY NOT A RECEIPT FIELD. It is proven after promotion, so
// a receipt written before it could only ever state "PENDING" -- a value that is
// never revised and therefore lies the moment the proof completes. The closure
// report is bound to the marker digest, and the marker covers this receipt, so
// the two are already joined through the generation.
//
// Published is the local artifact written beside the graph marker. It pairs the
// receipt with the generation that contains it.
//
// GENERATION IS NOT A FIELD OF THE RECEIPT, and the reason is structural rather
// than stylistic. The generation digest is computed over the content the
// publication promotes -- content that includes this receipt. A receipt that
// also named the generation would have to be hashed before it existed.
//
// So the binding runs the other way, and is stronger for it:
//
//	the receipt commits to the revision            (its own digest)
//	the generation commits to the receipt          (by containing it)
//
// Nothing in that pair is self-referential, and neither half can be changed
// without breaking the other.
type Published struct {
	Receipt     Receipt `json:"receipt"`
	ReceiptIRI  string  `json:"receipt_iri"`
	Generation  string  `json:"graph_generation"`
	TripleCount int64   `json:"triple_count"`
}

const (
	receiptClassIRI = seedmeta.NamespaceIRI + "DomainPublicationReceipt"
	pointerClassIRI = seedmeta.NamespaceIRI + "DomainPublicationPointer"
	receiptPrefix   = "https://globular.io/awareness/publication/receipt/sha256-"
	pointerPrefix   = "https://globular.io/awareness/publication/current/"
	typeIRI         = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	labelIRI        = "http://www.w3.org/2000/01/rdf-schema#label"
)

// field IRIs, one per receipt attribute.
const (
	pDomain    = seedmeta.NamespaceIRI + "publicationDomain"
	pRevision  = seedmeta.NamespaceIRI + "publicationSourceRevision"
	pTree      = seedmeta.NamespaceIRI + "publicationSourceTree"
	pState     = seedmeta.NamespaceIRI + "publicationSourceState"
	pRoot      = seedmeta.NamespaceIRI + "publicationSourceRoot"
	pPath      = seedmeta.NamespaceIRI + "publicationSourcePath"
	pVersion   = seedmeta.NamespaceIRI + "publicationReceiptVersion"
	pSourceDig = seedmeta.NamespaceIRI + "publicationSourceDigest"
	pClosure   = seedmeta.NamespaceIRI + "publicationClosure"
	pCurrent   = seedmeta.NamespaceIRI + "currentPublication"
	pRepo      = seedmeta.NamespaceIRI + "repo"
)

// Identity is the receipt's immutable name: a digest over every field its
// version carries.
//
// Revision participates directly, so two commits with identical sources get
// DIFFERENT receipt identities. That is the point -- see the package comment.
//
// THE ALGORITHM IS VERSIONED AND v1 IS FROZEN. A1's succession proof rests on a
// v1 receipt; recomputing it under a changed algorithm would report that
// historical evidence as invalid, which is rewriting the past rather than
// improving the present. v2 is a different algorithm over a different field
// set, not a correction applied retroactively to v1.
func (r Receipt) Identity() string {
	fields := [][2]string{
		{"domain", r.Domain},
		{"revision", r.Revision},
		{"tree", r.Tree},
		{"state", string(r.State)},
		{"source_digest", r.SourceDigest},
	}
	if r.version() == ReceiptV2 {
		// Every field a v2 receipt carries participates. SourceRoot is absent
		// from v2 entirely rather than present-and-unhashed, which is the shape
		// that let an operational path ride inside an immutable record without
		// being covered by its digest.
		fields = [][2]string{
			{"version", string(ReceiptV2)},
			{"domain", r.Domain},
			{"revision", r.Revision},
			{"tree", r.Tree},
			{"state", string(r.State)},
			{"source_path", r.SourcePath},
			{"source_digest", r.SourceDigest},
		}
	}
	var b strings.Builder
	for _, kv := range fields {
		// Length-prefixed so no field's value can impersonate a field boundary.
		fmt.Fprintf(&b, "%s:%d:%s\n", kv[0], len(kv[1]), kv[1])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// IRI is the immutable receipt node.
func (r Receipt) IRI() string { return receiptPrefix + r.Identity() }

// PointerIRI is the per-domain "current publication" node. It is stable across
// publications; only what it points AT changes.
func PointerIRI(domain string) string { return pointerPrefix + escapeIRISegment(domain) }

// CurrentPublicationPredicate is the pointer edge, exported so a server can
// resolve it by bounded lookup instead of dumping the graph.
const CurrentPublicationPredicate = pCurrent

// Triples renders the receipt and the pointer that names it current.
//
// The receipt carries NO aw:repo tag, so the scoped per-domain replacement --
// which deletes subjects tagged with the domain -- cannot reclaim it. Receipts
// accumulate; history is preserved rather than repainted.
//
// The pointer DOES carry the tag, so each publication replaces it. Exactly one
// current publication per domain, and every previous one still readable.
func (r Receipt) Triples() []byte {
	var out strings.Builder
	iri := r.IRI()
	fmt.Fprintf(&out, "<%s> <%s> <%s> .\n", iri, typeIRI, receiptClassIRI)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", iri, labelIRI, r.label())
	// v2 publishes the durable repo-relative path and NOT the operational
	// SourceRoot. Emitting both would reintroduce exactly what v2 removes: a
	// field inside an immutable record that its digest does not cover.
	stated := [][2]string{
		{pDomain, r.Domain},
		{pRevision, r.Revision},
		{pTree, r.Tree},
		{pState, string(r.State)},
		{pRoot, r.SourceRoot},
		{pSourceDig, r.SourceDigest},
	}
	if r.version() == ReceiptV2 {
		stated = [][2]string{
			{pVersion, string(ReceiptV2)},
			{pDomain, r.Domain},
			{pRevision, r.Revision},
			{pTree, r.Tree},
			{pState, string(r.State)},
			{pPath, r.SourcePath},
			{pSourceDig, r.SourceDigest},
		}
	}
	for _, kv := range stated {
		if kv[1] == "" {
			// An absent revision is left ABSENT. Writing "" would be a stated
			// value, and Unknown means the field was never established.
			continue
		}
		fmt.Fprintf(&out, "<%s> <%s> %q .\n", iri, kv[0], kv[1])
	}
	ptr := PointerIRI(r.Domain)
	fmt.Fprintf(&out, "<%s> <%s> <%s> .\n", ptr, typeIRI, pointerClassIRI)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", ptr, pRepo, r.Domain)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", ptr, pDomain, r.Domain)
	fmt.Fprintf(&out, "<%s> <%s> <%s> .\n", ptr, pCurrent, iri)
	return []byte(out.String())
}

func (r Receipt) label() string {
	rev := r.Revision
	if rev == "" {
		rev = "revision unknown"
	} else if len(rev) > 12 {
		rev = rev[:12]
	}
	return fmt.Sprintf("%s published from %s (%s)", r.Domain, rev, r.State)
}

// Parse reads receipts back out of N-Triples. Used to verify what the store
// actually holds, rather than trusting what was sent to it.
func Parse(nt []byte) map[string]Receipt {
	out := map[string]Receipt{}
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "<"+receiptPrefix) {
			continue
		}
		subj, pred, obj, ok := splitTriple(line)
		if !ok {
			continue
		}
		r := out[subj]
		switch pred {
		case pDomain:
			r.Domain = obj
		case pRevision:
			r.Revision = obj
		case pTree:
			r.Tree = obj
		case pState:
			r.State = SourceState(obj)
		case pRoot:
			r.SourceRoot = obj
		case pPath:
			r.SourcePath = obj
		case pVersion:
			r.Version = ReceiptVersion(obj)
		case pSourceDig:
			r.SourceDigest = obj
		}
		out[subj] = r
	}
	return out
}

// PointerState is how a current-publication lookup ended.
//
// ABSENT and DANGLING are DIFFERENT WORLDS and collapsing them fails open on
// the second: "nothing was ever published here" is a benign steady state, while
// "a pointer exists and its target cannot be found" means the publication
// record is corrupt. A start gate told ABSENT for a dangling pointer would
// report never-published for a broken world.
type PointerState int

const (
	// PointerAbsent: no current-publication pointer exists for the domain.
	PointerAbsent PointerState = iota
	// PointerDangling: a pointer exists and names a receipt that is not
	// present, or is present and unparseable.
	PointerDangling
	// PointerResolved: the pointer names a receipt that was found.
	PointerResolved
)

// Resolve returns the STORED pointer target, the receipt it names, and how the
// lookup ended.
//
// The stored target is returned because verification must compare two
// INDEPENDENTLY DERIVED values. Recomputing an identity from a receipt's fields
// and then checking it against an identity recomputed from the same fields is a
// tautology that passes for any tampered receipt; the honest check is
// recomputed-vs-stored, and only a caller holding the stored value can make it.
func Resolve(nt []byte, domain string) (storedTarget string, r Receipt, state PointerState) {
	want := "<" + PointerIRI(domain) + "> <" + pCurrent + "> <"
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, want) {
			rest := strings.TrimPrefix(line, want)
			if i := strings.Index(rest, ">"); i >= 0 {
				storedTarget = rest[:i]
			}
		}
	}
	if storedTarget == "" {
		return "", Receipt{}, PointerAbsent
	}
	r, ok := Parse(nt)[storedTarget]
	if !ok {
		return storedTarget, Receipt{}, PointerDangling
	}
	return storedTarget, r, PointerResolved
}

// ReceiptFromTriples parses one receipt from the triples describing a single
// subject, for callers that resolve by bounded lookup rather than a whole-graph
// dump.
//
// AMBIGUITY IS AN ERROR, NOT AN INPUT. A subject carrying two distinct values
// for one identity-bearing predicate has no single value, and the parser would
// otherwise keep whichever row arrived last. SPARQL SELECT has no defined
// order, so the same stored graph could verify under one ordering and refuse
// under another -- a receipt whose meaning depends on row order is not an
// identity. This is the pointer-ambiguity rule applied to the receipt body,
// which is where it was missing.
func ReceiptFromTriples(subject string, predicates, objects []string) (Receipt, error) {
	seen := map[string]map[string]struct{}{}
	for i := range predicates {
		if !strings.HasPrefix(predicates[i], seedmeta.NamespaceIRI+"publication") {
			continue
		}
		if seen[predicates[i]] == nil {
			seen[predicates[i]] = map[string]struct{}{}
		}
		seen[predicates[i]][objects[i]] = struct{}{}
	}
	for _, pred := range sortedKeys(seen) {
		if len(seen[pred]) > 1 {
			return Receipt{}, fmt.Errorf(
				"receipt field %s has %d distinct values, so the receipt has no single identity",
				pred, len(seen[pred]))
		}
	}
	var b strings.Builder
	for i := range predicates {
		fmt.Fprintf(&b, "<%s> <%s> %q .\n", subject, predicates[i], objects[i])
	}
	r, ok := Parse([]byte(b.String()))[subject]
	if !ok {
		return Receipt{}, fmt.Errorf("no receipt could be parsed from the triples describing %s", subject)
	}
	return r, nil
}

func sortedKeys(m map[string]map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Current returns the receipt the pointer for domain names, and whether the
// pointer resolved to a receipt actually present in nt.
//
// Prefer Resolve: this loses the stored target, so a caller cannot verify the
// receipt against anything but itself.
func Current(nt []byte, domain string) (Receipt, bool) {
	want := "<" + PointerIRI(domain) + "> <" + pCurrent + "> <"
	var target string
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, want) {
			rest := strings.TrimPrefix(line, want)
			if i := strings.Index(rest, ">"); i >= 0 {
				target = rest[:i]
			}
		}
	}
	if target == "" {
		return Receipt{}, false
	}
	r, ok := Parse(nt)[target]
	return r, ok
}

// FieldsMatchVersion refuses a receipt carrying fields its version does not
// authenticate.
//
// A v1 receipt backfilled with publicationSourcePath parses fine, and the FROZEN
// v1 identity does not hash that field -- so the receipt still verifies while
// exposing an unauthenticated path. That is precisely the present-and-unhashed
// shape v2 was created to remove, reappearing through the version-agnostic
// parser. A field a version cannot authenticate must make the receipt
// unreadable, not decorate it.
func (r Receipt) FieldsMatchVersion() error {
	if r.version() == ReceiptV1 && r.SourcePath != "" {
		return fmt.Errorf(
			"a v1 receipt carries publicationSourcePath %q, which the v1 identity does not hash: "+
				"the field is unauthenticated and must not be served as verified", r.SourcePath)
	}
	if r.version() == ReceiptV2 && r.SourceRoot != "" {
		return fmt.Errorf(
			"a v2 receipt carries publicationSourceRoot, which v2 does not hash or publish")
	}
	return nil
}

// VerifyIdentity reports whether a receipt read back from the store still
// hashes to the IRI it is stored under. It catches a receipt whose fields were
// altered after publication.
func VerifyIdentity(iri string, r Receipt) bool {
	return iri == r.IRI()
}

func splitTriple(line string) (subj, pred, obj string, ok bool) {
	if !strings.HasPrefix(line, "<") {
		return "", "", "", false
	}
	end := strings.Index(line, ">")
	if end < 0 {
		return "", "", "", false
	}
	subj = line[1:end]
	rest := strings.TrimSpace(line[end+1:])
	if !strings.HasPrefix(rest, "<") {
		return "", "", "", false
	}
	pend := strings.Index(rest, ">")
	if pend < 0 {
		return "", "", "", false
	}
	pred = rest[1:pend]
	rest = strings.TrimSpace(rest[pend+1:])
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ".")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, `"`) {
		if i := strings.LastIndex(rest, `"`); i > 0 {
			obj = unescapeLiteral(rest[1:i])
			return subj, pred, obj, true
		}
		return "", "", "", false
	}
	if strings.HasPrefix(rest, "<") && strings.HasSuffix(rest, ">") {
		return subj, pred, rest[1 : len(rest)-1], true
	}
	return "", "", "", false
}

func unescapeLiteral(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	return strings.ReplaceAll(s, `\\`, `\`)
}

func escapeIRISegment(s string) string {
	repl := strings.NewReplacer(" ", "%20", "<", "%3C", ">", "%3E", `"`, "%22", "\\", "%5C")
	return repl.Replace(s)
}

// SortedIRIs is a deterministic ordering helper for callers that report
// receipts.
func SortedIRIs(m map[string]Receipt) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
