// SPDX-License-Identifier: AGPL-3.0-only

package main

// Golden usefulness tests for the Phase-1 ImplementationPattern corpus. These
// load the REAL authored patterns from the sibling services repo (rather than
// hand-copied fact fixtures) and assert that representative edit tasks surface
// the correct recipe through Preflight. Loading the live corpus means the test
// fails if an authored activation trigger drifts away from the task language an
// agent would actually use — which is the whole point of a golden test.
//
// The corpus lives in a different repo; the tests skip when it is not
// resolvable. Set SERVICES_REPO to override the search path.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// goldenPreflightCases pairs an agent task with the pattern id it must surface.
// Each task is phrased the way an agent or operator would actually describe the
// work — the authored when_to_use triggers must contain a phrase the task
// contains for a strong match.
var goldenPreflightCases = []struct {
	name        string
	task        string
	wantPattern string
}{
	{
		name:        "doctor rule task",
		task:        "add a new cluster-doctor rule that detects stale repository findings",
		wantPattern: "implementation_pattern:globular.pattern.doctor_rule_diagnostic_only",
	},
	{
		name:        "repository publish task",
		task:        "change repository publish workflow installability behavior",
		wantPattern: "implementation_pattern:globular.pattern.repository_metadata_authority",
	},
	{
		name:        "workflow resume task",
		task:        "modify workflow resume after a failed step",
		wantPattern: "implementation_pattern:globular.pattern.workflow_durable_step_receipt",
	},
	{
		name:        "rbac access task",
		task:        "change RBAC access validation",
		wantPattern: "implementation_pattern:globular.pattern.rbac_explicit_deny_precedence",
	},
}

func TestPreflightGolden_AuthoredPatternsSurfaceForTasks(t *testing.T) {
	facts := loadCorpusPatternFacts(t)

	for _, tc := range goldenPreflightCases {
		t.Run(tc.name, func(t *testing.T) {
			invalidateImplementationPatternCacheForTest()
			s := newServer(fakeStore{
				classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
					if classIRI == rdf.ClassImplementationPattern {
						return facts, nil
					}
					return nil, nil
				},
			})

			resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
				Task: tc.task,
				Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
			})
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}

			var matched *awarenesspb.MatchedImplementationPattern
			for _, p := range resp.GetImplementationPatterns() {
				if p.GetId() == tc.wantPattern {
					matched = p
					break
				}
			}
			if matched == nil {
				var got []string
				for _, p := range resp.GetImplementationPatterns() {
					got = append(got, p.GetId())
				}
				t.Fatalf("task %q did not surface %s; got %v", tc.task, tc.wantPattern, got)
			}
			if matched.GetMatchStrength() != "strong" {
				t.Errorf("pattern %s matched at %q, want strong", tc.wantPattern, matched.GetMatchStrength())
			}
			// A surfaced recipe must carry its must_follow steps and reference
			// files — that is the value over a bare "it compiles" signal.
			if len(matched.GetMustFollow()) == 0 {
				t.Errorf("pattern %s surfaced with no must_follow steps", tc.wantPattern)
			}
			if len(matched.GetReferenceFiles()) == 0 {
				t.Errorf("pattern %s surfaced with no reference files", tc.wantPattern)
			}
		})
	}
}

// loadCorpusPatternFacts imports the authored implementation_patterns directory
// and converts the emitted N-Triples into store.ImpactFact rows shaped exactly
// like ClassFacts returns them (NodeIRI with brackets, bare predicate IRI,
// literal object). Skips the test when the corpus is not resolvable.
func loadCorpusPatternFacts(t *testing.T) []store.ImpactFact {
	t.Helper()
	dir := resolveServicesImplPatternDir()
	if dir == "" {
		t.Skip("services implementation_patterns dir not resolvable; set SERVICES_REPO to run")
	}
	var buf bytes.Buffer
	if _, _, err := extractor.ImportAwarenessDir(dir, &buf); err != nil {
		t.Fatalf("ImportAwarenessDir(%s): %v", dir, err)
	}
	facts := parseCorpusNTFacts(buf.String(), rdf.ClassImplementationPattern)
	if len(facts) == 0 {
		t.Fatalf("no facts parsed from corpus at %s", dir)
	}
	return facts
}

func resolveServicesImplPatternDir() string {
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

// parseCorpusNTFacts reads the emitter's N-Triples output into ImpactFact rows
// stamped with the given typeIRI (callers feed one corpus class at a time).
// rdf.Lit guarantees the closing quote of a literal is the last quote on the
// line, so the literal body is everything between the first and last quote.
func parseCorpusNTFacts(nt string, typeIRI string) []store.ImpactFact {
	var out []store.ImpactFact
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
		obj := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest[pEnd+1:]), "."))

		var val string
		switch {
		case strings.HasPrefix(obj, `"`):
			last := strings.LastIndexByte(obj, '"')
			if last <= 0 {
				continue
			}
			r := strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t", `\r`, "\r")
			val = r.Replace(obj[1:last])
		case strings.HasPrefix(obj, "<") && strings.HasSuffix(obj, ">"):
			val = obj[1 : len(obj)-1]
		default:
			val = obj
		}
		out = append(out, store.ImpactFact{
			NodeIRI:   subj,
			TypeIRI:   typeIRI,
			Predicate: pred,
			Object:    val,
		})
	}
	return out
}
