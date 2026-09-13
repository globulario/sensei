package main

// The ACTIVE generation pointer. Laws 1, 4, 5 and the rest of 12 all waited on it.
//
// Before this, a generation was identified only by a digest inside a marker FILE, and
// the marker's path resolves from the current directory. So nothing recorded WHICH
// generation is active for a domain, which meant:
//
//	law 1   no single ACTIVE identity per domain
//	law 4   a run pinned a digest, but not a generation anything else agreed on
//	law 5   no query could prove it belonged to the pinned generation
//	law 12  two services claiming one domain were undetectable
//
// The owner already exists: ~/.sensei/domains.yaml is operator-controlled and kept
// outside any published repository, which is precisely the property an ACTIVE pointer
// needs — a repository must not be able to declare its own graph active.
//
// It is inert until an operator or a publication records a generation, so behaviour is
// unchanged for every existing caller.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAServedGenerationMatchingTheRegistryIsActive(t *testing.T) {
	if err := verifyActiveGeneration("example.com/acme/thing", "abc123def456", "abc123def456"); err != nil {
		t.Fatalf("the served generation matches the registry and was refused: %v", err)
	}
}

// Law 12: two claimants for one domain is an error, not a choice for the caller.
func TestAServedGenerationTheRegistryDoesNotDeclareActiveIsRefused(t *testing.T) {
	err := verifyActiveGeneration("example.com/acme/thing", "abc123def456", "999999999999")
	if err == nil {
		t.Fatal("a graph serving a generation the registry does not declare active was accepted")
	}
	var amb *activeGenerationMismatchError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want a typed activeGenerationMismatchError", err)
	}
	for _, want := range []string{"example.com/acme/thing", "abc123def456", "999999999999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %q: %v", want, err)
		}
	}
	// It must say this is ambiguity rather than staleness: the caller cannot pick.
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "which") && !strings.Contains(lower, "ambigu") {
		t.Errorf("the refusal does not present this as an unresolvable choice: %v", err)
	}
}

// Inert while unset, in BOTH directions of absence — and absence is reported as
// unverifiable rather than as agreement, the same rule markerAgreement follows.
func TestAnUndeclaredGenerationIsInertNotAgreement(t *testing.T) {
	// No registry declaration: nothing to contradict, so existing callers proceed.
	if err := verifyActiveGeneration("example.com/acme/thing", "", "abc123def456"); err != nil {
		t.Errorf("an undeclared active generation refused a served graph: %v", err)
	}
	// The registry declares one and the graph states none: that cannot be verified,
	// and an unverifiable identity is not a matching one.
	err := verifyActiveGeneration("example.com/acme/thing", "abc123def456", "")
	if err == nil {
		t.Fatal("a graph stating no generation was accepted against a declared active one")
	}
	if !strings.Contains(err.Error(), "states no generation") {
		t.Errorf("the refusal does not say the graph stated nothing: %v", err)
	}
}

// Case and surrounding whitespace are transport noise, not identity.
func TestGenerationComparisonIgnoresTransportNoise(t *testing.T) {
	if err := verifyActiveGeneration("d", " ABC123def456 ", "abc123DEF456"); err != nil {
		t.Errorf("normalisation rejected an equal generation: %v", err)
	}
}

// A prefix is a different string. Short digest forms are printed everywhere in
// this codebase (marker.Digest[:12]), so a comparison that accepted a prefix would
// let a 7-character coincidence pass for a generation — exactly the "triple count
// is evidence, not identity" mistake (law 13) in another currency.
func TestAPrefixIsNotTheGeneration(t *testing.T) {
	if err := verifyActiveGeneration("d", "c0b660fcaaaa", "c0b660fc"); err == nil {
		t.Error("a served generation that is only a PREFIX of the declared one was accepted")
	}
	if err := verifyActiveGeneration("d", "c0b660fc", "c0b660fcaaaa"); err == nil {
		t.Error("a served generation that merely EXTENDS the declared one was accepted")
	}
}

