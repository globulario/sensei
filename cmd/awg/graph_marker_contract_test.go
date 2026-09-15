// SPDX-License-Identifier: AGPL-3.0-only

package main

// Slice G4 of the graph-identity front: "Resolve the actual --graph-marker-file
// contract." The plan lists the symptom it is resolving — "marker paths not
// following the defaults callers believed they had."
//
// Measured before changing anything. One flag name carried FOUR different contracts,
// and no command said which one it had taken:
//
//	1. an explicit --graph-marker-file            that path
//	2. serve in embedded-seed mode, no flag       NO marker at all
//	3. serve --no-seed, or any other command      <root>/<statedir>/graph-authority.json
//	4. no resolvable project root                 a RELATIVE path, resolved against
//	                                              whatever directory the caller stood in
//
// Tier 4 is the same defect the ACTIVE generation pointer fixed for generations: an
// identity whose answer depends on where it is asked from is not an identity.
// statedir.Path("") returns ".sensei/graph-authority.json", and callers reached it by
// discarding the error from resolveProjectRoot("") — including, before this, the
// Endpoint block of `sensei metadata`.
//
// Tier 3 has a second mouth: statedir.Name prefers ".sensei" and falls back to a
// pre-existing ".awg". When BOTH exist the legacy marker is orphaned — still on disk,
// certifying a generation nothing serves, and reported by nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectRoot makes a directory that is a project by the same definition
// resolveProjectRoot searches for: a state directory holding a config. A bare
// .sensei directory is NOT one, and every fixture here said otherwise until the
// choke-point refusal exposed them.
func projectRoot(t *testing.T, at string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(at, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(at, ".sensei", "config.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestAnExplicitMarkerFlagIsAlwaysHonored(t *testing.T) {
	path, source, err := resolveGraphMarkerFile("/tmp/explicit.json", t.TempDir(), false)
	if err != nil {
		t.Fatalf("an explicit flag was refused: %v", err)
	}
	if path != "/tmp/explicit.json" {
		t.Errorf("path = %q, want the flag's value", path)
	}
	if !strings.Contains(source, "flag") {
		t.Errorf("source = %q, want it to name the flag the operator passed", source)
	}
}

// Tier 4, fail-closed. A marker path that resolves against the current directory
// certifies a different graph depending on where the command ran, which is not a
// certification at all.
func TestAMarkerPathThatDependsOnTheWorkingDirectoryIsRefused(t *testing.T) {
	_, _, err := resolveGraphMarkerFile("", "", false)
	if err == nil {
		t.Fatal("a rootless resolution produced a marker path instead of refusing")
	}
	for _, want := range []string{"project root", "working directory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain the cause (%q): %v", want, err)
		}
	}
	// And it must not hand back a usable-looking relative path alongside the error.
	path, _, _ := resolveGraphMarkerFile("", "", false)
	if path != "" {
		t.Errorf("a relative marker path escaped the refusal: %q", path)
	}
}

func TestTheDefaultMarkerLivesInTheProjectsStateDirectory(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	path, source, err := resolveGraphMarkerFile("", root, false)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	want := filepath.Join(root, ".sensei", "graph-authority.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path %q is not absolute, so it still depends on the caller's directory", path)
	}
	if !strings.Contains(source, ".sensei") {
		t.Errorf("source = %q, want it to name the state directory it chose", source)
	}
}

// A RELATIVE root carries the same defect as no root: the marker it names resolves
// against the caller's directory. t.TempDir() is already absolute, so the absolute-path
// assertion above cannot see whether anything absolutised it — this is the case that
// can.
func TestARelativeProjectRootStillYieldsAnAbsoluteMarkerPath(t *testing.T) {
	base := t.TempDir()
	projectRoot(t, filepath.Join(base, "sub"))
	t.Chdir(base)
	path, _, err := resolveGraphMarkerFile("", "sub", false)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path = %q, which resolves against whatever directory the caller stood in", path)
	}
	if !strings.HasSuffix(path, filepath.Join("sub", ".sensei", "graph-authority.json")) {
		t.Errorf("path = %q, want the given root absolutised rather than replaced", path)
	}
}

// A repository initialized before the rename keeps working, and the source says so —
// a caller looking for .sensei/graph-authority.json and finding nothing needs to be
// told the marker is in .awg rather than concluding none exists.
func TestALegacyStateDirectoryIsUsedAndNamed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".awg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".awg", "config.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, source, err := resolveGraphMarkerFile("", root, false)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	if path != filepath.Join(root, ".awg", "graph-authority.json") {
		t.Errorf("path = %q, want the legacy directory's marker", path)
	}
	if !strings.Contains(source, ".awg") {
		t.Errorf("source = %q, want it to name the legacy directory", source)
	}
}

