// SPDX-License-Identifier: AGPL-3.0-only

package main

// LAW 3: "All production readers resolve graph identity through one owner. They must not
// independently choose an Oxigraph port."
//
// It was false. Measured 2026-09-13: EIGHTEEN commands in this package declare
// `fs.String("addr", defaultServiceAddr(), …)` and dial it, and exactly one — `metadata`
// — resolved through the G2 owner. `briefing` is the sharpest case because it ALREADY
// resolves the governed domain (`resolveRepositoryDomain`) and then ignores it, dialing
// the netcfg default instead.
//
// Proven live during the migration: `sensei briefing --file docs/awareness/invariants.yaml`
// failed with "dial tcp 127.0.0.1:10120: connect: connection refused" in a repository
// whose own config states :10122. The graph it should read was running the whole time.
//
// The repair is not a different default port. It is that a production reader states the
// domain it is reading for and receives the endpoint AND the generation identity from the
// one owner — so the port stops being something a reader picks.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readerFixture(t *testing.T, configuredAddr, activeGeneration string) (root, registry string) {
	t.Helper()
	root = projectRoot(t, t.TempDir())
	if configuredAddr != "" {
		writeProjectConfig(t, root, configuredAddr)
	}
	gen := ""
	if activeGeneration != "" {
		gen = "\n        active_generation: " + activeGeneration
	}
	registry = writeRegistryFixture(t, `domains:
    example.com/acme/thing:
        repository_identity: acme/thing
        allowed_corpus_roots:
            - docs/awareness`+gen+`
`)
	return root, registry
}

// A. The domain resolves to service A while the netcfg default (:10120) would be B.
// The reader must use A.
func TestAProductionReaderUsesTheDomainsEndpointNotTheDefaultPort(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr != "localhost:10122" {
		t.Fatalf("reader addr = %q, want the domain's configured endpoint, not %q", r.Addr, defaultServiceAddr())
	}
	if strings.Contains(r.Source, "built-in") {
		t.Errorf("the reader reports the built-in default as its source: %q", r.Source)
	}
}

// B. :10120 may be perfectly healthy and belong to another graph. Reachability is not
// identity, so nothing may select it merely because it answers.
func TestReachabilityIsNotIdentity(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr == defaultServiceAddr() {
		t.Fatalf("the reader selected the built-in default %q while the domain states another endpoint", r.Addr)
	}
	// And the resolution must be decided before any dial: a resolver that needed to
	// probe endpoints would be choosing by reachability.
	if r.Addr == "" {
		t.Fatal("the reader resolved no endpoint at all")
	}
}

// C. When the resolved endpoint is unavailable the reader fails closed. There is no
// second port to try, because trying one would be choosing a graph by liveness.
func TestThereIsNoFallbackEndpoint(t *testing.T) {
	root, registry := readerFixture(t, "localhost:19999", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr != "localhost:19999" {
		t.Fatalf("addr = %q, want the configured (unreachable) endpoint", r.Addr)
	}
	// The structural half: no reader may name a second endpoint to fall back to.
	if n := strings.Count(briefingSource(t), "defaultServiceAddr()"); n != 0 {
		t.Errorf("briefing still names the built-in default %d time(s); a fallback port is a graph chosen by liveness", n)
	}
}

// D. The endpoint answers, but with a generation other than the declared ACTIVE one.
// The reader must refuse rather than report findings from a graph the domain does not
// declare.
func TestAReaderRefusesAResponseFromAnUndeclaredGeneration(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "c0b660fcaaaa")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.DeclaredGeneration != "c0b660fcaaaa" {
		t.Fatalf("declared generation = %q", r.DeclaredGeneration)
	}
	if err := r.verifyServed("c0b660fcaaaa"); err != nil {
		t.Errorf("the declared generation answered and was refused: %v", err)
	}
	err := r.verifyServed("999999999999")
	if err == nil {
		t.Fatal("a response from an undeclared generation was accepted")
	}
	for _, want := range []string{"c0b660fcaaaa", "999999999999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %q: %v", want, err)
		}
	}
	// Inert where nothing is declared — adding this must not refuse today's callers.
	rootB, registryB := readerFixture(t, "localhost:10122", "")
	rb := resolveGraphReader(emptyFlags(), rootB, "example.com/acme/thing", "", registryB)
	if err := rb.verifyServed("anything"); err != nil {
		t.Errorf("an undeclared generation refused a reader: %v", err)
	}
}

