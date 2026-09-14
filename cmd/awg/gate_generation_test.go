// SPDX-License-Identifier: AGPL-3.0-only

package main

// `sensei gate` enforces a graph-derived verdict, so law 5 applies to it: the verdict
// must be proven to come from the generation the registry declares ACTIVE for the domain.
//
// #361 wired gate to the G2 owner, so it no longer CHOOSES a port. It stopped there:
// gate resolved a graphReader carrying DeclaredGeneration and never asked whether the
// endpoint answering it served that generation. The reader census recorded gate under
// "cannot verify" because EditCheckResponse carries no GraphAuthority -- true of that
// message, and not true of the command, which holds a connection on which Metadata
// answers with one. That is the ninth instance on this front of a property a helper can
// prove that no caller is bound to.
//
// These drive the real runGate over a real gRPC server, because the defect was precisely
// that the helper was reachable and unreached.

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// gateAdversary is a healthy, valid-looking server. Its answers are well-formed; the
// only thing wrong with them is which generation produced them, which is the whole point
// -- "it answered" is the evidence law 2 exists to reject.
type gateAdversary struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	servedGeneration string
	omitAuthority    bool
	metadataFails    bool
	// republishAs is the generation the store serves once Metadata has answered: the
	// external store is republished mid-run, which no gRPC connection pins.
	republishAs string
	// restoreAfterEditCheck returns the store to its original generation once the verdict
	// has been produced, so the reviewer's switch-and-return is covered too.
	restoreAfterEditCheck bool
	restoreTo             string
	// omitAuthorityOnEditCheckOnly models an older server: Metadata states the generation,
	// the EditCheck response does not.
	omitAuthorityOnEditCheckOnly bool
	editChecks                   atomic.Int32
	metadataCalls                atomic.Int32
}

func (a *gateAdversary) Metadata(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
	a.metadataCalls.Add(1)
	if a.metadataFails {
		return nil, status.Error(codes.Unavailable, "the store is not loaded")
	}
	if a.omitAuthority {
		return &awarenesspb.MetadataResponse{}, nil
	}
	served := a.servedGeneration
	if a.republishAs != "" {
		// Answer this call honestly, THEN become another generation.
		a.servedGeneration, a.republishAs = a.republishAs, ""
	}
	return &awarenesspb.MetadataResponse{
		Authority: &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: served},
	}, nil
}

// EditCheck returns a BLOCKING warning, so a gate that reaches it enforces a verdict.
// Counting the calls is how "verification happens before enforcement" is proven: a
// refusal that still consulted the graph verdict would have verified too late.
func (a *gateAdversary) EditCheck(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	a.editChecks.Add(1)
	producedBy := a.servedGeneration
	if a.restoreAfterEditCheck && a.restoreTo != "" {
		// The rollback: by the time anything samples again, the store looks right.
		a.servedGeneration = a.restoreTo
	}
	var auth *awarenesspb.GraphAuthority
	if !a.omitAuthority && !a.omitAuthorityOnEditCheckOnly {
		auth = &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: producedBy}
	}
	return &awarenesspb.EditCheckResponse{
		RulesEvaluated: 1,
		Authority:      auth,
		Warnings: []*awarenesspb.EditWarning{{
			RuleId: "adversary.rule", Enforcement: "block", Message: "a verdict from the wrong generation",
		}},
	}, nil
}

func startGateAdversary(t *testing.T, a *gateAdversary) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, a)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

const gateWitnessDomain = "example.com/acme/thing"

