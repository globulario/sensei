// SPDX-License-Identifier: AGPL-3.0-only

package main

// THE SERVED-GENERATION AUTHORITY FAMILY: the witnesses.
//
// docs/architecture/served-generation-authority.md derives the census (20 production
// graph-reading subjects, 20/20 resolving the endpoint through the owner, 7 comparing the
// served generation, 13 not) and groups the 13 into three consumption shapes.
//
// Each of these began as a COUNTEREXAMPLE -- it drove a real command and showed
// graph-derived information consumed as authoritative while the generation that produced it
// was one the domain does not declare ACTIVE -- and each is paired with its OPPOSITE, which
// proves the command still does its job when the generation agrees. A refusal with no
// opposite witness is indistinguishable from a command that stopped working.
//
// Every adversary here is HEALTHY. It answers promptly, its responses are well-formed,
// and where the response carries a self-certification it certifies itself as
// authoritative and CURRENT. The only thing wrong with any of them is WHICH GENERATION
// produced the answer -- which is the whole point: "it answered" is the evidence law 2
// exists to reject, and a graph that certifies itself is not a graph anyone compared.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
)

const (
	servedWitnessDomain = "example.com/acme/served"
	declaredGen         = "1111111111111111111111111111111111111111111111111111111111111111"
	foreignGen          = "2222222222222222222222222222222222222222222222222222222222222222"
)

// servedAdversary answers Preflight, Impact, EditCheck and Metadata as a healthy server
// that self-certifies. servedGeneration is the generation it actually served.
type servedAdversary struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	servedGeneration string
	// omitAuthority models an older server: a well-formed answer carrying no authority at
	// all. Absence must be refused rather than skipped when a generation IS declared.
	omitAuthority bool
	// noWarnings makes EditCheck answer CLEAN, which is the dangerous answer for a surface
	// whose output an agent reads as permission.
	noWarnings bool
	// statesTopLevelDigest makes Metadata state live_store_graph_digest_sha256 at the TOP level.
	// Combined with omitAuthority it is a server built before GraphAuthority existed on this
	// message: the canonical served identity is present, the auxiliary structure is not.
	statesTopLevelDigest bool
	// republishAfterMetadata is the store being republished mid-run: Metadata answers
	// honestly with the declared generation, and every later call is answered by another
	// one. Nothing about a gRPC connection pins a generation, so a command that verified
	// ONCE and then made a second call has verified the wrong response.
	republishAfterMetadata string
}

func (a *servedAdversary) authority() *awarenesspb.GraphAuthority {
	if a.omitAuthority {
		return nil
	}
	// Self-certified as good as it gets: this is exactly what requireAuthoritativeGraph
	// asks for, so any subject relying on that helper alone lets this through.
	return &awarenesspb.GraphAuthority{
		Authoritative:              true,
		GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		LiveStoreGraphDigestSha256: a.servedGeneration,
	}
}

func (a *servedAdversary) Preflight(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	return &awarenesspb.PreflightResponse{
		RiskClass: awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
		Authority: a.authority(),
	}, nil
}

func (a *servedAdversary) Metadata(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
	auth := a.authority()
	served := a.servedGeneration
	if a.republishAfterMetadata != "" {
		// Answer THIS call honestly, then become another generation.
		a.servedGeneration, a.republishAfterMetadata = a.republishAfterMetadata, ""
	}
	resp := &awarenesspb.MetadataResponse{
		AvailableDomains: []string{servedWitnessDomain},
		Authority:        auth,
	}
	if a.statesTopLevelDigest {
		resp.LiveStoreGraphDigestSha256 = served
	}
	return resp, nil
}

// EditCheck returns a BLOCKING warning, so a guard that reaches it denies a write.
func (a *servedAdversary) EditCheck(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	if a.noWarnings {
		return &awarenesspb.EditCheckResponse{RulesEvaluated: 3, Authority: a.authority()}, nil
	}
	return &awarenesspb.EditCheckResponse{
		RulesEvaluated: 1,
		Authority:      a.authority(),
		Warnings: []*awarenesspb.EditWarning{{
			RuleId:      "adversary.forbidden",
			Severity:    "critical",
			Class:       "ForbiddenFix",
			Enforcement: "block",
			Message:     "a rule from a generation this domain does not declare active",
		}},
	}, nil
}

