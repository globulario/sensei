// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
)

// classIRISegment maps a governed source schema to the IRI segment its
// identities project under. Only schemas listed here are REQUIRED to project;
// anything else is reported as declared/excluded rather than missing, so this
// gate never invents obligations the importer was never asked to meet.
var classIRISegment = map[string]string{
	"invariants":        "invariant/",
	"failure_modes":     "failureMode/",
	"forbidden_fixes":   "forbiddenFix/",
	"incident_patterns": "incidentPattern/",
	// NOTE: required_tests are deliberately absent. Their identities are file
	// paths that the emitter percent-encodes into the IRI
	// (test:golang%2Fnode_agent%2F...), so proving their projection needs the
	// emitter's exact encoding rather than string concatenation. Claiming to
	// verify them without it would report every test as missing.
}

func runDomainClosure(args []string) int {
	fs := flag.NewFlagSet("sensei domain-closure", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	input := fs.String("input", "docs/awareness", "certified source corpus directory")
	ntFile := fs.String("ntriples", "", "emitted N-Triples to verify (default: compile from -input)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei domain-closure [flags]

Proves DOMAIN CLOSURE for a published slice: that it contains every identity its
certified source declares, and contains nothing authored outside that source.

Projection coverage alone is insufficient. A slice built from the wrong working
directory projects perfectly — it is simply the wrong corpus — and reports
itself current and authoritative. Closure is the check that contradicts it.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: %v\n", err)
		return 2
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: %s is not a directory\n", root)
		return 2
	}

	// Compile the corpus. This is the same import the build uses, so the
	// identities compared here are the ones a publication would carry.
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: import: %v\n", err)
		return 1
	}

	// An unparseable governed file makes closure unprovable, not merely
	// incomplete: its identities are unknown, so "missing" cannot be computed.
	if report.HasInvalid() {
		fmt.Fprintln(os.Stderr, "  ✗ DOMAIN CLOSURE UNPROVABLE — governed source files could not be parsed:")
		for _, f := range report.Skipped() {
			if f.Status == extractor.StatusInvalid {
				fmt.Fprintf(os.Stderr, "    %s: %s\n", f.Path, f.Reason)
			}
		}
		return 1
	}

	expected, excluded, err := expectedIdentities(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: %v\n", err)
		return 1
	}

	ntReader := func() (*bytes.Reader, error) {
		if *ntFile == "" {
			return bytes.NewReader(buf.Bytes()), nil
		}
		b, err := os.ReadFile(*ntFile)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(b), nil
	}
	r, err := ntReader()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: %v\n", err)
		return 1
	}
	subs, err := ParseSubjects(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei domain-closure: parse n-triples: %v\n", err)
		return 1
	}

	c := ComputeClosure(root, expected, excluded, subs)
	fmt.Println("── Domain closure ──")
	if !writeClosure(os.Stdout, &c) {
		return 1
	}
	return 0
}

// expectedIdentities walks the corpus and returns, for every governed schema
// that projects, the identity id -> canonical subject IRI it must produce.
//
// Deliberately derived from the SOURCE, not from the graph: an expectation read
// out of the artifact being verified proves nothing.
func expectedIdentities(root string) (map[string]string, []string, error) {
	expected := map[string]string{}
	var excluded []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml")) {
			return nil
		}
		// Generated trees are build output, not authored source.
		if strings.Contains(path, string(filepath.Separator)+"generated"+string(filepath.Separator)) {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		top, ids := topLevelKeyAndIDs(data)
		seg, governed := classIRISegment[top]
		if !governed {
			for _, id := range ids {
				excluded = append(excluded, id)
			}
			return nil
		}
		for _, id := range ids {
			expected[id] = awarenessNS + seg + id
		}
		return nil
	})
	return expected, excluded, err
}

// topLevelKeyAndIDs extracts the first top-level mapping key and every `id:`
// declared beneath it.
//
// Line-oriented on purpose. The corpus files use three different list indents
// (0, 2 and 4 spaces across invariants/forbidden_fixes/failure_modes), and this
// gate must read all of them without asserting a convention — the convention
// mismatch is itself one of the defects under investigation.
func topLevelKeyAndIDs(data []byte) (string, []string) {
	var top string
	var ids []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if top == "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") &&
			strings.HasSuffix(strings.TrimSpace(line), ":") {
			top = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		t := strings.TrimSpace(line)
		t = strings.TrimPrefix(t, "- ")
		if !strings.HasPrefix(t, "id:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(t, "id:"))
		v = strings.Trim(v, `"'`)
		// Only top-level entry ids: nested `id:` under a relation block is
		// indented deeper than the entry's own key. Entry ids appear directly
		// after a list dash, which TrimPrefix above has already removed.
		if v == "" || strings.ContainsAny(v, " {}[]") {
			continue
		}
		if strings.HasPrefix(strings.TrimRight(raw, " \t\r"), strings.Repeat(" ", 8)) {
			continue // too deep to be an entry id
		}
		ids = append(ids, v)
	}
	return top, ids
}
