package main

// G2: resolve `domain -> endpoint` through ONE owner, so callers stop choosing an
// Oxigraph/awareness port and the port becomes an implementation detail the owner
// returns.
//
// The owner already exists. ~/.sensei/domains.yaml registers every domain with its
// repository identity and allowed corpus roots, and it is deliberately kept OUTSIDE
// any published repository — its own comment says a registry living inside the repo
// being published would be exactly as untrustworthy as the corpus it vouches for.
// What it does not carry is the endpoint, so nothing can answer "which graph serves
// this domain" without consulting a port.
//
// Because no registered domain declares an endpoint today, this field is inert until
// an operator fills it: behaviour is unchanged for every existing caller.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registryNaming(t *testing.T, domain, addr string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "domains.yaml")
	body := "domains:\n  " + domain + ":\n    repository_identity: globulario/thing\n" +
		"    allowed_corpus_roots:\n      - docs/awareness\n"
	if addr != "" {
		body += "    service_addr: " + addr + "\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The registry outranks the project's own configuration, for the reason the
// registry exists: it is the operator-controlled authority kept outside the
// repository, and a repository must not be able to redirect its own graph.
func TestTheRegistryOutranksTheProjectConfiguration(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	reg := registryNaming(t, domain, "localhost:10199")
	root := projectNaming(t, "localhost:10122")

	addr, source := resolveDomainServiceAddr(addrFlagSet(t, false, ""), root, domain, "localhost:10120", reg)
	if addr != "localhost:10199" {
		t.Errorf("resolved %q, want the registry's localhost:10199", addr)
	}
	if !strings.Contains(source, "registry") {
		t.Errorf("source = %q, want it to name the registry", source)
	}
}

// An explicit flag still wins: an operator naming an endpoint at the point of use is
// the one authority above the registry, and it is visible in the command they ran.
func TestAnExplicitFlagOutranksEvenTheRegistry(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	reg := registryNaming(t, domain, "localhost:10199")
	addr, source := resolveDomainServiceAddr(addrFlagSet(t, true, "localhost:19999"), projectNaming(t, "localhost:10122"), domain, "localhost:19999", reg)
	if addr != "localhost:19999" || !strings.Contains(source, "command line") {
		t.Errorf("resolved %q from %q; an explicit flag must win", addr, source)
	}
}

// A registry that declares no endpoint changes nothing. This is what keeps the
// field inert until an operator fills it, so adding it cannot move any existing
// caller's endpoint.
func TestASilentRegistryLeavesTheExistingPrecedenceIntact(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	reg := registryNaming(t, domain, "")
	addr, source := resolveDomainServiceAddr(addrFlagSet(t, false, ""), projectNaming(t, "localhost:10122"), domain, "localhost:10120", reg)
	if addr != "localhost:10122" {
		t.Errorf("resolved %q, want the project's configured endpoint when the registry is silent", addr)
	}
	if !strings.Contains(source, "project") {
		t.Errorf("source = %q", source)
	}

	// An unregistered domain, and an unreadable registry, both fall through rather
	// than failing: this resolver reports an endpoint, it does not gate access.
	if addr, _ := resolveDomainServiceAddr(addrFlagSet(t, false, ""), projectNaming(t, "localhost:10122"), "github.com/other/repo", "localhost:10120", reg); addr != "localhost:10122" {
		t.Errorf("an unregistered domain resolved %q", addr)
	}
	if addr, _ := resolveDomainServiceAddr(addrFlagSet(t, false, ""), projectNaming(t, "localhost:10122"), domain, "localhost:10120", filepath.Join(t.TempDir(), "absent.yaml")); addr != "localhost:10122" {
		t.Errorf("an unreadable registry resolved %q", addr)
	}
}
