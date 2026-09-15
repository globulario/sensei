// SPDX-License-Identifier: AGPL-3.0-only

package main

// PUBLICATION GOVERNED-DOMAIN IDENTITY vs GRAPH TAGGING KIND.
//
// The invariant:
//
//	Publication governed-domain identity must never be derived from graph tagging kind / node
//	classification.
//
// These are two different vocabularies that share a flag name:
//
//	--repo    github.com/owner/name   the GOVERNED DOMAIN whose graph is being published
//	--domain  repo | shared           the default TAGGING KIND for untagged nodes
//
// publishedDomain(repoFlag, domainFlag) returns the first non-empty of the two, so with --repo
// omitted the tagging kind becomes the governed domain — and that value is then used by three
// separate authorities: store custody (verifyStoreOwnership), the store mutation intent recorded for
// the upload, and the ACTIVE generation pointer (activateGeneration).
//
// The authority to tell the two apart ALREADY EXISTS: repodomain.Validate, reached through
// validateDomain, requires host/path and rejects both tagging kinds. publishedDomain never consults
// it. That is the defect — not the string "shared", which is a legitimate tagging kind.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

// THE INVERTED SPECIMEN. Formerly TestTaggingKindMustNotBecomeThePublishedGovernedDomain recording
// that the tagging kind DID become the governed domain. It now requires the substitution to be
// refused, and requires the refusal to rest on the governed-domain authority rather than on a banned
// string.
func TestTaggingKindMustNotBecomeThePublishedGovernedDomain(t *testing.T) {
	const taggingKind = "shared"

	// PREMISE: "shared" is a real tagging kind and NOT a governed domain. Both asserted, so the repair
	// cannot be satisfied by banning a string.
	if err := validateDomain(taggingKind); err == nil {
		t.Fatalf("validateDomain accepts %q as a governed domain; the two vocabularies no longer differ",
			taggingKind)
	}
	if rdfDomainShared != taggingKind {
		t.Fatalf("the tagging kind constant is %q, not %q", rdfDomainShared, taggingKind)
	}

	// THE SUBSTITUTION IS REFUSED — by yielding no governed domain, not by erroring on the kind. A
	// tagging kind in --domain is legitimate; it simply is not a publication identity.
	got, err := publishedGovernedDomain("", taggingKind)
	if err != nil {
		t.Fatalf("a legitimate tagging kind in --domain produced an error (%v); --domain repo|shared is "+
			"its documented use and must keep working", err)
	}
	if got != "" {
		t.Fatalf("publishedGovernedDomain returned %q from the tagging kind; a tagging kind must never "+
			"become a governed domain", got)
	}

	// AND NO AUTHORITY STATE IS WRITTEN UNDER IT. This is the consequence that made the defect matter:
	// an ACTIVE pointer registered under a name that is not a governed domain.
	dir := t.TempDir()
	registry := filepath.Join(dir, "domains.yaml")
	if err := os.WriteFile(registry, []byte("domains:\n    "+taggingKind+
		":\n        repository_identity: acme/whatever\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const gen = "7777777777777777777777777777777777777777777777777777777777777777"
	marker := seedmeta.Marker{Digest: gen, IRI: "urn:sensei:graph:" + gen, TripleCount: 99}
	var notice bytes.Buffer
	if err := activateGeneration(&notice, filepath.Join(dir, "marker.json"), marker, got, registry); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	reg, err := LoadDomainRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if active := strings.TrimSpace(reg.Domains[taggingKind].ActiveGeneration); active != "" {
		t.Fatalf("an ACTIVE generation %q was registered under the tagging kind %q", active, taggingKind)
	}
	// And the absence is REPORTED, not silent: a publication naming no governed domain says so.
	if !strings.Contains(notice.String(), "NOT updated") {
		t.Fatalf("a publication with no governed domain did not report that the pointer was left "+
			"alone:\n%s", notice.String())
	}

	t.Logf("INVARIANT ESTABLISHED: --domain %q yields no governed domain, no ACTIVE pointer is "+
		"registered under it, and the omission is reported. The refusal rests on validateDomain "+
		"(host/path), not on the string %q.", taggingKind, taggingKind)
}

// A GOVERNED DOMAIN IN THE TAGGING-KIND FLAG is an operator error with a clear remedy, so it is
// refused by name rather than silently dropped — dropping it would publish without the pointer they
// intended.
func TestAGovernedDomainPassedAsATaggingKindIsRefusedByName(t *testing.T) {
	const governed = "github.com/globulario/sensei"
	if err := validateDomain(governed); err != nil {
		t.Fatalf("fixture: %q is not a governed domain: %v", governed, err)
	}
	got, err := publishedGovernedDomain("", governed)
	if err == nil {
		t.Fatalf("a governed domain in --domain was accepted silently (result %q); the operator meant "+
			"--repo and would otherwise publish with no ACTIVE pointer", got)
	}
	if got != "" {
		t.Fatalf("a refused resolution still returned %q", got)
	}
	for _, must := range []string{governed, "--repo", "tagging kind"} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the refusal omits %q, so the remedy is not actionable: %v", must, err)
		}
	}
	t.Logf("REFUSED BY NAME: %v", firstLine(err.Error()))
}

