// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// ntContains / ntOmits assert on the emitted N-Triples string, with a reason.
func ntContains(t *testing.T, nt, sub, why string) {
	t.Helper()
	if !strings.Contains(nt, sub) {
		t.Errorf("expected NT to contain %q (%s)", sub, why)
	}
}
func ntOmits(t *testing.T, nt, sub, why string) {
	t.Helper()
	if strings.Contains(nt, sub) {
		t.Errorf("NT must NOT contain %q (%s)", sub, why)
	}
}

// A repo-scoped (pilot) forbidden fix must compile to the domain triples the
// scope filter reads (aw:repo + aw:domain), the source-set, and the full
// provenance receipt — plus the SourceFile→implements anchor that lets a
// briefing-by-file surface it. This is the producer half of the no-cross-domain
// -leak contract: without these triples, scope.go has nothing to isolate on.
func TestDomainScope_RepoScopedForbiddenFix_EmitsScopeAndProvenance(t *testing.T) {
	root := makeDir(t, map[string]string{
		"forbidden_fixes.yaml": `
forbidden_fixes:
  - id: caddy.reverseproxy.no_fmt_errorf_in_caddyfile
    title: Do not use fmt.Errorf in a Caddyfile unmarshaler
    reason: It strips the directive's source location.
    protects:
      files:
        - modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go
    repo: github.com/caddyserver/caddy
    domain: repo
    source_set: pilot/caddy
    origin: coldsource
    provenance:
      bundle_id: caddy-reverseproxy-forwardauth-2026-06
      commit_range: HEAD~500..HEAD
      citations:
        - "github.com/caddyserver/caddy#7814 review comment 3390101816"
        - "github.com/caddyserver/caddy#7814 review comment 3390255669"
      review_label: load-bearing
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	ntContains(t, out, rdf.IRI(rdf.PropRepo), "scope filter keys on aw:repo")
	ntContains(t, out, `"github.com/caddyserver/caddy"`, "the repo domain value")
	ntContains(t, out, rdf.IRI(rdf.PropDomain), "aw:domain marks the node repo-scoped")
	ntContains(t, out, rdf.IRI(rdf.PropSourceSet), "aw:sourceSet namespaces the rule")
	ntContains(t, out, `"pilot/caddy"`, "the source-set value")
	ntContains(t, out, rdf.IRI(rdf.PropOrigin), "aw:origin records the rule was cold-sourced")

	// Provenance receipt.
	ntContains(t, out, rdf.IRI(rdf.PropProvenanceBundleID), "provenance bundle id")
	ntContains(t, out, `"caddy-reverseproxy-forwardauth-2026-06"`, "bundle id value")
	ntContains(t, out, rdf.IRI(rdf.PropProvenanceCommitRange), "provenance commit range")
	ntContains(t, out, `"HEAD~500..HEAD"`, "commit range value")
	ntContains(t, out, rdf.IRI(rdf.PropProvenanceCitation), "provenance citations")
	ntContains(t, out, "3390101816", "first citation present")
	ntContains(t, out, "3390255669", "second citation present")
	ntContains(t, out, rdf.IRI(rdf.PropReviewLabel), "human review label")
	ntContains(t, out, `"load-bearing"`, "review label value")

	// Anchor: file → aw:implements → node, so briefing-by-file can reach it.
	ntContains(t, out, rdf.IRI(rdf.PropImplements), "reverse anchor edge for briefing-by-file")
	ntContains(t, out, "caddyfile.go", "the anchored Caddy file")
}

// A shared meta-principle (domain: shared) emits aw:domain=shared and NEVER an
// aw:repo — shared rules surface in every scope precisely because they belong to
// no repo.
func TestDomainScope_SharedInvariant_EmitsSharedNotRepo(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: meta.absence_scope_must_be_explicit
    title: Absence scope must be explicit
    domain: shared
    protects:
      files:
        - golang/server/scope.go
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	ntContains(t, out, rdf.IRI(rdf.PropDomain), "aw:domain present")
	ntContains(t, out, `"`+rdf.DomainShared+`"`, "domain value is shared")
	ntOmits(t, out, rdf.IRI(rdf.PropRepo), "a shared rule must carry no aw:repo")
}

// The seed-unchanged guarantee: an ordinary (untagged) entry emits NONE of the
// domain-scope or provenance triples. This is what keeps every existing
// home-domain rule compiling byte-for-byte as before — the pilot is purely
// additive.
func TestDomainScope_UntaggedEntry_EmitsNoDomainTriples(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: globular.repository.publish_is_scylla_first
    title: Publish is Scylla-first
    protects:
      files:
        - golang/repository/repository_server/publish.go
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	ntOmits(t, out, rdf.IRI(rdf.PropRepo), "untagged entry has no aw:repo")
	ntOmits(t, out, rdf.IRI(rdf.PropDomain), "untagged entry has no aw:domain")
	ntOmits(t, out, rdf.IRI(rdf.PropSourceSet), "untagged entry has no aw:sourceSet")
	ntOmits(t, out, rdf.IRI(rdf.PropProvenanceBundleID), "untagged entry has no provenance")
}
