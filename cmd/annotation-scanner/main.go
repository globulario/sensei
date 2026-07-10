// SPDX-License-Identifier: AGPL-3.0-only

// Command annotation-scanner walks a Go source tree, extracts @awareness
// annotations, validates them against a namespace registry, and writes
// generated code_symbols.yaml, code_edges.yaml, and annotation_report.yaml
// to an output directory.
//
// Usage:
//
//	annotation-scanner \
//	  --registry  docs/awareness/namespaces.yaml \
//	  --source    golang/echo/echo_server \
//	  --repo-root /absolute/path/to/repo \
//	  --output    docs/awareness/generated \
//	  [--strict]  [--dry-run]  [--prefix echo_service]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness-graph/golang/scanner"
)

func main() {
	registry := flag.String("registry", "", "path to namespaces.yaml")
	source := flag.String("source", "", "directory to scan (absolute or relative to repo-root)")
	repoRoot := flag.String("repo-root", "", "absolute path to repo root (default: current directory)")
	output := flag.String("output", "", "output directory for generated YAML files")
	prefix := flag.String("prefix", "", "filename prefix for generated files (e.g. echo_service → echo_service_code_symbols.yaml)")
	strict := flag.Bool("strict", false, "treat warnings as errors")
	dryRun := flag.Bool("dry-run", false, "scan and validate but do not write files")
	flag.Parse()

	if *registry == "" || *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: annotation-scanner --registry <path> --source <dir> --output <dir> [--repo-root <path>] [--prefix <name>] [--strict] [--dry-run]")
		os.Exit(2)
	}

	root := *repoRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			log.Fatalf("getwd: %v", err)
		}
	}

	// Resolve registry and source relative to repo-root if not absolute.
	regPath := resolve(root, *registry)
	srcPath := resolve(root, *source)
	outPath := resolve(root, *output)

	reg, err := scanner.LoadRegistry(regPath)
	if err != nil {
		log.Fatalf("load registry: %v", err)
	}

	sc := &scanner.Scanner{Registry: reg, RepoRoot: root, Strict: *strict}
	result, err := sc.Scan(srcPath)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}

	// Print summary.
	fmt.Printf("Scanned:    %d files\n", result.ScannedFiles)
	fmt.Printf("Annotated:  %d symbols\n", result.AnnotatedSymbols)
	fmt.Printf("Annotations:%d total\n", len(result.Annotations))
	if len(result.Warnings) > 0 {
		fmt.Printf("Warnings:   %d\n", len(result.Warnings))
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "  WARN  %s\n", w)
		}
	}
	if len(result.Errors) > 0 {
		fmt.Printf("Errors:     %d\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  ERROR %s\n", e)
		}
	}

	if *dryRun {
		fmt.Println("(dry-run — no files written)")
		if result.HasErrors() {
			os.Exit(1)
		}
		return
	}

	if result.HasErrors() && *strict {
		fmt.Fprintln(os.Stderr, "strict mode: aborting due to errors")
		os.Exit(1)
	}

	if err := os.MkdirAll(outPath, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", outPath, err)
	}

	pfx := *prefix
	if pfx != "" && !strings.HasSuffix(pfx, "_") {
		pfx += "_"
	}

	syms, edges, tests := scanner.BuildSymbolsAndEdges(result)

	// Write code_symbols.yaml
	symFile := filepath.Join(outPath, pfx+"code_symbols.yaml")
	if err := writeFile(symFile, func(f *os.File) error {
		return scanner.WriteSymbolsYAML(f, syms, tests)
	}); err != nil {
		log.Fatalf("write symbols: %v", err)
	}
	fmt.Printf("Wrote:      %s (%d symbols)\n", symFile, len(syms))

	// Write code_edges.yaml
	edgeFile := filepath.Join(outPath, pfx+"code_edges.yaml")
	if err := writeFile(edgeFile, func(f *os.File) error {
		return scanner.WriteEdgesYAML(f, edges)
	}); err != nil {
		log.Fatalf("write edges: %v", err)
	}
	fmt.Printf("Wrote:      %s (%d edges)\n", edgeFile, len(edges))

	// Write annotation_report.yaml
	reportFile := filepath.Join(outPath, pfx+"annotation_report.yaml")
	if err := writeFile(reportFile, func(f *os.File) error {
		return scanner.WriteReportYAML(f, result)
	}); err != nil {
		log.Fatalf("write report: %v", err)
	}
	fmt.Printf("Wrote:      %s\n", reportFile)
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func writeFile(path string, fn func(*os.File) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}
