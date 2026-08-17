// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/propose"
)

// checkout builds a repository whose awareness directory a server could be
// pointed at. domain == "" leaves repository.domain unconfigured.
func checkout(t *testing.T, domain string) (awarenessDir string) {
	t.Helper()
	root := t.TempDir()
	awarenessDir = filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if domain != "" {
		cfg := "repository:\n    domain: " + domain + "\n"
		if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return awarenessDir
}

// THE defect: three signals said sensei-code while the file landed in services'
// queue, and `accepted: true` came back.
func TestProposalForAnotherRepositoryIsRefusedNotMisfiled(t *testing.T) {
	dir := checkout(t, "github.com/globulario/services")

	reason := foreignDomainClaim(dir, propose.Request{Domain: "github.com/globulario/sensei-code"})
	if reason == "" {
		t.Fatal("a proposal claiming another repository's domain must be refused, not filed here")
	}
	// The refusal must name BOTH domains, or an operator cannot tell what went
	// where.
	for _, want := range []string{"github.com/globulario/sensei-code", "github.com/globulario/services"} {
		if !strings.Contains(reason, want) {
			t.Errorf("refusal does not name %q: %s", want, reason)
		}
	}
}

// The matching case is accepted: this is a custody check, not a blanket ban on
// naming a domain.
func TestMatchingDomainClaimIsHonoured(t *testing.T) {
	dir := checkout(t, "github.com/globulario/services")

	if reason := foreignDomainClaim(dir, propose.Request{Domain: "github.com/globulario/services"}); reason != "" {
		t.Fatalf("a matching claim must be honoured, got refusal: %s", reason)
	}
}

// The `repo` field participates in the generated node id exactly as `domain`
// does (propose.domainHint falls back to it), so it must be checked too —
// otherwise the same misfiling happens through the other field.
func TestRepoFieldIsCheckedLikeDomain(t *testing.T) {
	dir := checkout(t, "github.com/globulario/services")

	if reason := foreignDomainClaim(dir, propose.Request{Repo: "github.com/globulario/sensei-code"}); reason == "" {
		t.Fatal("a foreign claim made through `repo` must be refused too")
	}
	if reason := foreignDomainClaim(dir, propose.Request{Repo: "github.com/globulario/services"}); reason != "" {
		t.Fatalf("matching `repo` claim refused: %s", reason)
	}
}

// A claim that cannot be verified is refused rather than filed on hope. An
// unverifiable claim and a verified-matching one are different facts.
func TestUnverifiableClaimIsRefused(t *testing.T) {
	unconfigured := checkout(t, "")

	reason := foreignDomainClaim(unconfigured, propose.Request{Domain: "github.com/globulario/services"})
	if reason == "" {
		t.Fatal("a claim against a queue with no declared owner must be refused")
	}
	if !strings.Contains(reason, "repository.domain") {
		t.Fatalf("refusal must say how to fix it: %s", reason)
	}
}

// Absent claim, unchanged behaviour: this must not become a requirement to name
// a domain on every proposal.
func TestNoDomainClaimIsUnaffected(t *testing.T) {
	for _, dir := range []string{checkout(t, "github.com/globulario/services"), checkout(t, "")} {
		if reason := foreignDomainClaim(dir, propose.Request{Title: "no domain named"}); reason != "" {
			t.Fatalf("an unqualified proposal must be unaffected, got: %s", reason)
		}
	}
}

// Scope vocabulary is not a repository-identity claim. `domain: shared` must not
// be mistaken for a repository domain and refused.
func TestScopeVocabularyIsNotARepositoryClaim(t *testing.T) {
	dir := checkout(t, "github.com/globulario/services")

	for _, v := range []string{"shared", "repo"} {
		if reason := foreignDomainClaim(dir, propose.Request{Domain: v}); reason != "" {
			t.Fatalf("domain %q is scope vocabulary, not a repository claim; got: %s", v, reason)
		}
	}
}

// The request must never be able to establish which repository a directory
// belongs to — that would let the claim verify itself. Proven by construction:
// the same claim gets opposite verdicts depending only on the WRITE PATH's
// configured identity, never on the request.
func TestTheClaimCannotVerifyItself(t *testing.T) {
	claim := propose.Request{Domain: "github.com/globulario/sensei-code"}

	own := checkout(t, "github.com/globulario/sensei-code")
	foreign := checkout(t, "github.com/globulario/services")

	if reason := foreignDomainClaim(own, claim); reason != "" {
		t.Fatalf("identical claim refused against its own repository: %s", reason)
	}
	if reason := foreignDomainClaim(foreign, claim); reason == "" {
		t.Fatal("identical claim accepted against a foreign repository; the claim is verifying itself")
	}
}

// A write path outside any Sensei checkout has no owner to compare against.
func TestWritePathOutsideACheckoutIsRefused(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "loose", "awareness")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	if reason := foreignDomainClaim(bare, propose.Request{Domain: "github.com/globulario/services"}); reason == "" {
		t.Fatal("a claim against a queue with no owning checkout must be refused")
	}
}
