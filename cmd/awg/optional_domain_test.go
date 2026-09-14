// SPDX-License-Identifier: AGPL-3.0-only

package main

// FINDING 1, raised by blind review of head 59372102 at cmd_edit_check.go:43.
//
// The served-generation family's Shape B repair covered commands with NO domain source. It did
// not cover commands whose --domain flag is OPTIONAL: omit the flag and the raw "" reaches the
// owner, declaredActiveGeneration reads reg.Domains[""], the zero value, and the comparison is
// structurally incapable of detecting a wrong-domain graph.
//
// The finding named edit-check. The measurement is worse: ELEVEN of the twenty subjects hand the
// owner a raw flag value, and six of those are among the seven that verified BEFORE this family
// began. So the defect is not in edit-check and not in my repair -- it is in what the owner is
// given, which is why it is repaired at the owner and not in eleven commands.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// F1-REPRODUCER. The whole chain, at the level the defect lives: a project that states its
// domain, a registry that declares a generation ACTIVE for it, and a reader built from an
// omitted optional flag. The reader resolves fine. It just cannot detect anything.
func TestAnOmittedOptionalDomainLeavesTheComparisonIncapable(t *testing.T) {
	root := servedWorld(t, declaredGen, "")

	// The project CAN answer the question, which is what makes this a defect rather than an
	// unconfigured environment.
	if got := resolveRepositoryDomain(root, "").Domain; got != servedWitnessDomain {
		t.Fatalf("the project states no resolvable domain (%q); this witness cannot distinguish "+
			"an incapable comparison from an unanswerable one", got)
	}

	// Explicitly named: the comparison works, and refuses a foreign generation.
	explicit := productionReaderFor(emptyFlags(), servedWitnessDomain, "")
	if !explicit.declaresGeneration() {
		t.Fatal("the explicitly-named domain sees no declared generation; the registry fixture is wrong")
	}
	if err := explicit.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
	}); err == nil {
		t.Fatal("the explicitly-named reader accepted a foreign generation")
	}

	// Omitted: the SAME project, the SAME registry, the SAME foreign generation -- accepted.
	omitted := productionReaderFor(emptyFlags(), "", "")
	if err := omitted.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
	}); err != nil {
		return // repaired
	}
	t.Fatalf("with --domain omitted the comparison accepted generation %s while %s declares %s "+
		"ACTIVE: the expected domain was the raw flag value, so verification cannot fail",
		foreignGen, servedWitnessDomain, declaredGen)
}

// F1-CENSUS. The repair is at the owner, so "hands the owner a raw flag" stopped being the
// defect -- passing the raw flag is now correct, because the owner resolves it. The universal
// claim is assembled from two derived facts instead, and neither is a list:
//
//  1. every subject resolves through the owner -- already derived, per command, by
//     TestTheReaderCensusStatesWhyEachReaderDoesOrDoesNotVerify;
//  2. the owner ALWAYS resolves the expected domain, and is the ONLY place a graphReader is
//     constructed, so there is no second way to obtain one with an unresolved domain.
//
// This is the second half. A reader built anywhere else could carry any Domain the writer felt
// like, including "", with nothing to say it had not been resolved -- which is exactly the state
// blind review found reaching the comparison.
func TestOnlyTheOwnerConstructsAProductionGraphReader(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sites []string
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				id, ok := lit.Type.(*ast.Ident)
				if !ok || id.Name != "graphReader" {
					return true
				}
				sites = append(sites, fmt.Sprintf("%s:%d", filepath.Base(name),
					fset.Position(lit.Pos()).Line))
				return true
			})
		}
	}
	// ANTI-VACUITY. Finding zero would mean the detector stopped working, and zero sites is
	// indistinguishable from a compliant tree.
	if len(sites) == 0 {
		t.Fatal("no graphReader construction was found at all; this check has lost its subject")
	}
	if len(sites) != 1 {
		sort.Strings(sites)
		t.Errorf("a production graphReader is constructed at %d sites:\n  %s\n\nExactly one is "+
			"allowed, inside resolveGraphReader. Any other site can carry an expected domain "+
			"nothing resolved -- which is the state that made verification incapable.",
			len(sites), strings.Join(sites, "\n  "))
	}
	if !strings.HasPrefix(sites[0], "endpoint_binding.go:") {
		t.Errorf("the single construction site is %s, not the owner's file", sites[0])
	}
}

// F1-OWNER. The owner resolves, in both directions: an omitted domain becomes the checkout's,
// and an explicitly-named one wins over it. Without the second half a repair that always used
// the configured domain would pass the first and silently ignore every --domain an operator
// typed.
func TestTheOwnerResolvesTheExpectedDomainAndAnExplicitOneStillWins(t *testing.T) {
	root := servedWorld(t, declaredGen, "")

	omitted := productionReaderFor(emptyFlags(), "", "")
	if omitted.Domain != servedWitnessDomain {
		t.Errorf("with the flag omitted the expected domain is %q, want the checkout's %q",
			omitted.Domain, servedWitnessDomain)
	}
	if omitted.DomainInvalid != nil {
		t.Errorf("a resolvable checkout reported an unresolved domain: %v", omitted.DomainInvalid)
	}
	if !omitted.declaresGeneration() {
		t.Error("the resolved reader sees no declared generation, so it still cannot compare")
	}

	const other = "example.com/acme/elsewhere"
	named := productionReaderFor(emptyFlags(), other, "")
	if named.Domain != other {
		t.Errorf("an explicitly named domain was overridden: got %q, want %q", named.Domain, other)
	}
	_ = root
}