// gateWorld writes a domain registry in a controlled HOME declaring `declared` ACTIVE,
// and a real git repository with one uncommitted change for gate to diff (--diff is a git
// RANGE, so "HEAD" means working tree vs HEAD). When configuredAddr is non-empty the
// project config names the endpoint, which exercises canonical resolution rather than an
// operator override.
func gateWorld(t *testing.T, declared, configuredAddr string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	gen := ""
	if declared != "" {
		gen = "\n        active_generation: " + declared
	}
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+gateWitnessDomain+`:
        repository_identity: acme/thing
        allowed_corpus_roots:
            - docs/awareness`+gen+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if configuredAddr != "" {
		writeProjectConfig(t, root, configuredAddr)
	}
	src := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"add", "-A"}, {"-c", "commit.gpgsign=false", "commit", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v: %s", err, out)
		}
	}
	// The uncommitted added line gate will evaluate.
	if err := os.WriteFile(src, []byte("package thing\n\nvar Added = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// productionReaderFor resolves the project root from the working directory, not from
	// --repo-root, so canonical resolution only sees this project's config from inside it.
	t.Chdir(root)
	return root
}

func gateArgs(root, addr string, extra ...string) []string {
	a := []string{"--repo-root", root, "--diff", "HEAD", "--domain", gateWitnessDomain}
	if addr != "" {
		a = append(a, "--addr", addr)
	}
	return append(a, extra...)
}

// 1. HEALTHY PATH: the served generation is the declared ACTIVE one, so gate behaves
// exactly as before -- it evaluates the diff and enforces the blocking verdict.
func TestGateEnforcesWhenTheServedGenerationIsTheDeclaredActiveOne(t *testing.T) {
	const gen = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	root := gateWorld(t, gen, "")
	a := &gateAdversary{servedGeneration: gen}
	addr := startGateAdversary(t, a)

	var code int
	out := captureStdout(t, func() { code = runGate(gateArgs(root, addr, "--enforce")) })
	if code == 0 {
		t.Fatalf("exit=0: the blocking verdict was not enforced on the healthy path")
	}
	if a.editChecks.Load() == 0 {
		t.Errorf("gate never evaluated the diff on the healthy path")
	}
	// The verdict must actually be RENDERED, not merely produce a non-zero exit. A refusal
	// also exits non-zero, so asserting the code alone cannot tell enforcement from refusal
	// -- and a binding that read the wrong authority field refused every run while this test
	// still passed.
	if !strings.Contains(out, "adversary.rule") {
		t.Errorf("the healthy path did not render the verdict it evaluated, so gate refused a generation that matched:\n%s", out)
	}
	if !strings.Contains(out, "BLOCKED") {
		t.Errorf("the healthy path did not report BLOCKED:\n%s", out)
	}
}

// 2. THE FINDING. A valid-looking verdict from a generation the domain does not declare
// ACTIVE must be refused -- and refused BEFORE the verdict is consulted at all.
func TestGateRefusesAVerdictFromAnUndeclaredGeneration(t *testing.T) {
	root := gateWorld(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	a := &gateAdversary{servedGeneration: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	addr := startGateAdversary(t, a)

	out := captureStdout(t, func() {
		if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
			t.Errorf("exit=0: gate enforced a verdict from a generation this domain does not declare ACTIVE")
		}
	})
	if a.editChecks.Load() != 0 {
		t.Errorf("gate consulted the graph verdict %d time(s) before refusing: verification is too late to matter",
			a.editChecks.Load())
	}
	if strings.Contains(out, "PASS") || strings.Contains(out, "adversary.rule") {
		t.Errorf("gate rendered the wrong generation's verdict:\n%s", out)
	}
}

// 3. A healthy service serving the wrong domain's graph is still the wrong graph.
func TestGateRefusesAHealthyServiceServingAnotherGeneration(t *testing.T) {
	// Well-formed, responsive, and answering from a graph published elsewhere. Reached
	// through CANONICAL resolution -- the project config names it, no operator override --
	// so this covers the resolution path the override witness below does not.
	a := &gateAdversary{servedGeneration: "2222222222222222222222222222222222222222222222222222222222222222"}
	addr := startGateAdversary(t, a)
	root := gateWorld(t, "1111111111111111111111111111111111111111111111111111111111111111", addr)
	if code := runGate(gateArgs(root, "", "--enforce")); code == 0 {
		t.Fatalf("exit=0: HTTP/gRPC health passed for graph identity")
	}
	if a.metadataCalls.Load() == 0 {
		t.Errorf("gate never asked the endpoint which generation it serves")
	}
}

// 4. NO FALLBACK. When the reachable endpoint serves another generation, gate must refuse
// rather than degrade into using it -- a fallback here would make the wrong graph the
// answer whenever the right one is down.
func TestGateDoesNotFallBackToAReachableWrongGeneration(t *testing.T) {
	root := gateWorld(t, "3333333333333333333333333333333333333333333333333333333333333333", "")
	a := &gateAdversary{servedGeneration: "4444444444444444444444444444444444444444444444444444444444444444"}
	addr := startGateAdversary(t, a)
	// --report-only is gate's fail-open advisory posture: the one mode that could be
	// argued into continuing. It must still not render the wrong generation's verdict.
	code := -1
	out := captureStdout(t, func() {
		code = runGate(gateArgs(root, addr, "--report-only"))
	})
	if a.editChecks.Load() != 0 {
		t.Errorf("report-only consulted the wrong generation's verdict %d time(s)", a.editChecks.Load())
	}
	if strings.Contains(out, "adversary.rule") {
		t.Errorf("report-only rendered the wrong generation's findings:\n%s", out)
	}
	// report-only's contract is to exit 0, and DEGRADED is how it says it produced no
	// verdict. Refusing by exiting non-zero here would break the advisory contract;
	// printing a verdict would break law 5. It must do neither.
	if code != 0 {
		t.Errorf("exit=%d: report-only must stay fail-open and exit 0", code)
	}
	if !strings.Contains(out, "DEGRADED") {
		t.Errorf("report-only did not state that it produced no verdict:\n%s", out)
	}
}

// 8. NOT KNOWING is not agreement. When the endpoint cannot say which generation it
// serves, gate has not proven the verdict belongs to this domain's ACTIVE graph, so it
// must refuse. A mutant that returned nil here survived every other witness.
func TestGateRefusesWhenTheServedGenerationCannotBeObtained(t *testing.T) {
	a := &gateAdversary{metadataFails: true}
	addr := startGateAdversary(t, a)
	root := gateWorld(t, "9999999999999999999999999999999999999999999999999999999999999999", "")
	if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
		t.Fatalf("exit=0: a verdict was enforced although which graph produced it could not be established")
	}
	if a.editChecks.Load() != 0 {
		t.Errorf("gate evaluated the diff although it could not establish the generation")
	}
}

// 5. An explicit non-canonical --addr is an operator naming an endpoint, not a licence to
// skip proving which generation it serves. The override changes WHERE, never WHETHER.
func TestGateVerifiesTheGenerationEvenUnderAnExplicitAddr(t *testing.T) {
	root := gateWorld(t, "5555555555555555555555555555555555555555555555555555555555555555", "")
	a := &gateAdversary{servedGeneration: "6666666666666666666666666666666666666666666666666666666666666666"}
	addr := startGateAdversary(t, a)
	// The same flag an operator would use to point gate somewhere deliberately.
	if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
		t.Fatalf("exit=0: a named endpoint bypassed generation verification")
	}
	if a.editChecks.Load() != 0 {
		t.Errorf("an explicit --addr skipped verification and reached the verdict")
	}
}

