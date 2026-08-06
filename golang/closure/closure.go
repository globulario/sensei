// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Domain closure: proving that a published domain slice both CONTAINS what its
// certified source declares and contains NOTHING ELSE.
//
// Projection coverage alone is not enough. On 2026-08-05 a `sensei build --repo
// globular` was run with the working directory set to the sensei repo instead
// of the services repo. `-input` defaults to docs/awareness relative to cwd and
// `--repo` replaces that domain's whole slice, so sensei's own corpus was
// published as domain `globular` and the services slice was destroyed. The
// receipt then reported:
//
//	certified_services_repo_commit: d7c1a87c
//	authoritative:                  true
//	graph_freshness_state:          CURRENT
//
// while resolve(four_layer.layer_has_single_writing_actor) returned found:false.
// A store certifying a services commit contained zero services identities, and
// nothing in the system could state that contradiction.
//
// "Current" only ever meant "the store matches the latest publication
// transaction". It never meant "the artifact represents the source it claims to
// certify". Those are separate dimensions and this file computes the second.
//
// Two directions, both required:
//
//	MISSING     — an identity the certified source declares that never projected
//	UNEXPECTED  — an identity in the slice whose provenance is NOT in the
//	              certified source snapshot (the wrong-workspace signature)
//
// invariant:    graph.certified_domain_must_match_published_domain_content
// failure_mode: graph.wrong_workspace_publication_certifies_unrepresented_repository

const (
	NS         = "https://globular.io/awareness#"
	rdfTypeIRI = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	authoredIn = NS + "authoredIn"
)

// Subject is one governed identity as it exists in the emitted graph.
type Subject struct {
	IRI        string
	Class      string // e.g. "Invariant"; empty when the subject carries no rdf:type
	AuthoredIn []string
	Count      int // triples with this subject
}

// ClosureCensus is the bidirectional accounting for one domain slice.
type ClosureCensus struct {
	SourceRoot string
	// SourceRoots is every corpus directory the publication read. Provenance is
	// judged against all of them.
	SourceRoots []string

	SourceIdentities  []string // declared by the certified source corpus
	ExpectedToProject []string // source identities of a governed, projecting class
	Projected         []string // expected identities found as typed graph subjects
	Missing           []string // expected but absent — projection failure
	Excluded          []string // declared non-authority; expected NOT to project

	// Unexpected is the contamination direction: a governed subject whose
	// authoredIn provenance lies outside the certified source root. This is the
	// wrong-workspace signature.
	Unexpected []Subject
	// Unproven is a governed subject with no authoredIn at all. It cannot be
	// attributed to the certified source, so it cannot be vouched for.
	Unproven []Subject
	// Duplicate canonical subjects: the same identity typed more than once.
	Duplicates []string
	// ProvenanceNotEmitted are governed identities of a class the emitter does
	// not give provenance to at all. Not contamination — an attributability gap.
	ProvenanceNotEmitted []Subject
}

// Authoritative reports whether this domain slice may be treated as
// architectural authority, and why not when it may not.
//
// Fail-closed by construction: every condition must be affirmatively satisfied.
// A slice is never authoritative merely because a publication succeeded.
func (c *ClosureCensus) Authoritative() (bool, []string) {
	var reasons []string
	if len(c.Missing) > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%d required identity/identities declared by the certified source are ABSENT from the published slice: %s",
			len(c.Missing), sample(c.Missing, 5)))
	}
	if len(c.Unexpected) > 0 {
		var iris []string
		for _, s := range c.Unexpected {
			iris = append(iris, fmt.Sprintf("%s (authoredIn %s)", shortIRI(s.IRI), sample(s.AuthoredIn, 1)))
		}
		reasons = append(reasons, fmt.Sprintf(
			"%d identity/identities in this slice were authored OUTSIDE the certified source root %q — the slice does not represent the repository it certifies: %s",
			len(c.Unexpected), c.SourceRoot, sample(iris, 5)))
	}
	if len(c.Unproven) > 0 {
		var iris []string
		for _, s := range c.Unproven {
			iris = append(iris, shortIRI(s.IRI))
		}
		reasons = append(reasons, fmt.Sprintf(
			"%d identity/identities carry no authoredIn provenance and cannot be attributed to the certified source: %s",
			len(c.Unproven), sample(iris, 5)))
	}
	if len(c.Duplicates) > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%d duplicate canonical subject(s): %s", len(c.Duplicates), sample(c.Duplicates, 5)))
	}
	// A slice that projected nothing at all cannot be authoritative, whatever
	// else reconciles — that is the "scanner never saw the corpus" shape.
	if len(c.ExpectedToProject) > 0 && len(c.Projected) == 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%d identities were expected to project and NONE did — the slice was published from input it never read",
			len(c.ExpectedToProject)))
	}
	return len(reasons) == 0, reasons
}

