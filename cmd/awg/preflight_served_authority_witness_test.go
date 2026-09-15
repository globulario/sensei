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

// THE WITNESS. Expected authority exists, served authority exists, they differ, and runPreflight
// consumes the response as an authoritative verdict.
//
// This test asserts the DEFECT. It is the mechanical record that the invariant does not hold on
// 8e3366f6. Repairing category C must INVERT it: the mismatch must then produce a refusal, and the
// assertions below marked DEFECT must become assertions of that refusal.
func TestPreflightConsumesAServedGenerationThatContradictsTheDeclaredOne(t *testing.T) {
	svc, _ := witnessWorld(t, witnessDeclared)

	var code int
	out := captureStdout(t, func() {
		code = runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})

	// The command really did reach the real service, and really did ask about the governed domain.
	if svc.calls != 1 {
		t.Fatalf("the service was called %d times; the witness never reached the real path", svc.calls)
	}
	if svc.sawDomain != witnessDomain {
		t.Fatalf("the service was asked about %q, want %q", svc.sawDomain, witnessDomain)
	}

	// DEFECT 1 — the command succeeds.
	if code != 0 {
		t.Fatalf("runPreflight returned %d; the defect this witness records is that it returns 0.\n"+
			"If category C has been repaired, invert this witness: require the refusal here.\nout:\n%s",
			code, out)
	}

	// DEFECT 2 — it reports the graph as fully authoritative.
	if !strings.Contains(out, "Authority: authoritative (current") {
		t.Fatalf("expected the served graph to be reported authoritative and current; got:\n%s", out)
	}

	// DEFECT 3 — it prints the served generation, so the contradiction was in hand and unexamined.
	if !strings.Contains(out, witnessServed) {
		t.Fatalf("expected the served generation %s in the output; got:\n%s", witnessServed, out)
	}
	if strings.Contains(out, witnessDeclared) {
		t.Fatalf("the declared generation appeared in the output, so preflight does surface the "+
			"expectation after all; re-measure before repairing.\nout:\n%s", out)
	}

	// DEFECT 4 — the authoritative payload was consumed and reported.
	if !strings.Contains(out, "Risk: ARCHITECTURE_SENSITIVE") || !strings.Contains(out, "path_test.go:TestReloadFresh") {
		t.Fatalf("expected the authoritative verdict and required tests to be consumed; got:\n%s", out)
	}

	t.Logf("DEFECT REPRODUCED AS STATED.\n"+
		"  governed domain      %s (resolved from the repository)\n"+
		"  declared generation  %s (registry, non-empty)\n"+
		"  served generation    %s (healthy, self-consistent, CURRENT, self-certified authoritative)\n"+
		"  result               exit 0, reported authoritative, verdict and required tests consumed\n"+
		"  comparison           none — the served generation was printed and never checked",
		witnessDomain, witnessDeclared, witnessServed)
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

// EMPTY-EXPECTATION CONTROL. The governed domain resolves perfectly, the owner carries it, and the
// registry declares no active_generation for it — the shape github.com/globulario/services has in
// the real operator registry today.
//
// The state this records is `expectation absent / comparison not established`. It is NOT
// verification: verifyServed returns nil against an empty expectation, so a check added here would
// accept the same wrong generation the witness above proves is accepted now.
//
// This control fixes no policy. It exists so that a later repair cannot become structurally inert
// without a test noticing.
func TestPreflightWithNoDeclaredGenerationHasNoEstablishedComparison(t *testing.T) {
	svc, _ := witnessWorld(t, "") // registry entry present and well formed, generation absent

	reader := productionReaderFor(emptyFlags(), ".", witnessDomain, "")
	if reader.Domain != witnessDomain {
		t.Fatalf("the owner carried domain %q, want %q", reader.Domain, witnessDomain)
	}
	if reader.DeclaredGeneration != "" {
		t.Fatalf("declared generation = %q, want empty", reader.DeclaredGeneration)
	}
	// The comparison a category-C repair would add, exercised directly against the wrong
	// generation. It does not refuse, and it does not report that it could not decide.
	if err := reader.verifyServed(witnessServed); err != nil {
		t.Fatalf("verifyServed refused against an empty expectation: %v", err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runPreflight([]string{"--task", "raise the risk class of a protected file"})
	})
	if svc.calls != 1 || code != 0 {
		t.Fatalf("calls=%d code=%d; expected the command to run normally.\nout:\n%s", svc.calls, code, out)
	}

	t.Logf("EXPECTATION ABSENT / COMPARISON NOT ESTABLISHED.\n"+
		"  governed domain      %s (resolved correctly, carried by the owner)\n"+
		"  declared generation  <absent from the registry entry>\n"+
		"  served generation    %s\n"+
		"  verifyServed         returns nil — silently skips, and reports nothing to the caller\n"+
		"  therefore            this state must NOT be recorded as `verified`; a comparison added\n"+
		"                       here is structurally inert for every domain in this shape",
		witnessDomain, witnessServed)
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
