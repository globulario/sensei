// SPDX-License-Identifier: AGPL-3.0-only

package main

// CATEGORY C FAILING WITNESS — runPreflight.
//
// The invariant under test:
//
//	A command must not consume graph-derived authoritative output as verified when it has a
//	non-empty declared generation and the served generation differs.
//
// runPreflight is the strongest case for this invariant, because nothing else is missing. It
// resolves its governed domain from the repository before resolving the reader; the owner hands it a
// declared generation from the registry; the response carries a served generation; and
// printGraphAuthority RENDERS that served generation to the operator. The one thing it never does is
// compare the two.
//
// These witnesses run the real runPreflight against a real gRPC service over a real socket. Nothing
// is stubbed at the seam under test.
//
// WHAT IS DELIBERATELY NOT USED, so the witness isolates only the missing comparison:
// the service is reachable; the response is well formed and internally self-consistent; the
// GraphAuthority stamp is present and self-certified CURRENT and authoritative; the expected
// generation is non-empty; the endpoint is the canonical one the owner resolved from the registry.
// The ONLY defect in the world these tests build is which generation answered.

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
)

const (
	witnessDomain   = "example.com/acme/witness"
	witnessDeclared = "1111111111111111111111111111111111111111111111111111111111111111"
	witnessServed   = "2222222222222222222222222222222222222222222222222222222222222222"
)

// servedAuthorityService is a HEALTHY awareness graph. It answers Preflight with a complete,
// coherent, self-certified-current response whose only peculiarity is the generation it serves.
type servedAuthorityService struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	servedGeneration string
	calls            int
	sawDomain        string
}

func (s *servedAuthorityService) Preflight(_ context.Context, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	s.calls++
	s.sawDomain = req.GetDomain()
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
		Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
		Authority: &awarenesspb.GraphAuthority{
			// Maximally trustworthy by its own account: InterpretAuthority returns
			// Authoritative=true with no warning for exactly this combination.
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			// Internally self-consistent: the live store's digest and the seed it was built
			// from agree. This graph is not broken, not stale by its own reckoning, and not
			// lying about itself. It is simply a different generation.
			LiveStoreGraphDigestSha256:      s.servedGeneration,
			EmbeddedSeedDigestSha256:        s.servedGeneration,
			LiveStoreGraphTripleCount:       4211,
			GraphBuildCommit:                "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c",
			EmbeddedTransactionStampPresent: true,
			EmbeddedTransactionMatchesSeed:  true,
		},
		Coverage: &awarenesspb.CoverageSummary{
			Sufficient:        true,
			DirectAnchorCount: 7,
			Note:              "direct anchors resolved",
		},
		RequiredActions: []string{"run the named tests"},
		ForbiddenFixes:  []string{"do not cache the reload path"},
		TestsToRun:      []string{"path_test.go:TestReloadFresh"},
	}, nil
}

