// SPDX-License-Identifier: AGPL-3.0-only

// active_generation.go answers the question Phase 2 of the graph-identity front
// asked and could not get an answer to:
//
//	For domain github.com/globulario/sensei-code, which exact graph generation
//	is ACTIVE right now?
//
// Before this, a generation was identified only by a digest inside a marker FILE,
// and that file's path resolves from the current working directory. So the answer
// depended on where you stood, which is not an answer. Four laws waited on it:
//
//	law 1   one active graph identity per governed domain
//	law 4   a governed run pins a graph identity, not merely a URL
//	law 5   every query must prove it belongs to the pinned identity
//	law 12  two instances claiming ACTIVE for one domain is an error
//
// The owner is the domain registry, for the same reason it owns domain -> endpoint
// (see resolveDomainServiceAddr): it is operator-controlled and kept OUTSIDE any
// published repository, and a repository must not be able to declare its own graph
// active any more than it may vouch for its own corpus.
//
// It is inert until an operator or a successful publication records a generation,
// so no existing caller's behaviour moves today.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
	"gopkg.in/yaml.v3"
)

// activeGenerationMismatchError is law 12 made typed: the served graph and the
// registry name different active generations for one domain. It is stated as an
// unresolvable ambiguity rather than a staleness, because the caller genuinely
// cannot tell which one is right and must not pick.
type activeGenerationMismatchError struct {
	Domain   string
	Declared string
	Served   string
}

func (e *activeGenerationMismatchError) Error() string {
	if strings.TrimSpace(e.Served) == "" {
		return fmt.Sprintf("graph identity is unverifiable for domain %s: the registry declares generation %s ACTIVE "+
			"and the served graph states no generation.\n"+
			"  a triple count is evidence, not identity (law 13), so nothing here can stand in for the missing one\n"+
			"  publish through the transactional path, which records the generation it activated",
			e.Domain, e.Declared)
	}
	return fmt.Sprintf("ambiguous graph identity for domain %s: which generation is ACTIVE cannot be decided here.\n"+
		"  registry declares: %s\n"+
		"  served graph says: %s\n"+
		"  two claimants for one domain is an error, not a choice for the caller (law 12)\n"+
		"  resolve it by pointing this caller at the store serving %s, or by recording the new generation in the registry",
		e.Domain, e.Declared, e.Served, e.Declared)
}

// verifyActiveGeneration refuses a served graph that is not the generation the
// registry declares ACTIVE for domain.
//
// Absence is handled asymmetrically, deliberately:
//
//   - no declaration: there is nothing to contradict, so this is inert and every
//     existing caller proceeds exactly as before;
//   - a declaration and no served generation: that cannot be VERIFIED, and an
//     unverifiable identity is not a matching one. This is the same rule
//     markerAgreement follows, and law 13's reason — a matching triple count is
//     not a matching graph.
func verifyActiveGeneration(domain, declared, served string) error {
	want := normalizeGeneration(declared)
	if want == "" {
		return nil
	}
	if got := normalizeGeneration(served); got == want {
		return nil
	}
	return &activeGenerationMismatchError{
		Domain:   domain,
		Declared: strings.TrimSpace(declared),
		Served:   strings.TrimSpace(served),
	}
}