// 6. MISSING AUTHORITY, per the actual contract rather than a guess.
// verifyActiveGeneration compares a declared generation against the served one and an
// absent served value is not equal to it, so a response carrying no GraphAuthority
// REFUSES. That is the direction the Phase 7 proof demanded: the served-digest field was
// blank exactly when the served graph was wrong.
func TestGateRefusesWhenTheEndpointStatesNoAuthority(t *testing.T) {
	root := gateWorld(t, "7777777777777777777777777777777777777777777777777777777777777777", "")
	a := &gateAdversary{omitAuthority: true}
	addr := startGateAdversary(t, a)
	if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
		t.Fatalf("exit=0: an endpoint that stated no generation was treated as serving the right one")
	}
	if a.editChecks.Load() != 0 {
		t.Errorf("gate enforced a verdict from an endpoint that named no generation")
	}
}

// 7. INERT WITHOUT A DECLARATION. An absent ACTIVE pointer contradicts nothing, so gate
// must behave exactly as it did before this check existed. Without this, the repair could
// pass every test above by refusing unconditionally.
func TestGateIsUnchangedWhenNoGenerationIsDeclared(t *testing.T) {
	root := gateWorld(t, "", "")
	a := &gateAdversary{servedGeneration: "8888888888888888888888888888888888888888888888888888888888888888"}
	addr := startGateAdversary(t, a)
	if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
		t.Fatalf("exit=0: the blocking verdict was not enforced when nothing is declared ACTIVE")
	}
	if a.editChecks.Load() == 0 {
		t.Errorf("gate refused although the registry declares no ACTIVE generation to disagree with")
	}
}

