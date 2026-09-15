// SPDX-License-Identifier: AGPL-3.0-only

package main

// THE VISIBILITY DUTY that replaced the requireServerAddrAgreement refusal in runPreflight.
//
// The invariant:
//
//	When endpoint ownership selects a higher-authority endpoint that differs from repository-local
//	configuration, the operation follows the owner and visibly reports the disagreement.
//	Repository-local configuration must not veto the canonical owner.
//
// Why a notice and not a refusal: the refusal's only remaining reachable case was a
// registry-declared endpoint the project config does not name, and the registry outranks the
// project config precisely so that a repository cannot redirect its own graph. See
// issue_212_reachability_test.go for why the refusal no longer has a failure to catch.

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
)

// The precedence the typed authority encodes, asserted rather than assumed. `>` must mean
// `outranks`, so a future constant inserted in the wrong place fails here instead of silently
// changing who may veto whom.
func TestTheEndpointAuthorityOrderIsThePrecedenceOrder(t *testing.T) {
	ordered := []endpointAuthority{
		endpointAuthorityBuiltInDefault,
		endpointAuthorityProjectConfig,
		endpointAuthorityDomainRegistry,
		endpointAuthorityOperatorFlag,
	}
	for i := 1; i < len(ordered); i++ {
		if !(ordered[i] > ordered[i-1]) {
			t.Fatalf("%v does not outrank %v; the constant order is not the precedence order",
				ordered[i], ordered[i-1])
		}
	}
	if !endpointAuthorityDomainRegistry.outranksProjectConfig() {
		t.Error("the domain registry must outrank the project config")
	}
	if !endpointAuthorityOperatorFlag.outranksProjectConfig() {
		t.Error("an explicit operator override must outrank the project config")
	}
	if endpointAuthorityBuiltInDefault.outranksProjectConfig() {
		t.Error("the built-in default must NOT outrank the project config")
	}
	if endpointAuthorityProjectConfig.outranksProjectConfig() {
		t.Error("the project config does not outrank itself")
	}
	// Every constant must render as something an operator can read; an unnamed authority in a
	// notice is worse than no notice.
	for _, a := range ordered {
		if s := a.String(); s == "" || strings.Contains(s, "unrecorded") {
			t.Errorf("authority %d renders as %q", int(a), s)
		}
	}
	if endpointAuthorityUnset.String() != "an unrecorded source" {
		t.Errorf("the zero value must be visibly unrecorded, got %q", endpointAuthorityUnset.String())
	}
}

// THE FIVE REQUIRED CASES, one table so a case cannot be quietly dropped.
func TestTheFiveEndpointDisagreementCases(t *testing.T) {
	const A, B = "127.0.0.1:41001", "127.0.0.1:41002"
	for _, c := range []struct {
		name       string
		resolved   string
		authority  endpointAuthority
		configured string
		overridden bool
		wantNotice bool
		why        string
	}{
		{"1 config A, registry A, proceed silently", A, endpointAuthorityDomainRegistry, A, false, false,
			"they agree, so there is nothing to report"},
		{"2 config A, registry B, proceed with B and report", B, endpointAuthorityDomainRegistry, A, false, true,
			"the registry outranks the project config: the operator is told, never refused"},
		{"3 config A, registry silent, proceed with A", A, endpointAuthorityProjectConfig, A, false, false,
			"the config itself decided, so it cannot disagree with itself"},
		{"4 no project config, registry B, proceed with B", B, endpointAuthorityDomainRegistry, "", false, false,
			"no repository-local configuration means no disagreement to report"},
		{"5 explicit --addr B keeps law 14 semantics only", B, endpointAuthorityOperatorFlag, A, true, false,
			"nonCanonicalReaderNotice already says the stronger thing, that nothing has verified " +
				"this endpoint serves the domain's graph; a second weaker line trains an operator to skim"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := endpointDisagreementNotice(c.resolved, c.authority, c.configured, c.overridden)
			if (got != "") != c.wantNotice {
				t.Fatalf("notice=%q wantNotice=%v - %s", got, c.wantNotice, c.why)
			}
			if c.wantNotice {
				for _, must := range []string{c.resolved, c.configured, "stale"} {
					if !strings.Contains(got, must) {
						t.Errorf("the notice omits %q, so an operator cannot act on it: %s", must, got)
					}
				}
			}
			if strings.Contains(got, "refusing") {
				t.Fatalf("case %q produced a refusal: %s", c.name, got)
			}
		})
	}
}