// F1-THREE-STATES. Two absences that used to be one empty string, now separate facts. This is
// what blind review asked to be distinguished, and each has a different consequence.
func TestAnUnboundCheckoutAndAnInvalidDomainAreDifferentFacts(t *testing.T) {
	// (a) UNBOUND: the checkout states no domain. Outside the invariant's quantifier -- it made
	// no declaration, so there is none to violate -- but recorded as its own fact so it can
	// never again be mistaken for "the registry declares nothing for this domain".
	root := servedWorld(t, declaredGen, "")
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unbound := productionReaderFor(emptyFlags(), "", "")
	if unbound.DomainUnbound == nil {
		t.Fatal("a checkout stating no domain did not record itself as unbound")
	}
	if unbound.DomainInvalid != nil {
		t.Errorf("an unbound checkout was recorded as stating an INVALID domain: %v", unbound.DomainInvalid)
	}
	if unbound.declaresGeneration() {
		t.Error("an unbound reader claims a declared generation; it never asked the registry")
	}
	if err := unbound.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: foreignGen,
	}); err != nil {
		t.Errorf("an unbound checkout was refused; it violates no declaration because it made "+
			"none, and refusing turns every ad-hoc read into a failure: %v", err)
	}

	// (b) STATED AND INVALID: something claimed an identity and it cannot be trusted. Refused.
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: \"unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := productionReaderFor(emptyFlags(), "", "")
	if invalid.DomainInvalid == nil {
		t.Fatal("a malformed checkout identity produced a reader with no recorded failure")
	}
	if invalid.DomainUnbound != nil {
		t.Errorf("an invalid domain was recorded as merely unbound: %v", invalid.DomainUnbound)
	}
	err := invalid.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: declaredGen,
	})
	if err == nil {
		t.Fatal("a malformed checkout identity verified successfully, even against the declared " +
			"generation; cannot-verify read as verified")
	}
	if !strings.Contains(err.Error(), "cannot verify") {
		t.Errorf("the refusal does not present itself as cannot-verify: %v", err)
	}
}

// F1-INVALID-EXPLICIT. An invalid domain an OPERATOR typed is refused the same way a malformed
// config is: the tier it came from does not change that an identity was claimed and cannot be
// trusted. Without this, --domain would be the one tier that could smuggle an untrusted value in.
func TestAnInvalidExplicitDomainIsRefusedLikeAMalformedConfiguration(t *testing.T) {
	servedWorld(t, declaredGen, "")
	r := productionReaderFor(emptyFlags(), "not a valid domain at all", "")
	if r.DomainInvalid == nil {
		t.Fatal("an invalid --domain value produced a reader with no recorded failure")
	}
	if err := r.verifyServedAuthority(&awarenesspb.GraphAuthority{
		LiveStoreGraphDigestSha256: declaredGen,
	}); err == nil {
		t.Fatal("an invalid --domain value verified successfully")
	}
}

// F1-END-TO-END, at the exact surface blind review named. edit-check's --domain is optional, and
// the whole point is that an operator who omits it is not opting out of authority. Driven
// against a real server so the finding is closed where it was raised, not only at the owner.
func TestEditCheckWithTheDomainFlagOmittedStillRefusesAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: foreignGen, noWarnings: true})
	root := servedWorld(t, declaredGen, addr)
	if err := os.MkdirAll(filepath.Join(root, "golang"), 0o755); err != nil {
		t.Fatal(err)
	}

	// NO --domain. This exited 0 and printed a clean verdict at head 59372102.
	var code int
	out := captureStdout(t, func() {
		code = runEditCheck([]string{"--file", "golang/thing.go", "--content", "package thing\n"})
	})
	if code == 0 {
		t.Errorf("edit-check exited 0 with --domain omitted, on a verdict from generation %s while "+
			"this checkout's domain declares %s ACTIVE", foreignGen, declaredGen)
	}
	if strings.Contains(out, "no advisory rule tripped") {
		t.Fatalf("edit-check printed a CLEAN verdict with --domain omitted:\n%s", out)
	}
}

// F1-END-TO-END-opposite. Omitting the flag must not become a refusal either: the resolved
// domain has to be USED, and used correctly, or the repair is just a new way to fail.
func TestEditCheckWithTheDomainFlagOmittedStillReportsWhenTheGenerationAgrees(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{servedGeneration: declaredGen, noWarnings: true})
	root := servedWorld(t, declaredGen, addr)
	if err := os.MkdirAll(filepath.Join(root, "golang"), 0o755); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runEditCheck([]string{"--file", "golang/thing.go", "--content", "package thing\n"})
	})
	if code != 0 || !strings.Contains(out, "no advisory rule tripped") {
		t.Fatalf("edit-check with --domain omitted refused the declared ACTIVE generation "+
			"(exit=%d):\n%s", code, out)
	}
}
