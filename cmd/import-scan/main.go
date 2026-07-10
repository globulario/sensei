// SPDX-License-Identifier: Apache-2.0

// import-scan — CLI wrapper over golang/extractor/importgraph.
//
// Scans Go imports and emits a `components:` YAML of aw:dependsOn (and, via an
// optional classifier config, reads_from / writes_to / exposes_contract) edges
// between the repo's Components. The reusable core lives in the importgraph
// package so `awg bootstrap` can call it in-process.
//
// Usage:
//
//	import-scan -repo-root . \
//	  -output docs/awareness/architecture/awareness_graph_import_graph.yaml
//
//	import-scan ... -config docs/awareness/import_classifier.yaml  # optional rules
//	import-scan ... -check     # regenerate in memory, diff committed output, exit 1 if stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/awareness-graph/golang/extractor/importgraph"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("import-scan", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repo root to scan")
	lang := fs.String("lang", "go", "language to scan (go)")
	output := fs.String("output", "", "output YAML path (default: stdout)")
	config := fs.String("config", "", "optional classifier rules YAML (upgrades imports into semantic edges)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: import-scan -repo-root <dir> [-lang go] [-config classifiers.yaml] [-output f] [-check]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var cfg importgraph.Config
	if *config != "" {
		c, err := importgraph.LoadConfig(*config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "import-scan: %v\n", err)
			return 2
		}
		cfg = c
	}

	doc, err := importgraph.Scan(*repoRoot, *lang, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import-scan: %v\n", err)
		return 1
	}
	rendered, err := importgraph.Render(doc, *lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import-scan: render: %v\n", err)
		return 1
	}

	if *check {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "import-scan: -check requires -output")
			return 2
		}
		committed, err := os.ReadFile(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "import-scan: read committed %s: %v\n", *output, err)
			return 1
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "STALE: %s — run `make import-graph` and commit.\n", *output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "import-scan: %s is fresh (%s, %d components).\n", *output, *lang, len(doc.Components))
		return 0
	}

	if *output == "" {
		os.Stdout.Write(rendered)
		return 0
	}
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "import-scan: write %s: %v\n", *output, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "import-scan: wrote %d components to %s\n", len(doc.Components), *output)
	return 0
}