func startServedAdversary(t *testing.T, a *servedAdversary) string {
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

// servedWorld writes a domain registry in a controlled HOME declaring `declared` ACTIVE
// for servedWitnessDomain, and a project whose own config states BOTH that domain and the
// endpoint -- so a command resolves its domain and its endpoint through the owner, with no
// flag. Deliberately not an --addr override: an override is recorded as non-canonical, and
// a family about trusting what was served should not be proven only under the one input
// that announces itself as unverified.
//
// It also writes the repo-local proof inputs repair-plan loads BEFORE it reaches any
// authority gate. Without them the command exits 1 on a missing file, which a reproducer
// asserting "non-zero" would read as the gate firing.
//
// Returns the project root, and chdirs into it because productionReaderFor resolves the
// project root from the working directory.
func servedWorld(t *testing.T, declared, addr string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	gen := ""
	if declared != "" {
		gen = "\n        active_generation: " + declared
	}
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+servedWitnessDomain+`:
        repository_identity: acme/served
        allowed_corpus_roots:
            - docs/awareness`+gen+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "repository:\n    domain: " + servedWitnessDomain + "\n"
	if addr != "" {
		cfg += "server:\n    addr: " + addr + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join("docs", "awareness", "candidates", "authority_surface_candidates.yaml"): "authority_surfaces: []\n",
		filepath.Join("docs", "awareness", "generated", "proof_obligations.yaml"):             "obligations: []\n",
		filepath.Join("docs", "awareness", "architecture", "forbidden_fixes.yaml"):            "forbidden_fixes: []\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

// --- SHAPE A -----------------------------------------------------------------------
//
// The subject knows its domain, receives the authority, and never compares it.

// A1. THE SHARPEST CASE IN THE FAMILY. `repair-plan` documents itself as failing closed
// "if the server cannot prove the graph is current and authoritative", and it enforces
// that with requireAuthoritativeGraph -- which asks whether the graph certifies ITSELF.
// A graph can answer that perfectly and still be the wrong generation for this domain, so
// the existing shared seam is INSUFFICIENT rather than absent. The plan an implementer
// then executes was built from rules this domain does not declare active.
func TestRepairPlanConsumesAPlanBuiltFromAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen})
	root := servedWorld(t, declaredGen, addr)

	var code int
	var out string
	stderr := captureStderr(t, func() {
		out = captureStdout(t, func() {
			code = runRepairPlan([]string{"--repo-root", root, "--task", "repair the thing",
				"--domain", servedWitnessDomain})
		})
	})
	if code == 0 {
		t.Fatalf("repair-plan exited 0 on a plan built from generation %s while this domain declares "+
			"%s ACTIVE:\n%s", foreignGen, declaredGen, out)
	}
	// The exit code alone cannot tell a refusal from any other failure -- this command exits
	// 1 for a dozen reasons. What must be true is that NO PLAN WAS RENDERED: the defect was
	// a complete, confident plan printed beside the wrong live_digest.
	if strings.Contains(out, "repair the thing") || strings.Contains(out, foreignGen) {
		t.Errorf("a plan from the undeclared generation still reached stdout:\n%s", out)
	}
	if !strings.Contains(stderr, declaredGen) || !strings.Contains(stderr, foreignGen) {
		t.Errorf("the refusal does not name both generations, so an operator cannot act on it: %s", stderr)
	}
}

// A1-opposite. The same command, the same declaration, everything the same -- except the
// graph serves the generation the domain declares. It must produce the plan. Without this, a
// repair that refused unconditionally would pass A1 and be indistinguishable from one that
// works.
func TestRepairPlanBuildsThePlanWhenTheServedGenerationIsTheDeclaredOne(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: declaredGen})
	root := servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() {
		code = runRepairPlan([]string{"--repo-root", root, "--task", "repair the thing",
			"--domain", servedWitnessDomain})
	})
	if code != 0 {
		t.Fatalf("repair-plan refused a graph serving the declared ACTIVE generation (exit=%d):\n%s", code, out)
	}
	if !strings.Contains(out, "repair the thing") {
		t.Errorf("no plan was produced on the healthy path:\n%s", out)
	}
}