// E. A test/dev override stays possible and must announce itself. An override that
// looked like canonical resolution would be the port choice returning through a flag.
func TestAnExplicitOverrideIsPossibleAndVisiblyNonCanonical(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "localhost:19191", registry)
	if r.Addr != "localhost:19191" {
		t.Fatalf("an explicit override was ignored: %q", r.Addr)
	}
	if !strings.Contains(strings.ToLower(r.Source), "command line") {
		t.Errorf("the override does not announce itself as operator-named: %q", r.Source)
	}
	if !r.Overridden {
		t.Error("the reader does not record that its endpoint was overridden, so nothing downstream can report it as non-canonical")
	}
	// Canonical resolution must not claim to be an override, and vice versa.
	canonical := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if canonical.Overridden {
		t.Error("a canonically resolved reader claims to be overridden")
	}
}

// F. metadata and briefing must resolve the same endpoint and identity for one domain.
// Asserted by construction: both call the same owner, and the test proves there is only
// one resolver reachable from the reader paths.
func TestMetadataAndBriefingResolveThroughTheSameOwner(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "c0b660fcaaaa")
	a := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	b := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if a.Addr != b.Addr || a.DeclaredGeneration != b.DeclaredGeneration {
		t.Fatalf("the owner is not deterministic: %+v vs %+v", a, b)
	}
	for _, f := range []string{"cmd_briefing.go", "cmd_metadata.go"} {
		src := readCmdSource(t, f)
		if !strings.Contains(src, "resolveGraphReader(") {
			t.Errorf("%s does not resolve through the shared owner", f)
		}
		if strings.Contains(src, "defaultServiceAddr()") {
			t.Errorf("%s still names the built-in default endpoint", f)
		}
	}
}

func briefingSource(t *testing.T) string { return readCmdSource(t, "cmd_briefing.go") }

func emptyFlags() *flag.FlagSet { return flag.NewFlagSet("test", flag.ContinueOnError) }

func readCmdSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// writeProjectConfig writes the .sensei/config.yaml a project uses to state its endpoint.
func writeProjectConfig(t *testing.T, root, addr string) {
	t.Helper()
	body := "store:\n    query_url: http://localhost:7899/query\nserver:\n    addr: " + addr + "\n"
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The generation check must be WIRED, and wired before the briefing is rendered: a
// verdict from an undeclared graph must never be read as findings about this domain.
//
// Source-anchored and labelled as such — driving a full briefing needs a live awareness
// service. Witness D above proves the predicate; this proves the call exists, runs before
// the render, and is given the response's OWN authority digest rather than some other
// value that would always agree.
func TestBriefingVerifiesTheServedGenerationBeforeRendering(t *testing.T) {
	src := briefingSource(t)
	call := strings.Index(src, "reader.verifyServed(")
	if call < 0 {
		t.Fatal("briefing never verifies that the graph which answered is the declared ACTIVE generation")
	}
	// The anchor must be the FIRST rendering after the RPC, and finding it takes two
	// steps because neither naive choice works:
	//
	//   - `if *asJSON {` appears twice; the `--task active` path returns early with its
	//     own copy, and indexing the first occurrence compared the check against a
	//     branch it never reaches (this test failed that way);
	//   - `printGraphAuthority` is unique but is NOT the first render: the `--json`
	//     branch returns before it, so a check placed between them leaves `--json`
	//     rendering an unverified graph — and a mutation doing exactly that survived
	//     until this was fixed.
	//
	// So: locate the RPC, then the first render after it.
	rpc := strings.Index(src, "client.Briefing(ctx,")
	if rpc < 0 {
		t.Fatal("the briefing RPC anchor is gone; this check has lost its anchor")
	}
	rel := strings.Index(src[rpc:], "if *asJSON {")
	if rel < 0 {
		t.Fatal("no rendering follows the briefing RPC; this check has lost its anchor")
	}
	render := rpc + rel
	if call < rpc {
		t.Errorf("the generation is verified before the response exists: call=%d rpc=%d", call, rpc)
	}
	if call > render {
		t.Errorf("the generation is verified AFTER the briefing is rendered: call=%d render=%d", call, render)
	}
	line := src[call:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if !strings.Contains(line, "resp.GetAuthority().GetLiveStoreGraphDigestSha256()") {
		t.Errorf("the check is not given the digest the response itself carried: %s", strings.TrimSpace(line))
	}
	// And a refusal must end the command rather than warn.
	tail := src[call:]
	if end := strings.Index(tail, "\n\t}"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "return 1") {
		t.Errorf("a refused generation does not end the briefing:\n%s", tail)
	}
}

// THE INVARIANT, enforced once for the whole package rather than by sixteen weak
// per-command checks:
//
//	For a governed repository/domain there is exactly ONE code path by which a
//	production command chooses which graph to read, and that path is the G2 owner.
//
// A command carrying `defaultServiceAddr()` as its --addr default owns a graph choice,
// whatever it does afterwards. So the mechanical form of the invariant is: no command
// declares a default endpoint. Only the owner may name the built-in tier.
//
// This is the check that makes the migration finishable and keeps it finished: a new
// command added next year with a default port fails here, which is the only way a
// sixteen-file migration does not silently regrow.
func TestNoProductionCommandDeclaresADefaultGraphEndpoint(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cmd_") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := readCmdSource(t, name)
		if strings.Contains(src, `fs.String("addr", defaultServiceAddr()`) {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%d command(s) still declare a default graph endpoint, so each still owns a graph choice: %s\n"+
			"Resolve through productionReader/resolveGraphReader and declare the flag with an empty default.",
			len(offenders), strings.Join(offenders, ", "))
	}
}

// The owner is the only place the built-in tier may be named, and it must still name it:
// removing the final tier would make an unresolvable domain produce an empty address
// rather than a truthful default.
func TestOnlyTheOwnerNamesTheBuiltInDefault(t *testing.T) {
	if !strings.Contains(readCmdSource(t, "endpoint_binding.go"), "defaultServiceAddr()") {
		t.Error("the resolution owner no longer names the built-in default; an unresolvable domain would yield an empty endpoint")
	}
}

// Every command that reaches the graph must resolve through the owner. Counted rather
// than sampled, so a command that dials without resolving is named.
func TestEveryGraphReachingCommandResolvesThroughTheOwner(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var unresolved []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cmd_") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := readCmdSource(t, name)
		if !strings.Contains(src, `fs.String("addr",`) {
			continue // not a graph reader
		}
		// `serve` is the exception with a reason: its --addr is the address it LISTENS
		// on, not a graph it reads. Named explicitly rather than pattern-excluded, so a
		// future reader cannot slip through by resembling it.
		if name == "cmd_serve.go" {
			continue
		}
		if !strings.Contains(src, "productionReaderFor(") && !strings.Contains(src, "resolveGraphReader(") {
			unresolved = append(unresolved, name)
		}
	}
	if len(unresolved) > 0 {
		t.Errorf("%d graph-reading command(s) choose an endpoint without the G2 owner: %s",
			len(unresolved), strings.Join(unresolved, ", "))
	}
}

