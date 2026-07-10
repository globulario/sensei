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
// The embedded seed (awareness.nt) is a SINGLE artifact generated from two
// repos: the awareness-graph corpus (this repo) and the services awareness YAML.
// A whole-file freshness comparison deadlocks a paired cross-repo change: the
// awareness-graph seed PR carries triples authored by a services PR that
// services master does not have yet, and vice versa — so neither side can be
// "fresh" against the other's master until the other has already merged.
//
// The fix is ownership-aware comparison. A differing triple is OWNED by this
// repo only if its subject is produced by regenerating from the
// awareness-graph-owned corpus alone (agOnly). Owned drift fails the gate (real
// in-repo staleness/drift). Any other differing triple is EXTERNAL context: it
// originates from the paired repo's YAML, which may legitimately lead or lag its
// own master during a cross-repo change — it is reported but never fails this
// repo's gate (the owning repo's gate is responsible for it).
//
// This does NOT hide real errors: owned drift still fails, and dangling refs /
// N-Triples validity / stale generated files are enforced by their own checks.

// classifySeedDiff partitions the line-level difference between a committed seed
// and a freshly generated seed into owned (ag-authored) and external diffs.
// agOnly is the seed regenerated from the awareness-graph-owned corpus alone.
//
// Ownership is keyed by subject+predicate+object-ownership-term, not subject
// alone. A shared subject can legitimately carry triples from both repos (for
// example a source file referenced by awareness-graph docs and also annotated
// from services YAML). Subject-only ownership would misclassify any
// services-authored edge on that shared subject as awareness-graph-owned drift;
// subject+predicate is still too coarse when both repos emit different relation
// targets under the same predicate. The object-ownership-term is the FULL minted
// id for awareness IRIs (B/#141) — collapsing to the class family ("invariant",
// "failureMode", ...) was itself too coarse when both repos point the same
// (subject, predicate) at DIFFERENT objects of the same family (for example AG and
// services invariants both protecting one source file). Literals still collapse to
// a kind so a changed literal value on an owned edge still counts as owned drift.
func classifySeedDiff(committed, generated, agOnly []byte) (owned, external []string) {
	agOwnershipKeys := ntOwnershipKeys(agOnly)
	committedSet := ntLineSet(committed)
	generatedSet := ntLineSet(generated)

	var diffs []string
	for _, l := range ntLines(generated) { // present in generated, missing from committed
		if !committedSet[l] {
			diffs = append(diffs, l)
		}
	}
	for _, l := range ntLines(committed) { // present in committed, missing from generated
		if !generatedSet[l] {
			diffs = append(diffs, l)
		}
	}

	for _, l := range diffs {
		if agOwnershipKeys[ntOwnershipKey(l)] {
			owned = append(owned, l)
		} else {
			external = append(external, l)
		}
	}
	return owned, external
}

// ntLines returns the non-empty, trimmed triple lines of an N-Triples buffer.
func ntLines(b []byte) []string {
	raw := strings.Split(string(b), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func ntLineSet(b []byte) map[string]bool {
	m := map[string]bool{}
	for _, l := range ntLines(b) {
		m[l] = true
	}
	return m
}

// ntSubject returns the subject term of an N-Triples line (the first
// whitespace-delimited token, e.g. "<iri>" or "_:bnode").
func ntSubject(line string) string {
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}

func ntSubjects(b []byte) map[string]bool {
	m := map[string]bool{}
	for _, l := range ntLines(b) {
		m[ntSubject(l)] = true
	}
	return m
}

// ntSubjectPredicate returns the subject + predicate portion of an N-Triples
// line.
func ntSubjectPredicate(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		return fields[0] + " " + fields[1]
	}
	return line
}

// ntOwnershipKey returns the ownership bucket for a triple: subject +
// predicate + object ownership-term. For a minted awareness IRI the term is the
// FULL id (e.g. invariant/convergence.identity_is_build_id), NOT the collapsed
// class family.
//
// B (#141): collapsing every invariant object to the family "invariant" mis-owned
// a services-invariant edge as awareness-graph-owned whenever an AG-owned invariant
// referenced the SAME source file in its protects.files — the
// subject+predicate+family key collided (e.g. AG `state_authority_invariants.yaml`
// and the services `convergence.identity_is_build_id` both protect
// release_runtime_convergence.go). Keying by the full minted id distinguishes the
// specific target so each repo's invariant edge classifies by its own owner.
// Non-awareness IRIs/bnodes/literals still collapse to a kind so a changed literal
// value on an owned edge still counts as owned drift.
func ntOwnershipKey(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return line
	}
	return fields[0] + " " + fields[1] + " " + ntObjectOwnershipTerm(fields[2])
}

