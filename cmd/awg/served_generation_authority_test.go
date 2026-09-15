// SPDX-License-Identifier: AGPL-3.0-only

package main

// CATEGORY C — SERVED-GENERATION AUTHORITY for runVerifyObligations and runEditBrief.
//
// The governing contract:
//
//	A production graph-reading command that consumes an authoritative graph for a governed domain
//	may consume that payload only after proving served generation == declared ACTIVE generation for
//	that governed domain. The comparison must occur before consumption. A mismatch must refuse. An
//	absent declared ACTIVE generation is NOT verification and must also refuse before consumption.
//
// runPreflight's defect witness lives in preflight_served_authority_witness_test.go and is
// deliberately left where it is; no subject's witness stands in for another's.
//
// Each witness here proves five things for its own subject: a declared ACTIVE generation exists; the
// served graph is authoritative and internally coherent; the served generation differs; the command
// reaches and consumes the authoritative payload; and the mismatch is not rejected before that.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"google.golang.org/grpc"
)

// realEditBriefRPC captures the production transport at package initialisation, BEFORE any test can
// replace it.
//
// Nine sites in cmd_edit_brief_test.go and edit_brief_delivery_test.go assign editBriefRPC without
// restoring it, so the stub leaks into every later test in the package and these witnesses would
// otherwise pass or fail by test ORDER rather than by behaviour. Depending on global state a test does
// not own is how a witness fails for the wrong reason, and a failure for the wrong reason is not
// evidence. See TestObservedEditBriefRPCStubsLeakAcrossTests.
var realEditBriefRPC = editBriefRPC

const (
	catCDomain   = "example.com/acme/generation"
	catCDeclared = "3333333333333333333333333333333333333333333333333333333333333333"
	catCServed   = "4444444444444444444444444444444444444444444444444444444444444444"
)

// catCGraph is a HEALTHY graph: self-certified authoritative and CURRENT, internally coherent, and
// serving a generation that is simply not the one the registry declares ACTIVE.
type catCGraph struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	served    string
	preflight int
	briefing  int
}

func (s *catCGraph) authority() *awarenesspb.GraphAuthority {
	return &awarenesspb.GraphAuthority{
		Authoritative:                   true,
		GraphFreshnessState:             awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		LiveStoreGraphDigestSha256:      s.served,
		EmbeddedSeedDigestSha256:        s.served,
		LiveStoreGraphTripleCount:       8123,
		GraphBuildCommit:                "9a8b7c6d5e4f30211302f4e5d6c7b8a99a8b7c6d",
		EmbeddedTransactionStampPresent: true,
		EmbeddedTransactionMatchesSeed:  true,
	}
}

func (s *catCGraph) Preflight(context.Context, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	s.preflight++
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
		Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
		Authority:  s.authority(),
		TestsToRun: []string{"cmd/awg/authority_test.go:TestTheGraphIsTheIntendedOne"},
	}, nil
}

func (s *catCGraph) Briefing(context.Context, *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
	s.briefing++
	ok := awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
	return &awarenesspb.BriefingResponse{
		Status:        ok,
		FileStatus:    &ok,
		Prose:         "GOVERNING KNOWLEDGE FROM THE WRONG GENERATION",
		ReferencedIds: []string{"inv.example"},
		Authority:     s.authority(),
	}, nil
}