// witnessWorld builds the whole world: a healthy service, a registry declaring the generation the
// operator expects, and a repository that states its governed domain.
//
// The project config deliberately does NOT state server.addr. runPreflight calls
// requireServerAddrAgreement with the RAW --addr flag, before the owner has resolved anything, so a
// configured server.addr is compared against "" and refuses. That ordering is a separate question
// (see TestObservedTheAddrAgreementCheckRunsBeforeTheOwnerResolves) and is not this witness's
// subject; the fixture stays clear of it so that only the generation comparison is under test.
func witnessWorld(t *testing.T, declaredGeneration string) (svc *servedAuthorityService, root string) {
	t.Helper()

	svc = &servedAuthorityService{servedGeneration: witnessServed}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	addr := lis.Addr().String()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	generation := ""
	if declaredGeneration != "" {
		generation = "\n        active_generation: " + declaredGeneration
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+witnessDomain+`:
        repository_identity: acme/witness
        allowed_corpus_roots:
            - docs/awareness
        service_addr: `+addr+generation+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	root = projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+witnessDomain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	// PRECONDITIONS, asserted rather than assumed. If any of these stops holding, the witness is
	// measuring something else and says so instead of quietly passing.
	if got := resolveRepositoryDomain(".", "").Domain; got != witnessDomain {
		t.Fatalf("governed domain resolved to %q, want %q", got, witnessDomain)
	}
	reader := productionReaderFor(emptyFlags(), ".", witnessDomain, "")
	if reader.Addr != addr {
		t.Fatalf("the owner resolved %q, want the registry's service_addr %q", reader.Addr, addr)
	}
	if reader.Overridden {
		t.Fatal("the reader reports an override; the witness must exercise the canonical path")
	}
	if reader.DeclaredGeneration != declaredGeneration {
		t.Fatalf("declared generation = %q, want %q", reader.DeclaredGeneration, declaredGeneration)
	}
	return svc, root
}

// THE INVERTED WITNESS. Formerly TestPreflightConsumesAServedGenerationThatContradictsTheDeclaredOne,
// which recorded that the mismatch was consumed. It now requires the refusal, and requires it BEFORE
// any part of the authoritative payload is consumed.
//
// The fixture is unchanged. Only the required outcome moved.
func TestPreflightRefusesAServedGenerationThatContradictsTheDeclaredOne(t *testing.T) {
	svc, _ := witnessWorld(t, witnessDeclared)

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})
	out := stdout + stderr

	// CLAIM: the real path was reached and the real domain was asked about.
	if svc.calls != 1 {
		t.Fatalf("the service was called %d times; the witness never reached the real path", svc.calls)
	}
	if svc.sawDomain != witnessDomain {
		t.Fatalf("the service was asked about %q, want %q", svc.sawDomain, witnessDomain)
	}

	// CLAIM: the command refused.
	if code == 0 {
		t.Fatalf("runPreflight returned 0 on a served generation the registry does not declare "+
			"ACTIVE.\nout:\n%s", out)
	}

	// CLAIM: it refused for the MISMATCH reason, distinctly from an absent declaration.
	if !strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("expected the mismatch refusal; got:\n%s", out)
	}
	if strings.Contains(out, "not established") {
		t.Fatalf("the refusal used the NOT_ESTABLISHED wording for a MISMATCH; the two states must "+
			"not share a message.\nout:\n%s", out)
	}
	// CLAIM: both generations are named, so an operator can act.
	if !strings.Contains(out, witnessDeclared) || !strings.Contains(out, witnessServed) {
		t.Fatalf("the refusal does not name both generations.\nout:\n%s", out)
	}

	// CLAIM: the refusal came BEFORE consumption. None of the authoritative payload was reported.
	for _, leaked := range []string{"ARCHITECTURE_SENSITIVE", "path_test.go:TestReloadFresh",
		"do not cache the reload path", "Authority: authoritative"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("the authoritative payload was consumed despite the refusal (%q appeared), so "+
				"the comparison ran after consumption.\nout:\n%s", leaked, out)
		}
	}

	t.Logf("INVARIANT ESTABLISHED (preflight/MISMATCH): declared %s, served %s, exit %d, refused "+
		"with the mismatch wording, no payload consumed.", witnessDeclared, witnessServed, code)
}

