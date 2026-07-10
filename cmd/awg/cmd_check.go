// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/extractor"
)

func runCheck(args []string) int {
	fs := flag.NewFlagSet("awg check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var inputDirs stringSlice
	fs.Var(&inputDirs, "input", "awareness YAML directory (repeatable; default: docs/awareness)")
	strict := fs.Bool("strict", false, "fail on unrecognized or invalid YAML schemas")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg check [flags]

Validates awareness YAML sources without building or loading.
Checks schema recognition, reference integrity, and N-Triples validity.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if len(inputDirs) == 0 {
		inputDirs = []string{"docs/awareness"}
	}

	var buf bytes.Buffer
	hasErrors := false

	for _, dir := range inputDirs {
		info, err := os.Stat(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg check: %v\n", err)
			return 1
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "awg check: %s is not a directory\n", dir)
			return 1
		}

		emitter, report, err := extractor.ImportAwarenessDir(dir, &buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: ERROR: %v\n", dir, err)
			hasErrors = true
			continue
		}

		skipped := len(report.Skipped())
		ignored := len(report.Ignored())
		status := "OK"
		if skipped > 0 {
			status = fmt.Sprintf("%d not imported", skipped)
			if *strict {
				for _, f := range report.Skipped() {
					if f.Status == extractor.StatusUnknownSchema || f.Status == extractor.StatusInvalid {
						hasErrors = true
						break
					}
				}
			}
		} else if ignored > 0 {
			status = fmt.Sprintf("OK, %d ignored non-authority", ignored)
		}
		fmt.Fprintf(os.Stdout, "  %s: %d files, %d triples [%s]\n",
			dir, len(report.Files), emitter.Triples, status)
	}

	// Validate generated N-Triples.
	if errs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes())); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "  N-Triples validation: %d errors\n", len(errs))
		for i, e := range errs {
			if i >= 10 {
				fmt.Fprintf(os.Stderr, "  ... %d more\n", len(errs)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "    %s\n", e)
		}
		hasErrors = true
	} else {
		fmt.Fprintf(os.Stdout, "  N-Triples: valid\n")
	}

	if hasErrors {
		fmt.Fprintln(os.Stdout, "\nCheck FAILED.")
		return 1
	}
	fmt.Fprintln(os.Stdout, "\nAll checks passed.")
	return 0
}