// catCWorld: a registry declaring an ACTIVE generation, a project stating its domain, and a healthy
// service serving a DIFFERENT generation. declaredGeneration empty builds the missing-expectation
// world instead.
func catCWorld(t *testing.T, declaredGeneration string) (svc *catCGraph, root string) {
	t.Helper()

	svc = &catCGraph{served: catCServed}
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
	t.Setenv("AWG_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	t.Setenv("AWG_EVENT_LOG", filepath.Join(home, "events.jsonl"))
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	gen := ""
	if declaredGeneration != "" {
		gen = "\n        active_generation: " + declaredGeneration
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
		catCDomain+":\n        repository_identity: acme/generation\n        allowed_corpus_roots:\n"+
		"            - docs/awareness\n        service_addr: "+addr+gen+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root = projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: "+catCDomain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	// Exercise the REAL transport, whatever an earlier test left behind.
	restore := editBriefRPC
	editBriefRPC = realEditBriefRPC
	t.Cleanup(func() { editBriefRPC = restore })

	// PRECONDITIONS, asserted not assumed.
	reader := productionReaderFor(emptyFlags(), root, catCDomain, "")
	if reader.Addr != addr {
		t.Fatalf("the owner resolved %q, want the registry's %q", reader.Addr, addr)
	}
	if reader.DeclaredGeneration != declaredGeneration {
		t.Fatalf("declared generation = %q, want %q", reader.DeclaredGeneration, declaredGeneration)
	}
	return svc, root
}

// INVERTED WITNESS — runVerifyObligations must refuse a wrong-generation graph before the anchors
// become obligations. Formerly TestVerifyObligationsConsumesRequiredTestsFromAWrongGenerationGraph.
func TestVerifyObligationsRefusesRequiredTestsFromAWrongGenerationGraph(t *testing.T) {
	svc, _ := catCWorld(t, catCDeclared)
	results := writeEmptyResults(t)

	stdout, stderr, code := captureBoth(t, func() int {
		return runVerifyObligations([]string{"--task", "category C witness", "--results", results})
	})
	out := stdout + stderr

	if svc.preflight != 1 {
		t.Fatalf("the service was asked for required tests %d times.\nout:\n%s", svc.preflight, out)
	}
	if code == 0 {
		t.Fatalf("runVerifyObligations returned 0 on a wrong-generation graph.\nout:\n%s", out)
	}
	if !strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("expected the mismatch refusal.\nout:\n%s", out)
	}
	if strings.Contains(out, "not established") {
		t.Fatalf("a MISMATCH was reported with the NOT_ESTABLISHED wording.\nout:\n%s", out)
	}
	// BEFORE CONSUMPTION: the anchor must never have become an obligation.
	if strings.Contains(out, "TestTheGraphIsTheIntendedOne") {
		t.Fatalf("the required test from the wrong generation was consumed into the report, so the "+
			"comparison ran after consumption.\nout:\n%s", out)
	}
	t.Logf("INVARIANT ESTABLISHED (verify-obligations/MISMATCH): exit %d, refused, no anchor consumed.", code)
}

// MISSING-EXPECTATION CONTROL — verify-obligations.
func TestVerifyObligationsRefusesWhenNoActiveGenerationIsDeclared(t *testing.T) {
	svc, _ := catCWorld(t, "")
	results := writeEmptyResults(t)

	stdout, stderr, code := captureBoth(t, func() int {
		return runVerifyObligations([]string{"--task", "category C witness", "--results", results})
	})
	out := stdout + stderr

	if svc.preflight != 1 {
		t.Fatalf("the service was asked %d times.\nout:\n%s", svc.preflight, out)
	}
	if code == 0 {
		t.Fatalf("runVerifyObligations returned 0 with no ACTIVE generation declared.\nout:\n%s", out)
	}
	if !strings.Contains(out, "no ACTIVE generation is declared") {
		t.Fatalf("expected the NOT_ESTABLISHED wording.\nout:\n%s", out)
	}
	if strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("absence was reported as a mismatch.\nout:\n%s", out)
	}
	if strings.Contains(out, "TestTheGraphIsTheIntendedOne") {
		t.Fatalf("the anchor was consumed despite NOT_ESTABLISHED.\nout:\n%s", out)
	}
	t.Logf("INVARIANT ESTABLISHED (verify-obligations/NOT_ESTABLISHED): exit %d, absence named, no "+
		"anchor consumed.", code)
}

// MATCHED CONTROL — verify-obligations. Same fixture, declared == served; must be accepted and the
// anchors consumed. The only difference from the refusing witness is the generation identity.
func TestVerifyObligationsAcceptsAMatchingGeneration(t *testing.T) {
	svc, _ := catCWorld(t, catCServed) // declared == served
	results := writeEmptyResults(t)

	stdout, stderr, _ := captureBoth(t, func() int {
		return runVerifyObligations([]string{"--task", "category C witness", "--results", results})
	})
	out := stdout + stderr

	if svc.preflight != 1 {
		t.Fatalf("the service was asked %d times.\nout:\n%s", svc.preflight, out)
	}
	if strings.Contains(out, "ambiguous graph identity") || strings.Contains(out, "not established") {
		t.Fatalf("a matching generation was refused.\nout:\n%s", out)
	}
	if !strings.Contains(out, "TestTheGraphIsTheIntendedOne") {
		t.Fatalf("a matching generation's anchor was not consumed.\nout:\n%s", out)
	}
	t.Logf("MATCHED CONTROL (verify-obligations): declared == served == %s, anchors consumed.", catCServed)
}

