// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/packcustody"
	"github.com/globulario/sensei/golang/seedmeta"
)

// A cross-repo build spells its corpus relative to the working directory
// ("--input ../other/docs/awareness"), and the published slice records
// provenance in exactly that spelling. Closure accepted only the absolute form
// plus a relative form computed for inputs INSIDE the working directory, so a
// corpus outside it matched no certified root at all — and the repository's own
// identities were reported as authored elsewhere.
//
// That fails closure on a CORRECT publication, which is the worst shape for
// this check: the report names real-looking foreign provenance, so the operator
// goes looking for a contaminated corpus that does not exist. Observed live
// while running issue #176's regression matrix — 21 of sensei-code's own
// identities called foreign, dropping to 0 when the identical build was given
// an absolute --input.
func TestClosureRootsAcceptTheInputSpellingProvenanceRecords(t *testing.T) {
	// A corpus OUTSIDE the working directory, as every cross-repo build has.
	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "failure_modes.yaml"), []byte(
		"failure_modes:\n  - id: failure.example.only_identity\n    title: the one identity this corpus declares\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "..") {
		t.Skipf("temp dir %s is not outside the working directory; this test needs the cross-repo shape", corpus)
	}

	// The slice records provenance as the input was spelled.
	const subject = "<https://globular.io/awareness#failureMode/failure.example.only_identity>"
	nt := fmt.Sprintf("%s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#FailureMode> .\n"+
		"%s <https://globular.io/awareness#authoredIn> %q .\n",
		subject, subject, filepath.ToSlash(filepath.Join(rel, "failure_modes.yaml")))

	rep := buildClosureReport("example.com/domain", []string{rel},
		filepath.Join(t.TempDir(), "graph-authority.json"),
		seedmeta.Marker{Digest: "d0", TripleCount: 2}, []byte(nt))
	if rep == nil {
		t.Fatal("no closure report was produced for a corpus outside the working directory")
	}
	if rep.Unexpected != 0 {
		t.Errorf("%d identity/identities of this corpus were reported as foreign; the build's own --input spelling must count as a certified root\nreasons: %v",
			rep.Unexpected, rep.FailureReasons)
	}
}

// A project that correctly installs the principle pack holds a mirror it does
// not author. Custody refuses to publish that mirror under the project's own
// domain, so closure must not expect the project to project its identities —
// otherwise the correct state is unprovable and every such repository reports
// its whole mirror missing.
//
// Observed live while running issue #176's regression matrix: sensei-code
// reported 23/161 projected with 138 missing, which is exactly the pack's
// principle count, and could never become authoritative.
func TestInstalledPrincipleMirrorIsNotExpectedOfTheInstallingDomain(t *testing.T) {
	corpus := t.TempDir()
	// One genuinely authored identity.
	if err := os.WriteFile(filepath.Join(corpus, "failure_modes.yaml"), []byte(
		"failure_modes:\n  - id: failure.example.authored_here\n    title: authored by this repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And an installed mirror, which declares itself generated.
	mirror := "# Meta-Principles.\n#\n# " + packcustody.GeneratedMarker + "\n" +
		"meta_principles:\n  - id: meta.projected_from_the_pack\n    title: authored upstream, installed here\n"
	if err := os.WriteFile(filepath.Join(corpus, "meta_principles.yaml"), []byte(mirror), 0o644); err != nil {
		t.Fatal(err)
	}

	expected, excluded, err := expectedIdentities(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := expected["meta.projected_from_the_pack"]; present {
		t.Error("an installed pack mirror's identity is required of the installing domain; that domain cannot publish it, so the correct state would be unprovable")
	}
	if _, present := expected["failure.example.authored_here"]; !present {
		t.Error("the repository's own authored identity stopped being expected; the exclusion is too broad")
	}
	var sawExcluded bool
	for _, id := range excluded {
		if id == "meta.projected_from_the_pack" {
			sawExcluded = true
		}
	}
	if !sawExcluded {
		t.Error("the mirror's identity is neither expected nor recorded as excluded; it vanished from the accounting rather than being typed as not-ours")
	}
}
