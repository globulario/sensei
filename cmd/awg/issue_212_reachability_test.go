// SPDX-License-Identifier: AGPL-3.0-only

package main

// ISSUE #212 REACHABILITY WITNESS.
//
// #212's failure mode: a command reports a verdict from a server the operator did not name, because
// the command chose an endpoint itself while the project configuration named another one. The
// requireServerAddrAgreement refusal existed to catch that.
//
// The claim this file establishes, BEFORE that refusal is re-scoped into a visibility duty:
//
//	A production graph reader may no longer silently dial an independent/default endpoint when
//	project configuration names another endpoint.
//
// It is a STRUCTURAL claim, so it is proven structurally rather than by sampling commands. Silently
// dialling an independent endpoint required two things, and the endpoint-ownership family removed
// both: a subject had to resolve its own endpoint, and it had to have a non-empty default to fall
// back on. What remains is one owner that reads the project configuration itself, which is why a
// refusal comparing the configuration against the owner's answer no longer has a failure to catch.
//
// This witness deliberately does NOT assert anything about the refusal or the notice. It establishes
// only that #212's failure is unreachable, so that re-scoping the guard is a measured consequence
// rather than a defensive removal.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LIMB 1 — the owner prefers the project's configuration over the built-in default. This is the
// substantive half: the endpoint #212 feared (an independent default) is not what the owner picks
// when the project has named one.
func TestIssue212_TheOwnerPrefersTheProjectConfigurationOverTheBuiltInDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SENSEI_DOMAIN", "")
	t.Setenv("SENSEI_ADDR", "")
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A registry that is well formed and SILENT about service_addr, so precedence reaches the
	// project configuration rather than stopping above it.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    example.com/acme/reach:
        repository_identity: acme/reach
`), 0o644); err != nil {
		t.Fatal(err)
	}

	const configured = "127.0.0.1:41212"
	if configured == defaultServiceAddr() {
		t.Fatalf("the fixture's configured endpoint equals the built-in default (%s), so this "+
			"witness could not tell them apart", defaultServiceAddr())
	}

	root := projectRoot(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"),
		[]byte("repository:\n    domain: example.com/acme/reach\nserver:\n    addr: "+configured+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	reader := productionReaderFor(emptyFlags(), root, "example.com/acme/reach", "")
	if reader.Addr == defaultServiceAddr() {
		t.Fatalf("the owner resolved the built-in default %q while the project configuration named "+
			"%q — #212's failure mode is reachable and the refusal must not be re-scoped",
			reader.Addr, configured)
	}
	if reader.Addr != configured {
		t.Fatalf("the owner resolved %q, want the configured %q", reader.Addr, configured)
	}
	t.Logf("LIMB 1: configured %s, built-in default %s, owner chose %s (%s)",
		configured, defaultServiceAddr(), reader.Addr, reader.Source)
}

// LIMB 2 — no production graph command has a non-empty endpoint default to fall back on. Without a
// default of its own, a subject cannot dial an independent endpoint even if it wanted to. Derived
// from the census population, not from a list.
func TestIssue212_NoSubjectCarriesAnIndependentEndpointDefault(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	const floor = 15
	if len(subjects) < floor {
		t.Fatalf("the census discovered %d subject(s); %d+ are known, so it has stopped enumerating "+
			"and this witness would pass by finding nothing", len(subjects), floor)
	}
	seen := map[string]bool{}
	for key := range subjects {
		file := key
		if i := strings.Index(key, ":"); i > 0 {
			file = key[:i]
		}
		if seen[file] {
			continue
		}
		seen[file] = true
		src := readCmdSource(t, file)
		// The owner's rule: every reader's --addr default is empty, so a non-empty value is always
		// an operator naming an endpoint rather than one the command picked for them.
		for _, forbidden := range []string{
			`fs.String("addr", defaultServiceAddr()`,
			`fs.String("addr", netcfg.ServiceAddr()`,
		} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s declares an endpoint default (%s); a subject with its own default can "+
					"dial an independent endpoint, which is #212's failure mode", file, forbidden)
			}
		}
	}
	t.Logf("LIMB 2: %d subject(s) across %d file(s), none declaring an endpoint default",
		len(subjects), len(seen))
}

// LIMB 3 — every discovered subject resolves through the owner, so none of them performs the
// endpoint selection #212 was about. This restates the completion oracle deliberately: the
// unreachability claim must not rest on a test living in another file that could be narrowed
// independently.
func TestIssue212_EverySubjectDelegatesEndpointSelectionToTheOwner(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	if len(subjects) < 15 {
		t.Fatalf("the census discovered %d subject(s); it has stopped enumerating", len(subjects))
	}
	_, _, noOwner := classifyGraphCommands(subjects)
	if len(noOwner) != 0 {
		t.Fatalf("%d subject(s) still select an endpoint without the owner:\n  %s\n\nWhile any does, "+
			"#212's failure mode remains reachable through it.", len(noOwner), strings.Join(noOwner, "\n  "))
	}
	t.Logf("LIMB 3: %d of %d subject(s) delegate endpoint selection to the owner", len(subjects), len(subjects))
}