// Law 5 for every command that CAN check. Seven response types carry a GraphAuthority
// (Briefing, Impact, Metadata, Preflight, Query, ReferenceSites, Resolve), so the
// commands consuming them can compare the generation that answered against the declared
// ACTIVE one. Those that cannot are named here rather than left as a silent gap.
//
// This is the check that stops the gap reopening: a command that gains an
// authority-carrying response and forgets the comparison fails here.
func TestEveryCommandThatCanVerifyTheServedGenerationDoes(t *testing.T) {
	canVerify := map[string]string{
		"cmd_briefing.go":  "Briefing",
		"cmd_metadata.go":  "Metadata",
		"cmd_impact.go":    "Impact",
		"cmd_preflight.go": "Preflight",
		"cmd_query.go":     "Query",
		"cmd_resolve.go":   "Resolve",
		// gate consumes EditCheck, which carries no authority, and asks Metadata on the
		// same connection for the served generation. See verifyGateServedGeneration; the
		// behaviour is pinned by the witnesses in gate_generation_test.go.
		"cmd_gate.go": "Metadata",
	}
	for name, rpc := range canVerify {
		src := readCmdSource(t, name)
		if name == "cmd_metadata.go" {
			// metadata REPORTS the verdict rather than refusing on it: its whole purpose
			// is to describe a disagreement, so refusing would hide the one answer an
			// operator ran it to get.
			// The verdict is rendered by renderEndpointBlock (active_generation.go),
			// which is where verifyActiveGeneration is called; metadata's obligation is
			// to invoke it.
			if !strings.Contains(src, "renderEndpointBlock(") {
				t.Errorf("%s no longer renders the endpoint block that reports the generation verdict", name)
			}
			continue
		}
		if !strings.Contains(src, "reader.verifyServed(") {
			t.Errorf("%s consumes a %sResponse carrying a GraphAuthority but never checks the generation that answered", name, rpc)
		}
	}
}