// normalizeGeneration strips what is transport noise rather than identity:
// surrounding whitespace and digest case. It does NOT shorten or expand a digest —
// a prefix is a different string, and treating one as equal to its longer form
// would let a 7-character coincidence pass for a generation.
func normalizeGeneration(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// declaredActiveGeneration reports the generation the registry declares ACTIVE for
// domain, or "" when there is no declaration to read.
//
// Every way of having no answer collapses to "": no registry file, an unreadable
// or malformed one, an unregistered domain, a registered domain that states
// nothing. That is safe HERE and only here, because "" means inert — the caller
// proceeds as it did before this file existed. It is deliberately not used to
// decide anything else: a malformed registry must still fail loudly wherever the
// registry grants authority, which is AdmitPublication's job, not this reader's.
func declaredActiveGeneration(registryPath, domain string) string {
	if strings.TrimSpace(registryPath) == "" {
		return ""
	}
	reg, err := LoadDomainRegistry(registryPath)
	if err != nil || reg == nil {
		return ""
	}
	return strings.TrimSpace(reg.Domains[domain].ActiveGeneration)
}

// recordActiveGeneration writes generation as domain's ACTIVE pointer.
//
// This is the activation transition of law 6, and only the transactional
// publication path may call it: the one caller that has already proven the served
// store holds exactly what the marker certifies. It overwrites a previous
// declaration on purpose — that IS the activation, and the whole point of a single
// pointer is that the previous value stops being current.
//
// A domain with no registry entry is left alone and the call succeeds. The
// registry records what an operator ADMITTED; a publication may update a
// declaration but must not create an admission, or a build would grant itself the
// standing the registry exists to withhold.
//
// The edit is surgical — a yaml.Node rewrite rather than marshalling
// DomainRegistry back out — because this file is operator-edited and carries
// comments and fields this program does not model. Re-marshalling the parsed
// struct would silently delete all of them.
func recordActiveGeneration(registryPath, domain, generation string) error {
	gen := strings.TrimSpace(generation)
	if strings.TrimSpace(registryPath) == "" || strings.TrimSpace(domain) == "" || gen == "" {
		return nil
	}
	// The registry is SHARED. A publication holds a lock on its own store, and two
	// domains publishing to two stores hold two different locks -- neither of which
	// serializes this file. Both then read the same YAML, both rewrite the whole of it,
	// and one successful publication's pointer is lost while both report success
	// (reproduced on the first attempt: TestTwoConcurrentActiveGenerationUpdatesBothSurvive).
	//
	// So the lock is held across the READ and the WRITE, not around the write alone: the
	// value written depends on the bytes read, and a lock that spans only the second half
	// of a read-modify-write serializes nothing.
	//
	// It WAITS rather than failing fast, which is the opposite of the graph publication
	// lock and deliberately so: two publications to one store must not both proceed, but
	// two domains updating their own pointers must BOTH finish. Failing fast here would
	// turn a legitimate concurrent publication into a lost pointer, which is the defect.
	unlock, err := lockDomainRegistry(registryPath)
	if err != nil {
		return err
	}
	defer unlock()

	raw, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read domain registry %s: %w", registryPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("domain registry %s: %w", registryPath, err)
	}
	entry := mappingValue(mappingValue(documentRoot(&doc), "domains"), domain)
	if entry == nil || entry.Kind != yaml.MappingNode {
		// Not registered: nothing to update, and nothing to invent.
		return nil
	}
	setMappingScalar(entry, "active_generation", gen)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("domain registry %s: %w", registryPath, err)
	}
	return writeFileAtomic(registryPath, out)
}

