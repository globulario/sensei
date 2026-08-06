// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/extractor"
)

// runCheck validates awareness YAML sources without building or loading.
//
// The governing law, learned the hard way on 2026-08-05: ABSENCE OF OBSERVED
// FAILURE IS NOT EVIDENCE THAT THE OBSERVER WAS ALIVE.
//
// This command used to report skipped files as an informational suffix —
// "docs/awareness: 210 files, 117939 triples [35 not imported]" — and then
// print "All checks passed." It only failed on a skipped file under -strict,
// and only for two of the four non-imported statuses. So when
// docs/awareness/invariants.yaml stopped parsing (a mis-indented entry appended
// by `sensei propose`), the file was silently dropped from the corpus and the
// validator declared success. Roughly 11,000 triples and three whole files went
// missing from the published graph, and briefings were served from the
// truncated corpus for an entire session without a single warning.
//
// A file that cannot be parsed is not a statistic. It is a hole in the
// knowledge plane, and it must be impossible to report as clean.
//
// So the fatal cases below are unconditional — never gated behind -strict —
// and the census is always printed, so "0 findings" can be distinguished from
// "the scanner never saw the corpus".
func runCheck(args []string) int {
	fs := flag.NewFlagSet("sensei check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var inputDirs stringSlice
	fs.Var(&inputDirs, "input", "awareness YAML directory (repeatable; default: docs/awareness)")
	strict := fs.Bool("strict", false,
		"retained for compatibility; unparseable and unknown-schema files now fail unconditionally")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei check [flags]

Validates awareness YAML sources without building or loading.
Checks schema recognition, reference integrity, and N-Triples validity.

Always fails (regardless of -strict) when a governed file cannot be parsed,
when a file's schema is unrecognized, when the file census does not reconcile,
or when a non-empty corpus imported nothing.

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
			fmt.Fprintf(os.Stderr, "sensei check: %v\n", err)
			return 1
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "sensei check: %s is not a directory\n", dir)
			return 1
		}

		emitter, report, err := extractor.ImportAwarenessDir(dir, &buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: ERROR: %v\n", dir, err)
			hasErrors = true
			continue
		}

		if !reportCensus(dir, emitter.Triples, report, *strict) {
			hasErrors = true
		}
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

// censusOf buckets a walk's files by disposition. Every discovered file lands
// in exactly one bucket, so the totals must reconcile — see reportCensus.
type census struct {
	discovered       int
	imported         int
	ignored          int
	knownUnsupported []extractor.FileReport
	unknownSchema    []extractor.FileReport
	unparseable      []extractor.FileReport
	other            []extractor.FileReport
}

func censusOf(report *extractor.ImportReport) census {
	c := census{discovered: len(report.Files)}
	for _, f := range report.Files {
		switch f.Status {
		case extractor.StatusImported:
			c.imported++
		case extractor.StatusIgnored:
			c.ignored++
		case extractor.StatusKnownUnsupported:
			c.knownUnsupported = append(c.knownUnsupported, f)
		case extractor.StatusUnknownSchema:
			c.unknownSchema = append(c.unknownSchema, f)
		case extractor.StatusInvalid:
			c.unparseable = append(c.unparseable, f)
		default:
			// A status this command does not know about is itself a defect:
			// it means files can be dropped through a bucket nobody counts.
			c.other = append(c.other, f)
		}
	}
	return c
}

func (c census) accountedFor() int {
	return c.imported + c.ignored + len(c.knownUnsupported) +
		len(c.unknownSchema) + len(c.unparseable) + len(c.other)
}

// reportCensus prints the explicit accounting and returns false if the corpus
// is not fully and legitimately accounted for.
func reportCensus(dir string, triples int, report *extractor.ImportReport, strict bool) bool {
	c := censusOf(report)
	ok := true

	fmt.Fprintf(os.Stdout, "  %s\n", dir)
	fmt.Fprintf(os.Stdout, "    source files discovered  %d\n", c.discovered)
	fmt.Fprintf(os.Stdout, "    source files parsed      %d\n", c.discovered-len(c.unparseable))
	fmt.Fprintf(os.Stdout, "    source files imported    %d\n", c.imported)
	fmt.Fprintf(os.Stdout, "    source files rejected    %d\n", len(c.unparseable)+len(c.unknownSchema)+len(c.other))
	fmt.Fprintf(os.Stdout, "    declared exclusions      %d (ignored %d, known_unsupported %d)\n",
		c.ignored+len(c.knownUnsupported), c.ignored, len(c.knownUnsupported))
	fmt.Fprintf(os.Stdout, "    triples produced         %d\n", triples)

	// A governed file that cannot be parsed is a hole in the knowledge plane.
	// This is the case that shipped silently; it is fatal, always.
	if n := len(c.unparseable); n > 0 {
		ok = false
		fmt.Fprintf(os.Stderr, "  FAIL: %d governed file(s) could not be parsed — their content is ABSENT from the graph:\n", n)
		for _, f := range c.unparseable {
			fmt.Fprintf(os.Stderr, "    %s: %s\n", f.Path, f.Reason)
		}
	}

	// A file whose schema is unrecognized was discovered and then dropped. That
	// is an expected awareness file being skipped, which the corpus never
	// declared — fatal regardless of -strict.
	if n := len(c.unknownSchema); n > 0 {
		ok = false
		fmt.Fprintf(os.Stderr, "  FAIL: %d file(s) have an unrecognized schema and were skipped without a declared exclusion:\n", n)
		for _, f := range c.unknownSchema {
			fmt.Fprintf(os.Stderr, "    %s: %s\n", f.Path, f.Reason)
		}
	}

	if n := len(c.other); n > 0 {
		ok = false
		fmt.Fprintf(os.Stderr, "  FAIL: %d file(s) have a disposition this checker does not account for:\n", n)
		for _, f := range c.other {
			fmt.Fprintf(os.Stderr, "    %s: status=%s %s\n", f.Path, f.Status, f.Reason)
		}
	}

	// The buckets must add up. If they ever don't, files are being dropped
	// through a gap in the accounting itself and no other number here is
	// trustworthy.
	if got := c.accountedFor(); got != c.discovered {
		ok = false
		fmt.Fprintf(os.Stderr,
			"  FAIL: census does not reconcile — %d discovered but %d accounted for; files are being dropped uncounted\n",
			c.discovered, got)
	}

	// "Zero findings" must never mean "the scanner never saw the corpus."
	if c.discovered > 0 && c.imported == 0 {
		ok = false
		fmt.Fprintf(os.Stderr,
			"  FAIL: %d file(s) discovered but NONE imported — the corpus was found and then not read\n", c.discovered)
	}
	if c.imported > 0 && triples == 0 {
		ok = false
		fmt.Fprintf(os.Stderr,
			"  FAIL: %d file(s) imported but 0 triples produced — import ran and emitted nothing\n", c.imported)
	}

	// Declared exclusions are legitimate — a known_unsupported file is
	// explicitly classified, not silently skipped — but they must stay VISIBLE.
	// The original bug was not that files were excluded; it was that exclusions
	// and data loss were reported through the same anonymous counter.
	//
	// Deliberately not fatal under -strict: that would redefine an existing
	// contract (sensei's own corpus legitimately carries namespaces.yaml as
	// known_unsupported), and it is not what the flag has ever meant.
	for _, f := range c.knownUnsupported {
		fmt.Fprintf(os.Stdout, "    excluded (importer not implemented, phase %s): %s\n", f.Phase, f.Path)
	}
	_ = strict // retained for compatibility; the checks above are unconditional

	return ok
}