// --repo must itself be a governed domain. The flag is the publication identity, so an invalid value
// must not reach the three authorities that consume it.
func TestAnInvalidRepoFlagIsRefusedAsAGovernedDomain(t *testing.T) {
	for _, bad := range []string{"shared", "repo", "acme", "not a domain"} {
		got, err := publishedGovernedDomain(bad, "")
		if err == nil {
			t.Errorf("--repo %q was accepted as a governed domain (result %q)", bad, got)
		}
		if got != "" {
			t.Errorf("--repo %q was refused but still returned %q", bad, got)
		}
	}
	// And a real one passes through untouched.
	const good = "github.com/globulario/sensei-code"
	got, err := publishedGovernedDomain(good, "shared")
	if err != nil {
		t.Fatalf("--repo %q was refused: %v", good, err)
	}
	if got != good {
		t.Fatalf("--repo %q resolved to %q; a valid governed domain must pass through verbatim, and the "+
			"tagging kind beside it must not interfere", good, got)
	}
	t.Logf("GOVERNED DOMAIN AUTHORITY: --repo is validated by validateDomain; a tagging kind alongside " +
		"it is ignored for identity purposes.")
}

// The three authorities that consume the published governed domain, named from source so a future
// fourth consumer cannot be added without this specimen noticing.
func TestEveryConsumerOfThePublishedGovernedDomainIsAccountedFor(t *testing.T) {
	src := readCmdSource(t, "cmd_build.go")
	if n := strings.Count(src, "publishedGovernedDomain("); n != 1 {
		t.Fatalf("the published governed domain is resolved %d times, want exactly 1", n)
	}
	for _, consumer := range []string{
		"verifyStoreOwnership(reg, govDomain,",                         // store custody
		"Domain: govDomain,",                                           // recorded store mutation intent
		"activateGeneration(os.Stderr, markerPath, marker, govDomain,", // the ACTIVE generation pointer
	} {

		if !strings.Contains(src, consumer) {
			t.Errorf("expected consumer not found, so the specimen's account of the blast radius is "+
				"stale: %s", consumer)
		}
	}
	t.Log("BLAST RADIUS: one resolution feeds store custody, the recorded store mutation intent, and " +
		"the ACTIVE generation pointer. A tagging kind can no longer reach any of them, and a fourth " +
		"consumer cannot be added on another route without this failing.")
}

// THE REFUSAL MUST BE FATAL, proven through the real command.
//
// A resolution that computes a refusal and proceeds anyway is worse than no resolution: it publishes
// under an identity it has already judged invalid. Reachable with no store, because the resolution
// happens immediately after flag parsing — so this is a behavioural witness, not a source assertion.
func TestBuildRefusesAGovernedDomainPassedAsATaggingKind(t *testing.T) {
	// Isolated HOME: this must not read or write operator state, and it must not reach a store.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	t.Chdir(projectRoot(t, t.TempDir()))

	stdout, stderr, code := captureBoth(t, func() int {
		return runBuild([]string{"--all", "--domain", "github.com/globulario/sensei"})
	})
	out := stdout + stderr

	if code == 0 {
		t.Fatalf("sensei build accepted a governed domain in the tagging-kind flag.\nout:\n%s", out)
	}
	if !strings.Contains(out, "names the node tagging kind") {
		t.Fatalf("build did not refuse for the identity-conflation reason; it must not proceed on an "+
			"identity it already judged invalid.\nout:\n%s", out)
	}
	// It must refuse BEFORE any store work: no upload, no marker, no pointer.
	for _, leaked := range []string{"replaces the ENTIRE store", "graph marker:", "ACTIVE generation:"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("build proceeded past the refusal (%q appeared), so the refusal is not fatal."+
				"\nout:\n%s", leaked, out)
		}
	}
	t.Logf("FATAL REFUSAL: exit %d before any store work.", code)
}

// And a legitimate tagging kind must still reach the store path — the refusal must be aimed at the
// identity conflation, not at --domain existing.
func TestBuildStillAcceptsALegitimateTaggingKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("AWG_DOMAIN", "")
	t.Chdir(projectRoot(t, t.TempDir()))

	stdout, stderr, _ := captureBoth(t, func() int {
		return runBuild([]string{"--all", "--domain", "shared", "--store-url", "http://127.0.0.1:9/store?default"})
	})
	out := stdout + stderr
	if strings.Contains(out, "names the node tagging kind") {
		t.Fatalf("a legitimate tagging kind was refused as an identity conflation; --domain repo|shared "+
			"is its documented use.\nout:\n%s", out)
	}
	t.Log("a legitimate tagging kind is not refused by the identity guard")
}