// A2. `edit-guard` BLOCKS a write. A deny derived from another generation's forbidden-fix
// rules is the strongest authoritative consumption in the family: it stops work, and the
// agent it stops cannot see which graph decided.
func TestEditGuardDeniesAWriteOnRulesFromAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_input": map[string]any{"file_path": target, "content": "package thing\n\nvar X = 1\n"},
	})

	// Both output adapters, because each is a separate path to the agent and therefore a
	// separate chance to emit the denial. `claude` writes the deny JSON to stdout; `exit-code`
	// writes the reason to STDERR -- and a mutant that decided first and verified afterwards
	// survived a witness that read only stdout and the exit code.
	for _, format := range []string{"claude", "exit-code"} {
		var code int
		var out string
		stderr := captureStderr(t, func() {
			code, out = runGuardCode(t, []string{"--root", root, "--domain", servedWitnessDomain,
				"--format", format}, string(payload))
		})
		// The guard fails OPEN by contract, so the edit proceeds. What changed is that the
		// BLOCK is withheld -- a denial derived from another generation's forbidden-fix rules
		// no longer stops work this domain does not forbid.
		if code != 0 {
			t.Errorf("--format %s: edit-guard wedged the edit (exit=%d) instead of failing open:\n%s",
				format, code, out)
		}
		for where, text := range map[string]string{"stdout": out, "stderr": stderr} {
			if strings.Contains(text, "adversary.forbidden") || strings.Contains(text, "deny") {
				t.Errorf("--format %s: the denial from generation %s reached %s while this domain "+
					"declares %s ACTIVE:\n%s", format, foreignGen, where, declaredGen, text)
			}
		}
	}
}

// A2-opposite. The guard must still BLOCK when the rule comes from the declared ACTIVE
// generation. A repair that simply stopped blocking would pass A2 and destroy the command.
func TestEditGuardStillBlocksOnRulesFromTheDeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: declaredGen})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_input": map[string]any{"file_path": target, "content": "package thing\n\nvar X = 1\n"},
	})

	code, out := runGuardCode(t, []string{"--root", root, "--domain", servedWitnessDomain,
		"--format", "exit-code"}, string(payload))
	if code != 2 {
		t.Fatalf("edit-guard did not block a critical rule match from the declared ACTIVE "+
			"generation (exit=%d):\n%s", code, out)
	}
}

// A3. Absence is a third outcome, not a skipped check. A server that carries NO authority
// leaves which generation answered unestablished, and unestablished must never read as
// established.
func TestRepairPlanConsumesAnAnswerThatStatesNoGenerationAtAll(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen, omitAuthority: true})
	root := servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() {
		code = runRepairPlan([]string{"--repo-root", root, "--task", "repair the thing",
			"--domain", servedWitnessDomain})
	})
	// requireAuthoritativeGraph DOES refuse a nil authority, so this one is expected to be
	// refused today -- by the internal-authority gate, for its own reason. The assertion
	// records which gate fired, so the repair cannot be credited to the wrong one.
	t.Logf("absent-authority outcome today: exit=%d out=%q", code, out)
	if code == 0 {
		t.Fatalf("a plan was built from a response that stated no generation at all:\n%s", out)
	}
}

