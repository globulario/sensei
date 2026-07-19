// SPDX-License-Identifier: Apache-2.0

package factextract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAuthorityCandidates_FindsGuardedMutationAndLifecycle(t *testing.T) {
	root := t.TempDir()
	src := `package demo

import "net/http"

type Globule struct{}

func (g *Globule) registerRoutes() {
	http.HandleFunc("/save_config", g.saveConfigHandler)
}

func (g *Globule) saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !security.ValidateToken("x") {
		return
	}
	os.WriteFile("config.json", []byte("x"), 0644)
	g.setConfig("domain", "example.org")
}

func (g *Globule) startServices() {
	service.Start()
	process.Signal(sigterm)
}
`
	if err := os.WriteFile(filepath.Join(root, "server.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := extractAuthorityCandidates(root)
	if err != nil {
		t.Fatalf("extractAuthorityCandidates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate count=%d, want 2", len(got))
	}

	var guarded, lifecycle *authoritySurfaceCandidate
	for i := range got {
		switch got[i].Kind {
		case "guarded_mutation_handler":
			guarded = &got[i]
		case "lifecycle_control":
			lifecycle = &got[i]
		}
	}
	if guarded == nil {
		t.Fatalf("missing guarded mutation handler candidate: %+v", got)
	}
	if lifecycle == nil {
		t.Fatalf("missing lifecycle candidate: %+v", got)
	}
	if !containsStringAuthority(guarded.RequiredGuards, "ValidateToken") {
		t.Fatalf("guarded.RequiredGuards=%v, want ValidateToken", guarded.RequiredGuards)
	}
	if !containsStringAuthority(guarded.MutatesState, "config_state") {
		t.Fatalf("guarded.MutatesState=%v, want config_state", guarded.MutatesState)
	}
	if !containsStringAuthority(guarded.Routes, "/save_config") {
		t.Fatalf("guarded.Routes=%v, want /save_config", guarded.Routes)
	}
	if !containsStringAuthority(lifecycle.ControlsLifecycle, "signal") || !containsStringAuthority(lifecycle.ControlsLifecycle, "start") {
		t.Fatalf("lifecycle.ControlsLifecycle=%v, want signal+start", lifecycle.ControlsLifecycle)
	}
}

func TestRenderAuthorityCandidates_UsesCandidateHeader(t *testing.T) {
	out, err := renderAuthorityCandidates("/repo", []authoritySurfaceCandidate{{
		ID:          "candidate.authority.demo.save_config",
		Class:       "AuthoritySurface",
		Status:      "candidate",
		Confidence:  "candidate",
		Kind:        "guarded_mutation_handler",
		SourceFiles: []string{"server.go"},
		Symbols:     []string{"saveConfig"},
	}})
	if err != nil {
		t.Fatalf("renderAuthorityCandidates: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "status:candidate only") {
		t.Fatalf("missing candidate header:\n%s", body)
	}
	if !strings.Contains(body, "authority_surface_candidates:") {
		t.Fatalf("missing top-level key:\n%s", body)
	}
}

func TestAuthorityCandidateOutputRemainsEquivalent(t *testing.T) {
	root := t.TempDir()
	src := `package demo
import "net/http"
func registerRoutes() { http.HandleFunc("/save_config", saveConfigHandler) }
func saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !security.ValidateToken("x") { return }
	setConfig("domain", "example.org")
}
`
	if err := os.WriteFile(filepath.Join(root, "server.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := extractAuthorityCandidates(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("candidate count=%d, want 1: %#v", len(got), got)
	}
	c := got[0]
	if c.ID != "candidate.authority.server.saveconfighandler" ||
		c.Status != "candidate" ||
		c.Kind != "guarded_mutation_handler" ||
		c.Confidence != "proven" ||
		c.ConfidenceScore != 93 {
		t.Fatalf("authority candidate drifted: %#v", c)
	}
}

func TestAuthorityObservationEmitsMutatesStateFact(t *testing.T) {
	report := authorityFactReport(t)
	if !hasAuthorityFact(report, "mutates_state", "config_state") {
		t.Fatalf("missing mutates_state fact: %#v", report.Facts)
	}
}

func TestAuthorityObservationEmitsRouteFact(t *testing.T) {
	report := authorityFactReport(t)
	if !hasAuthorityFact(report, "exposes_route", "/save_config") {
		t.Fatalf("missing exposes_route fact: %#v", report.Facts)
	}
}

func TestAuthorityObservationDoesNotInferOwner(t *testing.T) {
	report := authorityFactReport(t)
	for _, f := range report.Facts {
		if f.Predicate == "owns_state" || f.Predicate == "is_authoritative_for" || f.Predicate == "is_the_only_writer" {
			t.Fatalf("authority observation inferred owner claim: %#v", f)
		}
	}
}

func TestBareSetterDoesNotBecomeAuthorityClaim(t *testing.T) {
	root := t.TempDir()
	src := `package demo
type Config struct{ Value string }
func Set(c *Config) { c.Value = "x" }
`
	if err := os.WriteFile(filepath.Join(root, "server.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	report := buildInvariantReportForTest(t, root)
	for _, c := range report.Candidates {
		if c.Kind == "authority" {
			t.Fatalf("bare setter became authority candidate: %#v", c)
		}
	}
}

func authorityFactReport(t *testing.T) invariantExtractionReport {
	t.Helper()
	root := t.TempDir()
	src := `package demo
import "net/http"
func registerRoutes() { http.HandleFunc("/save_config", saveConfigHandler) }
func saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !security.ValidateToken("x") { return }
	setConfig("domain", "example.org")
}
`
	if err := os.WriteFile(filepath.Join(root, "server.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return buildInvariantReportForTest(t, root)
}

func hasAuthorityFact(report invariantExtractionReport, predicate, object string) bool {
	for _, f := range report.Facts {
		if f.Kind == "authority_observation" && f.Predicate == predicate && f.Object == object {
			return true
		}
	}
	return false
}

func containsStringAuthority(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
