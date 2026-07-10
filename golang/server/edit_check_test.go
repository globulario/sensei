// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const caddyRuleID = "caddy.reverseproxy.forwardauth_errf_preserves_location"
const caddyDomain = "github.com/caddyserver/caddy"
const caddyFile = "modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go"

// caddyDetectFacts returns the store facts for the promoted Caddy rule's node,
// including its detect block and provenance — the same shape DetectFacts yields
// from Oxigraph.
func caddyDetectFacts() []store.ImpactFact {
	iri := mintedIRI(rdf.ClassInvariant, caddyRuleID)
	f := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		f(rdf.PropLabel, "Caddyfile directive errors must use dispenser.Errf, not fmt.Errorf"),
		f(rdf.PropDetectForbiddenPattern, `\bfmt\.Errorf\(`),
		f(rdf.PropDetectRequiredPattern, `\bdispenser\.Errf\(`),
		f(rdf.PropDetectAppliesToPath, "modules/caddyhttp/**/caddyfile.go"),
		f(rdf.PropDetectMessage, "Use dispenser.Errf so the Caddyfile error keeps its location."),
		f(rdf.PropDetectEnforcement, "block"),
		f(rdf.PropRepo, caddyDomain),
		f(rdf.PropOrigin, "coldsource"),
		f(rdf.PropReviewLabel, "load-bearing"),
	}
}

// A rule tagged enforcement=block surfaces enforcement="block" on its warning,
// so a gate can partition would-block from warn. EditCheck itself stays
// advisory — severity is still "warning".
func TestEditCheck_Enforcement_BlockSurfacedAdvisory(t *testing.T) {
	s := newEditCheckServer(scopeFacts(map[string]string{caddyRuleID: caddyDomain}))
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: caddyFile, ProposedContent: badContent, Domain: caddyDomain,
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if len(resp.GetWarnings()) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(resp.GetWarnings()))
	}
	w := resp.GetWarnings()[0]
	if w.GetEnforcement() != "block" {
		t.Errorf("enforcement = %q, want block", w.GetEnforcement())
	}
	if w.GetSeverity() != "warning" {
		t.Errorf("severity = %q, want warning (EditCheck stays advisory even for block rules)", w.GetSeverity())
	}
}

// A detect rule with no enforcement defaults to "warn" on the warning.
func TestEditCheck_Enforcement_DefaultsToWarn(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return scopeFacts(map[string]string{caddyRuleID: caddyDomain}), nil
		},
		detectFacts: func(_ context.Context) ([]store.ImpactFact, error) {
			iri := mintedIRI(rdf.ClassInvariant, caddyRuleID)
			mk := func(p, o string) store.ImpactFact {
				return store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: p, Object: o}
			}
			// Same rule, but WITHOUT aw:detectEnforcement.
			return []store.ImpactFact{
				mk(rdf.PropLabel, "rule"),
				mk(rdf.PropDetectForbiddenPattern, `\bfmt\.Errorf\(`),
				mk(rdf.PropRepo, caddyDomain),
			}, nil
		},
	})
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: caddyFile, ProposedContent: badContent, Domain: caddyDomain,
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if len(resp.GetWarnings()) != 1 || resp.GetWarnings()[0].GetEnforcement() != "warn" {
		t.Fatalf("expected one warning with enforcement=warn, got %v", resp.GetWarnings())
	}
}

func newEditCheckServer(impact []store.ImpactFact) *server {
	return newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) { return impact, nil },
		detectFacts:   func(_ context.Context) ([]store.ImpactFact, error) { return caddyDetectFacts(), nil },
	})
}

const badContent = `func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	return fmt.Errorf("cannot re-declare uri: %s", uri)
}`

const goodContent = `func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	return d.Dispenser.Errf("cannot re-declare uri: %s", uri) // dispenser.Errf(
}`

// Bad shape (fmt.Errorf) in the Caddy file, Caddy domain → exactly one warning
// naming the rule, with provenance.
func TestEditCheck_BadShape_WarnsInCaddyDomain(t *testing.T) {
	s := newEditCheckServer(scopeFacts(map[string]string{caddyRuleID: caddyDomain}))
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: caddyFile, ProposedContent: badContent, Domain: caddyDomain,
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if got := len(resp.GetWarnings()); got != 1 {
		t.Fatalf("expected 1 warning, got %d (%v)", got, resp.GetWarnings())
	}
	w := resp.GetWarnings()[0]
	if w.GetRuleId() != caddyRuleID {
		t.Errorf("warning names %q, want %q", w.GetRuleId(), caddyRuleID)
	}
	if w.GetSeverity() != "warning" {
		t.Errorf("severity = %q, want warning (advisory only)", w.GetSeverity())
	}
	if w.GetProvenance() == "" {
		t.Errorf("warning should carry provenance")
	}
}

// Compliant shape (dispenser.Errf, no fmt.Errorf) → rule evaluated, no warning.
func TestEditCheck_CompliantShape_NoWarn(t *testing.T) {
	s := newEditCheckServer(scopeFacts(map[string]string{caddyRuleID: caddyDomain}))
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: caddyFile, ProposedContent: goodContent, Domain: caddyDomain,
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if got := len(resp.GetWarnings()); got != 0 {
		t.Fatalf("compliant content must not warn, got %v", resp.GetWarnings())
	}
	if resp.GetRulesEvaluated() != 1 {
		t.Errorf("rule should have been evaluated; rules_evaluated=%d", resp.GetRulesEvaluated())
	}
}

// The Caddy rule must NOT warn for a Globular file: the rule is not anchored to
// it, so it is not in scope and never evaluated — even with a bad shape.
func TestEditCheck_GlobularFile_NoCaddyWarn(t *testing.T) {
	globularImpact := scopeFacts(map[string]string{"globular.repository.publish_is_scylla_first": ""})
	s := newEditCheckServer(globularImpact)
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: "golang/repository/repository_server/publish.go", ProposedContent: badContent, Domain: "",
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if got := len(resp.GetWarnings()); got != 0 {
		t.Fatalf("Caddy rule LEAKED into a Globular file's edit-check: %v", resp.GetWarnings())
	}
}

// A multi-domain graph for the file with no requested domain → fail closed,
// never a mixed evaluation.
func TestEditCheck_MultiDomainUnscoped_FailsClosed(t *testing.T) {
	mixed := scopeFacts(map[string]string{
		caddyRuleID:       caddyDomain,
		"globular.rule.a": "",
	})
	s := newEditCheckServer(mixed)
	_, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: caddyFile, ProposedContent: badContent, Domain: "",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("multi-domain unscoped EditCheck must fail closed, got %v", err)
	}
}

// applies_to_paths refinement: a bad shape on a file that does not match the
// rule's path globs is not evaluated (rules_evaluated=0), so no warning.
func TestEditCheck_PathMismatch_NotEvaluated(t *testing.T) {
	s := newEditCheckServer(scopeFacts(map[string]string{caddyRuleID: caddyDomain}))
	resp, err := s.EditCheck(context.Background(), &awarenesspb.EditCheckRequest{
		File: "modules/caddyhttp/reverseproxy/streaming.go", ProposedContent: badContent, Domain: caddyDomain,
	})
	if err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if resp.GetRulesEvaluated() != 0 {
		t.Errorf("rule should be skipped by applies_to_paths; rules_evaluated=%d", resp.GetRulesEvaluated())
	}
	if len(resp.GetWarnings()) != 0 {
		t.Errorf("no warning expected for a non-matching path, got %v", resp.GetWarnings())
	}
}