// The duty must read semantic state, not the human-readable source prose, and must consume the
// owner's answer rather than the raw flag variable.
//
// The prose assertions matter because the source strings are duplicated across two functions in
// endpoint_binding.go and one of those copies predates the registry entirely, so a reader deciding
// by prose would be reading a closed set that already disagrees with the precedence contract.
func TestTheVisibilityDutyConsumesResolvedStateAndNotProse(t *testing.T) {
	src := readCmdSource(t, "cmd_preflight.go")

	if n := strings.Count(src, "endpointDisagreementNotice("); n != 1 {
		t.Fatalf("expected exactly one endpointDisagreementNotice call in cmd_preflight.go, found %d", n)
	}
	if !strings.Contains(src, "endpointDisagreementNotice(reader.Addr, reader.Authority, cfg.configuredServerAddr(), reader.Overridden)") {
		t.Fatal("the duty must be called with the owner's resolved address, the owner's authority, " +
			"the repository's configured address, and the override flag")
	}
	if strings.Contains(src, "requireServerAddrAgreement(") {
		t.Fatal("runPreflight still calls requireServerAddrAgreement; the refusal was re-scoped into " +
			"a visibility duty, so the call must not remain on this path")
	}
	if strings.Contains(src, "endpointDisagreementNotice(*addr") {
		t.Fatal("the duty is reading the raw --addr flag variable rather than the owner's answer")
	}
	for _, prose := range []string{"reader.Source ==", "strings.Contains(reader.Source"} {
		if strings.Contains(src, prose) {
			t.Fatalf("cmd_preflight.go decides behaviour by matching source prose (%s); read "+
				"reader.Authority instead", prose)
		}
	}
	// The config must be read from the root the OWNER resolved from, or the notice compares a
	// different configuration than the one that lost.
	if !strings.Contains(src, "loadEndpointConfig(graphConfigRoot(*repo))") {
		t.Fatal("the duty must read the configuration from graphConfigRoot(*repo), the root " +
			"productionReaderFor resolved from")
	}
	adopt := strings.Index(src, "*addr = reader.Addr")
	notice := strings.Index(src, "endpointDisagreementNotice(")
	dial := strings.Index(src, "client.DialConn(")
	if adopt < 0 || !(adopt < notice && notice < dial) {
		t.Fatalf("required order is adopt(%d) < notice(%d) < dial(%d): the operator must be told "+
			"before the endpoint is used", adopt, notice, dial)
	}
	t.Log("the duty names reader.Addr and reader.Authority, reads the owner's config root, decides " +
		"nothing by prose, and reports before the dial")
}

// CASE 2 END TO END, through the real command. The registry names an endpoint the project config
// does not; runPreflight proceeds against the REGISTRY's endpoint and says so. This is exactly what
// the old refusal blocked and what the precedence contract requires be permitted.
func TestRegistryOutranksProjectConfigAndTheOperationSaysSo(t *testing.T) {
	svc := &orderingService{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	registryAddr := lis.Addr().String()
	const configuredAddr = "127.0.0.1:19" // what the repository names; serves nothing

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
		orderingDomain+":\n        repository_identity: acme/ordering\n        service_addr: "+
		registryAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
		"    domain: "+orderingDomain+"\nserver:\n    addr: "+configuredAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	reader := productionReaderFor(emptyFlags(), root, orderingDomain, "")
	if reader.Authority != endpointAuthorityDomainRegistry {
		t.Fatalf("authority = %v, want the domain registry", reader.Authority)
	}
	if reader.Addr != registryAddr {
		t.Fatalf("the owner resolved %q, want the registry's %q", reader.Addr, registryAddr)
	}

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "registry outranks config"})
	})
	out := stdout + stderr

	if code != 0 {
		t.Fatalf("runPreflight returned %d: repository-local configuration must not veto the "+
			"registry.\nout:\n%s", code, out)
	}
	if svc.calls != 1 {
		t.Fatalf("the registry's endpoint was contacted %d times, want 1: the operation must follow "+
			"the owner.\nout:\n%s", svc.calls, out)
	}
	if strings.Contains(out, "refusing to act on an endpoint") {
		t.Fatalf("the operation was refused.\nout:\n%s", out)
	}
	for _, must := range []string{registryAddr, configuredAddr, "stale"} {
		if !strings.Contains(out, must) {
			t.Fatalf("the disagreement was not visibly reported (missing %q).\nout:\n%s", must, out)
		}
	}
	t.Logf("CASE 2 END TO END: registry %s outranked configured %s. Exit 0, registry contacted once, "+
		"disagreement reported to the operator.", registryAddr, configuredAddr)
}