// OPPOSITE WITNESS (positive control). Same domain, same reader, same response shape, same service.
// The ONLY difference from the witness above is that the served generation equals the declared one.
// This case must remain accepted after the repair, so it is what proves a future refusal is aimed at
// the mismatch and not at the fixture.
func TestPreflightAcceptsAServedGenerationThatMatchesTheDeclaredOne(t *testing.T) {
	svc, _ := witnessWorld(t, witnessServed) // declared == served
	if svc.servedGeneration != witnessServed {
		t.Fatalf("served generation = %q, want %q", svc.servedGeneration, witnessServed)
	}

	var code int
	out := captureStdout(t, func() {
		code = runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})

	if svc.calls != 1 {
		t.Fatalf("the service was called %d times", svc.calls)
	}
	if code != 0 {
		t.Fatalf("the matched-generation control returned %d, want 0; the repair must not refuse "+
			"this case.\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "Authority: authoritative (current") || !strings.Contains(out, "Risk: ARCHITECTURE_SENSITIVE") {
		t.Fatalf("expected the matched case to be accepted and consumed; got:\n%s", out)
	}
	t.Logf("MATCHED CONTROL: declared == served == %s, accepted and consumed. The only difference "+
		"from the failing witness is the generation identity.", witnessServed)
}

// MISSING-EXPECTATION CONTROL, inverted. Formerly recorded that an absent declaration silently
// passed; it now requires a refusal that is distinguishable from a mismatch.
//
// This matters beyond the fixture: github.com/globulario/services declares no active_generation
// today, so this is the live shape, not a hypothetical.
func TestPreflightRefusesWhenNoActiveGenerationIsDeclared(t *testing.T) {
	svc, _ := witnessWorld(t, "") // registry entry present and well formed, generation absent

	reader := productionReaderFor(emptyFlags(), ".", witnessDomain, "")
	if reader.Domain != witnessDomain {
		t.Fatalf("the owner carried domain %q, want %q", reader.Domain, witnessDomain)
	}
	if reader.DeclaredGeneration != "" {
		t.Fatalf("declared generation = %q, want empty", reader.DeclaredGeneration)
	}

	// CLAIM: the state is NOT_ESTABLISHED, and it is not VERIFIED.
	state, err := reader.classifyServedGeneration(witnessServed)
	if state != generationNotEstablished {
		t.Fatalf("state = %v, want NOT_ESTABLISHED", state)
	}
	if state == generationVerified {
		t.Fatal("an absent declaration was classified VERIFIED")
	}
	if err == nil {
		t.Fatal("NOT_ESTABLISHED produced no refusal, so a consumer can reach the payload by testing " +
			"err == nil")
	}
	// CLAIM: verifyServed still permits it, so the distinction lives in the new guard rather than in
	// a changed meaning for the reporting commands.
	if verr := reader.verifyServed(witnessServed); verr != nil {
		t.Fatalf("verifyServed now refuses an absent declaration (%v); runMetadata and runBriefing "+
			"depend on the reporting reading and this family did not authorize changing it", verr)
	}

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})
	out := stdout + stderr

	if svc.calls != 1 {
		t.Fatalf("the service was called %d times.\nout:\n%s", svc.calls, out)
	}
	if code == 0 {
		t.Fatalf("runPreflight returned 0 with no ACTIVE generation declared; NOT_ESTABLISHED is not "+
			"verification.\nout:\n%s", out)
	}
	// CLAIM: the refusal names ABSENCE, distinctly from disagreement.
	if !strings.Contains(out, "not established") || !strings.Contains(out, "no ACTIVE generation is declared") {
		t.Fatalf("expected the NOT_ESTABLISHED refusal wording.\nout:\n%s", out)
	}
	if strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("an absent declaration was reported as a mismatch.\nout:\n%s", out)
	}
	// CLAIM: nothing was consumed.
	for _, leaked := range []string{"ARCHITECTURE_SENSITIVE", "path_test.go:TestReloadFresh", "Authority: authoritative"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("the payload was consumed despite NOT_ESTABLISHED (%q).\nout:\n%s", leaked, out)
		}
	}
	t.Logf("INVARIANT ESTABLISHED (preflight/NOT_ESTABLISHED): no declaration, exit %d, refused with "+
		"absence wording distinct from mismatch, no payload consumed. verifyServed still permits it, "+
		"so the reporting commands are unchanged.", code)
}

// OBSERVATION, not a subject of this step. requireServerAddrAgreement is called with the RAW --addr
// flag before productionReaderFor resolves anything, so a project config that states server.addr is
// compared against "" and the command refuses. Recorded because it dictated the fixture's shape
// above and because a category-C repair will be written on this same path.
//
// No production behaviour is changed and nothing is asserted about whether this is correct.
func TestObservedTheAddrAgreementCheckRunsBeforeTheOwnerResolves(t *testing.T) {
	_, root := witnessWorld(t, witnessDeclared)

	// Add the one thing the witness fixture omits.
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+witnessDomain+"\nserver:\n    addr: 127.0.0.1:19999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})
	t.Logf("OBSERVED with server.addr configured and --addr omitted: exit %d.\nstdout:\n%s", code, out)
}