// INVERTED WITNESS — runEditBrief must withhold prose from a wrong-generation graph.
//
// Withholding, not blocking: this command's existing contract is that a briefing it cannot serve is
// never a reason to block an edit. The authority requirement is that the payload is NOT CONSUMED; the
// exit code keeps the non-blocking promise.
func TestEditBriefWithholdsProseFromAWrongGenerationGraph(t *testing.T) {
	svc, root := catCWorld(t, catCDeclared)
	edited := writeEditedFile(t, root)

	stdout, stderr, code := captureBoth(t, func() int {
		return runEditBrief([]string{"--file", edited})
	})
	out := stdout + stderr

	if svc.briefing != 1 {
		t.Fatalf("the service was asked for a briefing %d times.\nout:\n%s", svc.briefing, out)
	}
	// NOT CONSUMED: the prose must not be delivered as governing context.
	if strings.Contains(out, "GOVERNING KNOWLEDGE FROM THE WRONG GENERATION") {
		t.Fatalf("the wrong-generation prose was delivered.\nout:\n%s", out)
	}
	if strings.Contains(out, "additionalContext") {
		t.Fatalf("an edit-brief context payload was emitted from an unverified graph.\nout:\n%s", out)
	}
	if !strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("expected the mismatch refusal in the withholding reason.\nout:\n%s", out)
	}
	// NON-BLOCKING: the edit still proceeds, which is this command's own contract.
	if code != 0 {
		t.Fatalf("runEditBrief returned %d; withholding a briefing must not block the edit.\nout:\n%s",
			code, out)
	}
	t.Logf("INVARIANT ESTABLISHED (edit-brief/MISMATCH): prose withheld, no context emitted, edit "+
		"not blocked (exit %d).", code)
}

// MISSING-EXPECTATION CONTROL — edit-brief.
func TestEditBriefWithholdsWhenNoActiveGenerationIsDeclared(t *testing.T) {
	svc, root := catCWorld(t, "")
	edited := writeEditedFile(t, root)

	stdout, stderr, code := captureBoth(t, func() int {
		return runEditBrief([]string{"--file", edited})
	})
	out := stdout + stderr

	if svc.briefing != 1 {
		t.Fatalf("the service was asked %d times.\nout:\n%s", svc.briefing, out)
	}
	if strings.Contains(out, "GOVERNING KNOWLEDGE FROM THE WRONG GENERATION") || strings.Contains(out, "additionalContext") {
		t.Fatalf("prose was delivered despite NOT_ESTABLISHED.\nout:\n%s", out)
	}
	if !strings.Contains(out, "no ACTIVE generation is declared") {
		t.Fatalf("expected the NOT_ESTABLISHED wording.\nout:\n%s", out)
	}
	if strings.Contains(out, "ambiguous graph identity") {
		t.Fatalf("absence was reported as a mismatch.\nout:\n%s", out)
	}
	if code != 0 {
		t.Fatalf("runEditBrief returned %d; withholding must not block.\nout:\n%s", code, out)
	}
	t.Logf("INVARIANT ESTABLISHED (edit-brief/NOT_ESTABLISHED): prose withheld, absence named, edit " +
		"not blocked.")
}

// MATCHED CONTROL — edit-brief.
func TestEditBriefDeliversProseFromTheDeclaredGeneration(t *testing.T) {
	svc, root := catCWorld(t, catCServed) // declared == served
	edited := writeEditedFile(t, root)

	stdout, stderr, code := captureBoth(t, func() int {
		return runEditBrief([]string{"--file", edited})
	})
	out := stdout + stderr

	if svc.briefing != 1 {
		t.Fatalf("the service was asked %d times.\nout:\n%s", svc.briefing, out)
	}
	if !strings.Contains(out, "GOVERNING KNOWLEDGE FROM THE WRONG GENERATION") {
		t.Fatalf("prose from the DECLARED generation was withheld.\nout:\n%s", out)
	}
	if !strings.Contains(out, "additionalContext") {
		t.Fatalf("no context payload was emitted for a verified generation.\nout:\n%s", out)
	}
	if code != 0 {
		t.Fatalf("runEditBrief returned %d.\nout:\n%s", code, out)
	}
	t.Logf("MATCHED CONTROL (edit-brief): declared == served == %s, prose delivered.", catCServed)
}

func writeEmptyResults(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(p, []byte(`{"tests":[]}`), 0o644); err != nil {
		t.Fatalf("fixture: could not write the results file: %v", err)
	}
	return p
}

