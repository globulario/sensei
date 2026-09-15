package main

// Law 14: a raw --store-url override is test/dev/maintenance authority, not normal
// production discovery. If it is retained, it must be explicit and VISIBLY
// non-canonical.
//
// Today it is explicit and silent. requireEndpointAgreement returns nil the moment
// the flag was passed, and `import` cannot load a slice WITHOUT passing it
// (cmd_import.go:217 treats an empty --store-url as "print the command, load
// nothing"). So on the import path the endpoint-custody guard introduced by #212
// can never fire: the flag that is required to publish is the flag that disables
// the check.
//
// Measured on this machine, three stores answer healthily and hold different
// graphs — :7878 237,049 triples and served by nothing, :7881 142,739 for the
// sensei domain, :7882 35,268 for sensei-code — so a mistyped or copy-pasted
// --store-url publishes into a graph nobody asked for, with no warning.

import (
	"strings"
	"testing"
)

func TestAnAgreeingExplicitStoreURLIsNotWarnedAbout(t *testing.T) {
	note := nonCanonicalStoreURLNotice("http://localhost:7878/store?default", "http://localhost:7878/store?default")
	if note != "" {
		t.Errorf("an explicit endpoint the config also names was reported as non-canonical: %q", note)
	}
}

func TestADisagreeingExplicitStoreURLIsVisiblyNonCanonical(t *testing.T) {
	const configured = "http://localhost:7882/store?default"
	const resolved = "http://localhost:7878/store?default"
	note := nonCanonicalStoreURLNotice(configured, resolved)
	if note == "" {
		t.Fatal("an explicit endpoint the config does not name produced no notice; law 14 requires it be visibly non-canonical")
	}
	for _, want := range []string{configured, resolved, "non-canonical"} {
		if !strings.Contains(note, want) {
			t.Errorf("the notice omits %q: %q", want, note)
		}
	}
	// It must say the override is not production discovery, so it cannot be read
	// as a routine choice between equals.
	lower := strings.ToLower(note)
	if !strings.Contains(lower, "not production") && !strings.Contains(lower, "maintenance") {
		t.Errorf("the notice does not mark the override as non-production authority: %q", note)
	}
}

// A config that names no store is not agreement. Silence must not read as assent.
func TestAnUnconfiguredStoreIsSaidToBeUnverifiable(t *testing.T) {
	note := nonCanonicalStoreURLNotice("", "http://localhost:7878/store?default")
	if note == "" {
		t.Fatal("an endpoint that no config corroborates produced no notice; absence of a configured value is not agreement")
	}
	if !strings.Contains(note, "names no store") && !strings.Contains(note, "cannot corroborate") {
		t.Errorf("the notice does not say the config could not corroborate the endpoint: %q", note)
	}
}
