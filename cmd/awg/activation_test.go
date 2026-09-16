// SPDX-License-Identifier: AGPL-3.0-only

package main

// Slice G4's second half: "Make build, import, refresh, bootstrap, rebuild, and
// served-store handoff use the same publication/identity primitive rather than each
// owning a slightly different lifecycle."
//
// Measured across the five commands, the difference was narrower and sharper than
// "five lifecycles":
//
//	step                        build  import  rebuild  governance
//	stage/promote/verify/drop     y      -        -         -
//	verify the live graph         y      -        y         y
//	WRITE THE MARKER              y      -        y         y
//	write a publication receipt   y      -        -      (its own richer record)
//	RECORD THE ACTIVE POINTER     y      -        -         -
//
// All three verify before writing. Only `build` records which generation it activated.
// So rebuild and governance make a generation live and leave the registry's ACTIVE
// pointer naming the PREVIOUS one -- after which every reader that resolves through
// the registry refuses this graph. That is fail-closed, which is right, but the
// operator is given no reason, and the fix is invisible.
//
// The shared primitive is therefore the ACTIVATION TRANSITION: once verification has
// passed, {write the marker, record the ACTIVE generation} happen together, in that
// order, everywhere.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

func testMarker(digest string, triples int64) seedmeta.Marker {
	return seedmeta.Marker{IRI: "urn:sensei:test", Digest: digest, TripleCount: triples}
}

func TestActivationWritesTheMarkerAndRecordsThePointer(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	markerPath := filepath.Join(root, ".sensei", "graph-authority.json")
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: 000000000000
`)
	var out bytes.Buffer
	if err := activateGeneration(&out, markerPath, testMarker("c0b660fcaaaa", 10), "example.com/acme/thing", selectDomainRegistry(registry)); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	m, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatalf("the marker was not written: %v", err)
	}
	if m.Digest != "c0b660fcaaaa" {
		t.Errorf("marker digest = %q", m.Digest)
	}
	if got := declaredActiveGeneration(registry, "example.com/acme/thing"); got != "c0b660fcaaaa" {
		t.Errorf("the ACTIVE pointer was not updated: %q", got)
	}
}

// The pointer must never name a generation whose marker was not published. Ordering is
// the property, and a failed marker write is where it is observable.
func TestAFailedMarkerWriteLeavesThePointerAlone(t *testing.T) {
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: 000000000000
`)
	var out bytes.Buffer
	// A marker path inside a file, so creating the directory cannot succeed.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := writeFileAtomic(blocker, []byte("x")); err != nil {
		t.Fatal(err)
	}
	err := activateGeneration(&out, filepath.Join(blocker, "graph-authority.json"),
		testMarker("c0b660fcaaaa", 10), "example.com/acme/thing", selectDomainRegistry(registry))
	if err == nil {
		t.Fatal("a marker that could not be written reported success")
	}
	if got := declaredActiveGeneration(registry, "example.com/acme/thing"); got != "000000000000" {
		t.Errorf("the ACTIVE pointer moved to a generation whose marker was never published: %q", got)
	}
}

// A command that activates a generation without knowing which domain it is for cannot
// update the pointer. That is not a silent skip: readers resolving through the registry
// will refuse this graph, and the operator must be told why and what to do.
func TestActivationWithoutADomainSaysThePointerWasNotUpdated(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	markerPath := filepath.Join(root, ".sensei", "graph-authority.json")
	var out bytes.Buffer
	if err := activateGeneration(&out, markerPath, testMarker("c0b660fcaaaa", 10), "", selectDomainRegistry("")); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if _, err := seedmeta.ReadMarkerFile(markerPath); err != nil {
		t.Fatalf("the marker was not written: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "NOT updated") {
		t.Errorf("the report does not say the pointer was left alone:\n%s", got)
	}
	if !strings.Contains(got, "c0b660fcaaaa") {
		t.Errorf("the report does not name the generation that is now live:\n%s", got)
	}
	// Actionable, not merely honest: it must say how to declare it.
	if !strings.Contains(got, "active_generation") {
		t.Errorf("the report does not say how to declare the generation:\n%s", got)
	}
}

// A successful activation says so, with both facts a reader needs.
func TestActivationReportsWhatItDid(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
`)
	var out bytes.Buffer
	if err := activateGeneration(&out, filepath.Join(root, ".sensei", "graph-authority.json"),
		testMarker("c0b660fcaaaa", 10), "example.com/acme/thing", selectDomainRegistry(registry)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"c0b660fcaaaa", "example.com/acme/thing"} {
		if !strings.Contains(got, want) {
			t.Errorf("the activation report omits %q:\n%s", want, got)
		}
	}
}

// THE UNIFICATION CLAIM ITSELF, and the only way to falsify it: no command may write a
// graph marker except through the activation transition. A second writer is a second
// lifecycle, which is exactly what G4 exists to remove — and it would be invisible in
// behaviour, because the marker it writes looks identical.
func TestNoCommandWritesAGraphMarkerOutsideTheActivationTransition(t *testing.T) {
	writers := markerWriteSites(t)
	if len(writers) == 0 {
		t.Fatal("no marker write site found at all; this check has lost its anchor")
	}
	for file, count := range writers {
		if file == "publication_activation.go" {
			continue
		}
		t.Errorf("%s calls seedmeta.WriteMarkerFile directly (%d time(s)); activation is one transition, and a second writer skips the ACTIVE pointer", file, count)
	}
	if writers["publication_activation.go"] != 1 {
		t.Errorf("the activation transition writes the marker %d times; want exactly 1", writers["publication_activation.go"])
	}
}

// markerWriteSites counts direct seedmeta.WriteMarkerFile calls per file in this
// package, excluding tests: a test may legitimately construct a marker fixture.
func markerWriteSites(t *testing.T) map[string]int {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(raw), "seedmeta.WriteMarkerFile("); n > 0 {
			out[name] = n
		}
	}
	return out
}

// The scoped publication path is the one caller that KNOWS its domain, and passing "" there
// would silently degrade every scoped build to "pointer not updated" — a message that looks
// like the honest report rebuild and serve correctly emit. Behaviour cannot separate them,
// because both produce a marker and a note; only the argument differs.
//
// A source-anchored check, and labelled as one: the scoped path needs a live Oxigraph store
// to reach. It anchors on the receipt write, which only that path performs, so it cannot
// accidentally inspect the whole-store call above it.
func TestTheScopedPublicationPathActivatesUnderItsOwnDomain(t *testing.T) {
	raw, err := os.ReadFile("cmd_build.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	receipt := strings.Index(src, "writePublicationReceipt(markerPath, receipt, marker)")
	if receipt < 0 {
		t.Fatal("the scoped publication path no longer writes a receipt; this check has lost its anchor")
	}
	call := strings.LastIndex(src[:receipt], "activateGeneration(")
	if call < 0 {
		t.Fatal("the scoped publication path does not activate through the shared transition")
	}
	line := src[call:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if !strings.Contains(line, ", domain,") {
		t.Errorf("the scoped path does not activate under its own domain, so every scoped build would leave the ACTIVE pointer stale: %s", line)
	}
}
