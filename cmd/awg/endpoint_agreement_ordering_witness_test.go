// SPDX-License-Identifier: AGPL-3.0-only

package main

// ENDPOINT-AGREEMENT ORDERING WITNESS — runPreflight.
//
// The invariant under test:
//
//	An endpoint-agreement guard must compare project configuration against the canonical endpoint
//	actually resolved for the operation, not against the raw CLI flag before resolution.
//
// This is NOT a witness about endpoint disagreement. The fixture is deliberately built so that
// configuration and canonical resolution AGREE: one real service, one address, named by the project
// config, and resolvable by the owner from that same config. There is no wrong endpoint anywhere in
// the world these tests construct.
//
// The defect is that runPreflight calls requireServerAddrAgreement with the raw --addr flag, before
// productionReaderFor has resolved anything. With --addr omitted the operand is "", so the guard
// compares a configured address against nothing and refuses an operation that was never in
// disagreement.
//
// Category C's served-generation evidence lives in preflight_served_authority_witness_test.go and is
// untouched by this file.

import (
	"context"
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
)

const orderingDomain = "example.com/acme/ordering"

// orderingService records whether the endpoint was ever contacted. The guard's own promise is
// "nothing has been read from or written to either endpoint", so a refusal must leave calls at zero,
// and a proceed must leave it at one. That is what makes "proceeded past the guard" a fact rather
// than an inference from an exit code.
type orderingService struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	calls int
}

func (s *orderingService) Preflight(context.Context, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	s.calls++
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		RiskClass:  awarenesspb.RiskClass_LOW_RISK,
		Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
		Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		},
	}, nil
}