// documentRoot unwraps a parsed document down to its content node.
func documentRoot(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// mappingValue returns the value node for key, or nil. Absence is nil rather than
// an empty node so a caller cannot mistake "not there" for "there and empty".
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMappingScalar sets key to value in m, replacing an existing entry in place so
// the operator's key order and surrounding comments survive.
func setMappingScalar(m *yaml.Node, key, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Kind = yaml.ScalarNode
			m.Content[i+1].Tag = "!!str"
			m.Content[i+1].Value = value
			m.Content[i+1].Content = nil
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// --- G5 reporting ------------------------------------------------------------

// endpointReport is what the Endpoint block of `sensei metadata` states: which
// graph answered, where that choice came from, and whether the two records of its
// identity — the marker file and the registry's ACTIVE pointer — agree with it.
type endpointReport struct {
	Root string
	// RootError is why no project root could be resolved, when none could. Carried
	// rather than discarded because the marker path would otherwise be relative and
	// the report would name a different marker from each directory (G4).
	RootError    error
	Domain       string
	ResolvedAddr string
	AddrSource   string
	RegistryPath string
	LiveDigest   string
	LiveTriples  int
}

// renderEndpointBlock writes the Endpoint block.
//
// It reports both records of identity even when they agree, because G5 requires
// one place that states the whole canonical state: "no complaint" printed by a
// command that might not have looked is not the same claim as "looked, and it
// agrees". An unset ACTIVE pointer is therefore reported as NOT DECLARED and
// explicitly as unverifiable, never as agreement (law 13's reason).
func renderEndpointBlock(w io.Writer, r endpointReport) {
	fmt.Fprintln(w, "Endpoint:")
	fmt.Fprintf(w, "  Awareness address:   %s\n", r.ResolvedAddr)
	fmt.Fprintf(w, "  Chosen from:         %s\n", r.AddrSource)

	// Complaints are collected as each field is resolved and printed once at the end
	// (G5: "one read-only command that reports the entire canonical state in one
	// place"). A reader checking a graph should not have to know which of five lines
	// carries the problem, and a summary assembled from the same values the fields
	// printed cannot contradict them.
	var disagreements []string

	// ONE marker resolver, and it reports which of the four tiers answered (G4).
	markerPath, markerSource, markerErr := resolveGraphMarkerFile("", r.Root, false)
	switch {
	case markerErr != nil:
		fmt.Fprintf(w, "  Marker file:         cannot be resolved: %s\n", firstLine(markerErr.Error()))
		if r.RootError != nil {
			fmt.Fprintf(w, "                       (no project root: %v)\n", r.RootError)
		}
		fmt.Fprintf(w, "  Marker verdict:      cannot be verified: no marker file could be named\n")
		disagreements = append(disagreements, "no marker file can be named, so nothing certifies the served graph")
	default:
		fmt.Fprintf(w, "  Marker file:         %s\n", markerPath)
		fmt.Fprintf(w, "  Chosen from:         %s\n", markerSource)
		if strings.Contains(markerSource, "orphaned") {
			disagreements = append(disagreements, "an orphaned legacy marker exists and certifies a generation nothing serves")
		}
		if marker, err := seedmeta.ReadMarkerFile(markerPath); err == nil {
			verdict := markerAgreement(r.LiveDigest, r.LiveTriples, marker.Digest, int(marker.TripleCount))
			fmt.Fprintf(w, "  Marker verdict:      %s\n", verdict)
			if strings.HasPrefix(verdict, "DOES NOT") || strings.HasPrefix(verdict, "cannot be verified") {
				disagreements = append(disagreements, "the marker "+verdict)
			}
		} else {
			fmt.Fprintf(w, "  Marker verdict:      cannot be verified: the marker is not readable (%v)\n", err)
			disagreements = append(disagreements, "the marker file is not readable, so it certifies nothing")
		}
	}

	declared := declaredActiveGeneration(r.RegistryPath, r.Domain)
	switch {
	case declared == "":
		fmt.Fprintf(w, "  Active generation:   NOT DECLARED for %s in %s\n", r.Domain, registryPathOrNone(r.RegistryPath))
		fmt.Fprintf(w, "  Generation verdict:  cannot be verified: no ACTIVE generation is declared, so nothing here proves the served graph is the intended one\n")
		disagreements = append(disagreements, "no ACTIVE generation is declared for this domain, so the served graph cannot be proven to be the intended one")
	default:
		fmt.Fprintf(w, "  Active generation:   %s (declared in %s)\n", declared, registryPathOrNone(r.RegistryPath))
		if err := verifyActiveGeneration(r.Domain, declared, r.LiveDigest); err != nil {
			fmt.Fprintf(w, "  Generation verdict:  %s\n", strings.TrimSpace(firstLine(err.Error())))
			disagreements = append(disagreements, "the served graph is not the declared ACTIVE generation")
		} else {
			fmt.Fprintf(w, "  Generation verdict:  the served graph IS the declared ACTIVE generation\n")
		}
	}

	// Stated positively when there is nothing wrong. "No complaints printed" and
	// "checked, and everything agrees" are different claims, and only one of them is
	// evidence that a check ran at all -- the rule markerAgreement already follows.
	if len(disagreements) == 0 {
		fmt.Fprintf(w, "  Disagreements:       none (endpoint, marker and ACTIVE generation all agree)\n")
		return
	}
	fmt.Fprintf(w, "  Disagreements:       %d\n", len(disagreements))
	for _, d := range disagreements {
		fmt.Fprintf(w, "                       - %s\n", d)
	}
}

func registryPathOrNone(path string) string {
	if strings.TrimSpace(path) == "" {
		return "(no registry path)"
	}
	return path
}

// generationAuthority is the verdict renderEndpointBlock has always stated in prose, as a value a
// consumer can act on.
//
// The three states are not two. "No ACTIVE generation is declared" is NOT agreement: it is the
// absence of anything to agree with, and verifyServed returns nil for it because its callers REPORT
// the verdict rather than consume on it. A command that consumes an authoritative payload cannot use
// that reading -- accepting an unverifiable graph is exactly the fail-open this distinction exists to
// prevent -- so the states are kept apart and named.
type generationAuthority int

const (
	generationAuthorityUnset generationAuthority = iota
	// generationNotEstablished: the registry declares no ACTIVE generation for this domain, so
	// nothing proves the served graph is the intended one. Never report this as verified.
	generationNotEstablished
	// generationMismatch: both sides are present and they disagree. Two claimants for one domain
	// is an error, not a choice for the caller (law 12).
	generationMismatch
	// generationVerified: the served graph IS the declared ACTIVE generation.
	generationVerified
	// generationDomainUnresolved: NO governed domain was resolved, so "the ACTIVE generation for
	// this domain" is not a question that can be asked.
	//
	// This is the ONE state that does not refuse, and it is typed rather than implicit so that it
	// can be counted and audited instead of reading as a silent bypass.
	//
	// The precedent is the server's own, in graphAuthorityFor: with no publication_domain requested
	// it returns PUBLICATION_RESOLUTION_UNSPECIFIED and explicitly declines a home-domain fallback,
	// because "answering an unasked question with the server's favourite domain produces a
	// well-formed receipt for something the caller did not ask about". An ungoverned project has no
	// ACTIVE pointer to disagree with; refusing there would make the cold-start stranger path
	// unusable, and it would be refusing over the absence of a question rather than the absence of
	// an answer.
	//
	// What it must NEVER do is report verification. VERIFIED is a claim about a governed domain, and
	// this state has no domain to make it about.
	generationDomainUnresolved
)

func (g generationAuthority) String() string {
	switch g {
	case generationNotEstablished:
		return "NOT_ESTABLISHED"
	case generationMismatch:
		return "MISMATCH"
	case generationVerified:
		return "VERIFIED"
	case generationDomainUnresolved:
		return "DOMAIN_UNRESOLVED"
	}
	return "UNSET"
}

// undeclaredActiveGenerationError names the NOT_ESTABLISHED refusal, kept distinct from
// activeGenerationMismatchError so an operator can tell "nobody said which generation is right" from
// "two sources disagree about it". They have different remedies and must not share a message.
type undeclaredActiveGenerationError struct {
	Domain string
	Served string
}

func (e *undeclaredActiveGenerationError) Error() string {
	domain := strings.TrimSpace(e.Domain)
	if domain == "" {
		domain = "(no governed domain resolved)"
	}
	return fmt.Sprintf("graph identity is not established for domain %s: no ACTIVE generation is declared, "+
		"so nothing proves the served graph is the intended one.\n"+
		"  served graph says: %s\n"+
		"  this is not agreement, it is the absence of anything to agree with, and this command consumes\n"+
		"  the graph's answer as authoritative rather than merely reporting it\n"+
		"  declare the ACTIVE generation for this domain by publishing through the transactional path",
		domain, orNone(strings.TrimSpace(e.Served)))
}

// classifyServedGeneration decides which of the three states holds, and returns the refusal that
// states it. The error is non-nil for every state except VERIFIED, so a caller cannot reach a
// consuming path by ignoring the state and testing only err == nil, or vice versa.
func (r graphReader) classifyServedGeneration(served string) (generationAuthority, error) {
	// No governed domain: the question is unasked, not unanswered. See generationDomainUnresolved.
	if strings.TrimSpace(r.Domain) == "" {
		return generationDomainUnresolved, nil
	}
	if strings.TrimSpace(r.DeclaredGeneration) == "" {
		return generationNotEstablished, &undeclaredActiveGenerationError{Domain: r.Domain, Served: served}
	}
	if err := verifyActiveGeneration(r.Domain, r.DeclaredGeneration, served); err != nil {
		return generationMismatch, err
	}
	return generationVerified, nil
}

// requireVerifiedServedGeneration is the guard for a command that CONSUMES an authoritative graph.
//
// It refuses unless the served generation IS the declared ACTIVE one, and it must be called BEFORE
// the payload is used -- a check after consumption reports on a decision already taken.
//
// Distinct from verifyServed, which returns nil when nothing is declared. That reading is right for
// runMetadata and runBriefing, which report the verdict to an operator; it is wrong for a command
// whose output is acted on as governed truth.
// It returns the STATE as well as the refusal, because a caller that must render the refusal in a
// machine-readable channel needs to name which state it was -- and because a second entry point for
// "classify, then decide" would be a second way to consume authority that the census could not see.
// One guard, one name.
func (r graphReader) requireVerifiedServedGeneration(served string) (generationAuthority, error) {
	return r.classifyServedGeneration(served)
}

// claimsVerifiedGeneration reports whether this reader may state that the served graph was PROVEN to
// be the domain's ACTIVE generation. Only VERIFIED may.
//
// Separate from requireVerifiedServedGeneration because "may proceed" and "may claim verification"
// are different questions, and DOMAIN_UNRESOLVED answers them differently: it proceeds and it does
// not claim. Collapsing the two is how a bypass becomes indistinguishable from a proof.
func (r graphReader) claimsVerifiedGeneration(served string) bool {
	state, _ := r.classifyServedGeneration(served)
	return state == generationVerified
}

// generationRefusal is the MACHINE-READABLE form of a served-generation refusal.
//
// A refusal that only reaches stderr is invisible to a --json consumer, which reads stdout and gets
// an empty buffer: scripts/lib/preflight-verdict.sh then classifies it "malformed:parse_error", and a
// real authority refusal is reported as a broken response. That is the same dropped-representation
// shape as a helper returning a payload without its authority stamp -- the refusal exists, and not in
// the channel its consumer reads.
//
// Deliberately NOT shaped like a PreflightResponse. It carries no status, risk_class or coverage, so
// no consumer can mistake it for an actionable answer; preflight_verdict's existing "missing_status"
// branch would reject it even without the explicit refusal branch. The envelope reuses that script's
// established verdict vocabulary rather than adding a parallel protocol.
type generationRefusal struct {
	Refusal generationRefusalBody `json:"refusal"`
}

type generationRefusalBody struct {
	// Kind distinguishes the two refusing states. An operator's remedy differs: declare a
	// generation, versus reconcile two that disagree.
	Kind string `json:"kind"`
	// Surface names the command, so a refusal found in a file says what produced it.
	Surface            string `json:"surface"`
	Domain             string `json:"domain"`
	DeclaredGeneration string `json:"declared_generation,omitempty"`
	ServedGeneration   string `json:"served_generation,omitempty"`
	Detail             string `json:"detail"`
}

// generationRefusalKinds is the closed set, read by membership by the shell classifier.
const (
	generationRefusalNotEstablished = "served_generation_not_established"
	generationRefusalMismatch       = "served_generation_mismatch"
)

// refusalKind maps a state to its wire kind. Only the two refusing states have one: VERIFIED and
// DOMAIN_UNRESOLVED do not refuse, so asking for their kind is a caller error and returns "".
func (g generationAuthority) refusalKind() string {
	switch g {
	case generationNotEstablished:
		return generationRefusalNotEstablished
	case generationMismatch:
		return generationRefusalMismatch
	}
	return ""
}

// emitGenerationRefusalJSON writes the typed refusal to STDOUT and returns a nonzero exit code.
//
// stdout, because that is where a --json consumer looks. Nonzero, because the command did not answer
// the question it was asked. Both, because either alone is a half-signal: an exit code with no
// payload is unparseable, and a payload with exit 0 reads as success.
func emitGenerationRefusalJSON(surface string, state generationAuthority, r graphReader, served string, cause error) int {
	kind := state.refusalKind()
	if kind == "" {
		// A non-refusing state must never reach here; say so rather than emit a refusal nobody can
		// act on.
		fmt.Fprintf(os.Stderr, "%s: internal: asked to emit a refusal for state %v\n", surface, state)
		return 1
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	b, err := json.MarshalIndent(generationRefusal{Refusal: generationRefusalBody{
		Kind:               kind,
		Surface:            surface,
		Domain:             r.Domain,
		DeclaredGeneration: strings.TrimSpace(r.DeclaredGeneration),
		ServedGeneration:   strings.TrimSpace(served),
		Detail:             detail,
	}}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: encode refusal json: %v\n", surface, err)
		return 1
	}
	fmt.Println(string(b))
	return 1
}
