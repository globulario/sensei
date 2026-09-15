// SPDX-License-Identifier: AGPL-3.0-only

package main

// MEASUREMENT of the 18 remaining generation/authority gaps on head 8e3366f6, after endpoint
// ownership closed. Measurement only: no production behaviour is changed and no verification call
// is added.
//
// Every one of the 18 lacks a comparison, so "no comparison" does not discriminate between them.
// What discriminates is whether a comparison WOULD BE ABLE TO FAIL if it were added, and that is
// decided by whether the owner could ever hand the subject a non-empty DeclaredGeneration. The
// reproducers below are built so that each category fails for its own reason with a graph that is
// reachable, self-consistent, and simply the wrong generation.

import (
	"os"
	"path/filepath"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

const (
	gapDomain    = "example.com/acme/measured"
	gapDeclared  = "1111111111111111111111111111111111111111111111111111111111111111"
	gapForeign   = "2222222222222222222222222222222222222222222222222222222222222222"
	gapStoreAddr = "localhost:19100"
)

// gapWorld: a project that STATES its domain, and a registry that declares a generation ACTIVE for
// it. Both halves present, so an inert comparison cannot be blamed on an unconfigured environment.
func gapWorld(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+gapDomain+`:
        repository_identity: acme/measured
        allowed_corpus_roots:
            - docs/awareness
        active_generation: `+gapDeclared+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+gapDomain+"\nserver:\n    addr: "+gapStoreAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

// servedForeign is a HEALTHY response: well formed, self-certified authoritative and CURRENT. The
// only thing wrong with it is which generation answered.
func servedForeign() *awarenesspb.GraphAuthority {
	return &awarenesspb.GraphAuthority{
		Authoritative:              true,
		GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		LiveStoreGraphDigestSha256: gapForeign,
	}
}

// CATEGORY A — EXPECTED DOMAIN STRUCTURALLY UNRESOLVED.
//
// Five subjects hand the owner a literal "": benchmark-brief, benchmark-score, pattern-check,
// synthesis-run and domains. declaredActiveGeneration reads reg.Domains[""], the zero value, so
// DeclaredGeneration is permanently empty and verifyServed is inert. Adding a comparison to these
// five would change nothing — which is why they are a different repair from the rest.
func TestCategoryA_ALiteralEmptyDomainMakesAnyComparisonInert(t *testing.T) {
	gapWorld(t)

	asTheFiveCallIt := productionReaderFor(emptyFlags(), "", "", "")
	if asTheFiveCallIt.DeclaredGeneration != "" {
		t.Fatalf("a literal empty domain produced DeclaredGeneration %q; this measurement's premise "+
			"is gone", asTheFiveCallIt.DeclaredGeneration)
	}
	// The comparison the next family would add, against a healthy WRONG generation: it passes.
	if err := asTheFiveCallIt.verifyServed(servedForeign().GetLiveStoreGraphDigestSha256()); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	t.Log("CATEGORY A CONFIRMED: with a literal empty domain, a served generation of " + gapForeign +
		" is accepted while the registry declares " + gapDeclared + " — a comparison added here " +
		"cannot fail")

	// And the SAME project can answer the question, which is what makes this a defect rather than
	// an unconfigured environment.
	resolvable := resolveRepositoryDomain(".", "").Domain
	if resolvable != gapDomain {
		t.Errorf("the project states no resolvable domain (%q); category A would then be an "+
			"environment limit rather than a defect", resolvable)
	}
}

// CATEGORY B — EXPECTED DOMAIN CONDITIONALLY UNRESOLVED.
//
// Nine subjects pass the raw --domain flag, whose default is empty (edit-guard's is $AWG_DOMAIN,
// which may also be empty). A comparison added to these WOULD work when an operator names the
// domain and would be inert otherwise, so the gap is the flag, not the check.
func TestCategoryB_ARawDomainFlagDecidesWhetherAComparisonCanFail(t *testing.T) {
	gapWorld(t)

	// With the flag omitted — the default path — the comparison is inert.
	omitted := productionReaderFor(emptyFlags(), "", "", "")
	if err := omitted.verifyServed(gapForeign); err != nil {
		t.Fatalf("unexpected refusal with the domain omitted: %v", err)
	}

	// With the flag given, the very same comparison refuses.
	named := productionReaderFor(emptyFlags(), "", gapDomain, "")
	if named.DeclaredGeneration != gapDeclared {
		t.Fatalf("a named domain saw DeclaredGeneration %q, want %q", named.DeclaredGeneration, gapDeclared)
	}
	if err := named.verifyServed(gapForeign); err == nil {
		t.Fatal("a named domain accepted a foreign served generation; category B's premise is gone")
	}
	t.Log("CATEGORY B CONFIRMED: identical code, identical graph — inert with --domain omitted, " +
		"refusing with it named. The gap is the unresolved flag, not the missing comparison")
}

// CATEGORY C — AUTHORITY PRESENT AND RESOLVED, NEVER COMPARED.
//
// Four subjects already resolve a governed domain before resolution — preflight and
// verify-obligations through resolveRepositoryDomain, edit-brief from the repository configuration,
// contract-bootstrap from its task file. For these the owner hands over a real declared generation,
// the response carries served authority, and nothing compares them. A comparison added here is
// immediately effective.
func TestCategoryC_AResolvedDomainMakesTheMissingComparisonImmediatelyEffective(t *testing.T) {
	gapWorld(t)

	// This is what preflight/verify-obligations/edit-brief compute before resolving.
	resolved := resolveRepositoryDomain(".", "").Domain
	if resolved != gapDomain {
		t.Fatalf("resolveRepositoryDomain gave %q, want %q", resolved, gapDomain)
	}
	reader := productionReaderFor(emptyFlags(), "", resolved, "")
	if reader.DeclaredGeneration != gapDeclared {
		t.Fatalf("DeclaredGeneration = %q, want %q", reader.DeclaredGeneration, gapDeclared)
	}
	if err := reader.verifyServed(servedForeign().GetLiveStoreGraphDigestSha256()); err == nil {
		t.Fatal("a resolved domain accepted a foreign served generation; category C's premise is gone")
	}
	t.Log("CATEGORY C CONFIRMED: the owner supplies " + gapDeclared + ", the response supplies " +
		gapForeign + ", and nothing in these four commands compares them — the check is simply absent")
}

// NO SUBJECT IS EXEMPT FOR WANT OF AUTHORITY ON THE WIRE. Measured against the proto rather than
// assumed, so category "response lacks authority representation" is empty by evidence.
func TestNoSubjectLacksAuthorityOnTheWire(t *testing.T) {
	for _, m := range []interface {
		GetAuthority() *awarenesspb.GraphAuthority
	}{
		&awarenesspb.BriefingResponse{}, &awarenesspb.EditCheckResponse{},
		&awarenesspb.ImpactResponse{}, &awarenesspb.MetadataResponse{},
		&awarenesspb.PreflightResponse{}, &awarenesspb.QueryResponse{},
		&awarenesspb.ReferenceSitesResponse{}, &awarenesspb.ResolveResponse{},
	} {
		// Compiling at all proves the accessor exists on every response type a subject consumes.
		_ = m.GetAuthority()
	}
	t.Log("all eight response types a production reader can consume expose GraphAuthority; no gap " +
		"is explained by a missing authority representation")
}

// CATEGORY D — EXPECTATION NOT REACHABLE BY THE LOOKUP DIRECTION THE OWNER OFFERS.
//
// `sensei domains` asks a question that ranges over ALL domains, so it names none. That is not a
// forgotten flag: the command has no single subject domain to pass.
//
// It is still not exempt. A governed store belongs to exactly ONE domain — validateStoreOwnership
// refuses a registry binding one store to two — so the store answering `domains` HAS a declared
// active generation, and the reply is consumed as authoritative (it is what an operator then passes
// as --domain, and what the editor's domain filter offers). The expectation exists; runDomains
// simply cannot reach it, because the registry is indexed domain -> addr and runDomains starts from
// an addr.
//
// And the inverse is not merely missing, it is not well defined: the uniqueness invariant covers
// StoreURL, NOT ServiceAddr. Two domains may legally declare the same endpoint with different
// active generations, and the registry loads without complaint.
func TestCategoryD_TheInverseLookupDomainsWouldNeedIsNotWellDefined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".sensei", "domains.yaml")

	// Two domains, ONE shared endpoint, DIFFERENT active generations, DISTINCT stores (so the only
	// invariant that exists is satisfied).
	if err := os.WriteFile(path, []byte(`domains:
    example.com/acme/first:
        repository_identity: acme/first
        service_addr: `+gapStoreAddr+`
        store_url: http://localhost:7878/first
        active_generation: `+gapDeclared+`
    example.com/acme/second:
        repository_identity: acme/second
        service_addr: `+gapStoreAddr+`
        store_url: http://localhost:7878/second
        active_generation: `+gapForeign+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadDomainRegistry(path)
	if err != nil {
		t.Fatalf("the registry was refused at load: %v — if endpoint uniqueness is now enforced, "+
			"category D collapses into category A and this measurement must be redone", err)
	}

	// The lookup runDomains would have to perform, written out: addr -> domain.
	var claimants []string
	for name, rd := range reg.Domains {
		if rd.ServiceAddr == gapStoreAddr {
			claimants = append(claimants, name+"@"+rd.ActiveGeneration)
		}
	}
	if len(claimants) < 2 {
		t.Fatalf("expected two claimants for one endpoint, got %v", claimants)
	}
	t.Logf("CATEGORY D CONFIRMED: endpoint %s is claimed by %d domains declaring DIFFERENT active "+
		"generations (%v), and the registry loaded without objection — so `domains` cannot derive a "+
		"single expected generation from the address it holds. The uniqueness invariant that would "+
		"make this inverse a function exists only for store_url.", gapStoreAddr, len(claimants), claimants)

	// The asymmetry, stated as a fact rather than an inference: the SAME shape is refused when
	// expressed through store_url.
	if err := os.WriteFile(path, []byte(`domains:
    example.com/acme/first:
        repository_identity: acme/first
        store_url: http://localhost:7878/shared
        active_generation: `+gapDeclared+`
    example.com/acme/second:
        repository_identity: acme/second
        store_url: http://localhost:7878/shared
        active_generation: `+gapForeign+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDomainRegistry(path); err == nil {
		t.Fatal("two domains sharing one STORE loaded without objection; the asymmetry this " +
			"category rests on does not exist")
	}
	t.Log("asymmetry confirmed: one store claimed twice is refused at load, one endpoint claimed " +
		"twice is accepted")
}

// CATEGORY E — DOMAIN CORRECTLY RESOLVED, REGISTRY DECLARES NO GENERATION.
//
// This is NOT a fifth group of subjects. It is a property of the DOMAIN, and it cuts across every
// category: a subject in category C, which resolves its governed domain perfectly, still gets an
// empty expectation when the registry declares no active generation for that domain.
//
// It is measured, not hypothetical. The operator registry on this machine declares
// active_generation for github.com/globulario/sensei and github.com/globulario/sensei-code, and
// NOT for github.com/globulario/services. So a repair that lands a comparison in all 18 subjects is
// effective for two of the three governed domains and silently inert for the third.
func TestCategoryE_AResolvedDomainStillYieldsNoExpectationWhenTheRegistryDeclaresNone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A fully well-formed registration for the domain — missing only active_generation, exactly the
	// shape github.com/globulario/services has today.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+gapDomain+`:
        repository_identity: acme/measured
        allowed_corpus_roots:
            - docs/awareness
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+gapDomain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	// The subject does everything right: it resolves its governed domain from the project.
	resolved := resolveRepositoryDomain(".", "").Domain
	if resolved != gapDomain {
		t.Fatalf("domain resolution failed (%q); this reproducer would then be measuring the wrong "+
			"thing", resolved)
	}
	reader := productionReaderFor(emptyFlags(), "", resolved, "")
	if reader.Domain != gapDomain {
		t.Fatalf("the owner carried domain %q, want %q", reader.Domain, gapDomain)
	}
	if reader.DeclaredGeneration != "" {
		t.Fatalf("DeclaredGeneration = %q; the registry declares none, so this must be empty",
			reader.DeclaredGeneration)
	}
	if err := reader.verifyServed(gapForeign); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	t.Log("CATEGORY E CONFIRMED: the domain resolved correctly and the owner carried it, yet the " +
		"expectation is empty because the registry declares no active_generation — so a comparison " +
		"added to a category-C subject is inert for exactly those domains. This is orthogonal to " +
		"categories A-D and is a registry-completeness precondition, not a code defect.")
}