// A4. Absence, on a subject that has NO self-certification gate. A3 is refused today, but
// by requireAuthoritativeGraph and for its own reason -- and only four of the thirteen call
// it. The other nine consume an answer whose generation is entirely unestablished. Here a
// write is BLOCKED on rules from a graph that stated no generation at all.
func TestEditGuardDeniesAWriteOnRulesWhoseGenerationIsUnestablished(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen, omitAuthority: true})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"tool_input": map[string]any{"file_path": target, "content": "package thing\n\nvar X = 1\n"},
	})

	for _, format := range []string{"claude", "exit-code"} {
		var code int
		var out string
		stderr := captureStderr(t, func() {
			code, out = runGuardCode(t, []string{"--root", root, "--domain", servedWitnessDomain,
				"--format", format}, string(payload))
		})
		if code != 0 {
			t.Errorf("--format %s: edit-guard wedged the edit (exit=%d):\n%s", format, code, out)
		}
		for where, text := range map[string]string{"stdout": out, "stderr": stderr} {
			if strings.Contains(text, "adversary.forbidden") {
				t.Errorf("--format %s: a rule from a response that established no generation "+
					"reached %s, while this domain declares %s ACTIVE:\n%s",
					format, where, declaredGen, text)
			}
		}
	}
}

// --- SHAPE B -----------------------------------------------------------------------

// B1. Four subjects hand the owner an EMPTY domain, so the comparison is not omitted --
// it is structurally impossible. declaredActiveGeneration reads reg.Domains[""], the zero
// value, so DeclaredGeneration is "" and verifyServed is permanently inert. Adding the
// check at those call sites would change nothing, which is why they are a separate repair
// shape: the domain must be resolved first.
//
// The same project resolves a domain that DOES declare a generation, which is what makes
// this a defect rather than a configuration gap.
func TestASubjectThatPassesNoDomainStillGetsAResolvedExpectedDomain(t *testing.T) {
	root := servedWorld(t, declaredGen, "")

	if got := resolveRepositoryDomain(root, "").Domain; got != servedWitnessDomain {
		t.Fatalf("the project states no resolvable domain (%q), so this witness cannot distinguish "+
			"a structural gap from an unconfigured one", got)
	}
	// The four subjects pass the empty value, which is what they have. What must be true is
	// that the empty value no longer produces an incapable comparison: the owner resolves the
	// domain this checkout states.
	//
	// The original form of this witness asserted that resolveGraphReader(.., "", ..) sees no
	// declared generation -- true when written, and DELIBERATELY no longer true. Finding 1 of
	// the blind review of head 59372102 showed the same incapable state was reachable through
	// eleven subjects' optional --domain flags, so the resolution moved into the owner and
	// "resolved with no domain" stopped existing. See optional_domain_test.go.
	asTheSubjectsCallIt := productionReaderFor(emptyFlags(), "", "")
	if asTheSubjectsCallIt.DomainInvalid != nil {
		t.Fatalf("the owner could not resolve this checkout's domain: %v", asTheSubjectsCallIt.DomainInvalid)
	}
	if asTheSubjectsCallIt.Domain != servedWitnessDomain {
		t.Fatalf("expected domain = %q, want the domain the project's own config states (%q)",
			asTheSubjectsCallIt.Domain, servedWitnessDomain)
	}
	if !asTheSubjectsCallIt.declaresGeneration() {
		t.Fatal("the resolved reader still sees no declared generation, so the four formerly " +
			"domainless subjects remain structurally unable to compare anything")
	}
	if err := asTheSubjectsCallIt.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
	}); err == nil {
		t.Error("the resolved reader accepted a foreign generation")
	}
}

// B1-opposite. A malformed repository configuration must be REFUSED rather than fall through to
// the empty domain. Falling through would restore exactly the permanent inertness this
// repair removes, while looking like a reader that simply had nothing to check -- and
// checkout identity is an authority boundary, so it fails visibly.
func TestARepositoryScopedReaderRefusesAMalformedDomainConfiguration(t *testing.T) {
	root := servedWorld(t, declaredGen, "")
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: \"unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := productionReaderFor(emptyFlags(), "", "")
	if r.DomainInvalid == nil {
		t.Fatal("a malformed repository configuration produced a reader with a resolved expected " +
			"domain; an unparseable checkout identity must never read as 'no domain'")
	}
	// And it must REFUSE at the comparison, not merely record the reason: this registry declares
	// a generation ACTIVE, so an unresolvable expected domain cannot be treated as inert.
	if err := r.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: declaredGen,
	}); err == nil {
		t.Fatal("a malformed checkout identity verified successfully")
	}
}