// ntObjectOwnershipTerm returns the ownership-distinguishing term for a triple's
// object. A minted awareness IRI returns its FULL id so edges to different
// invariants (or any minted node) classify by the specific target's owner, not a
// collapsed family (B/#141). Non-awareness IRIs, bnodes, and literals collapse to
// a kind, preserving owned-drift detection on changed literal values.
func ntObjectOwnershipTerm(term string) string {
	if strings.HasPrefix(term, "<https://globular.io/awareness#") {
		trimmed := strings.TrimPrefix(term, "<https://globular.io/awareness#")
		return strings.TrimSuffix(trimmed, ">")
	}
	if strings.HasPrefix(term, "<") {
		return "iri"
	}
	if strings.HasPrefix(term, "_:") {
		return "bnode"
	}
	if strings.HasPrefix(term, "\"") {
		return "literal"
	}
	return term
}

func ntOwnershipKeys(b []byte) map[string]bool {
	m := map[string]bool{}
	for _, l := range ntLines(b) {
		m[ntOwnershipKey(l)] = true
	}
	return m
}

// generateAgOnlyNT regenerates the seed from the awareness-graph-owned corpus
// alone (this repo's docs/awareness). The resulting subjects define what this
// repo "owns" for ownership-aware freshness. On any error it returns a nil slice
// — callers MUST treat nil as "ownership unknown" and fall back to strict
// comparison so a generation failure can never silently hide drift.
func generateAgOnlyNT(agRepo string) []byte {
	if strings.TrimSpace(agRepo) == "" {
		return nil
	}
	dir := filepath.Join(agRepo, "docs", "awareness")
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	nt, _, _, err := generateNT([]string{dir}, "", "", agRepo)
	if err != nil {
		return nil
	}
	return nt
}

// runSeedFreshness is the `awg seed-freshness` subcommand. It performs an
// ownership-aware comparison of a committed seed against a freshly generated
// one, exiting non-zero only when this repo's OWNED triples drift. It is the
// awareness-graph-side gate (called by build-awareness-graph.sh), the mirror of
// the services-side embeddata-freshness audit check.
func runSeedFreshness(args []string) int {
	fs := flag.NewFlagSet("awg seed-freshness", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	committedPath := fs.String("committed", "", "path to the committed seed (awareness.nt)")
	generatedPath := fs.String("generated", "", "path to the freshly generated seed")
	agRepo := fs.String("ag-repo", "", "awareness-graph repo root (provides the owned corpus); auto-detect cwd")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg seed-freshness -committed <path> -generated <path> [-ag-repo <path>]

Ownership-aware seed freshness. Fails only when triples OWNED by the
awareness-graph corpus drift; triples authored by the paired services repo are
reported as cross-repo context and never fail this repo's gate.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *committedPath == "" || *generatedPath == "" {
		fmt.Fprintln(os.Stderr, "awg seed-freshness: -committed and -generated are required")
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
		fmt.Fprintf(os.Stderr, "awg seed-freshness: read committed: %v\n", err)
		return 1
	}
	generated, err := os.ReadFile(*generatedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg seed-freshness: read generated: %v\n", err)
		return 1
	}

	agOnly := generateAgOnlyNT(root)
	if agOnly == nil {
		// Ownership unknown — fall back to strict whole-file comparison so a
		// generation failure cannot hide drift.
		fmt.Fprintln(os.Stderr, "awg seed-freshness: WARNING could not derive owned corpus; falling back to strict comparison")
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
		for i, l := range owned {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(owned)-20)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", l)
		}
		fmt.Fprintln(os.Stderr, "Run scripts/build-awareness-graph.sh and commit the regenerated seed.")
		return 1
	}
	fmt.Println("seed-freshness: owned triples current")
	return 0
}
