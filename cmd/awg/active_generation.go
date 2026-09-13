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
	// ONE marker resolver, and it reports which of the four tiers answered (G4).
	markerPath, markerSource, markerErr := resolveGraphMarkerFile("", r.Root, false)
	switch {
	case markerErr != nil:
		fmt.Fprintf(w, "  Marker file:         cannot be resolved: %s\n", firstLine(markerErr.Error()))
		if r.RootError != nil {
			fmt.Fprintf(w, "                       (no project root: %v)\n", r.RootError)
		}
		fmt.Fprintf(w, "  Marker verdict:      cannot be verified: no marker file could be named\n")
	default:
		fmt.Fprintf(w, "  Marker file:         %s\n", markerPath)
		fmt.Fprintf(w, "  Chosen from:         %s\n", markerSource)
		if marker, err := seedmeta.ReadMarkerFile(markerPath); err == nil {
			fmt.Fprintf(w, "  Marker verdict:      %s\n",
				markerAgreement(r.LiveDigest, r.LiveTriples, marker.Digest, int(marker.TripleCount)))
		} else {
			fmt.Fprintf(w, "  Marker verdict:      cannot be verified: the marker is not readable (%v)\n", err)
		}
	}
	declared := declaredActiveGeneration(r.RegistryPath, r.Domain)
	if declared == "" {
		fmt.Fprintf(w, "  Active generation:   NOT DECLARED for %s in %s\n", r.Domain, registryPathOrNone(r.RegistryPath))
		fmt.Fprintf(w, "  Generation verdict:  cannot be verified: no ACTIVE generation is declared, so nothing here proves the served graph is the intended one\n")
		return
	}
	fmt.Fprintf(w, "  Active generation:   %s (declared in %s)\n", declared, registryPathOrNone(r.RegistryPath))
	if err := verifyActiveGeneration(r.Domain, declared, r.LiveDigest); err != nil {
		fmt.Fprintf(w, "  Generation verdict:  %s\n", strings.TrimSpace(firstLine(err.Error())))
		return
	}
	fmt.Fprintf(w, "  Generation verdict:  the served graph IS the declared ACTIVE generation\n")
}

func registryPathOrNone(path string) string {
	if strings.TrimSpace(path) == "" {
		return "(no registry path)"
	}
	return path
}
