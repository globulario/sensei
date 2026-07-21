// SPDX-License-Identifier: AGPL-3.0-only

// proto-scan — CLI wrapper over golang/extractor/protoscan.
//
// Parses .proto files and emits a `contracts:` YAML (the architecture_contracts
// schema) with one Contract per gRPC service + one per RPC. The reusable core
// lives in the protoscan package so `awg bootstrap` can call it in-process.
//
// Usage:
//
//	proto-scan -proto proto/awareness_graph.proto \
//	  -repo-root . \
//	  -component AwarenessGraph=component.awareness_graph_service \
//	  -output docs/awareness/architecture/awareness_graph_proto_contracts.yaml
//
//	proto-scan ... -check     # regenerate in memory, diff committed output, exit 1 if stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/extractor/protoscan"
)

// stringList implements flag.Value for repeatable flags.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("proto-scan", flag.ContinueOnError)
	var protos stringList
	var components stringList
	fs.Var(&protos, "proto", "path to a .proto file (repeatable)")
	fs.Var(&components, "component", "map a service to a component id: Service=component.id (repeatable)")
	repoRoot := fs.String("repo-root", ".", "repo root for relative source_files paths")
	output := fs.String("output", "", "output YAML path (default: stdout)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: proto-scan -proto <file> [-proto ...] [-component Service=id] [-output f] [-check]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(protos) == 0 {
		fmt.Fprintln(os.Stderr, "proto-scan: at least one -proto is required")
		return 2
	}

	componentByService, err := protoscan.ParseComponentMap(components)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proto-scan: %v\n", err)
		return 2
	}

	var doc protoscan.Doc
	for _, p := range protos {
		cs, err := protoscan.ScanProto(p, *repoRoot, componentByService)
		if err != nil {
			fmt.Fprintf(os.Stderr, "proto-scan: %s: %v\n", p, err)
			return 1
		}
		doc.Contracts = append(doc.Contracts, cs...)
	}

	rendered, err := protoscan.Render(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proto-scan: render: %v\n", err)
		return 1
	}

	if *check {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "proto-scan: -check requires -output")
			return 2
		}
		committed, err := os.ReadFile(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "proto-scan: read committed %s: %v\n", *output, err)
			return 1
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "STALE: %s — run `make proto-contracts` and commit.\n", *output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "proto-scan: %s is fresh (%d contracts).\n", *output, len(doc.Contracts))
		return 0
	}

	if *output == "" {
		os.Stdout.Write(rendered)
		return 0
	}
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "proto-scan: write %s: %v\n", *output, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "proto-scan: wrote %d contracts to %s\n", len(doc.Contracts), *output)
	return 0
}
