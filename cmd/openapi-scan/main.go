// SPDX-License-Identifier: Apache-2.0

// openapi-scan — CLI wrapper over golang/extractor/openapiscan.
//
// Parses OpenAPI/Swagger spec files and emits a `contracts:` YAML (the
// architecture_contracts schema) with one Contract per spec + one per
// path×method operation. The reusable core lives in the openapiscan package so
// `awg bootstrap` can call it in-process.
//
// Usage:
//
//	openapi-scan -repo-root . -output docs/awareness/architecture/rest_contracts.yaml
//	openapi-scan -spec api/openapi.yaml -output out.yaml
//	openapi-scan ... -check     # regenerate in memory, diff committed output, exit 1 if stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor/openapiscan"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("openapi-scan", flag.ContinueOnError)
	var specs stringList
	fs.Var(&specs, "spec", "path to an OpenAPI/Swagger spec (repeatable; default: discover under -repo-root)")
	repoRoot := fs.String("repo-root", ".", "repo root for relative source_files paths and spec discovery")
	output := fs.String("output", "", "output YAML path (default: stdout)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: openapi-scan [-spec f ...] [-repo-root dir] [-output f] [-check]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	files := []string(specs)
	if len(files) == 0 {
		found, err := openapiscan.FindSpecFiles(*repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "openapi-scan: discover: %v\n", err)
			return 1
		}
		files = found
	}

	var doc openapiscan.Doc
	for _, f := range files {
		cs, err := openapiscan.ScanSpec(f, *repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "openapi-scan: %s: %v\n", f, err)
			return 1
		}
		doc.Contracts = append(doc.Contracts, cs...)
	}

	rendered, err := openapiscan.Render(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-scan: render: %v\n", err)
		return 1
	}

	if *check {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "openapi-scan: -check requires -output")
			return 2
		}
		committed, err := os.ReadFile(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "openapi-scan: read committed %s: %v\n", *output, err)
			return 1
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "STALE: %s — regenerate and commit.\n", *output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "openapi-scan: %s is fresh (%d contracts).\n", *output, len(doc.Contracts))
		return 0
	}

	if *output == "" {
		os.Stdout.Write(rendered)
		return 0
	}
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "openapi-scan: write %s: %v\n", *output, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "openapi-scan: wrote %d contracts to %s\n", len(doc.Contracts), *output)
	return 0
}