// The rest of the census, stated as three groups rather than two.
//
// It used to say "the commands that genuinely cannot: their RPCs carry no authority, so
// there is nothing to compare". That sentence was true of the MESSAGES and false as a
// statement about the commands, and the gap let `sensei gate` enforce a verdict from any
// generation for as long as the list said gate could not check. Measured against the
// response schema on 2026-09-13, nine of the twelve already hold a GraphAuthority.
//
// So the groups are now: verifies (above), HOLDS AUTHORITY AND DOES NOT CHECK IT (an open
// finding, named here so it is countable rather than rediscovered), and consumes no
// authority-bearing response at all (which is not the same as unable -- gate was in this
// group and left it by spending one Metadata call).
func TestTheReaderCensusStatesWhyEachReaderDoesOrDoesNotVerify(t *testing.T) {
	// OPEN FINDING. Each of these already receives a GraphAuthority and never compares the
	// generation that answered against the one the registry declares ACTIVE. Three of them
	// call requireAuthoritativeGraph, which asks whether the graph is internally
	// authoritative -- a different question: a graph can be perfectly authoritative and
	// still be the wrong generation for this domain (law 13).
	holdsAuthorityButDoesNotCheck := map[string]string{
		"cmd_verify_obligations.go": "PreflightResponse.authority",
		"cmd_edit_brief.go":         "BriefingResponse.authority",
		"cmd_contract_bootstrap.go": "ImpactResponse.authority and PreflightResponse.authority",
		"cmd_repair_plan.go":        "PreflightResponse.authority, kept in repairPlanResult.Authority",
		"cmd_pattern_check.go":      "BriefingResponse.authority",
		"cmd_repair_report.go":      "MetadataResponse.authority, already fetched via repairReportMetadata",
		"cmd_benchmark_brief.go":    "PreflightResponse.authority, via buildAuthoritativeRepairPlan",
		"cmd_benchmark_score.go":    "PreflightResponse.authority, via buildAuthoritativeRepairPlan",
		"cmd_synthesis_run.go":      "MetadataResponse.authority, via composeSynthesisRunIdentity",
	}
	// These consume only EditCheck, which states no generation. Verifying costs them a
	// separate Metadata call, exactly as it costs gate.
	consumesNoAuthority := []string{"cmd_edit_check.go", "cmd_edit_guard.go"}

	for name := range holdsAuthorityButDoesNotCheck {
		src := readCmdSource(t, name)
		// Endpoint selection is closed for every reader, verified or not.
		if !strings.Contains(src, "productionReaderFor(") {
			t.Errorf("%s does not resolve through the G2 owner", name)
		}
		if strings.Contains(src, "reader.verifyServed(") {
			t.Errorf("%s now verifies the served generation; move it into canVerify above and out of the open finding", name)
		}
	}
	for _, name := range consumesNoAuthority {
		src := readCmdSource(t, name)
		if !strings.Contains(src, "productionReaderFor(") {
			t.Errorf("%s does not resolve through the G2 owner", name)
		}
	}
	// The count is stated, so a reader added or reclassified cannot pass unnoticed.
	const verifying = 7
	total := verifying + len(holdsAuthorityButDoesNotCheck) + len(consumesNoAuthority)
	if total != 18 {
		t.Errorf("the reader census is %d (%d verifying + %d holding authority unchecked + %d without authority), expected 18",
			total, verifying, len(holdsAuthorityButDoesNotCheck), len(consumesNoAuthority))
	}
}

// The override notice must actually be EMITTED by the helper every command calls.
// Testing nonCanonicalReaderNotice in isolation proved the sentence can be composed; a
// mutant that deleted the emission survived exactly that gap, so this drives the real
// helper and captures what it writes.
func TestTheSharedHelperAnnouncesANonCanonicalOverride(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	writeProjectConfig(t, root, "localhost:10122")
	t.Chdir(root)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("addr", "", "")
	if err := fs.Parse([]string{"-addr", "localhost:19191"}); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() {
		r := productionReaderFor(fs, "example.com/acme/thing", "localhost:19191")
		if r.Addr != "localhost:19191" {
			t.Errorf("the override was not honoured: %q", r.Addr)
		}
	})
	if !strings.Contains(out, "non-canonical override") {
		t.Errorf("the helper did not announce the override:\n%s", out)
	}
	if !strings.Contains(out, "localhost:19191") {
		t.Errorf("the announcement does not name the endpoint it is reading:\n%s", out)
	}

	// And canonical resolution stays silent — a notice printed every run is a notice
	// nobody reads.
	quiet := captureStderr(t, func() {
		fs2 := flag.NewFlagSet("test2", flag.ContinueOnError)
		fs2.String("addr", "", "")
		_ = fs2.Parse(nil)
		productionReaderFor(fs2, "example.com/acme/thing", "")
	})
	if strings.Contains(quiet, "non-canonical") {
		t.Errorf("canonical resolution announced itself as an override:\n%s", quiet)
	}
}
