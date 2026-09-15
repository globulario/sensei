// SPDX-License-Identifier: AGPL-3.0-only

package main

// ROOT / GOVERNED-DOMAIN COHERENCE.
//
// The invariant:
//
//	Executing from any directory inside the same governed repository resolves the same project
//	root, governed domain, endpoint authority class, registry entry, and canonical endpoint as
//	executing from the repository root.
//
// Five things, not one. Comparing endpoints alone is not enough: the dangerous case is a nested run
// that dials the SAME address as the root run while having silently skipped the registry, because
// then the weaker authority answered and nothing looks wrong.
//
// Mechanism under test: resolveProjectRoot walks up only when its argument is empty --
//
//	if explicit != "" { return filepath.Abs(explicit) }
//
// -- and --repo / --repo-root default to ".", which is never empty. So for those subjects the
// walk-up branch is unreachable by default, and the governed domain is looked for in whatever
// directory the operator happens to be standing in. The flags that default to "" document
// "walk up for docs/awareness or .sensei/config.yaml" and behave correctly, which is the contrast
// that shows this is a defect rather than a design.

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
)

const coherenceDomain = "example.com/acme/coherence"

// resolutionFingerprint is the five-part answer the invariant says must not depend on where the
// operator stands.
type resolutionFingerprint struct {
	ProjectRoot   string
	Domain        string
	Authority     endpointAuthority
	RegistryEntry string // the registry's service_addr for the resolved domain, "" when none applies
	Endpoint      string
}

func (f resolutionFingerprint) String() string {
	return "root=" + f.ProjectRoot + " domain=" + f.Domain + " authority=" + f.Authority.String() +
		" registryEntry=" + f.RegistryEntry + " endpoint=" + f.Endpoint
}

// fingerprintAsPreflightDoes reproduces runPreflight's real resolution sequence: resolve the project
// root from the --repo flag, resolve the governed domain from that root, then ask the owner.
func fingerprintAsPreflightDoes(t *testing.T, repoFlag, registryPath string) resolutionFingerprint {
	t.Helper()
	// Exactly what a repaired subject does: normalise the hint once, then use it everywhere.
	repoFlag = governedRepoRoot(repoFlag)
	root, _ := resolveProjectRoot(repoFlag)
	domain := resolveRepositoryDomain(repoFlag, "").Domain
	reader := productionReaderFor(emptyFlags(), repoFlag, domain, "")
	entry := ""
	if domain != "" {
		if reg, err := LoadDomainRegistry(registryPath); err == nil && reg != nil {
			entry = strings.TrimSpace(reg.Domains[domain].ServiceAddr)
		}
	}
	return resolutionFingerprint{
		ProjectRoot:   root,
		Domain:        domain,
		Authority:     reader.Authority,
		RegistryEntry: entry,
		Endpoint:      reader.Addr,
	}
}

