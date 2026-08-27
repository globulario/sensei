// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/packcustody"
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
		top, ids, noncanonical := topLevelKeyIDsAndStatus(data)
		// A file that declares itself a generated projection of the principle
		// pack is not authored by the repository holding it. Custody already
		// refuses to publish it under this domain, so counting its identities
		// as ones this domain must project makes a CORRECT publication fail
		// closure: every project that installs the pack reports its whole
		// mirror missing. Observed live on sensei-code — 138 of 161 identities
		// "absent", which is exactly the pack's principle count.
		//
		// The authoring domain is unaffected: it authors principles in its own
		// corpus and generates the template from them, so it holds no mirror to
		// exclude.
		seg, governed := classIRISegment[top]
		if governed && packcustody.DeclaresManagedMirror(data) {
			excluded = append(excluded, ids...)
			return nil
		}
		if !governed {
			for _, id := range ids {
				excluded = append(excluded, id)
			}
			return nil
		}
		for _, id := range ids {
			// A candidate is not a canonical claim. It is DECLARED, and it is
			// expected NOT to project, which is exactly what Excluded records.
			if noncanonical[id] {
				excluded = append(excluded, id)
				continue
			}
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
// nonCanonicalStatus is the closed set of statuses that mean "declared, but not
// canonical governed truth".
//
// Read by MEMBERSHIP of the NON-canonical set, deliberately, so an unrecognised
// status stays REQUIRED to project. The other direction -- listing the canonical
// statuses and excluding everything else -- fails OPEN: a typo, or a status
// added later, would quietly excuse an identity from closure. Here an unknown
// status keeps its obligation, which is the safe direction for a check whose
// whole job is to notice absence.
var nonCanonicalStatus = map[string]bool{
	"candidate": true,
}

func topLevelKeyAndIDs(data []byte) (string, []string) {
	top, ids, _ := topLevelKeyIDsAndStatus(data)
	return top, ids
}

// topLevelKeyIDsAndStatus also reports which entry ids declare a non-canonical
// status.
//
// A candidate is not a canonical claim, so it must never be counted among the
// identities a domain is REQUIRED to project. Requiring it made every foreign
// repository fail closure the moment it was onboarded: `sensei import` writes
// extracted candidates under a top-level `invariants:` key -- a governed class --
// and correctly refuses to publish them, so the build demanded the publication
// of exactly what the design forbids publishing. Observed on golang/sync: 24
// required, 0 projected, domain not authoritative, and no governed run could
// start.
//
// Status is read at the entry's own field indentation, so a `status:` nested
// inside a relation block cannot be mistaken for the entry's.
func topLevelKeyIDsAndStatus(data []byte) (string, []string, map[string]bool) {
	var top string
	var ids []string
	noncanonical := map[string]bool{}

	// Each list entry is read AS A UNIT: its fields are gathered between one
	// `- ` boundary and the next, and only then are id and status paired. The
	// first version read the file as a stream and attached a status to the
	// last id seen, so an entry whose `status:` preceded its own `id:` handed
	// that status to the PREVIOUS entry -- which both re-created the defect
	// (the candidate stayed required) and excused a canonical neighbour from
	// closure. Field order inside an entry is not a fact about the entry, and
	// a neighbour's formatting must never change an entry's obligation.
	type entry struct {
		id, status string
	}
	var entries []entry
	var cur *entry
	entryIndent := -1
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
		}
		cur = nil
	}
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
		indent := len(line) - len(strings.TrimLeft(line, " "))
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || t == "-" {
			// A new entry begins at the list's own indentation. A dash deeper
			// than that is a nested list inside the current entry.
			if entryIndent < 0 || indent <= entryIndent {
				flush()
				entryIndent = indent
				cur = &entry{}
			}
			t = strings.TrimSpace(strings.TrimPrefix(t, "-"))
			indent += 2 // the field column for `- key: value`
		}
		if cur == nil {
			continue
		}
		// Only the entry's OWN fields: those at the entry's field column.
		// Anything deeper belongs to a nested block (a relation, an evidence
		// list) and must not be read as the entry's id or status.
		if indent != entryIndent+2 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "id:"):
			v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "id:")), `"'`)
			if v != "" && !strings.ContainsAny(v, " {}[]") && cur.id == "" {
				cur.id = v
			}
		case strings.HasPrefix(t, "status:"):
			if cur.status == "" {
				cur.status = strings.ToLower(strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "status:")), `"'`))
			}
		}
	}
	flush()
	for _, e := range entries {
		if e.id == "" {
			continue
		}
		ids = append(ids, e.id)
		if nonCanonicalStatus[e.status] {
			noncanonical[e.id] = true
		}
	}
	return top, ids, noncanonical
}