// --- POSITIVE CONTROL -------------------------------------------------------------

// C1. Inertness, in the direction that must keep working. When the registry declares no
// ACTIVE generation there is nothing to contradict, and every subject must behave exactly
// as it did before this family existed. A repair that refused here would take the
// unconfigured case out of service, which is the failure mode the pointer's own contract
// forbids.
func TestWithNoDeclarationEveryReaderProceedsUnchanged(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen})
	root := servedWorld(t, "", addr)

	var code int
	out := captureStdout(t, func() {
		code = runRepairPlan([]string{"--repo-root", root, "--task", "repair the thing",
			"--domain", servedWitnessDomain})
	})
	if code != 0 {
		t.Fatalf("repair-plan refused while nothing was declared ACTIVE (exit=%d):\n%s", code, out)
	}
	if !strings.Contains(out, "repair the thing") {
		t.Errorf("repair-plan produced no plan on the inert path:\n%s", out)
	}
}

// --- THE SEAM'S OWN GUARANTEE ------------------------------------------------------

// The field the owner reads is the one that can detect the fault. GraphAuthority carries
// two commit-shaped strings: live_store_graph_digest_sha256 (the generation that answered)
// and graph_build_commit (the RULE SNAPSHOT's revision). Comparing the second against a
// declared generation would be the right position, the right predicate and the wrong
// operand -- a check that reads as enforcement and can never fire.
//
// cmd_edit_brief.go had already read graph_build_commit into a field it called
// "Generation", which is why this is pinned rather than assumed. It also replaces the half
// of each call site's source anchor that the seam absorbed.
func TestTheOwnerComparesTheServedGenerationNotTheRuleSnapshotRevision(t *testing.T) {
	r := graphReader{Domain: servedWitnessDomain, DeclaredGeneration: declaredGen}

	// The generation matches; only the rule-snapshot revision differs. Must pass.
	if err := r.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: declaredGen,
		GraphBuildCommit:           foreignGen,
	}); err != nil {
		t.Errorf("a served generation equal to the declared one was refused: %v", err)
	}

	// The rule-snapshot revision matches and the generation does not. Must refuse: a reader
	// comparing graph_build_commit would accept this, and accept it forever.
	if err := r.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
		GraphBuildCommit:           declaredGen,
	}); err == nil {
		t.Error("the owner compared graph_build_commit: a foreign served generation was accepted " +
			"because the rule-snapshot revision happened to match the declaration")
	}
}

// Absence is refused, and a nil authority is not a different answer from an empty one. A
// self-certifying response is not exempt either: authoritative + CURRENT says the graph
// vouches for itself, which is not the same fact as being the generation this domain
// declares.
func TestTheOwnerRefusesAnAuthorityThatEstablishesNoGeneration(t *testing.T) {
	r := graphReader{Domain: servedWitnessDomain, DeclaredGeneration: declaredGen}

	if err := r.verifyServedAuthority(nil); err == nil {
		t.Error("a nil authority was accepted while a generation was declared ACTIVE")
	}
	if err := r.verifyServedAuthority(&awarenesspb.GraphAuthority{
		Authoritative:       true,
		GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
	}); err == nil {
		t.Error("a self-certified authority stating no generation was accepted")
	}

	// Inert with nothing declared, in both shapes of absence.
	inert := graphReader{Domain: servedWitnessDomain}
	if err := inert.verifyServedAuthority(nil); err != nil {
		t.Errorf("a nil authority was refused while nothing was declared ACTIVE: %v", err)
	}
	if err := inert.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
	}); err != nil {
		t.Errorf("a served generation was refused while nothing was declared ACTIVE: %v", err)
	}
}

// --- THE OTHER REFUSAL SHAPES ------------------------------------------------------
//
// The census proves the comparison is REACHABLE from every subject. It cannot prove the
// refusal refuses: a check placed after consumption, or one that prints a caveat and
// continues, reaches the comparison and changes nothing. So each materially different
// refusal shape gets a driven witness, paired with its opposite.