// coherenceWorld builds one governed repository with a nested subdirectory.
//
// THE REGISTRY AND THE PROJECT CONFIG NAME THE SAME ADDRESS ON PURPOSE. That is what makes this the
// dangerous case rather than an obvious one: a nested run that skips the registry still dials the
// right port, so the endpoint comparison every existing witness makes cannot see the authority loss.
func coherenceWorld(t *testing.T) (root, sub, registryPath, sharedAddr string) {
	t.Helper()
	sharedAddr = "127.0.0.1:41777"

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	registryPath = filepath.Join(home, ".sensei", "domains.yaml")
	if err := os.WriteFile(registryPath, []byte("domains:\n    "+coherenceDomain+
		":\n        repository_identity: acme/coherence\n        service_addr: "+sharedAddr+"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	root = projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
		"    domain: "+coherenceDomain+"\nserver:\n    addr: "+sharedAddr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub = filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, sub, registryPath, sharedAddr
}

// THE WITNESS. Same repository, same command shape, two working directories.
func TestNestedExecutionResolvesTheSameGovernedIdentityAsTheRoot(t *testing.T) {
	root, sub, registryPath, sharedAddr := coherenceWorld(t)

	// "" is the OMITTED flag after the repair. Passing "." here would simulate `--repo .`, which is
	// an explicit operator choice and correctly does not walk -- asserted separately in
	// TestAnExplicitRootIsUsedVerbatimAndNeverWalkedAwayFrom.
	t.Chdir(root)
	atRoot := fingerprintAsPreflightDoes(t, "", registryPath)
	t.Chdir(sub)
	atSub := fingerprintAsPreflightDoes(t, "", registryPath)

	t.Logf("at root:   %s", atRoot)
	t.Logf("at nested: %s", atSub)

	// PREMISE: the root run must be the correct one, or this measures nothing.
	if atRoot.Domain != coherenceDomain {
		t.Fatalf("at the repository root the domain resolved to %q, want %q; the premise is gone",
			atRoot.Domain, coherenceDomain)
	}
	if atRoot.Authority != endpointAuthorityDomainRegistry {
		t.Fatalf("at the repository root the authority was %v, want the domain registry", atRoot.Authority)
	}
	if atRoot.RegistryEntry != sharedAddr {
		t.Fatalf("at the repository root the registry entry was %q, want %q", atRoot.RegistryEntry, sharedAddr)
	}
	// A premise about the ROOT run only. It must not compare root against nested: an earlier version
	// aborted here whenever the two endpoints differed, which made the canonical-endpoint dimension
	// below unreachable and would have reported a real invariant violation as a fixture problem.
	// Blind review P2 on ab1a17cf, and it held. The reason the two endpoints coincide is a property
	// of the fixture -- registry and project config name the same address -- and it is asserted where
	// it belongs, in TestEndpointEqualityAloneWouldNotHaveCaughtTheDefect.
	if atRoot.Endpoint != sharedAddr {
		t.Fatalf("at the repository root the endpoint was %q, want %q", atRoot.Endpoint, sharedAddr)
	}

	// The five-way comparison the invariant actually requires. Every dimension must be able to
	// contribute, so nothing above may abort on a root-versus-nested difference.
	var broken []string
	if atRoot.ProjectRoot != atSub.ProjectRoot {
		broken = append(broken, "project root: "+atRoot.ProjectRoot+" vs "+atSub.ProjectRoot)
	}
	if atRoot.Domain != atSub.Domain {
		broken = append(broken, "governed domain: "+q(atRoot.Domain)+" vs "+q(atSub.Domain))
	}
	if atRoot.Authority != atSub.Authority {
		broken = append(broken, "endpoint authority class: "+atRoot.Authority.String()+" vs "+atSub.Authority.String())
	}
	if atRoot.RegistryEntry != atSub.RegistryEntry {
		broken = append(broken, "registry entry: "+q(atRoot.RegistryEntry)+" vs "+q(atSub.RegistryEntry))
	}
	if atRoot.Endpoint != atSub.Endpoint {
		broken = append(broken, "canonical endpoint: "+atRoot.Endpoint+" vs "+atSub.Endpoint)
	}

	if len(broken) != 0 {
		t.Fatalf("root and nested execution disagree on %d of 5 dimensions:\n  %s\n\nThe invariant is "+
			"that executing from anywhere inside one governed repository resolves the same project "+
			"root, governed domain, endpoint authority class, registry entry and canonical endpoint.",
			len(broken), strings.Join(broken, "\n  "))
	}
	// The nested run must specifically have consulted the REGISTRY, not merely landed on the same
	// address. This is the assertion the old endpoint-only comparison could not make.
	if atSub.Authority != endpointAuthorityDomainRegistry {
		t.Fatalf("the nested run resolved its endpoint from %v; the registry must be consulted from "+
			"a subdirectory too, or the weaker authority answered at a coincidentally equal address",
			atSub.Authority)
	}
	if atSub.RegistryEntry != sharedAddr {
		t.Fatalf("the nested run consulted registry entry %q, want %q", atSub.RegistryEntry, sharedAddr)
	}
	t.Logf("INVARIANT ESTABLISHED: root and nested execution agree on all five dimensions, and the "+
		"nested run reached the registry (%s) rather than falling back to the project config at the "+
		"same address.", sharedAddr)
}

// EXPLICIT ROOTS KEEP THEIR EXPLICIT MEANING. An operator who names a path gets that path, and the
// walk must not quietly relocate them.
func TestAnExplicitRootIsUsedVerbatimAndNeverWalkedAwayFrom(t *testing.T) {
	root, sub, registryPath, _ := coherenceWorld(t)

	// Standing at the root, naming the nested directory: the operator's choice wins, so the governed
	// domain is NOT found there. That is the correct answer, not a defect -- they asked for that path.
	t.Chdir(root)
	named := fingerprintAsPreflightDoes(t, sub, registryPath)
	if named.ProjectRoot != sub {
		t.Fatalf("an explicitly named root resolved %q, want exactly %q; the walk must not override "+
			"an operator's choice", named.ProjectRoot, sub)
	}

	// And standing INSIDE the nested directory while naming the repository root: again verbatim.
	t.Chdir(sub)
	namedRoot := fingerprintAsPreflightDoes(t, root, registryPath)
	if namedRoot.ProjectRoot != root {
		t.Fatalf("an explicitly named root resolved %q, want %q", namedRoot.ProjectRoot, root)
	}
	if namedRoot.Domain != coherenceDomain {
		t.Fatalf("naming the repository root from a subdirectory resolved domain %q, want %q",
			namedRoot.Domain, coherenceDomain)
	}
	if namedRoot.Authority != endpointAuthorityDomainRegistry {
		t.Fatalf("naming the repository root gave authority %v, want the domain registry", namedRoot.Authority)
	}
	// "." is now an explicit choice like any other path: from a subdirectory it means THIS
	// directory, and must not walk. This is the distinction the repair exists to create.
	t.Chdir(sub)
	dotted := fingerprintAsPreflightDoes(t, ".", registryPath)
	if dotted.ProjectRoot != sub {
		t.Fatalf(`--repo "." from %s resolved %q; an explicit "." must mean that directory`, sub, dotted.ProjectRoot)
	}
	if dotted.Domain == coherenceDomain {
		t.Fatalf(`--repo "." from a subdirectory resolved the governed domain %q; "." must not walk, `+
			`or it means the same thing as an omitted flag again`, dotted.Domain)
	}
	t.Logf("EXPLICIT ROOTS: --repo %s from the root stays at %s; --repo %s from a subdirectory "+
		`resolves the repository and reaches the registry; --repo "." from %s stays there with no `+
		"governed domain, which is the operator's stated choice.", sub, named.ProjectRoot, root, sub)
}

// The trap preserved as its own assertion: endpoint equality alone would have certified the broken
// state, because the registry and the project config name the SAME address. If that ever stops
// holding, the fixture has lost the property that made the defect dangerous, and the five-dimension
// witness above degrades into an ordinary endpoint comparison without anyone noticing.
//
// This test previously loaded the configuration from the wrong directory and discarded both the
// config and its error, then logged that the property held. It asserted nothing and would have
// passed with the fixture's config missing or naming a different address. Blind review P2 on
// 40a5932b, and it held: evidence that cannot fail is not evidence.
func TestEndpointEqualityAloneWouldNotHaveCaughtTheDefect(t *testing.T) {
	root, _, registryPath, sharedAddr := coherenceWorld(t)

	reg, err := LoadDomainRegistry(registryPath)
	if err != nil {
		t.Fatalf("the fixture's registry did not load: %v", err)
	}
	fromRegistry := strings.TrimSpace(reg.Domains[coherenceDomain].ServiceAddr)
	if fromRegistry != sharedAddr {
		t.Fatalf("registry service_addr = %q, want %q", fromRegistry, sharedAddr)
	}

	// The project config, read from the REPOSITORY ROOT, and asserted rather than discarded.
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		t.Fatalf("the fixture's project configuration did not load from %s: %v", root, err)
	}
	fromConfig := cfg.configuredServerAddr()
	if fromConfig == "" {
		t.Fatalf("the fixture's project configuration at %s names no server.addr, so the two "+
			"authorities no longer name the same endpoint and the trap is gone", root)
	}
	if fromConfig != sharedAddr {
		t.Fatalf("project config server.addr = %q, registry service_addr = %q: the fixture must make "+
			"them EQUAL, or an endpoint-only check would have caught the defect and this witness is "+
			"measuring something else", fromConfig, sharedAddr)
	}

	t.Logf("PRESERVED: registry service_addr and project config server.addr both name %s (verified "+
		"from %s), so a check comparing only the resolved endpoint passes in both the broken and the "+
		"repaired state. Authority provenance is what discriminates.", sharedAddr, root)
}

// THE MECHANISM, isolated from any subject. resolveProjectRoot walks up only for an empty argument,
// so a flag whose DEFAULT is "." can never reach the walking branch.
func TestTheWalkUpBranchIsUnreachableForANonEmptyRootHint(t *testing.T) {
	root, sub, _, _ := coherenceWorld(t)
	t.Chdir(sub)

	walked, err := resolveProjectRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if walked != root {
		t.Fatalf("an empty hint resolved %q, want the repository root %q", walked, root)
	}

	dotted, err := resolveProjectRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if dotted == root {
		t.Fatalf("%q resolved to the repository root; resolveProjectRoot now walks up for a "+
			"non-empty hint and this mechanism witness is obsolete", ".")
	}
	if dotted != sub {
		t.Fatalf(`"." resolved %q, want the working directory %q`, dotted, sub)
	}
	// resolveProjectRoot is deliberately unchanged: it has callers far beyond the graph readers.
	// governedRepoRoot is what turns an omitted flag into the walking case.
	if got := governedRepoRoot(""); got != root {
		t.Fatalf("governedRepoRoot(\"\") resolved %q, want the repository root %q", got, root)
	}
	if got := governedRepoRoot("."); got != "." {
		t.Fatalf("governedRepoRoot(%q) returned %q; a non-empty hint must be used verbatim", ".", got)
	}
	t.Logf(`MECHANISM: from %s, resolveProjectRoot("") walks up to %s while resolveProjectRoot(".") `+
		`stays at %s -- unchanged. governedRepoRoot("") therefore resolves %s, and a non-empty hint `+
		`is passed through untouched.`, sub, walked, dotted, root)
}

// RE-ENTRY INVARIANT. A production graph-reader subject whose repository/root hint defaults to "."
// has reintroduced the defect: "." is never empty, so resolveProjectRoot cannot walk, and the
// governed domain is looked for wherever the operator stands.
//
// Derived from the census population every run, so a new subject is caught without anyone updating a
// list. This is the guard that replaces the hand-written set.
func TestNoGraphReaderSubjectDefaultsItsRepositoryHintToDot(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	const floor = 15
	if len(subjects) < floor {
		t.Fatalf("the census discovered %d subject(s); %d+ are known, so it has stopped enumerating "+
			"and this invariant would pass by finding nothing", len(subjects), floor)
	}
	files := map[string]bool{}
	for key := range subjects {
		if i := strings.Index(key, ":"); i > 0 {
			files[key[:i]] = true
		}
	}
	var offenders, normalised []string
	for f := range files {
		src := readCmdSource(t, f)
		for _, name := range []string{"repo", "repo-root", "root"} {
			if strings.Contains(src, `fs.String("`+name+`", ".", `) {
				offenders = append(offenders, f+" --"+name)
			}
			if strings.Contains(src, `fs.String("`+name+`", "", `) {
				normalised = append(normalised, f+" --"+name)
			}
		}
	}
	sort.Strings(offenders)
	sort.Strings(normalised)
	if len(offenders) != 0 {
		t.Errorf("%d graph-reader repository hint(s) still default to \".\":\n  %s\n\n"+
			"An omitted hint must be empty so it means \"discover the governed repository\"; \".\" "+
			"means \"the operator chose this directory\" and defeats the walk, which silently removes "+
			"the registry from endpoint resolution.", len(offenders), strings.Join(offenders, "\n  "))
	}
	if len(normalised) == 0 {
		t.Fatal("no graph-reader subject declares an empty repository hint, so this invariant is " +
			"vacuous and would pass on a tree with the defect fully restored")
	}
	t.Logf("RE-ENTRY GUARD: %d census subject file(s), %d empty-default repository hint(s), 0 "+
		"defaulting to \".\".", len(files), len(normalised))
}

// Every subject that declares an empty repository hint must normalise it, or the empty value reaches
// code that joins it into a path and silently means "/" or the process working directory.
func TestEverySubjectWithAnEmptyRepositoryHintNormalisesIt(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	files := map[string]bool{}
	for key := range subjects {
		if i := strings.Index(key, ":"); i > 0 {
			files[key[:i]] = true
		}
	}
	var missing []string
	checked := 0
	for f := range files {
		src := readCmdSource(t, f)
		for _, name := range []string{"repo", "repo-root"} {
			decl := `fs.String("` + name + `", "", `
			n := strings.Count(src, decl)
			if n == 0 {
				continue
			}
			checked += n
			if got := strings.Count(src, "governedRepoRoot("); got < n {
				missing = append(missing, fmt.Sprintf("%s --%s: %d declaration(s), %d normalisation(s)",
					f, name, n, got))
			}
		}
	}
	if len(missing) != 0 {
		t.Errorf("%d subject(s) declare an empty repository hint without normalising it:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if checked == 0 {
		t.Fatal("no empty repository hints were found, so this invariant is vacuous")
	}
	t.Logf("NORMALISATION: %d empty repository hint declaration(s), all normalised through "+
		"governedRepoRoot.", checked)
}

// END TO END, through the real command, from a subdirectory with --repo OMITTED.
//
// The registry names a LIVE endpoint; the project config names a DEAD one. So the authority actually
// used is observable in the exit code and in whether the live service was contacted, with no source
// inspection at all:
//
//	repaired  the domain resolves by walking up, the registry is consulted, the live endpoint answers
//	broken    the domain is empty, the registry is skipped, the dead config endpoint is dialled
//
// This is what makes the coherence repair behaviourally verifiable rather than only structurally
// asserted, and it is the case that was impossible to construct before the repair: previously a
// nested run could not reach the registry at all, so no fixture could distinguish the two roots by
// behaviour.
func TestFromASubdirectoryWithNoRepoFlagTheRegistryStillAnswers(t *testing.T) {
	svc := &orderingService{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	liveFromRegistry := lis.Addr().String()
	const deadFromConfig = "127.0.0.1:19"

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
		coherenceDomain+":\n        repository_identity: acme/coherence\n        service_addr: "+
		liveFromRegistry+"\n        active_generation: "+governedGeneration+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
		"    domain: "+coherenceDomain+"\nserver:\n    addr: "+deadFromConfig+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	// PREMISE: the two roots genuinely differ from here, so the walk is doing the work.
	shallow, _ := resolveProjectRoot(".")
	if shallow == root {
		t.Skipf("resolveProjectRoot(\".\") already returns the repository root from %s; this fixture "+
			"cannot distinguish the roots", sub)
	}

	stdout, stderr, code := captureBoth(t, func() int {
		return runPreflight([]string{"--task", "nested run, omitted repo flag"})
	})
	out := stdout + stderr

	if code != 0 {
		t.Fatalf("runPreflight returned %d from a subdirectory with --repo omitted. The governed "+
			"domain must resolve by walking up, so the registry is consulted and %s answers; a "+
			"failure here means the registry was skipped and the project config's %s was dialled."+
			"\nout:\n%s", code, liveFromRegistry, deadFromConfig, out)
	}
	if svc.calls != 1 {
		t.Fatalf("the registry's endpoint was contacted %d times, want 1.\nout:\n%s", svc.calls, out)
	}
	// The config's endpoint DOES appear, in the visibility notice the previous family added — which
	// is the two repairs composing: coherence made the registry reachable from here, and the notice
	// then reports that the repository's own configuration lost. What must not happen is it being
	// READ, and the notice says so explicitly.
	if !strings.Contains(out, "the domain registry names it for this domain") {
		t.Fatalf("the nested run did not report resolving through the registry.\nout:\n%s", out)
	}
	if !strings.Contains(out, "Nothing was read from "+deadFromConfig) {
		t.Fatalf("the notice does not state that the configured endpoint was left alone.\nout:\n%s", out)
	}
	t.Logf("BEHAVIOURAL COHERENCE: from %s with --repo omitted, the registry's %s answered (%d call); "+
		"the project config's %s was reported as stale and never read.",
		sub, liveFromRegistry, svc.calls, deadFromConfig)
}

// coherentGraph serves the three RPCs the affected subjects reach, so one fixture can drive all of
// them and record which endpoint answered.
type coherentGraph struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	calls int
}

func (s *coherentGraph) Preflight(context.Context, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	s.calls++
	return &awarenesspb.PreflightResponse{
		Status: awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			// governedGeneration: since Category C, runPreflight refuses a served generation the
			// registry does not declare ACTIVE, so a fixture that drives preflight to completion must
			// be a GOVERNED world. This served digest matches the active_generation these registries
			// declare; an endpoint witness is not the place to also be unverifiable.
			LiveStoreGraphDigestSha256: governedGeneration,
		},
	}, nil
}

func (s *coherentGraph) Briefing(context.Context, *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
	s.calls++
	return &awarenesspb.BriefingResponse{
		Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			// governedGeneration: since Category C, runPreflight refuses a served generation the
			// registry does not declare ACTIVE, so a fixture that drives preflight to completion must
			// be a GOVERNED world. This served digest matches the active_generation these registries
			// declare; an endpoint witness is not the place to also be unverifiable.
			LiveStoreGraphDigestSha256: governedGeneration,
		},
	}, nil
}