func writeEditedFile(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "golang", "server", "reload.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(p, []byte("package server\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return p
}

// OBSERVATION, recorded and NOT repaired here: editBriefOutcome.Generation is populated from
// GetGraphBuildCommit, the source revision the graph was built from, not the served graph's
// generation digest. It feeds the delivery ledger's GraphGeneration field, so that evidence column
// has been recording a build commit under a generation label. A comparison built on this field could
// never pass, which is why this family surfaces the served digest separately instead.
func TestObservedEditBriefOutcomeGenerationIsTheBuildCommitNotTheServedGeneration(t *testing.T) {
	svc, root := catCWorld(t, catCDeclared)
	edited := filepath.Join(root, "main.go")
	if err := os.WriteFile(edited, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	captured := editBriefOutcome{}
	real := editBriefRPC
	editBriefRPC = func(ctx context.Context, addr, file, depth, domain string) (editBriefOutcome, error) {
		o, err := real(ctx, addr, file, depth, domain)
		captured = o
		return o, err
	}
	t.Cleanup(func() { editBriefRPC = real })

	_, _, _ = captureBoth(t, func() int { return runEditBrief([]string{"--file", edited}) })
	if svc.briefing != 1 {
		t.Skipf("the briefing path was not reached (%d calls); cannot observe the field", svc.briefing)
	}
	if captured.Generation == catCServed {
		t.Fatalf("outcome.Generation now carries the served generation %q; this observation is "+
			"obsolete and the field may be used for the comparison", catCServed)
	}
	t.Logf("OBSERVED: served generation is %s, but outcome.Generation is %q (the authority's "+
		"GraphBuildCommit). The field is named for one identity and holds another, and it is what the "+
		"delivery ledger records as GraphGeneration.", catCServed, captured.Generation)
}

// OBSERVATION, recorded and NOT repaired here. editBriefRPC is a package-level var used as a test
// seam, and nine assignments to it never restore the previous value:
//
//	cmd_edit_brief_test.go     4 sites
//	edit_brief_delivery_test.go 5 sites
//
// So a stub installed by one test is still installed for every test that runs after it. It caused
// these witnesses to fail for the wrong reason before catCWorld began restoring the real transport
// explicitly, and it will do the same to the next test that needs the real RPC. Recorded as a measured
// fact with its own bounded repair rather than folded into this family.
func TestObservedEditBriefRPCStubsLeakAcrossTests(t *testing.T) {
	src := readCmdSource(t, "cmd_edit_brief_test.go") + readCmdSource(t, "edit_brief_delivery_test.go")
	assigns := strings.Count(src, "editBriefRPC = func(")
	restores := strings.Count(src, "editBriefRPC = restore") + strings.Count(src, "editBriefRPC = real")
	if assigns == 0 {
		t.Skip("no stub assignments remain in those files; this observation is obsolete")
	}
	if restores >= assigns {
		t.Skipf("all %d stub assignments in those files now restore; this observation is obsolete", assigns)
	}
	t.Logf("OBSERVED: %d assignments to the editBriefRPC seam in those two files, %d restorations. "+
		"A stub installed by one test remains installed for every test after it, so any later test "+
		"needing the real transport passes or fails by order. These witnesses defend against it by "+
		"reinstating realEditBriefRPC in catCWorld.", assigns, restores)
}

// THE DOMAIN-UNRESOLVED STATE, AUDITED. It is the only state that does not refuse, so it is the only
// possible bypass, and it must be provably a non-claim rather than a silent pass.
func TestAnUnresolvedGovernedDomainProceedsButNeverClaimsVerification(t *testing.T) {
	// No project config, so no governed domain resolves — the cold-start stranger path's shape.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	t.Chdir(root)

	reader := productionReaderFor(emptyFlags(), root, "", "")
	if reader.Domain != "" {
		t.Fatalf("a governed domain resolved (%q); this fixture cannot exercise DOMAIN_UNRESOLVED",
			reader.Domain)
	}

	state, err := reader.classifyServedGeneration(catCServed)
	if state != generationDomainUnresolved {
		t.Fatalf("state = %v, want DOMAIN_UNRESOLVED", state)
	}
	// IT PROCEEDS: refusing over the absence of a question, rather than the absence of an answer,
	// would make an ungoverned project unusable.
	if err != nil {
		t.Fatalf("DOMAIN_UNRESOLVED refused (%v); an ungoverned project has no ACTIVE pointer to "+
			"disagree with, and the server's own graphAuthorityFor declines a home-domain fallback "+
			"for exactly this reason", err)
	}
	// IT DOES NOT CLAIM: this is what keeps it from being a proof-shaped bypass.
	if reader.claimsVerifiedGeneration(catCServed) {
		t.Fatal("DOMAIN_UNRESOLVED claimed verification; VERIFIED is an assertion about a governed " +
			"domain and there is no domain here to assert it about")
	}
	if state == generationVerified {
		t.Fatal("DOMAIN_UNRESOLVED compared equal to VERIFIED")
	}
	// And it is DISTINCT from the state that must refuse.
	if state == generationNotEstablished {
		t.Fatal("an unresolved domain was conflated with a resolved domain declaring no generation; " +
			"the first proceeds and the second must refuse")
	}
	t.Logf("AUDITED: with no governed domain the state is %v — proceeds, and claimsVerifiedGeneration "+
		"is false. The bypass surface is exactly one typed state, and it cannot report a proof.", state)
}

// The four states are mutually exclusive and each is reachable. A state nothing can reach is a state
// nothing enforces.
func TestEveryGenerationAuthorityStateIsReachableAndDistinct(t *testing.T) {
	seen := map[generationAuthority]string{}
	record := func(r graphReader, served, label string) {
		state, err := r.classifyServedGeneration(served)
		if prev, dup := seen[state]; dup {
			t.Fatalf("%s and %s both produced %v", prev, label, state)
		}
		seen[state] = label
		// Only VERIFIED and DOMAIN_UNRESOLVED may be non-refusing.
		refuses := err != nil
		wantRefusal := state == generationNotEstablished || state == generationMismatch
		if refuses != wantRefusal {
			t.Errorf("%s: state %v refuses=%v, want refuses=%v", label, state, refuses, wantRefusal)
		}
	}
	record(graphReader{}, catCServed, "no domain")
	record(graphReader{Domain: catCDomain}, catCServed, "domain, no declaration")
	record(graphReader{Domain: catCDomain, DeclaredGeneration: catCDeclared}, catCServed, "disagreement")
	record(graphReader{Domain: catCDomain, DeclaredGeneration: catCServed}, catCServed, "agreement")

	for _, want := range []generationAuthority{
		generationDomainUnresolved, generationNotEstablished, generationMismatch, generationVerified,
	} {
		if _, ok := seen[want]; !ok {
			t.Errorf("state %v is unreachable", want)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("reached %d distinct states, want 4: %v", len(seen), seen)
	}
	t.Logf("FOUR STATES REACHABLE AND DISTINCT: %v", seen)
}

// THE JSON REFUSAL is the channel a --json consumer reads, so its shape is pinned here and its kind
// strings are the closed set scripts/lib/preflight-verdict.sh matches by membership.
func TestTheJSONRefusalIsTypedAndNotMistakableForAnAnswer(t *testing.T) {
	for _, c := range []struct {
		name  string
		state generationAuthority
		kind  string
	}{
		{"absent declaration", generationNotEstablished, "served_generation_not_established"},
		{"disagreement", generationMismatch, "served_generation_mismatch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.state.refusalKind(); got != c.kind {
				t.Fatalf("refusalKind() = %q, want %q — preflight-verdict.sh matches these by "+
					"membership, so a rename here silently becomes malformed:unknown_refusal_kind", got, c.kind)
			}
		})
	}
	// The two NON-refusing states must have no kind: asking for one is a caller error, and emitting a
	// refusal for them would report a refusal nobody can act on.
	for _, s := range []generationAuthority{generationVerified, generationDomainUnresolved, generationAuthorityUnset} {
		if k := s.refusalKind(); k != "" {
			t.Errorf("state %v has refusal kind %q; only the refusing states may have one", s, k)
		}
	}

	// The envelope must not be mistakable for a PreflightResponse. preflight-verdict.sh reads
	// status/risk_class/coverage; a refusal carrying any of them could be classified as an answer.
	r := graphReader{Domain: catCDomain, DeclaredGeneration: catCDeclared}
	_, cause := r.classifyServedGeneration(catCServed)
	out := captureStdoutOnly(t, func() {
		if code := emitGenerationRefusalJSON("sensei preflight", generationMismatch, r, catCServed, cause); code == 0 {
			t.Error("a refusal returned exit 0; a payload with a zero exit reads as success")
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("the refusal is not parseable JSON (%v), which is the defect it exists to fix: %s", err, out)
	}
	for _, forbidden := range []string{"status", "risk_class", "coverage", "tests_to_run"} {
		if _, present := decoded[forbidden]; present {
			t.Errorf("the refusal carries %q, so a consumer could classify it as an actionable answer", forbidden)
		}
	}
	body, ok := decoded["refusal"].(map[string]any)
	if !ok {
		t.Fatalf("no refusal object: %s", out)
	}
	for _, required := range []string{"kind", "surface", "domain", "declared_generation", "served_generation", "detail"} {
		if v, present := body[required]; !present || v == "" {
			t.Errorf("the refusal omits %q, so an operator cannot act on it: %s", required, out)
		}
	}
	if body["declared_generation"] == body["served_generation"] {
		t.Error("a MISMATCH refusal names the same value on both sides")
	}
}

// THE CI PRECONDITION, proven at the mechanism rather than only in the workflow.
//
// The activation environment depends on: a REGISTERED domain (operator configuration) plus a
// publication through the canonical owner, which records the ACTIVE generation. Neither half alone
// suffices, and the failure mode of the missing half is SILENCE -- recordActiveGeneration invents
// nothing -- so it must be asserted rather than assumed.
func TestActivateGenerationRecordsThePointerOnlyForARegisteredDomain(t *testing.T) {
	const gen = "6666666666666666666666666666666666666666666666666666666666666666"
	// A complete marker: WriteMarkerFile refuses an incomplete one, and the IRI must derive from the
	// digest (AdmitLiveMarker refuses a subject whose IRI does not).
	marker := seedmeta.Marker{
		Digest:      gen,
		IRI:         "urn:sensei:graph:" + gen,
		TripleCount: 4211,
	}

	read := func(path, domain string) string {
		t.Helper()
		reg, err := LoadDomainRegistry(path)
		if err != nil || reg == nil {
			return ""
		}
		return strings.TrimSpace(reg.Domains[domain].ActiveGeneration)
	}

	t.Run("registered domain: the canonical owner records it", func(t *testing.T) {
		dir := t.TempDir()
		registry := filepath.Join(dir, "domains.yaml")
		if err := os.WriteFile(registry, []byte("domains:\n    "+catCDomain+
			":\n        repository_identity: acme/generation\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := activateGeneration(io.Discard, filepath.Join(dir, "marker.json"), marker, catCDomain, registry); err != nil {
			t.Fatalf("activateGeneration: %v", err)
		}
		if got := read(registry, catCDomain); got != gen {
			t.Fatalf("ACTIVE generation = %q, want %q — this is the step CI depends on", got, gen)
		}
	})

	t.Run("unregistered domain: nothing is invented, and it is silent", func(t *testing.T) {
		dir := t.TempDir()
		registry := filepath.Join(dir, "domains.yaml")
		if err := os.WriteFile(registry, []byte("domains: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := activateGeneration(io.Discard, filepath.Join(dir, "marker.json"), marker, catCDomain, registry); err != nil {
			t.Fatalf("activateGeneration: %v", err)
		}
		if got := read(registry, catCDomain); got != "" {
			t.Fatalf("an unregistered domain was given an ACTIVE generation %q; the owner must invent "+
				"nothing", got)
		}
		// THIS is why CI must register the domain first: the omission produces no error at all.
		t.Log("confirmed: an unregistered domain yields no pointer AND no error, so a CI world that " +
			"skips registration fails silently and only surfaces as refused consumption downstream")
	})

	t.Run("no governed domain: the pointer is not updated and says so", func(t *testing.T) {
		dir := t.TempDir()
		registry := filepath.Join(dir, "domains.yaml")
		if err := os.WriteFile(registry, []byte("domains:\n    "+catCDomain+
			":\n        repository_identity: acme/generation\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := activateGeneration(&buf, filepath.Join(dir, "marker.json"), marker, "", registry); err != nil {
			t.Fatalf("activateGeneration: %v", err)
		}
		if got := read(registry, catCDomain); got != "" {
			t.Fatalf("a domainless publication wrote a pointer for %s: %q", catCDomain, got)
		}
		if !strings.Contains(buf.String(), "NOT updated") {
			t.Fatalf("a domainless publication did not report that the pointer was left alone:\n%s", buf.String())
		}
		t.Log("this is the state CI was in: build --all names no governed domain, so the pointer was " +
			"never recorded and every reader refused")
	})
}

func captureStdoutOnly(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}