// Both directories present: one marker is authoritative and the other is ORPHANED,
// certifying a generation nothing serves. Law 10's read side — a marker cannot
// certify a different generation than the one being served — cannot be checked
// against a marker nobody knows is there.
func TestAnOrphanedLegacyMarkerIsReported(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, ".awg"), 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(root, ".awg", "graph-authority.json")
	if err := os.WriteFile(orphan, []byte(`{"digest":"stale"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	path, source, err := resolveGraphMarkerFile("", root, false)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	if path != filepath.Join(root, ".sensei", "graph-authority.json") {
		t.Errorf("the modern directory is no longer preferred: %q", path)
	}
	if !strings.Contains(source, ".awg") {
		t.Errorf("the orphaned legacy marker is not reported: %q", source)
	}
	// An orphan that does not exist is not mentioned: a report that always warns
	// teaches its reader to ignore it.
	clean := projectRoot(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(clean, ".awg"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, cleanSource, err := resolveGraphMarkerFile("", clean, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cleanSource, ".awg") {
		t.Errorf("a legacy directory holding no marker was reported as an orphan: %q", cleanSource)
	}
}

// Tier 2: serve in embedded-seed mode uses the marker compiled into the binary, so
// there is deliberately NO file. That is a real contract and it stays — but it is now
// stated, because "no marker file" and "the default marker file" were the same empty
// string before, and only one of them means the embedded marker is in force.
func TestServeInEmbeddedSeedModeStatesThatItUsesNoMarkerFile(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	path, source, err := resolveGraphMarkerFile("", root, true)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	if path != "" {
		t.Errorf("embedded-seed mode resolved a marker FILE: %q", path)
	}
	if !strings.Contains(strings.ToLower(source), "embedded") {
		t.Errorf("source = %q, want it to say the embedded marker is in force", source)
	}
	// An explicit flag still wins in embedded mode: that is the operator naming the
	// marker at the point of use.
	path, source, err = resolveGraphMarkerFile("/tmp/explicit.json", root, true)
	if err != nil {
		t.Fatalf("resolveGraphMarkerFile: %v", err)
	}
	if path != "/tmp/explicit.json" || !strings.Contains(source, "flag") {
		t.Errorf("an explicit flag lost to embedded mode: %q / %q", path, source)
	}
}

// The tier-4 refusal above is only as good as its trigger, and measuring the trigger
// found the real defect: resolveProjectRoot NEVER reports "not in a project". Its
// search walks up looking for docs/awareness or <statedir>/config.yaml, and when it
// finds neither it returns the CURRENT WORKING DIRECTORY with a nil error.
//
// So `root == ""` almost never happens, and the default marker silently becomes
// <cwd>/.sensei/graph-authority.json for a caller standing anywhere at all —
// precisely the cwd-dependent marker the refusal exists to prevent, arriving through
// a door the refusal does not watch.
//
// Changing resolveProjectRoot is out of scope: 34 non-test callers depend on its
// fail-open walk. The invariant goes at the choke point instead. The marker resolver
// requires the root to satisfy the SAME definition of a project resolveProjectRoot
// searches for, so a root that matched only by exhaustion is refused rather than
// silently used.
func TestAMarkerIsRefusedForADirectoryThatIsNotAProject(t *testing.T) {
	notAProject := t.TempDir()
	// A bare .sensei directory is not a project: `sensei build` creating a state
	// directory somewhere is not the same as that somewhere being the project.
	if err := os.MkdirAll(filepath.Join(notAProject, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := resolveGraphMarkerFile("", notAProject, false)
	if err == nil {
		t.Fatal("a directory that is not a project resolved a marker file")
	}
	if !strings.Contains(err.Error(), notAProject) {
		t.Errorf("the refusal does not name the directory it rejected: %v", err)
	}

	// The two things that DO make it a project — the same two resolveProjectRoot
	// looks for — are both accepted.
	for name, make := range map[string]func(root string) error{
		"a corpus at docs/awareness": func(root string) error {
			return os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755)
		},
		"a state-directory config": func(root string) error {
			if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("{}\n"), 0o644)
		},
	} {
		root := t.TempDir()
		if err := make(root); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolveGraphMarkerFile("", root, false); err != nil {
			t.Errorf("%s was not accepted as a project: %v", name, err)
		}
	}

	// An explicit flag is still honoured: the operator named the marker at the point
	// of use, so there is no root to infer and nothing silent about it.
	if _, _, err := resolveGraphMarkerFile("/tmp/explicit.json", notAProject, false); err != nil {
		t.Errorf("an explicit marker flag was refused for a non-project directory: %v", err)
	}
	// And embedded-seed mode needs no file at all, so it needs no project either.
	if _, _, err := resolveGraphMarkerFile("", notAProject, true); err != nil {
		t.Errorf("embedded-seed mode was refused for a non-project directory: %v", err)
	}
}