// CASE 2 WITH THE OPERATOR STANDING IN A SUBDIRECTORY.
//
// What this does NOT do, stated so the mutation accounting stays honest: it does not kill the mutant
// that reads the configuration from resolveProjectRoot instead of graphConfigRoot. That mutant is
// killed textually, by TestTheVisibilityDutyConsumesResolvedStateAndNotProse, and it is currently
// NOT behaviourally observable at all. The two roots differ only when *repo names a subdirectory,
// and in exactly that case resolveRepositoryDomain returns an empty domain, so the registry is
// skipped and no disagreement can be constructed to report — see
// TestObservedTheGovernedDomainIsNotResolvedFromASubdirectory. The shared-root requirement becomes
// behaviourally testable once that defect is repaired; until then it rests on the source assertion.
func TestTheDisagreementIsStillReportedFromASubdirectory(t *testing.T) {
	svc := &orderingService{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	registryAddr := lis.Addr().String()
	const configuredAddr = "127.0.0.1:19"

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
		orderingDomain+":\n        repository_identity: acme/ordering\n        service_addr: "+
		registryAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
		"    domain: "+orderingDomain+"\nserver:\n    addr: "+configuredAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	// The premise: the two roots disagree here, and only the walking one sees the configuration.
	shallow, _ := resolveProjectRoot(".")
	if cfg, err := loadEndpointConfig(shallow); err == nil && cfg.configuredServerAddr() != "" {
		t.Fatalf("the non-walking root already sees %q; this fixture cannot distinguish the roots",
			cfg.configuredServerAddr())
	}

	// --repo names the checkout explicitly. Standing in a subdirectory with --repo omitted hits a
	// DIFFERENT defect of the same class, recorded separately in
	// TestObservedTheGovernedDomainIsNotResolvedFromASubdirectory: resolveRepositoryDomain does not
	// walk up, so the domain comes back empty, the registry cannot be consulted by domain, and the
	// endpoint silently degrades to the project config. This test is about the duty's config root,
	// so it takes that variable out of play rather than depending on it.
	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "subdirectory disagreement", "--repo", root})
	})
	out := stdout + stderr

	if code != 0 || svc.calls != 1 {
		t.Fatalf("code=%d calls=%d; the operation must follow the owner.\nout:\n%s", code, svc.calls, out)
	}
	for _, must := range []string{registryAddr, configuredAddr, "stale"} {
		if !strings.Contains(out, must) {
			t.Fatalf("from a subdirectory the disagreement was NOT reported (missing %q). The duty is "+
				"reading a configuration the owner did not.\nout:\n%s", must, out)
		}
	}
	t.Logf("SUBDIRECTORY CASE 2: from %s the notice still named registry %s over configured %s.",
		sub, registryAddr, configuredAddr)
}

// OBSERVATION, found by the subdirectory case above and deliberately NOT repaired here.
//
// runPreflight resolves its governed domain with resolveRepositoryDomain(*repo, *domain), and *repo
// defaults to ".". resolveRepositoryDomain does not walk up, so from a subdirectory with --repo
// omitted the domain comes back empty. An empty domain cannot be looked up in the registry, so
// precedence 2 is skipped entirely and the endpoint falls through to the project config — quietly
// preferring the weaker authority exactly where the operator is least likely to notice.
//
// This is the same stale-root class as the repair in this family: the owner reads its configuration
// through graphConfigRoot, which walks up, while the domain that selects the registry entry is
// resolved from a root that does not. Recorded as a measured fact so it can be repaired as its own
// bounded family with its own witness, rather than folded into the visibility duty.
func TestObservedTheGovernedDomainIsNotResolvedFromASubdirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
		orderingDomain+":\n        repository_identity: acme/ordering\n        service_addr: 127.0.0.1:41999\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
		"    domain: "+orderingDomain+"\nserver:\n    addr: 127.0.0.1:19\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	atRoot := resolveRepositoryDomain(".", "")
	t.Chdir(sub)
	atSub := resolveRepositoryDomain(".", "")

	if atRoot.Domain != orderingDomain {
		t.Fatalf("at the root the domain resolved to %q, want %q; the premise is gone",
			atRoot.Domain, orderingDomain)
	}
	if atSub.Domain == orderingDomain {
		t.Skip("resolveRepositoryDomain now walks up; this observation is obsolete and the " +
			"subdirectory registry path no longer degrades")
	}
	// The consequence, stated as the fact it is: the higher authority is skipped.
	readerAtSub := productionReaderFor(emptyFlags(), ".", atSub.Domain, "")
	t.Logf("OBSERVED: at the root the domain is %q; from %s it is %q. The owner then resolves %q "+
		"from %q — the registry's 127.0.0.1:41999 is skipped because an empty domain has no registry "+
		"entry, so the weaker authority wins where the operator is least likely to look.",
		atRoot.Domain, sub, atSub.Domain, readerAtSub.Addr, readerAtSub.Source)
}