// orderingWorld: one real service; the project config NAMES that exact address; the registry is well
// formed and deliberately does NOT declare service_addr, so the canonical owner resolves the address
// from the project configuration — the real-world shape measured in both governed repos.
func orderingWorld(t *testing.T) (svc *orderingService, root, addr string) {
	t.Helper()

	svc = &orderingService{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	addr = lis.Addr().String()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Well formed, and silent about service_addr on purpose: precedence is
	// flag > registry service_addr > project config > built-in default, so this hands the
	// project config the answer.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+orderingDomain+`:
        repository_identity: acme/ordering
        allowed_corpus_roots:
            - docs/awareness
`), 0o644); err != nil {
		t.Fatal(err)
	}

	root = projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+orderingDomain+"\nserver:\n    addr: "+addr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return svc, root, addr
}

// THE INVERTED WITNESS. Formerly TestTheAddrAgreementGuardRefusesOverAnOperandThatDoesNotExistYet,
// which proved the refusal. It now proves the invariant that replaced it:
//
//	Endpoint agreement is evaluated against the endpoint the operation will actually use.
//
// The fixture is unchanged. Only the expected outcome moved, which is what makes this an inversion
// rather than a new test: the same world that refused now agrees, on the resolved endpoint.
func TestEndpointAgreementIsEvaluatedAgainstTheResolvedEndpoint(t *testing.T) {
	// Read the source BEFORE orderingWorld, which chdirs into the fixture checkout.
	src := readCmdSource(t, "cmd_preflight.go")

	svc, root, addr := orderingWorld(t)

	// FACT 1 — the raw CLI address is still empty. The repair must not restore a non-empty default.
	if !strings.Contains(src, `addr := fs.String("addr", "", "Sensei gRPC server address")`) {
		t.Fatal("cmd_preflight.go no longer declares --addr with an empty default; the repair must " +
			"not restore a CLI default")
	}

	// FACT 2 — the configured server address is non-empty, and is the service that exists.
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		t.Fatalf("the fixture's endpoint config is malformed: %v", err)
	}
	configured := cfg.configuredServerAddr()
	if configured != addr {
		t.Fatalf("configured server addr = %q, want the real listener %q", configured, addr)
	}

	// FACT 3 — the canonical owner, with no override, resolves that same configured address.
	reader := productionReaderFor(emptyFlags(), root, orderingDomain, "")
	if reader.Addr != configured {
		t.Fatalf("the owner resolved %q, want the configured %q", reader.Addr, configured)
	}
	if reader.Overridden {
		t.Fatal("the owner reports an override with no flag passed")
	}

	// FACT 4 — INVERTED. The guard now executes AFTER the owner resolves. Positional, from source.
	guardAt := strings.Index(src, "requireServerAddrAgreement(fs,")
	resolveAt := strings.Index(src, "productionReaderFor(fs,")
	dialAt := strings.Index(src, "client.DialConn(")
	if guardAt < 0 || resolveAt < 0 || dialAt < 0 {
		t.Fatal("cannot locate the owner call, the guard and the dial in cmd_preflight.go")
	}
	if !(resolveAt < guardAt && guardAt < dialAt) {
		t.Fatalf("required order is resolve(%d) < guard(%d) < dial(%d); the guard must be evaluated "+
			"after the canonical reader exists and before the endpoint is used", resolveAt, guardAt, dialAt)
	}

	// FACT 5 — the operand is what decides, and that has not changed about the FUNCTION. Keeping
	// both halves here is what proves the repair moved the operand rather than weakening the guard.
	if err := requireServerAddrAgreement(emptyFlags(), root, ""); err == nil {
		t.Fatal("the guard now accepts an empty operand; it has been weakened rather than re-pointed")
	}
	if err := requireServerAddrAgreement(emptyFlags(), root, reader.Addr); err != nil {
		t.Fatalf("the guard refused the owner-resolved address %q: %v", reader.Addr, err)
	}

	// THE OUTCOME, through the real command: it proceeds.
	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "probe the agreement guard"})
	})
	out := stdout + stderr

	if code != 0 {
		t.Fatalf("runPreflight returned %d, want 0 — the canonical no---addr path must be reachable."+
			"\nout:\n%s", code, out)
	}
	if strings.Contains(out, "refusing to act on an endpoint the project config does not name") {
		t.Fatalf("the agreement guard still refuses the canonical path.\nout:\n%s", out)
	}
	// It proceeded through the OWNER's endpoint, not by being skipped: the service was contacted.
	if svc.calls != 1 {
		t.Fatalf("the service was contacted %d times, want 1.\nout:\n%s", svc.calls, out)
	}
	// And it did so canonically — no override notice, because no flag was named.
	if strings.Contains(out, "non-canonical override") {
		t.Fatalf("the canonical path emitted a non-canonical override notice.\nout:\n%s", out)
	}

	t.Logf("INVARIANT ESTABLISHED.\n"+
		"  raw CLI address      \"\" (--addr omitted; declared default still empty)\n"+
		"  configured address   %s (.sensei/config.yaml server.addr)\n"+
		"  owner resolves       %s (same address, from this project's configuration)\n"+
		"  order                resolve(%d) < guard(%d) < dial(%d)\n"+
		"  guard on raw \"\"      still REFUSES (unchanged)\n"+
		"  guard on resolved    returns nil\n"+
		"  result               exit 0, endpoint contacted once, canonically",
		configured, reader.Addr, resolveAt, guardAt, dialAt)
}

// The shared-root half of the invariant. resolveProjectRoot does not walk up, so a guard reading the
// config from it would, from a subdirectory, find no configuration, conclude nothing is configured,
// and pass — a fail-open of exactly the class the guard exists to prevent. The owner resolves through
// graphConfigRoot, which walks up, so the guard must read the same root.
func TestEndpointAgreementReadsTheSameConfigurationTheOwnerDid(t *testing.T) {
	svc, root, addr := orderingWorld(t)

	sub := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	// The two roots genuinely differ here; that is the premise.
	shallow, _ := resolveProjectRoot(".")
	deep := graphConfigRoot(".")
	if shallow == deep {
		t.Skipf("resolveProjectRoot and graphConfigRoot agree from %s, so this fixture cannot "+
			"distinguish them", sub)
	}
	if cfg, err := loadEndpointConfig(shallow); err == nil && cfg.configuredServerAddr() != "" {
		t.Fatalf("the non-walking root already sees a configured addr (%q); the premise is gone",
			cfg.configuredServerAddr())
	}

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "probe from a subdirectory"})
	})
	out := stdout + stderr
	if code != 0 || svc.calls != 1 {
		t.Fatalf("code=%d calls=%d; the canonical path must work from a subdirectory.\nout:\n%s",
			code, svc.calls, out)
	}
	// The guard was genuinely evaluated, not skipped: the same call with a foreign operand refuses
	// from this same working directory.
	if err := requireServerAddrAgreement(emptyFlags(), graphConfigRoot("."), "127.0.0.1:19"); err == nil {
		t.Fatal("from this subdirectory the guard cannot refuse a foreign endpoint, so it is inert here")
	}
	t.Logf("SHARED ROOT: from %s, resolveProjectRoot gives %s (no config) and graphConfigRoot gives "+
		"%s (config names %s). The guard reads the owner's root, so it is live rather than inert here.",
		sub, shallow, deep, addr)
}

// POSITIVE CONTROL 1 — explicit matching override. Same fixture, --addr set to the configured
// address. If this proceeds, the fixture is valid and the refusal above is caused by operand timing
// rather than by the project configuration.
func TestNamingTheConfiguredAddressExplicitlyProceedsPastTheGuard(t *testing.T) {
	svc, _, addr := orderingWorld(t)

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "probe the agreement guard", "--addr", addr})
	})
	out := stdout + stderr

	if code != 0 {
		t.Fatalf("runPreflight returned %d, want 0.\nout:\n%s", code, out)
	}
	if svc.calls != 1 {
		t.Fatalf("the service was contacted %d times, want 1 — the command must have gone through "+
			"to the endpoint.\nout:\n%s", svc.calls, out)
	}
	if strings.Contains(out, "refusing to act on an endpoint") {
		t.Fatalf("the guard refused the configured address when named explicitly.\nout:\n%s", out)
	}
	t.Logf("CONTROL: the same fixture and the same address, named explicitly, proceeds and reaches "+
		"the service (%d call). The configuration is not the cause of the refusal; the operand is.",
		svc.calls)
}

// POSITIVE CONTROL 2 — genuine disagreement. Project config names A; the operator explicitly
// requests B. This is the case the guard exists for.
//
// This control asserts the OBSERVED behaviour, whatever it is, and names it, so a future repair
// cannot silently change it. It does not assume a refusal.
func TestAGenuineDisagreementBetweenConfigAndAnExplicitAddress(t *testing.T) {
	svc, root, addr := orderingWorld(t) // config names addr == A, the real service

	// B: a syntactically valid address that is NOT the configured one and hosts nothing.
	other := "127.0.0.1:19"

	// The guard, called directly with the disagreeing operand and the flag PASSED.
	fsWithFlag := flag.NewFlagSet("probe", flag.ContinueOnError)
	_ = fsWithFlag.String("addr", "", "")
	if err := fsWithFlag.Parse([]string{"--addr", other}); err != nil {
		t.Fatal(err)
	}
	guardErr := requireServerAddrAgreement(fsWithFlag, root, other)

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "probe the agreement guard", "--addr", other})
	})
	out := stdout + stderr

	refused := strings.Contains(out, "refusing to act on an endpoint the project config does not name")
	t.Logf("GENUINE DISAGREEMENT OBSERVED.\n"+
		"  configured (A)       %s\n"+
		"  requested  (B)       %s\n"+
		"  guard called direct  err=%v\n"+
		"  runPreflight exit    %d\n"+
		"  agreement refusal    %v\n"+
		"  A contacted          %d times\n"+
		"  output:\n%s", addr, other, guardErr, code, refused, svc.calls, out)

	// What must not happen either way: the command must not silently read the CONFIGURED endpoint
	// while the operator named a different one.
	if svc.calls != 0 {
		t.Fatalf("the configured endpoint A was contacted %d times although B was named", svc.calls)
	}
	// Record the guard's actual contract for a named flag, so a repair must confront it.
	if guardErr == nil {
		t.Logf("CONTRACT, established from existing code rather than from this behaviour: the " +
			"flagPassed short-circuit is DELIBERATE. requireEndpointAgreement's own doc comment " +
			"scopes it to the case where \"the operator did not name one on the command line\", and " +
			"nonCanonicalStoreURLNotice states Law 14 directly: a raw endpoint is " +
			"test/dev/maintenance authority which, \"if it is retained it must be explicit AND " +
			"visibly non-canonical\" — \"this does not take the escape away — an operator with a " +
			"reason keeps it\". So an explicit override is PERMITTED and must be VISIBLE, which it " +
			"is: nonCanonicalReaderNotice fires above. " +
			"What that notice says is the open obligation: \"nothing has verified that this " +
			"endpoint serves the graph for <domain>\". Category C is that later authority boundary. " +
			"Until a served-generation comparison lands, a healthy WRONG endpoint named explicitly " +
			"is accepted with a warning. This is recorded, not repaired here, and it is not a " +
			"defect of the operand-ordering family.")
	}
}

// POSITIVE CONTROL 3 — owner-resolution agreement, stated on its own. This is the direct opposite of
// the defective raw-flag comparison: with no override, the canonical owner chooses exactly the
// endpoint the project configuration names.
func TestTheOwnerWithNoOverrideChoosesTheConfiguredEndpoint(t *testing.T) {
	_, root, addr := orderingWorld(t)

	reader := productionReaderFor(emptyFlags(), root, orderingDomain, "")
	if reader.Addr != addr {
		t.Fatalf("the owner resolved %q, want the configured %q", reader.Addr, addr)
	}
	if reader.Overridden {
		t.Fatal("the owner reports an override with no flag passed")
	}
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.configuredServerAddr() != reader.Addr {
		t.Fatalf("configured %q != owner-resolved %q", cfg.configuredServerAddr(), reader.Addr)
	}
	// The guard is satisfied by that value. Both halves of the comparison the guard should be
	// making are available at the moment the owner returns.
	if err := requireServerAddrAgreement(emptyFlags(), root, reader.Addr); err != nil {
		t.Fatalf("the guard refused the owner-resolved address: %v", err)
	}
	t.Logf("OPPOSITE CONTROL: configured == owner-resolved == %s, and the guard accepts it. The "+
		"operation was never in disagreement; only the moment of comparison was wrong.", reader.Addr)
}

// OPERAND PROVENANCE. The guard's operand must be the owner's answer, named as such.
//
// This witness exists because one required mutant cannot be killed behaviourally. Passing *addr
// instead of reader.Addr at the guard is value-IDENTICAL on this tree: `*addr = reader.Addr` runs
// unconditionally two lines above, so no test can observe a difference between the two expressions.
// The distinction is real anyway — *addr is the operator's flag variable, which the migration pattern
// happens to overwrite, and reader.Addr is the endpoint the owner resolved. The regression this
// family repairs was precisely a guard reading the flag variable rather than the resolved endpoint.
//
// When behaviour cannot see a distinction, the source is the right place to pin it. Equivalence today
// is not a guarantee of equivalence after the next edit: remove or move `*addr = reader.Addr` and a
// guard reading *addr silently reverts to comparing against an empty flag.
func TestTheAgreementGuardsOperandIsTheOwnersResolvedAddress(t *testing.T) {
	src := readCmdSource(t, "cmd_preflight.go")

	if n := strings.Count(src, "requireServerAddrAgreement("); n != 1 {
		t.Fatalf("expected exactly one agreement call in cmd_preflight.go, found %d", n)
	}
	// The operand, by name.
	if !strings.Contains(src, "requireServerAddrAgreement(fs, graphConfigRoot(*repo), reader.Addr)") {
		t.Fatalf("the agreement guard must be called with the owner's resolved address and the " +
			"owner's config root: requireServerAddrAgreement(fs, graphConfigRoot(*repo), reader.Addr)")
	}
	// And never with the raw flag variable, whose value is only incidentally correct here.
	if strings.Contains(src, "requireServerAddrAgreement(fs, graphConfigRoot(*repo), *addr)") ||
		strings.Contains(src, "requireServerAddrAgreement(fs, preflightRoot, *addr)") {
		t.Fatal("the agreement guard is reading the raw --addr flag variable; that is the regression " +
			"this family repaired, and it is value-identical only while `*addr = reader.Addr` " +
			"immediately precedes it")
	}
	// The coupling that makes the two expressions equal today must still be present and adjacent,
	// because that is the only thing keeping the mutant equivalent rather than defective.
	adopt := strings.Index(src, "*addr = reader.Addr")
	guard := strings.Index(src, "requireServerAddrAgreement(")
	if adopt < 0 || adopt > guard {
		t.Fatal("`*addr = reader.Addr` no longer precedes the guard; re-examine whether the operand " +
			"mutant is still equivalent")
	}
	t.Log("OPERAND PINNED: the guard names reader.Addr and graphConfigRoot(*repo). The *addr mutant " +
		"is EQUIVALENT by value on this tree and is killed here textually, at the level where the " +
		"distinction actually lives.")
}
