// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/rigor"
)

// runRigor is the change-time proportional-rigor diagnostic (issue #93): given the changed files of
// a proposed change, it reports the EFFECTIVE rigor class and the proof obligations owed. It is
// CLI-only and advisory — it classifies the change, never repository or artifact state, and it
// enforces nothing (the existing guards/CI enforce the obligations it names). It fails closed: an
// unclassified file forces Class A, and a declared class can only raise rigor, never downgrade it.
func runRigor(args []string) int {
	fs := flag.NewFlagSet("rigor", flag.ContinueOnError)
	var files stringList
	fs.Var(&files, "file", "a changed file (repo-relative or absolute); repeatable")
	root := fs.String("root", "", "project root (default: walk up for docs/awareness or .sensei/config.yaml)")
	declared := fs.String("declared", "", "optional declared class A|B|C|D (can only RAISE rigor, never downgrade)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei rigor --file <path> [--file <path> ...] [--declared A|B|C|D]

Report the proportional-rigor class and proof obligations a change owes. Rigor
classifies governed SURFACES (from docs/rigor_classes.yaml), then binds
changed files to surfaces through their owned packages. Effective rigor is the
strictest touched surface; an unclassified file forces Class A.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "sensei rigor: at least one --file is required")
		return 2
	}

	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei rigor: %v\n", err)
		return 2
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "docs", "rigor_classes.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei rigor: no rigor manifest (docs/rigor_classes.yaml): %v\n", err)
		return 2
	}
	manifest, err := rigor.ParseManifest(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei rigor: %v\n", err)
		return 2
	}

	// Normalize each file to a repo-relative path so it can bind to a governed surface.
	var rel []string
	for _, f := range files {
		if r, ok := relWithinRoot(projectRoot, f); ok {
			rel = append(rel, filepath.ToSlash(r))
		} else {
			rel = append(rel, f) // outside the project → unclassified → forced to Class A
		}
	}

	d := rigor.ClassifyChange(manifest, rel, rigor.Class(strings.ToUpper(strings.TrimSpace(*declared))))
	fmt.Print(rigor.FormatDecision(d))
	return 0
}