// A5. THE FALSE CLEAN. edit-check is warning-only and exits 0 either way, so the danger is
// not a wrong warning -- it is printing "no advisory rule tripped for this edit." about a
// domain whose rules were never consulted. An agent reads that as permission.
func TestEditCheckDoesNotPrintACleanVerdictFromAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen, noWarnings: true})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runEditCheck([]string{"--file", "golang/thing.go", "--content", "package thing\n",
			"--domain", servedWitnessDomain})
	})
	if code == 0 {
		t.Errorf("edit-check exited 0 on a verdict from generation %s while this domain declares %s ACTIVE",
			foreignGen, declaredGen)
	}
	if strings.Contains(out, "no advisory rule tripped") {
		t.Fatalf("edit-check printed a CLEAN verdict about a graph this domain does not declare "+
			"active; an agent reads that as permission:\n%s", out)
	}
	if strings.Contains(out, "rules_evaluated") {
		t.Errorf("edit-check reported rules_evaluated from an undeclared generation:\n%s", out)
	}
}

// A5-opposite. The clean verdict must still be printed when the generation agrees, including
// through --json, which is a second output path and therefore a second chance to bypass.
func TestEditCheckStillReportsWhenTheGenerationAgrees(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: declaredGen, noWarnings: true})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runEditCheck([]string{"--file", "golang/thing.go", "--content", "package thing\n",
			"--domain", servedWitnessDomain})
	})
	if code != 0 || !strings.Contains(out, "no advisory rule tripped") {
		t.Fatalf("edit-check refused the declared ACTIVE generation (exit=%d):\n%s", code, out)
	}

	var jcode int
	jout := captureStdout(t, func() {
		jcode = runEditCheck([]string{"--file", "golang/thing.go", "--content", "package thing\n",
			"--domain", servedWitnessDomain, "--json"})
	})
	if jcode != 0 || !strings.Contains(jout, "rulesEvaluated") && !strings.Contains(jout, "rules_evaluated") {
		t.Errorf("the --json path did not report on the healthy generation (exit=%d):\n%s", jcode, jout)
	}
}

// A6. THE PICKER. `sensei domains` prints the domain set a graph offers, and whoever picks
// from it runs a governed operation against the choice. A list from a graph this project does
// not declare active is therefore consumed authoritatively one step later.
func TestDomainsDoesNotPrintAListFromAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen})
	servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() { code = runDomains(nil) })
	if code == 0 {
		t.Errorf("domains exited 0 on a list from generation %s while this domain declares %s ACTIVE",
			foreignGen, declaredGen)
	}
	if strings.Contains(out, servedWitnessDomain) {
		t.Fatalf("the domain list from an undeclared generation still reached stdout:\n%s", out)
	}
}

// A6-opposite.
func TestDomainsStillPrintsTheListWhenTheGenerationAgrees(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: declaredGen})
	servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() { code = runDomains(nil) })
	if code != 0 || !strings.Contains(out, servedWitnessDomain) {
		t.Fatalf("domains refused the declared ACTIVE generation (exit=%d):\n%s", code, out)
	}
}

// A7. THE CLASSIFICATION. repair-report and repair-gate always produce a verdict -- that is
// their contract -- so the refusal is a classification rather than an error. It must be its
// OWN classification: folding it into stale_authority would claim the graph said something
// about itself that it did not, and the two have different repairs.
func TestRepairGateFailsClosedOnAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runRepairGate([]string{"--repo-root", root, "--file", "golang/thing.go",
			"--task", "repair the thing", "--domain", servedWitnessDomain, "--format", "json"})
	})
	if code == 0 {
		t.Fatalf("repair-gate PASSED on evidence from generation %s while this domain declares "+
			"%s ACTIVE:\n%s", foreignGen, declaredGen, out)
	}
	if !strings.Contains(out, repairClassificationUndeclaredGeneration) {
		t.Errorf("the verdict does not carry its own classification (%q), so the reason is lost:\n%s",
			repairClassificationUndeclaredGeneration, out)
	}
	if strings.Contains(out, repairClassificationStaleAuthority) {
		t.Errorf("the refusal was reported as stale_authority, which is the graph's verdict about "+
			"ITSELF and a different repair:\n%s", out)
	}
}

