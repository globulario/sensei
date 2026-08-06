// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"
	"testing"
)

// Domain closure is bidirectional: a slice must contain everything its
// certified source declares, AND nothing authored outside it.
//
// The incident, 2026-08-05: `sensei build --repo globular` ran with cwd set to
// the sensei repo. `-input` defaults to docs/awareness relative to cwd, and
// `--repo` replaces that domain's entire slice, so sensei's corpus was
// published as domain `globular`. The receipt then reported
// certified_services_repo_commit=d7c1a87c with authoritative=true and
// freshness=CURRENT, while resolve(four_layer.layer_has_single_writing_actor)
// returned found:false.
//
// Coverage alone would not have caught it: the wrong corpus projects perfectly.
// Only the second direction — provenance outside the certified source root —
// states the contradiction.

const svcRoot = "/repo/services/docs/awareness"

func typed(iri, class string, authoredIn ...string) *Subject {
	return &Subject{IRI: awarenessNS + iri, Class: class, AuthoredIn: authoredIn}
}

func subjects(list ...*Subject) map[string]*Subject {
	m := map[string]*Subject{}
	for _, s := range list {
		m[s.IRI] = s
	}
	return m
}

func inv(id string) (string, string) { return id, awarenessNS + "invariant/" + id }

// TestClosureProvenWhenSliceMatchesItsCertifiedSource is the positive control.
// It covers a critical long-standing invariant and a newly proposed one, so the
// gate is exercised on both the old corpus and fresh additions.
func TestClosureProvenWhenSliceMatchesItsCertifiedSource(t *testing.T) {
	critical, criticalIRI := inv("four_layer.layer_has_single_writing_actor")
	fresh, freshIRI := inv("release.affected_package_suites_must_pass_before_publish")

	c := ComputeClosure(svcRoot,
		map[string]string{critical: criticalIRI, fresh: freshIRI},
		[]string{"relation_targets.runtime_roots"},
		subjects(
			typed("invariant/"+critical, "Invariant", svcRoot+"/invariants.yaml"),
			typed("invariant/"+fresh, "Invariant", svcRoot+"/invariants.yaml"),
		))

	ok, reasons := c.Authoritative()
	if !ok {
		t.Fatalf("a slice that exactly matches its certified source must prove closure; got %v", reasons)
	}
	if len(c.Projected) != 2 || len(c.Missing) != 0 {
		t.Errorf("projected=%v missing=%v", c.Projected, c.Missing)
	}
}

// TestWrongWorkspacePublicationFailsClosure replays the incident: the slice is
// internally perfect, but it is the wrong repository's corpus.
//
// This is the case where every other signal said "current" and "authoritative".
func TestWrongWorkspacePublicationFailsClosure(t *testing.T) {
	const senseiRoot = "/repo/sensei/docs/awareness"
	critical, criticalIRI := inv("four_layer.layer_has_single_writing_actor")

	c := ComputeClosure(svcRoot,
		map[string]string{critical: criticalIRI}, nil,
		subjects(
			// sensei's own corpus, published under the services domain
			typed("invariant/awareness.briefing.deterministic_compact_context", "Invariant", senseiRoot+"/invariants.yaml"),
			typed("invariant/awareness.annotation_grammar_is_language_neutral", "Invariant", senseiRoot+"/invariants.yaml"),
		))

	ok, reasons := c.Authoritative()
	if ok {
		t.Fatal("a slice built from the WRONG repository proved closure — this is the " +
			"2026-08-05 incident, where the store certified services commit d7c1a87c " +
			"while containing zero services identities")
	}
	joined := strings.Join(reasons, " ")
	if !strings.Contains(joined, "ABSENT") {
		t.Errorf("must report the missing required identity; got %v", reasons)
	}
	if !strings.Contains(joined, "authored OUTSIDE") {
		t.Errorf("must report foreign provenance — coverage alone cannot catch a wrong-corpus "+
			"publication, because the wrong corpus projects perfectly; got %v", reasons)
	}
	if len(c.Unexpected) != 2 {
		t.Errorf("both foreign identities must be named, got %d", len(c.Unexpected))
	}
}

// TestMissingIdentityFailsClosure is the projection direction on its own: source
// declares it, importer ran, identity never appeared as a typed subject.
func TestMissingIdentityFailsClosure(t *testing.T) {
	id, iri := inv("meta.contract_must_be_explicit_before_resolution")
	c := ComputeClosure(svcRoot, map[string]string{id: iri}, nil, subjects())

	if ok, _ := c.Authoritative(); ok {
		t.Fatal("an identity declared in source and absent from the graph proved closure")
	}
	if len(c.Missing) != 1 || c.Missing[0] != id {
		t.Errorf("missing = %v, want [%s]", c.Missing, id)
	}
}

// TestIDPresentButUntypedIsNotProjected. Checking that the id STRING appears
// somewhere is insufficient: an untyped subject is a dangling reference created
// by another node's relation, not a projected identity. The graph is full of
// them — every `#affects` object mentions an id it does not define.
func TestIDPresentButUntypedIsNotProjected(t *testing.T) {
	id, iri := inv("four_layer.layer_has_single_writing_actor")

	// The subject exists — but only as the object of someone else's relation,
	// so it carries no rdf:type.
	untyped := &Subject{IRI: iri, Count: 3}
	c := ComputeClosure(svcRoot, map[string]string{id: iri}, nil, subjects(untyped))

	if len(c.Projected) != 0 {
		t.Error("an untyped subject must not count as projected — requiring the correct " +
			"subject type is the whole difference between 'the string appears' and " +
			"'the identity exists'")
	}
	if len(c.Missing) != 1 {
		t.Fatalf("missing = %v, want the untyped identity reported missing", c.Missing)
	}
}

