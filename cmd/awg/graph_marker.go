// SPDX-License-Identifier: AGPL-3.0-only

// graph_marker.go owns ONE question: which graph marker file does this command use,
// and why.
//
// Slice G4 of the graph-identity front (docs/architecture/oxygraph_usage.md):
// "Resolve the actual --graph-marker-file contract." The plan names the symptom it is
// resolving — "marker paths not following the defaults callers believed they had."
//
// One flag name carried four contracts and no command said which it had taken:
//
//  1. an explicit --graph-marker-file        that path
//  2. serve in embedded-seed mode, no flag   NO marker file; the embedded one is in force
//  3. any other command, no flag             <root>/<statedir>/graph-authority.json
//  4. no resolvable project root             a RELATIVE path, resolved against
//     whatever directory the caller stood in
//
// Tier 4 is the defect the ACTIVE generation pointer removed for generations, in
// another currency: statedir.Path("") returns ".sensei/graph-authority.json", and
// callers reached it by DISCARDING the error from resolveProjectRoot(""). A marker
// whose location depends on where the command ran certifies a different graph from
// each directory, which is not a certification. It is now refused.
//
// Tier 3 has a second mouth. statedir.Name prefers ".sensei" and falls back to a
// pre-existing ".awg", so a repository from before the rename keeps working. When
// BOTH exist the legacy marker is orphaned: still on disk, certifying a generation
// nothing serves, and reported by nothing. Law 10's read side cannot be checked
// against a marker nobody knows is there, so the resolution names it.
//
// The (path, source) shape is the one G2's resolveDomainServiceAddr established: a
// resolution that cannot say where it came from makes two legitimately different
// answers indistinguishable from one answer having changed.
package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/statedir"
)

// resolveGraphMarkerFile resolves the marker file and states which tier answered.
//
// An empty path with a nil error is a real outcome and means exactly one thing: the
// embedded marker is in force. It is never the "default" — those were the same empty
// string before, and only one of them means a marker is being consulted at all.
func resolveGraphMarkerFile(configured, root string, serveEmbeddedSeed bool) (string, string, error) {
	if c := strings.TrimSpace(configured); c != "" {
		return c, "the --graph-marker-file flag", nil
	}
	if serveEmbeddedSeed {
		return "", "no marker file: the seed embedded in this binary carries its own marker", nil
	}
	if strings.TrimSpace(root) == "" {
		return "", "", fmt.Errorf("refusing to resolve a graph marker file without a project root.\n" +
			"  the default marker path would be relative, so it would resolve against the current working directory\n" +
			"  and certify a different graph depending on where this command was run\n\n" +
			"Run this from inside the project, or name the marker explicitly with --graph-marker-file.")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve project root %s: %w", root, err)
	}
	// The refusal above watches for an EMPTY root, and that door is almost never the
	// one this arrives through: resolveProjectRoot never reports "not in a project" --
	// its walk returns the working directory when it finds nothing, with a nil error.
	// So the cwd-dependent marker reaches here as a perfectly ordinary absolute path,
	// and the only way to tell is to ask whether the directory is a project at all,
	// using the same definition the walk searches for.
	if !looksLikeProjectRoot(abs) {
		return "", "", fmt.Errorf("refusing to resolve a graph marker file: %s is not a Sensei project.\n"+
			"  no docs/awareness corpus and no %s/config.yaml were found there\n"+
			"  the default marker would be written under a directory that is not the project, so it would\n"+
			"  certify a different graph depending on where this command was run\n\n"+
			"Run this from inside the project, or name the marker explicitly with --graph-marker-file.",
			abs, statedir.DefaultName)
	}
	path := seedmeta.RuntimeMarkerPath(abs)
	source := statedir.Name(abs) + "/graph-authority.json in the project root"
	if orphan := orphanedLegacyMarker(abs); orphan != "" {
		source += fmt.Sprintf(" (NOTE: %s also exists and is orphaned — it certifies a generation nothing serves)", orphan)
	}
	return path, source, nil
}

// orphanedLegacyMarker returns the path of a legacy-directory marker that is no
// longer the one being used, or "" when there is none.
//
// Only an orphan that actually EXISTS is reported. A report that always carries a
// warning teaches its reader to skip the line, and then the one time it matters it is
// not read either.
func orphanedLegacyMarker(root string) string {
	if statedir.Name(root) != statedir.DefaultName {
		return ""
	}
	legacy := filepath.Join(root, statedir.LegacyName, "graph-authority.json")
	if !pathExists(legacy) {
		return ""
	}
	return legacy
}

// defaultRuntimeMarkerFile is the pre-G4 entry point, kept so every existing caller
// keeps compiling, and now routed through the one resolver so they cannot drift. It
// discards the source; a caller that reports to a human should call
// resolveGraphMarkerFile directly and print it.
func defaultRuntimeMarkerFile() (string, error) {
	root, err := resolveProjectRoot("")
	if err != nil {
		return "", err
	}
	path, _, err := resolveGraphMarkerFile("", root, false)
	return path, err
}

func runtimeMarkerFileForRoot(root string) (string, error) {
	if root != "" {
		path, _, err := resolveGraphMarkerFile("", root, false)
		return path, err
	}
	return defaultRuntimeMarkerFile()
}
