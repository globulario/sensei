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

// BLIND-PASS FINDING (P2, cmd_briefing.go:87): briefing passed --repo as the project root.
//
// --repo means "repository checkout for --task active" and defaults to ".", so endpoint
// resolution read ./.sensei/config.yaml. Run from a SUBDIRECTORY the config was not found and
// resolution fell through, giving the same command a different endpoint depending on the working
// directory. Every other reader walks up via productionReaderFor.
//
// Driven through the real runBriefing and observed at the address it DIALS -- the RPC failure
// names it. A first version of this witness recomputed the resolution itself and passed either
// way, which is the same helper-not-caller shape this front keeps producing.
func TestBriefingDialsTheSameEndpointFromASubdirectory(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	// An endpoint nothing is listening on, so the dial fails and names itself.
	writeProjectConfig(t, root, "127.0.0.1:19191")
	sub := filepath.Join(root, "golang", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "thing.go"), []byte("package deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dialed := func(dir string) string {
		t.Helper()
		t.Chdir(dir)
		_, so, se := captureStdoutStderr(t, func() int {
			return runBriefing([]string{"--file", "golang/deep/thing.go", "--domain", "example.com/acme/thing"})
		})
		return so + se
	}
	fromRoot, fromSub := dialed(root), dialed(sub)
	const want = "127.0.0.1:19191"
	if !strings.Contains(fromRoot, want) {
		t.Fatalf("from the project root, briefing did not dial the configured endpoint:\n%s", fromRoot)
	}
	if !strings.Contains(fromSub, want) {
		t.Errorf("from a subdirectory, briefing dialed a DIFFERENT endpoint than from the root; the project config was not found:\n%s", fromSub)
	}
}
