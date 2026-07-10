// SPDX-License-Identifier: Apache-2.0

// grpcweb-scan — CLI wrapper over golang/extractor/grpcwebscan.
//
// Scans TS/JS for gRPC-web service-client usage and emits a `contracts:` YAML
// adding consumed_by edges from the consuming component to the backend Contract
// proto-scan defines (linked by id). The reusable core lives in the grpcwebscan
// package so `awg bootstrap` can call it in-process.
//
// Usage:
//
//	grpcweb-scan -repo-root . -output docs/awareness/generated/contract_consumption.yaml
//	grpcweb-scan ... -check   # regenerate in memory, diff committed output, exit 1 if stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/awareness-graph/golang/extractor/grpcwebscan"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("grpcweb-scan", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repo root to scan / for relative source_files")
	output := fs.String("output", "", "output YAML path (default: stdout)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: grpcweb-scan -repo-root <dir> [-output f] [-check]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	files, err := grpcwebscan.FindSourceFiles(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpcweb-scan: discover: %v\n", err)
		return 1
	}
	var all []grpcwebscan.Usage
	for _, f := range files {
		us, serr := grpcwebscan.ScanFile(f, *repoRoot)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "grpcweb-scan: %s: %v\n", f, serr)
			return 1
		}
		all = append(all, us...)
	}
	doc := grpcwebscan.Doc{Contracts: grpcwebscan.Aggregate(all)}

	rendered, err := grpcwebscan.Render(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpcweb-scan: render: %v\n", err)
		return 1
	}

	if *check {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "grpcweb-scan: -check requires -output")
			return 2
		}
		committed, rerr := os.ReadFile(*output)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "grpcweb-scan: read committed %s: %v\n", *output, rerr)
			return 1
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "STALE: %s — regenerate and commit.\n", *output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "grpcweb-scan: %s is fresh (%d contracts).\n", *output, len(doc.Contracts))
		return 0
	}

	if *output == "" {
		os.Stdout.Write(rendered)
		return 0
	}
	if werr := os.WriteFile(*output, rendered, 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "grpcweb-scan: write %s: %v\n", *output, werr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "grpcweb-scan: wrote %d contracts to %s\n", len(doc.Contracts), *output)
	return 0
}