// A8. THE WITHHELD DELIVERY. edit-brief pushes invariants and forbidden fixes into an agent's
// edit. It exits 0 always and must not wedge editing, so the refusal is silence -- and a
// LEDGER ROW, because an opportunity that produced no delivery is the row a delivery count
// silently drops. That is how a whole campaign was once measured as clean.
func TestEditBriefDeliversNothingFromAnUndeclaredGenerationAndRecordsWhy(t *testing.T) {
	root := servedWorld(t, declaredGen, "")
	ledger := filepath.Join(t.TempDir(), "delivery.jsonl")
	t.Setenv("AWG_EVENT_LOG", ledger)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := editBriefRPC
	t.Cleanup(func() { editBriefRPC = orig })
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return editBriefOutcome{
			Prose:     "ALWAYS hold the lock before touching this field",
			Status:    awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
			Wire:      awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
			Authority: &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: foreignGen},
		}, nil
	}

	var code int
	out := captureStdout(t, func() {
		code = runEditBrief([]string{"--root", root, "--file", target, "--domain", servedWitnessDomain})
	})
	if code != 0 {
		t.Errorf("edit-brief wedged the edit (exit=%d) instead of allowing it", code)
	}
	if strings.Contains(out, "hold the lock") {
		t.Fatalf("edit-brief pushed another generation's invariants into the edit:\n%s", out)
	}
	rows, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("no ledger row was written, so the withheld delivery is indistinguishable from no edit: %v", err)
	}
	if !strings.Contains(string(rows), foreignGen) && !strings.Contains(strings.ToLower(string(rows)), "generation") {
		t.Errorf("the ledger row does not say why nothing was delivered:\n%s", rows)
	}
}

// A8-opposite. The push must still happen when the generation agrees, or the hook is dead.
func TestEditBriefStillDeliversWhenTheGenerationAgrees(t *testing.T) {
	root := servedWorld(t, declaredGen, "")
	t.Setenv("AWG_EVENT_LOG", filepath.Join(t.TempDir(), "delivery.jsonl"))
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := editBriefRPC
	t.Cleanup(func() { editBriefRPC = orig })
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return editBriefOutcome{
			Prose:     "ALWAYS hold the lock before touching this field",
			Status:    awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
			Wire:      awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
			Authority: &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: declaredGen},
		}, nil
	}

	var code int
	out := captureStdout(t, func() {
		code = runEditBrief([]string{"--root", root, "--file", target, "--domain", servedWitnessDomain})
	})
	if code != 0 || !strings.Contains(out, "hold the lock") {
		t.Fatalf("edit-brief withheld a briefing from the declared ACTIVE generation (exit=%d):\n%s", code, out)
	}
}

// A9. VERIFIED ONCE IS NOT VERIFIED. The store is republished after Metadata answers, so the
// preflight that follows on the SAME connection comes from a different generation. A command
// that compares once, at the start, has verified a response it did not use.
//
// Found by mutation: deleting the per-response preflight comparison in generateRepairReport
// survived, because the metadata comparison fired first in every witness that existed.
func TestRepairGateVerifiesEachResponseNotJustTheFirst(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{
		servedGeneration:       declaredGen,
		republishAfterMetadata: foreignGen,
	})
	root := servedWorld(t, declaredGen, addr)
	target := filepath.Join(root, "golang", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runRepairGate([]string{"--repo-root", root, "--file", "golang/thing.go",
			"--task", "repair the thing", "--domain", servedWitnessDomain, "--format", "json"})
	})
	if code == 0 {
		t.Fatalf("repair-gate PASSED after the store was republished between Metadata and "+
			"Preflight; it verified the first response and consumed the second:\n%s", out)
	}
	if !strings.Contains(out, repairClassificationUndeclaredGeneration) {
		t.Errorf("the verdict does not name the generation disagreement, so it failed for some "+
			"other reason and this witness proves nothing:\n%s", out)
	}
}
