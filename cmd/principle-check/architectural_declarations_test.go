// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.architectural_declarations
// @awareness file_role=declaration_completeness_gate_for_architectural_principles

// Declaration-completeness gate for ARCHITECTURAL meta-principles.
//
// # WHY THIS EXISTS
//
// Code-shape principles (the 23 gated by ruleguard/regex) are checked against
// ASTs. Architectural principles — graceful_degradation, partition_response,
// bounded_staleness, harvest_vs_yield — live at a higher altitude: they are
// properties of the DESIGN, not of any single line, so an AST scanner cannot
// see them. They are enforced the only way an architectural property can be
// enforced mechanically: by DECLARATION + completeness.
//
// docs/awareness/architectural_declarations.yaml requires every in-scope
// service to NAME its stance (e.g. degradation_mode). This test gates the
// COMPLETENESS of that declaration set: a missing or invalid declaration fails
// CI. A value of `none` is allowed but must carry a reason — a named stance,
// not a silent gap (meta.negative_result_requires_coverage_attestation). It
// does NOT gate CORRECTNESS (is the service actually graceful?) — that is
// review/implementation work; the gate's job is to make the gap visible and
// force the design decision to be recorded.
//
// The test is generic over (principle, attribute), so adding the next
// architectural principle (partition_response_must_be_predeclared, etc.) is a
// YAML edit, not a code change.
//
// SERVICES_REPO selects the services checkout (default ../services), matching
// the principle-check Makefile targets.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type declPrincipleBlock struct {
	Principle       string                   `yaml:"principle"`
	Attribute       string                   `yaml:"attribute"`
	AllowedValues   []string                 `yaml:"allowed_values"`
	InScopeServices []string                 `yaml:"in_scope_services"`
	Declarations    []map[string]interface{} `yaml:"declarations"`
}

type declRegistry struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Principles    []declPrincipleBlock `yaml:"architectural_declarations"`
}

func servicesRepoRoot() string {
	if r := os.Getenv("SERVICES_REPO"); r != "" {
		return r
	}
	// `go test` runs with cwd = the package dir, so the standard sibling
	// checkout is three levels up; fall back to the historical ../services.
	for _, c := range []string{"../../../services", "../services"} {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return "../services"
}

// requireServicesRepo returns the services checkout, or SKIPs the test when it
// is genuinely absent. The cross-repo gates run in CI (where SERVICES_REPO is
// set); skipping locally is honest (SKIP != PASS) and never silently passes a
// real failure — a missing file UNDER an existing root still fails the test.
func requireServicesRepo(t *testing.T) string {
	t.Helper()
	root := servicesRepoRoot()
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		t.Skipf("services repo not found at %q — set SERVICES_REPO to the services checkout "+
			"(this cross-repo gate runs in CI where SERVICES_REPO is set).", root)
	}
	return root
}

// agRepoRoot returns the awareness-graph repo root. `go test ./cmd/principle-check/`
// runs with cwd = the package dir, so "../.." reaches the repo root. The generic
// meta-principle corpus lives at <agRepoRoot>/docs/awareness/generic since the
// 2026-06-13 move (it used to live in the services repo).
func agRepoRoot() string {
	if r := os.Getenv("AG_REPO"); r != "" {
		return r
	}
	return "../.."
}

func TestArchitecturalDeclarationsComplete(t *testing.T) {
	path := filepath.Join(requireServicesRepo(t), "docs", "awareness", "architectural_declarations.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read architectural_declarations.yaml at %s: %v\n"+
			"(set SERVICES_REPO to the services checkout; the architectural-principle "+
			"declaration gate cannot run without the registry)", path, err)
	}
	var reg declRegistry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("parse architectural_declarations.yaml: %v", err)
	}
	if len(reg.Principles) == 0 {
		t.Fatal("architectural_declarations.yaml has no principle blocks")
	}

	for _, pb := range reg.Principles {
		pb := pb
		name := pb.Principle
		if name == "" {
			t.Fatalf("a principle block is missing its `principle:` id")
		}
		t.Run(name, func(t *testing.T) {
			if pb.Attribute == "" {
				t.Fatalf("principle %s: missing `attribute:`", name)
			}
			allowed := map[string]bool{}
			for _, v := range pb.AllowedValues {
				allowed[v] = true
			}
			if len(allowed) == 0 {
				t.Fatalf("principle %s: empty `allowed_values:`", name)
			}

			// service -> declaration map (and its declared attribute value)
			declOf := map[string]map[string]interface{}{}
			for _, d := range pb.Declarations {
				svc, _ := d["service"].(string)
				if svc == "" {
					t.Errorf("principle %s: a declaration entry is missing `service:`", name)
					continue
				}
				if _, dup := declOf[svc]; dup {
					t.Errorf("principle %s: duplicate declaration for service %q", name, svc)
				}
				declOf[svc] = d
			}

			// Completeness + validity for every in-scope service.
			for _, svc := range pb.InScopeServices {
				d, ok := declOf[svc]
				if !ok {
					t.Errorf("principle %s: service %q is in_scope but has NO declaration of %q. "+
						"Declare its stance (or `%s: none` with a reason). A missing declaration is a "+
						"silent architectural gap (meta.negative_result_requires_coverage_attestation).",
						name, svc, pb.Attribute, pb.Attribute)
					continue
				}
				val, _ := d[pb.Attribute].(string)
				if val == "" {
					t.Errorf("principle %s: service %q declaration is missing the `%s:` field", name, svc, pb.Attribute)
					continue
				}
				if !allowed[val] {
					t.Errorf("principle %s: service %q has %s=%q, not one of allowed_values %v",
						name, svc, pb.Attribute, val, pb.AllowedValues)
					continue
				}
				if val == "none" {
					reason, _ := d["reason"].(string)
					if strings.TrimSpace(reason) == "" {
						t.Errorf("principle %s: service %q declared %s=none WITHOUT a reason. "+
							"`none` is an allowed stance but must be named, not silent.", name, svc, pb.Attribute)
					}
				}
			}

			declared := 0
			none := 0
			for _, svc := range pb.InScopeServices {
				if d, ok := declOf[svc]; ok {
					declared++
					if v, _ := d[pb.Attribute].(string); v == "none" {
						none++
					}
				}
			}
			t.Logf("%s: %d/%d in-scope services declared (%d named `none` — tracked gaps, %d with a mode)",
				pb.Attribute, declared, len(pb.InScopeServices), none, declared-none)
		})
	}
}
