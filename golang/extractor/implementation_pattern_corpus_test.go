// SPDX-License-Identifier: Apache-2.0

package extractor_test

// Corpus tests for the authored ImplementationPattern files that live in the
// sibling services repo (docs/awareness/implementation_patterns/*.yaml). These
// assert two things the unit tests with inline YAML cannot:
//
//  1. the real authored files route to the implementation_pattern importer and
//     each becomes a typed aw:ImplementationPattern node, and
//  2. every active pattern carries the minimum useful shape — at least one
//     activation trigger, one must_follow step, and two reference files — so a
//     pattern can never silently enter the graph as an empty stub.
//
// The corpus lives in a different repo, so the tests skip (not fail) when the
// services checkout is not resolvable. Set SERVICES_REPO to override.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// expectedImplPatterns are the pattern ids Phase 1 authored. grpc_client_standard
// predates Phase 1 and is validated by its own inline tests, so it is not
// required here — but if present in the corpus it must still pass the smoke check.
var expectedImplPatterns = []string{
	"globular.pattern.doctor_rule_diagnostic_only",
	"globular.pattern.remediation_via_workflow",
	"globular.pattern.workflow_durable_step_receipt",
	"globular.pattern.repository_metadata_authority",
	"globular.pattern.rbac_explicit_deny_precedence",
}

// resolveImplPatternDir locates the authored implementation_patterns directory
// in the sibling services repo. Returns "" when it cannot be found.
func resolveImplPatternDir() string {
	var cands []string
	if r := os.Getenv("SERVICES_REPO"); r != "" {
		cands = append(cands, filepath.Join(r, "docs", "awareness", "implementation_patterns"))
	}
	cands = append(cands,
		filepath.Join("..", "..", "..", "services", "docs", "awareness", "implementation_patterns"),
	)
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

func TestImplementationPatternCorpus_AuthoredPatternsImportAsTypedNodes(t *testing.T) {
	dir := resolveImplPatternDir()
	if dir == "" {
		t.Skip("services implementation_patterns dir not resolvable; set SERVICES_REPO to run")
	}

	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir(%s): %v", dir, err)
	}
	out := buf.String()

	// Every authored file must route to the implementation_pattern schema —
	// not to the intent importer (id+level) or unknown.
	for _, f := range report.Imported() {
		if f.Schema != "implementation_pattern" {
			t.Errorf("%s routed to schema %q, want implementation_pattern", f.Path, f.Schema)
		}
	}

	// Each Phase-1 pattern must appear as a typed ImplementationPattern node.
	for _, id := range expectedImplPatterns {
		subj := rdf.MintIRI(rdf.ClassImplementationPattern, id)
		typed := subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassImplementationPattern) + " ."
		if !strings.Contains(out, typed) {
			t.Errorf("pattern %q is not a typed ImplementationPattern node\nexpected triple: %s", id, typed)
		}
	}
}

// TestImplementationPatternCorpus_ActivePatternsHaveRequiredShape is the smoke
// test: each active authored pattern must carry >=1 activation trigger, >=1
// must_follow step, and >=2 reference files. A pattern that matches a task but
// has no recipe (must_follow) or no example (reference_files) is noise.
func TestImplementationPatternCorpus_ActivePatternsHaveRequiredShape(t *testing.T) {
	dir := resolveImplPatternDir()
	if dir == "" {
		t.Skip("services implementation_patterns dir not resolvable; set SERVICES_REPO to run")
	}

	var buf bytes.Buffer
	if _, _, err := extractor.ImportAwarenessDir(dir, &buf); err != nil {
		t.Fatalf("ImportAwarenessDir(%s): %v", dir, err)
	}

	facts := parseNTFacts(buf.String())

	type counts struct {
		status     string
		triggers   int
		mustFollow int
		refFiles   int
	}
	byNode := map[string]*counts{}
	get := func(iri string) *counts {
		c, ok := byNode[iri]
		if !ok {
			c = &counts{}
			byNode[iri] = c
		}
		return c
	}
	for _, f := range facts {
		switch f.pred {
		case rdf.PropStatus:
			get(f.subj).status = f.obj
		case rdf.PropActivationTrigger:
			get(f.subj).triggers++
		case rdf.PropMustFollow:
			get(f.subj).mustFollow++
		case rdf.PropReferenceFile:
			get(f.subj).refFiles++
		case rdf.PropType:
			if f.obj == rdf.ClassImplementationPattern {
				get(f.subj) // ensure the node exists even with no other props
			}
		}
	}

	if len(byNode) == 0 {
		t.Fatal("no ImplementationPattern nodes parsed from corpus")
	}

	for iri, c := range byNode {
		// Only active patterns are surfaced by the matcher, so only they must
		// satisfy the shape contract. draft/deprecated may be incomplete.
		if c.status != "" && c.status != "active" {
			continue
		}
		if c.triggers < 1 {
			t.Errorf("%s: active pattern has 0 activation triggers (need >=1)", iri)
		}
		if c.mustFollow < 1 {
			t.Errorf("%s: active pattern has 0 must_follow steps (need >=1)", iri)
		}
		if c.refFiles < 2 {
			t.Errorf("%s: active pattern has %d reference files (need >=2)", iri, c.refFiles)
		}
	}
}

// ── minimal N-Triples parser (corpus tests only) ───────────────────────────────

type ntFact struct {
	subj string // includes angle brackets, e.g. "<...>"
	pred string // bare predicate IRI (no brackets) — matches rdf.Prop* constants
	obj  string // literal value (unescaped) or IRI without brackets
}

// parseNTFacts is a deliberately small N-Triples reader good enough for the
// awareness emitter's output: one triple per line, "<s> <p> o .". Object is
// either a quoted literal (last quote on the line is the closer, per rdf.Lit)
// or an IRI. Datatype/lang suffixes are not produced by rdf.Lit and are not
// handled.
func parseNTFacts(nt string) []ntFact {
	var out []ntFact
	for _, line := range strings.Split(nt, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<") {
			continue
		}
		sEnd := strings.IndexByte(line, '>')
		if sEnd < 0 {
			continue
		}
		subj := line[:sEnd+1]
		rest := strings.TrimSpace(line[sEnd+1:])
		if !strings.HasPrefix(rest, "<") {
			continue
		}
		pEnd := strings.IndexByte(rest, '>')
		if pEnd < 0 {
			continue
		}
		pred := rest[1:pEnd]
		obj := strings.TrimSpace(rest[pEnd+1:])
		obj = strings.TrimSuffix(strings.TrimSpace(obj), ".")
		obj = strings.TrimSpace(obj)

		var val string
		switch {
		case strings.HasPrefix(obj, `"`):
			last := strings.LastIndexByte(obj, '"')
			if last <= 0 {
				continue
			}
			val = unescapeNTLiteral(obj[1:last])
		case strings.HasPrefix(obj, "<") && strings.HasSuffix(obj, ">"):
			val = obj[1 : len(obj)-1]
		default:
			val = obj
		}
		out = append(out, ntFact{subj: subj, pred: pred, obj: val})
	}
	return out
}

func unescapeNTLiteral(s string) string {
	r := strings.NewReplacer(
		`\\`, `\`,
		`\"`, `"`,
		`\n`, "\n",
		`\t`, "\t",
		`\r`, "\r",
	)
	return r.Replace(s)
}