// --- the pointer's owner: reading and recording it in the registry -----------

func TestTheRegistryAnswersWhichGenerationIsActive(t *testing.T) {
	path := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: c0b660fcaaaa
  example.com/acme/other:
    repository_identity: acme/other
`)
	if got := declaredActiveGeneration(path, "example.com/acme/thing"); got != "c0b660fcaaaa" {
		t.Errorf("declared generation = %q, want c0b660fcaaaa", got)
	}
	// A domain that declares none, and a domain that is not registered at all,
	// are both "no declaration" — inert, not an error.
	if got := declaredActiveGeneration(path, "example.com/acme/other"); got != "" {
		t.Errorf("a domain declaring no generation returned %q", got)
	}
	if got := declaredActiveGeneration(path, "example.com/nobody/here"); got != "" {
		t.Errorf("an unregistered domain returned %q", got)
	}
	if got := declaredActiveGeneration("/nonexistent/registry.yaml", "d"); got != "" {
		t.Errorf("a missing registry returned %q", got)
	}
}

// The activation transition (law 6) records the generation it activated. Only the
// transactional publication path may do this — it is the one caller that has just
// proven the store holds what the marker certifies.
func TestActivationRecordsTheGenerationItActivated(t *testing.T) {
	path := writeRegistryFixture(t, `# operator notes that must survive
domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: 000000000000
    allowed_corpus_roots: [docs/awareness]
  example.com/acme/other:
    repository_identity: acme/other
    active_generation: aaaaaaaaaaaa
`)
	if err := recordActiveGeneration(path, "example.com/acme/thing", "c0b660fcbbbb"); err != nil {
		t.Fatalf("recording the activated generation failed: %v", err)
	}
	if got := declaredActiveGeneration(path, "example.com/acme/thing"); got != "c0b660fcbbbb" {
		t.Errorf("after activation the registry declares %q, want c0b660fcbbbb", got)
	}
	// Law 12 is about two claimants for ONE domain. Recording one domain's
	// activation must not disturb another's.
	if got := declaredActiveGeneration(path, "example.com/acme/other"); got != "aaaaaaaaaaaa" {
		t.Errorf("another domain's active generation became %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The registry is an operator-edited file. A rewrite that drops what it does
	// not model would silently destroy operator configuration, so the update must
	// be surgical rather than a marshal of a parsed struct.
	for _, want := range []string{"operator notes that must survive", "allowed_corpus_roots", "acme/other"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("recording destroyed %q:\n%s", want, raw)
		}
	}
}

// A domain with no entry is not given one. The registry states what an operator
// admitted; a publication may update a declaration, not create an admission.
func TestActivationDoesNotAdmitAnUnregisteredDomain(t *testing.T) {
	path := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
`)
	if err := recordActiveGeneration(path, "example.com/nobody/here", "c0b660fcbbbb"); err != nil {
		t.Fatalf("recording for an unregistered domain must be inert, got: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "nobody/here") {
		t.Errorf("an unregistered domain was admitted into the registry:\n%s", raw)
	}
	// A domain that IS registered but declares nothing yet gets the declaration.
	if err := recordActiveGeneration(path, "example.com/acme/thing", "c0b660fcbbbb"); err != nil {
		t.Fatalf("recording onto a registered domain failed: %v", err)
	}
	if got := declaredActiveGeneration(path, "example.com/acme/thing"); got != "c0b660fcbbbb" {
		t.Errorf("a registered domain with no prior declaration got %q", got)
	}
}

func writeRegistryFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "domains.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- G5: the report states the generation verdict -----------------------------