// TestExcludedSourcesAreNotRequiredToProject. A declared non-authority source
// (pipeline config such as relation_targets.yaml) must not be demanded of the
// graph, or the gate becomes unsatisfiable and gets switched off.
func TestExcludedSourcesAreNotRequiredToProject(t *testing.T) {
	c := ComputeClosure(svcRoot, map[string]string{}, []string{"runtime_roots", "sibling_repositories"}, subjects())
	if ok, reasons := c.Authoritative(); !ok {
		t.Fatalf("declared exclusions must not fail closure; got %v", reasons)
	}
	if len(c.Excluded) != 2 {
		t.Errorf("exclusions must stay counted and visible, got %d", len(c.Excluded))
	}
}

// TestImporterAcceptsButEmitsNothing is the control the directive names: import
// succeeds, census reconciles, and the graph is empty. Every count except the
// projected one looks healthy.
func TestImporterAcceptsButEmitsNothing(t *testing.T) {
	expected := map[string]string{}
	for i := 0; i < 25; i++ {
		id, iri := inv(fmt.Sprintf("example.invariant_%02d", i))
		expected[id] = iri
	}
	c := ComputeClosure(svcRoot, expected, nil, subjects()) // importer emitted nothing

	ok, reasons := c.Authoritative()
	if ok {
		t.Fatal("a slice that projected NOTHING proved closure")
	}
	if !strings.Contains(strings.Join(reasons, " "), "NONE did") {
		t.Errorf("must state that nothing projected at all, not merely list 25 missing ids; got %v", reasons)
	}
}

// TestUnprovenIdentityFailsClosure: a node of a provenance-emitting class with
// no authoredIn cannot be attributed to the certified source, so it cannot be
// vouched for.
func TestUnprovenIdentityFailsClosure(t *testing.T) {
	c := ComputeClosure(svcRoot, map[string]string{}, nil,
		subjects(typed("invariant/mystery.no_provenance", "Invariant")))

	if ok, _ := c.Authoritative(); ok {
		t.Fatal("an invariant with no provenance proved closure")
	}
	if len(c.Unproven) != 1 {
		t.Errorf("unproven = %d, want 1", len(c.Unproven))
	}
}

// TestClassWithoutProvenanceIsNamedNotFailed. ForbiddenFix nodes are emitted as
// a single rdf:type triple with no label and no authoredIn — 475 of them in the
// services corpus. That is an attributability GAP in the emitter, not evidence
// of contamination, and conflating the two would either fail every honest build
// or hide real contamination behind an exemption.
func TestClassWithoutProvenanceIsNamedNotFailed(t *testing.T) {
	c := ComputeClosure(svcRoot, map[string]string{}, nil,
		subjects(typed("forbiddenFix/accept_raw_path_without_normalization", "ForbiddenFix")))

	if ok, reasons := c.Authoritative(); !ok {
		t.Fatalf("a class the emitter gives no provenance must not fail closure; got %v", reasons)
	}
	if len(c.ProvenanceNotEmitted) != 1 {
		t.Fatal("the attributability gap must still be COUNTED, not silently dropped")
	}
	if !strings.Contains(FormatClosure(&c), "class emits no provenance") {
		t.Error("the census must name the attributability gap so it stays visible")
	}
}

// TestParseSubjectsRequiresTypeAndProvenance pins the N-Triples reading, since
// every judgement above rests on it.
func TestParseSubjectsRequiresTypeAndProvenance(t *testing.T) {
	nt := `<https://globular.io/awareness#invariant/a.b> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Invariant> .
<https://globular.io/awareness#invariant/a.b> <https://globular.io/awareness#authoredIn> "/repo/services/docs/awareness/invariants.yaml" .
<https://globular.io/awareness#invariant/a.b> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#MetaPrinciple> .
<https://globular.io/awareness#failureMode/x.y> <https://globular.io/awareness#affects> <https://globular.io/awareness#invariant/never.defined> .
`
	subs, err := ParseSubjects(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	s := subs[awarenessNS+"invariant/a.b"]
	if s == nil || s.Class != "Invariant" {
		t.Fatalf("subject class = %+v; multi-typing (Invariant + MetaPrinciple) is legitimate "+
			"for meta principles and must not corrupt the class or be read as a duplicate", s)
	}
	if len(s.AuthoredIn) != 1 {
		t.Errorf("authoredIn = %v", s.AuthoredIn)
	}
	// A referenced-but-never-defined subject must exist with NO class, so the
	// closure check reports it missing rather than projected.
	if ref := subs[awarenessNS+"invariant/never.defined"]; ref != nil && ref.Class != "" {
		t.Error("a referenced-only subject must not acquire a class")
	}
}

// TestWithinRootRejectsPrefixCollisions guards the provenance comparison: a
// sibling directory that merely shares a string prefix is NOT inside the root.
func TestWithinRootRejectsPrefixCollisions(t *testing.T) {
	if withinRoot("/repo/services-old/docs/awareness/invariants.yaml", "/repo/services/docs/awareness") {
		t.Error("a path sharing a string prefix with the root must not count as inside it; " +
			"that would let a neighbouring checkout pass as the certified source")
	}
	if !withinRoot("/repo/services/docs/awareness/nested/x.yaml", "/repo/services/docs/awareness") {
		t.Error("a genuinely nested path must count as inside the root")
	}
}