// THE RE-REVIEW FINDING (P1, cmd_gate.go:75): "Bind each gate verdict to the verified
// generation."
//
// One Metadata call verified G and every subsequent EditCheck could be answered by G+1: the
// connection pins the ENDPOINT, never the external store's contents. The reviewer asked for
// exactly this witness -- change the served generation after Metadata and before EditCheck --
// and it reproduced: gate printed [BLOCK] from the republished generation.
//
// DOMAIN OF THE CLAIM, now that EditCheckResponse carries its own authority: the generation
// reported is the one that COMPUTED those warnings, so the binding covers the verdict itself
// rather than an interval around it. Nothing is inferred from a sample taken at another
// moment.
func TestGateRefusesAVerdictProducedByAGenerationOtherThanTheDeclaredOne(t *testing.T) {
	const declared = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	a := &gateAdversary{
		servedGeneration: declared,
		republishAs:      "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	addr := startGateAdversary(t, a)
	root := gateWorld(t, declared, "")

	out := captureStdout(t, func() {
		if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
			t.Errorf("exit=0: a verdict produced after the store was republished was enforced")
		}
	})
	if a.editChecks.Load() == 0 {
		t.Fatalf("the fixture never reached EditCheck, so it proves nothing about verdict binding")
	}
	if strings.Contains(out, "adversary.rule") || strings.Contains(out, "BLOCKED") {
		t.Errorf("gate rendered a verdict produced by a foreign generation:\n%s", out)
	}
}

// SWITCH AND RETURN. The store moves to another generation, produces the verdict, and is
// back before anything could sample again. A pre-loop sample and an adjacent post-loop
// sample both see the declared generation; only the verdict's own authority shows the truth.
func TestGateRefusesAVerdictWhoseGenerationSwitchedAndReturned(t *testing.T) {
	const declared = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	a := &gateAdversary{
		servedGeneration:      declared,
		republishAs:           "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		restoreAfterEditCheck: true,
		restoreTo:             declared,
	}
	addr := startGateAdversary(t, a)
	root := gateWorld(t, declared, "")

	out := captureStdout(t, func() {
		if code := runGate(gateArgs(root, addr, "--enforce")); code == 0 {
			t.Errorf("exit=0: a switch-and-return produced an enforced verdict")
		}
	})
	if strings.Contains(out, "adversary.rule") || strings.Contains(out, "BLOCKED") {
		t.Errorf("gate rendered a verdict from a generation that had already been rolled back:\n%s", out)
	}
}

// A response that states NO generation cannot support an enforced verdict. Absence is not
// agreement -- and an older server is exactly how absence arrives.
func TestGateRefusesAVerdictThatStatesNoGeneration(t *testing.T) {
	const declared = "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"
	// Metadata answers with the declared generation; the EditCheck response omits authority.
	a := &gateAdversary{servedGeneration: declared, omitAuthority: false}
	addr := startGateAdversary(t, a)
	root := gateWorld(t, declared, "")
	a.omitAuthorityOnEditCheckOnly = true
	// A non-zero exit is NOT the assertion: BLOCKED is also non-zero, so asserting it
	// could not fail whether or not the binding exists. What must be absent is the
	// VERDICT -- gate must refuse rather than enforce.
	out := captureStdout(t, func() { _ = runGate(gateArgs(root, addr, "--enforce")) })
	if strings.Contains(out, "adversary.rule") || strings.Contains(out, "BLOCKED") {
		t.Errorf("a verdict carrying no generation was enforced:\n%s", out)
	}
}