// ParseSubjects reads N-Triples and returns governed subjects in the awareness
// namespace, keyed by IRI.
func ParseSubjects(r io.Reader) (map[string]*Subject, error) {
	subs := map[string]*Subject{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		subj, pred, obj, ok := splitTriple(line)
		if !ok || !strings.HasPrefix(subj, NS) {
			continue
		}
		s := subs[subj]
		if s == nil {
			s = &Subject{IRI: subj}
			subs[subj] = s
		}
		s.Count++
		switch pred {
		case rdfTypeIRI:
			if strings.HasPrefix(obj, NS) {
				cls := strings.TrimPrefix(obj, NS)
				// Multi-typing is legitimate and must NOT be read as a
				// duplicate: a meta principle is typed both Invariant and
				// MetaPrinciple by design. Keep the first governed class seen;
				// duplicate DECLARATION is detected on the source side instead.
				if s.Class == "" {
					s.Class = cls
				}
			}
		case authoredIn:
			if v := strings.Trim(obj, `"`); v != "" {
				s.AuthoredIn = append(s.AuthoredIn, v)
			}
		}
	}
	return subs, sc.Err()
}

// splitClosureTriple parses `<s> <p> <o> .` / `<s> <p> "literal" .`
func splitTriple(line string) (subj, pred, obj string, ok bool) {
	if !strings.HasPrefix(line, "<") {
		return "", "", "", false
	}
	i := strings.Index(line, "> ")
	if i < 0 {
		return "", "", "", false
	}
	subj = line[1:i]
	rest := line[i+2:]
	if !strings.HasPrefix(rest, "<") {
		return "", "", "", false
	}
	j := strings.Index(rest, "> ")
	if j < 0 {
		return "", "", "", false
	}
	pred = rest[1:j]
	obj = strings.TrimSpace(rest[j+2:])
	obj = strings.TrimSuffix(strings.TrimSpace(obj), ".")
	obj = strings.TrimSpace(obj)
	obj = strings.TrimPrefix(obj, "<")
	obj = strings.TrimSuffix(obj, ">")
	return subj, pred, obj, true
}

// ComputeClosure reconciles declared source identities against the published
// subjects, in both directions.
//
// expected maps identity id -> the IRI it must project to. sourceRoot is the
// certified source snapshot's absolute root; any governed subject authored
// outside it is contamination.
func ComputeClosure(sourceRoot string, expected map[string]string, excluded []string, subs map[string]*Subject) ClosureCensus {
	return ComputeClosureRoots([]string{sourceRoot}, expected, excluded, subs)
}

// ComputeClosureRoots is the multi-root form.
//
// A publication legitimately reads more than one corpus directory (docs/awareness
// AND docs/intent, for example). Judging provenance against only the first root
// reported 673 false "foreign" identities on the real services corpus — every
// node authored in a sibling input dir. A check that fires on legitimate inputs
// gets switched off, so the certified source snapshot is ALL the roots the build
// actually read, and contamination means "authored outside every one of them".
func ComputeClosureRoots(sourceRoots []string, expected map[string]string, excluded []string, subs map[string]*Subject) ClosureCensus {
	roots := make([]string, 0, len(sourceRoots))
	for _, r := range sourceRoots {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, filepath.Clean(r))
		}
	}
	primary := ""
	if len(roots) > 0 {
		primary = roots[0]
	}
	c := ClosureCensus{SourceRoot: primary, SourceRoots: roots, Excluded: excluded}
	for id := range expected {
		c.SourceIdentities = append(c.SourceIdentities, id)
		c.ExpectedToProject = append(c.ExpectedToProject, id)
	}
	sort.Strings(c.SourceIdentities)
	sort.Strings(c.ExpectedToProject)

	for _, id := range c.ExpectedToProject {
		iri := expected[id]
		s := subs[iri]
		// Presence of the id string is NOT enough: the subject must carry a
		// governed rdf:type. An untyped subject is a dangling reference created
		// by some other node's relation, not a projected identity.
		if s == nil || s.Class == "" {
			c.Missing = append(c.Missing, id)
			continue
		}
		c.Projected = append(c.Projected, id)
	}

	// Contamination direction. Only subjects that are themselves typed count:
	// an untyped subject is a reference, and references legitimately point at
	// shared ontology and at other domains.
	for iri, s := range subs {
		// Only classes this gate verifies. Generated code symbols and other
		// build-derived nodes carry provenance through different predicates, so
		// judging them here would report thousands of false positives — and a
		// check that fires on everything is as useless as one that never fires.
		if !verifiedClasses[s.Class] {
			continue
		}
		if !provenanceEmittingClasses[s.Class] {
			c.ProvenanceNotEmitted = append(c.ProvenanceNotEmitted, *s)
			continue
		}
		if len(s.AuthoredIn) == 0 {
			c.Unproven = append(c.Unproven, *s)
			continue
		}
		foreign := true
		for _, p := range s.AuthoredIn {
			for _, root := range roots {
				if WithinRoot(p, root) {
					foreign = false
					break
				}
			}
			if !foreign {
				break
			}
		}
		if foreign {
			c.Unexpected = append(c.Unexpected, *s)
		}
		_ = iri
	}
	sortSubjects(c.Unexpected)
	sortSubjects(c.Unproven)
	sortSubjects(c.ProvenanceNotEmitted)
	sort.Strings(c.Duplicates)
	return c
}