// This is behavioural rather than structural: renderEndpointBlock is the function
// `sensei metadata` calls, so what it writes here is what that command prints.
func TestTheEndpointReportStatesWhetherTheServedGraphIsTheActiveGeneration(t *testing.T) {
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: c0b660fcaaaa
`)
	base := endpointReport{
		Root:         t.TempDir(),
		Domain:       "example.com/acme/thing",
		ResolvedAddr: "localhost:10120",
		AddrSource:   "the domain registry",
		RegistryPath: registry,
		LiveTriples:  35268,
	}

	agreeing := base
	agreeing.LiveDigest = "c0b660fcaaaa"
	out := renderBlock(t, agreeing)
	if !strings.Contains(out, "c0b660fcaaaa") {
		t.Errorf("the report does not state the declared generation:\n%s", out)
	}
	if !strings.Contains(out, "IS the declared ACTIVE generation") {
		t.Errorf("an agreeing graph is not reported as agreeing:\n%s", out)
	}

	// A healthy store serving a different generation: the report must say so, and
	// must not present it as a choice.
	wrong := base
	wrong.LiveDigest = "999999999999"
	out = renderBlock(t, wrong)
	if !strings.Contains(out, "ambiguous graph identity") {
		t.Errorf("a wrong generation is not reported as ambiguous:\n%s", out)
	}
	if strings.Contains(out, "IS the declared ACTIVE generation") {
		t.Errorf("a wrong generation was reported as agreement:\n%s", out)
	}

	// Unset: reported as NOT DECLARED and unverifiable, never as agreement — a
	// reader must be able to tell "checked and agrees" from "nothing to check".
	undeclared := base
	undeclared.LiveDigest = "c0b660fcaaaa"
	undeclared.RegistryPath = filepath.Join(t.TempDir(), "absent.yaml")
	out = renderBlock(t, undeclared)
	if !strings.Contains(out, "NOT DECLARED") || !strings.Contains(out, "cannot be verified") {
		t.Errorf("an undeclared pointer is not reported as unverifiable:\n%s", out)
	}
	if strings.Contains(out, "IS the declared ACTIVE generation") {
		t.Errorf("an undeclared pointer was reported as agreement:\n%s", out)
	}
}

func renderBlock(t *testing.T, r endpointReport) string {
	t.Helper()
	var buf bytes.Buffer
	renderEndpointBlock(&buf, r)
	return buf.String()
}

// TestActivationRecordsThePointerAfterTheMarkerIsPublished is a WIRING check on
// source order, not a behavioural one, and it is labelled that way on purpose:
// the scoped publication path needs a live Oxigraph store to reach, so a unit test
// cannot execute it. What it CAN falsify is the ordering the repair depends on —
// the pointer must be recorded after the marker exists, so it can never name a
// generation whose marker was never published, and before the receipt, so a
// receipt never describes a publication the pointer disagrees with.
//
// The behavioural evidence for recordActiveGeneration itself is above; this covers
// only where it is called from.
func TestActivationRecordsThePointerAfterTheMarkerIsPublished(t *testing.T) {
	raw, err := os.ReadFile("cmd_build.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	// Anchor on the receipt write, then look BACKWARDS for the marker write. This
	// file has more than one WriteMarkerFile call, in different functions, so
	// indexing the first one would let the pointer be recorded in a completely
	// different code path and still satisfy the assertion.
	receipt := strings.Index(src, "writePublicationReceipt(markerPath, receipt, marker)")
	if receipt < 0 {
		t.Fatal("the publication path no longer writes a receipt; this check has lost its anchor")
	}
	marker := strings.LastIndex(src[:receipt], "seedmeta.WriteMarkerFile(markerPath, marker)")
	record := strings.LastIndex(src[:receipt], "recordActiveGeneration(")
	if marker < 0 {
		t.Fatal("no marker write precedes the receipt write; this check has lost its anchor")
	}
	if record < 0 {
		t.Fatal("the ACTIVE generation pointer is never recorded before the receipt write")
	}
	if record < marker {
		t.Errorf("the ACTIVE pointer is recorded BEFORE the marker is published, so it can name a generation whose marker was never written: marker=%d record=%d", marker, record)
	}
	if strings.Count(src, "recordActiveGeneration(") != 1 {
		t.Errorf("recordActiveGeneration has %d call sites; the activation transition is one place, and a second caller would be a second claimant",
			strings.Count(src, "recordActiveGeneration("))
	}
}
