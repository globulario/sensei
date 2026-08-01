// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cross-repo seed freshness.
//
// The embedded seed is generated from Sensei awareness plus the paired services
// corpus. Whole-file equality would deadlock cross-repository changes, so drift
// is partitioned by the precise RDF statement shape emitted by Sensei alone.

// classifySeedDiff partitions line-level differences into Sensei-owned and
// external context. agOnly is regenerated from Sensei's corpus alone.
func classifySeedDiff(committed, generated, agOnly []byte) (owned, external []string) {
	agOwnershipKeys := ntOwnershipKeys(agOnly)
	committedSet := ntLineSet(committed)
	generatedSet := ntLineSet(generated)

	var diffs []string
	for _, line := range ntLines(generated) {
		if !committedSet[line] {
			diffs = append(diffs, line)
		}
	}
	for _, line := range ntLines(committed) {
		if !generatedSet[line] {
			diffs = append(diffs, line)
		}
	}

	for _, line := range diffs {
		if agOwnershipKeys[ntOwnershipKey(line)] {
			owned = append(owned, line)
		} else {
			external = append(external, line)
		}
	}
	return owned, external
}

func ntLines(b []byte) []string {
	raw := strings.Split(string(b), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func ntLineSet(b []byte) map[string]bool {
	set := map[string]bool{}
	for _, line := range ntLines(b) {
		set[line] = true
	}
	return set
}

func ntSubject(line string) string {
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}

func ntSubjects(b []byte) map[string]bool {
	set := map[string]bool{}
	for _, line := range ntLines(b) {
		set[ntSubject(line)] = true
	}
	return set
}

func ntSubjectPredicate(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		return fields[0] + " " + fields[1]
	}
	return line
}

// ntOwnershipKey uses subject, predicate, and the precise RDF object identity.
// Shared subjects and predicates are common across repositories. Collapsing all
// literals to one bucket caused a services-authored literal on such a shared
// edge to be misclassified as Sensei-owned. Exact object identity avoids that
// collision. A genuine owned value change still fails because the newly
// generated statement is present in agOnly and therefore remains owned.
func ntOwnershipKey(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return line
	}
	object := ntObjectTerm(line)
	if object == "" {
		return line
	}
	return fields[0] + " " + fields[1] + " " + ntObjectOwnershipTerm(object)
}

// ntObjectTerm extracts the complete N-Triples object, preserving literals
// containing spaces, language tags, and datatype suffixes. Subject and predicate
// are whitespace-free RDF terms, so the object starts after the second token and
// ends before the terminal " .".
func ntObjectTerm(line string) string {
	line = strings.TrimSpace(line)
	first := strings.IndexAny(line, " \t")
	if first < 0 {
		return ""
	}
	rest := strings.TrimLeft(line[first:], " \t")
	second := strings.IndexAny(rest, " \t")
	if second < 0 {
		return ""
	}
	object := strings.TrimSpace(rest[second:])
	if strings.HasSuffix(object, " .") {
		object = strings.TrimSpace(object[:len(object)-2])
	}
	return object
}

func ntObjectOwnershipTerm(term string) string {
	if strings.HasPrefix(term, "<https://globular.io/awareness#") {
		trimmed := strings.TrimPrefix(term, "<https://globular.io/awareness#")
		return strings.TrimSuffix(trimmed, ">")
	}
	// Blank-node labels may be generator-local identities. Keep them in one
	// bucket; all stable RDF objects use their complete lexical term.
	if strings.HasPrefix(term, "_:") {
		return "bnode"
	}
	return term
}

func ntOwnershipKeys(b []byte) map[string]bool {
	set := map[string]bool{}
	for _, line := range ntLines(b) {
		set[ntOwnershipKey(line)] = true
	}
	return set
}

// generateAgOnlyNT regenerates the seed from Sensei-owned awareness only. A nil
// result means ownership is unknown and callers must fall back to strict
// comparison.
func generateAgOnlyNT(agRepo string) []byte {
	if strings.TrimSpace(agRepo) == "" {
		return nil
	}
	dir := filepath.Join(agRepo, "docs", "awareness")
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	nt, _, _, err := generateNT([]string{dir}, "", "", agRepo, false)
	if err != nil {
		return nil
	}
	return nt
}

func runSeedFreshness(args []string) int {
	fs := flag.NewFlagSet("sensei seed-freshness", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	committedPath := fs.String("committed", "", "path to the committed seed (awareness.nt)")
	generatedPath := fs.String("generated", "", "path to the freshly generated seed")
	agRepo := fs.String("ag-repo", "", "Sensei repo root (provides the owned corpus); auto-detect cwd")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei seed-freshness -committed <path> -generated <path> [-ag-repo <path>]

Ownership-aware seed freshness. Fails only when triples owned by the Sensei
corpus drift; paired-services context is reported but tolerated.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *committedPath == "" || *generatedPath == "" {
		fmt.Fprintln(os.Stderr, "sensei seed-freshness: -committed and -generated are required")
		return 2
	}

	root := strings.TrimSpace(*agRepo)
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}

	committed, err := os.ReadFile(*committedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei seed-freshness: read committed: %v\n", err)
		return 1
	}
	generated, err := os.ReadFile(*generatedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei seed-freshness: read generated: %v\n", err)
		return 1
	}

	agOnly := generateAgOnlyNT(root)
	if agOnly == nil {
		fmt.Fprintln(os.Stderr, "sensei seed-freshness: WARNING could not derive owned corpus; falling back to strict comparison")
		if string(committed) == string(generated) {
			fmt.Println("seed-freshness: current (strict)")
			return 0
		}
		fmt.Fprintln(os.Stderr, "seed-freshness: STALE (strict fallback) — run scripts/build-awareness-graph.sh and commit")
		return 1
	}

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(external) > 0 {
		fmt.Printf("seed-freshness: %d external/context triple(s) differ (cross-repo lag, gated by the owning repo) — tolerated\n", len(external))
	}
	if len(owned) > 0 {
		fmt.Fprintf(os.Stderr, "seed-freshness: STALE — %d awareness-graph-owned triple(s) drift:\n", len(owned))
		for i, line := range owned {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(owned)-20)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		}
		fmt.Fprintln(os.Stderr, "Run scripts/build-awareness-graph.sh and commit the regenerated seed.")
		return 1
	}
	fmt.Println("seed-freshness: owned triples current")
	return 0
}