// verifiedClasses are the authored governed classes whose provenance this gate
// can prove. Deliberately narrow: each one is authored in a corpus YAML file and
// carries an authoredIn literal.
var verifiedClasses = map[string]bool{
	"Invariant":       true,
	"FailureMode":     true,
	"ForbiddenFix":    true,
	"IncidentPattern": true,
}

// provenanceEmittingClasses are the subset whose nodes actually carry an
// authoredIn literal today.
//
// ForbiddenFix is deliberately absent, and that is a FINDING, not a
// convenience: the emitter writes a forbidden fix as a single rdf:type triple
// with no label and no provenance, so 475 governed identities in the services
// corpus cannot be attributed to any source file. They are counted and named
// below as ProvenanceNotEmitted rather than either failing closure (they are
// not contamination) or vanishing (they are a real gap in attributability).
var provenanceEmittingClasses = map[string]bool{
	"Invariant":       true,
	"FailureMode":     true,
	"IncidentPattern": true,
}

func WithinRoot(path, root string) bool {
	if root == "" {
		return true
	}
	p := filepath.Clean(path)
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

func sortSubjects(s []Subject) {
	sort.Slice(s, func(i, j int) bool { return s[i].IRI < s[j].IRI })
}

func shortIRI(iri string) string { return strings.TrimPrefix(iri, NS) }

func sample(v []string, n int) string {
	if len(v) == 0 {
		return "(none)"
	}
	if len(v) <= n {
		return strings.Join(v, ", ")
	}
	return strings.Join(v[:n], ", ") + fmt.Sprintf(", … (+%d more)", len(v)-n)
}

// FormatClosure renders the census. Printed on success as well as failure: a
// gate that only speaks when it fails cannot be told apart from one that never
// ran.
func Format(c *ClosureCensus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    certified source root      %s\n", c.SourceRoot)
	fmt.Fprintf(&b, "    source identities          %d\n", len(c.SourceIdentities))
	fmt.Fprintf(&b, "    expected to project        %d\n", len(c.ExpectedToProject))
	fmt.Fprintf(&b, "    identities projected       %d\n", len(c.Projected))
	fmt.Fprintf(&b, "    identities missing         %d\n", len(c.Missing))
	fmt.Fprintf(&b, "    explicitly excluded        %d\n", len(c.Excluded))
	fmt.Fprintf(&b, "    unexpected (foreign prov)  %d\n", len(c.Unexpected))
	fmt.Fprintf(&b, "    unproven (no provenance)   %d\n", len(c.Unproven))
	fmt.Fprintf(&b, "    class emits no provenance  %d (attributability gap, not contamination)\n", len(c.ProvenanceNotEmitted))
	fmt.Fprintf(&b, "    duplicate canonical subj   %d\n", len(c.Duplicates))
	return b.String()
}

// writeClosure is a small helper so the CLI and tests share output shape.
func WriteClosure(w io.Writer, c *ClosureCensus) bool {
	fmt.Fprint(w, Format(c))
	ok, reasons := c.Authoritative()
	if ok {
		fmt.Fprintln(w, "  ✓ domain closure proven — slice represents its certified source, and nothing else")
		return true
	}
	fmt.Fprintln(os.Stderr, "  ✗ DOMAIN CLOSURE FAILED — slice must NOT be treated as authoritative:")
	for _, r := range reasons {
		fmt.Fprintf(os.Stderr, "    - %s\n", r)
	}
	return false
}
