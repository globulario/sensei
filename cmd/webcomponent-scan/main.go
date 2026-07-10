// SPDX-License-Identifier: Apache-2.0

// webcomponent-scan — CLI wrapper over golang/extractor/webcompscan.
//
// Scans TS/JS for native Web Component registrations (customElements.define /
// Lit @customElement) and emits a `components:` YAML of Component nodes. The
// reusable core lives in the webcompscan package so `awg bootstrap` can call it
// in-process.
//
// Usage:
//
//	webcomponent-scan -repo-root . -output docs/awareness/architecture/web_components.yaml
//	webcomponent-scan ... -check   # regenerate in memory, diff committed output, exit 1 if stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/extractor/webcompscan"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("webcomponent-scan", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repo root to scan / for relative source_files")
	output := fs.String("output", "", "output YAML path (default: stdout)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: webcomponent-scan -repo-root <dir> [-output f] [-check]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	files, err := webcompscan.FindSourceFiles(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webcomponent-scan: discover: %v\n", err)
		return 1
	}
	var all []webcompscan.Component
	for _, f := range files {
		cs, serr := webcompscan.ScanFile(f, *repoRoot)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "webcomponent-scan: %s: %v\n", f, serr)
			return 1
		}
		all = append(all, cs...)
	}
	doc := webcompscan.Doc{Components: webcompscan.Dedupe(all)}

	rendered, err := webcompscan.Render(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webcomponent-scan: render: %v\n", err)
		return 1
	}

	if *check {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "webcomponent-scan: -check requires -output")
			return 2
		}
		committed, rerr := os.ReadFile(*output)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "webcomponent-scan: read committed %s: %v\n", *output, rerr)
			return 1
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "STALE: %s — regenerate and commit.\n", *output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "webcomponent-scan: %s is fresh (%d components).\n", *output, len(doc.Components))
		return 0
	}

	if *output == "" {
		os.Stdout.Write(rendered)
		return 0
	}
	if werr := os.WriteFile(*output, rendered, 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "webcomponent-scan: write %s: %v\n", *output, werr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "webcomponent-scan: wrote %d components to %s\n", len(doc.Components), *output)
	return 0
}