// EVERY AFFECTED SUBJECT, from a subdirectory, with its repository flag OMITTED.
//
// The registry names a LIVE endpoint and the project config names a DEAD one, so which authority was
// used is visible in whether the live service was contacted — no source inspection. This is the
// behavioural half of the completion oracle: it is not enough that preflight is coherent.
func TestEveryAffectedSubjectResolvesTheRegistryFromASubdirectory(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(live string) int
	}{
		{"preflight", func(string) int {
			return runPreflight([]string{"--task", "nested coherence"})
		}},
		{"verify-obligations", func(string) int {
			// Checked, not discarded: a failed write makes runVerifyObligations exit early on a
			// missing --results file, and this subtest would then blame domain resolution for a
			// fixture problem. A witness that fails for the wrong reason is its own defect.
			results := filepath.Join(t.TempDir(), "results.json")
			if err := os.WriteFile(results, []byte(`{"tests":[]}`), 0o644); err != nil {
				t.Fatalf("fixture: could not write the results file: %v", err)
			}
			return runVerifyObligations([]string{"--task", "nested coherence", "--results", results})
		}},
		{"briefing", func(string) int {
			return runBriefing([]string{"--task", "nested coherence"})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &coherentGraph{}
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			srv := grpc.NewServer()
			awarenesspb.RegisterAwarenessGraphServer(srv, svc)
			go func() { _ = srv.Serve(lis) }()
			t.Cleanup(srv.Stop)
			live := lis.Addr().String()
			const dead = "127.0.0.1:19"

			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SENSEI_DOMAIN", "")
			t.Setenv("AWG_DOMAIN", "")
			t.Setenv("SENSEI_ADDR", "")
			if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte("domains:\n    "+
				coherenceDomain+":\n        repository_identity: acme/coherence\n        service_addr: "+
				live+"\n        active_generation: "+governedGeneration+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			root := projectRoot(t, t.TempDir())
			if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("repository:\n"+
				"    domain: "+coherenceDomain+"\nserver:\n    addr: "+dead+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			sub := filepath.Join(root, "golang", "server")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(sub)

			_, _, _ = captureBoth(t, func() int { return c.run(live) })

			// The only assertion that matters: the REGISTRY's endpoint answered. If the governed
			// domain failed to resolve from here, the registry is skipped and the dead config
			// endpoint is dialled instead, so calls stays at zero.
			if svc.calls == 0 {
				t.Fatalf("%s contacted the registry's endpoint %d times from %s with its repository "+
					"flag omitted. The governed domain must resolve by walking up, or the registry "+
					"disappears from resolution and the weaker project-config authority answers.",
					c.name, svc.calls, sub)
			}
			t.Logf("%s: registry endpoint %s answered (%d call) from a subdirectory with the "+
				"repository flag omitted; the config's %s was not used.", c.name, live, svc.calls, dead)
		})
	}
}

// WHY THE SHARED-ROOT MUTANT IS NOW EQUIVALENT RATHER THAN MERELY UNTESTED.
//
// The previous family left a debt: reading the endpoint configuration from resolveProjectRoot instead
// of graphConfigRoot could not be observed behaviourally. After this repair it still cannot — but for
// a better reason. The repository hint reaching those calls is already the walked root, so the two
// root functions provably return the SAME value there. The divergence was eliminated rather than
// tested, which is why the source assertion remains the right instrument for it.
func TestAfterNormalisationBothRootFunctionsAgree(t *testing.T) {
	root, sub, _, _ := coherenceWorld(t)
	t.Chdir(sub)

	normalised := governedRepoRoot("")
	if normalised != root {
		t.Fatalf("governedRepoRoot(\"\") = %q, want %q", normalised, root)
	}
	walked := graphConfigRoot(normalised)
	shallow, _ := resolveProjectRoot(normalised)
	if walked != shallow {
		t.Fatalf("after normalisation the two root functions still disagree: graphConfigRoot=%q "+
			"resolveProjectRoot=%q — the shared-root mutant is behaviourally observable again and "+
			"must be killed that way", walked, shallow)
	}
	if walked != root {
		t.Fatalf("both root functions agree on %q, but that is not the repository root %q", walked, root)
	}
	t.Logf("EQUIVALENCE EXPLAINED: from %s, the normalised hint is %s, and graphConfigRoot and "+
		"resolveProjectRoot both return it. The shared-root divergence no longer exists to observe.", sub, walked)
}

// q renders an empty value visibly, so a diff between "" and a real value reads as a difference
// rather than as trailing whitespace.
func q(s string) string {
	if s == "" {
		return `""`
	}
	return s
}
