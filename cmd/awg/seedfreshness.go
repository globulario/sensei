// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
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
// repo only if its precise RDF statement identity is produced by regenerating
// from the awareness-graph-owned corpus alone (agOnly). Owned drift fails the
// gate. Any other differing triple is EXTERNAL context and is reported without
// failing this repo's gate; the owning repo remains responsible for it.
//
// This does NOT hide real errors: owned drift still fails, and dangling refs /
// N-Triples validity / stale generated files are enforced by their own checks.

// classifySeedDiff partitions the line-level difference between a committed seed
// and a freshly generated seed into owned (ag-authored) and external diffs.
// agOnly is the seed regenerated from the awareness-graph-owned corpus alone.
func classifySeedDiff(committed, generated, agOnly []byte) (owned, external []string) {
	agOwnershipKeys := ntOwnershipKeys(agOnly)
	agLines := ntLineSet(agOnly)
	agLinesBySubjectPredicate := ntLinesBySubjectPredicate(agOnly)
	committedSet := ntLineSet(committed)
	generatedSet := ntLineSet(generated)

	// ADDED drift — present in generated, missing from the committed seed.
	//
	// Ownership here is EXACT-LINE, not key-based. Awareness-graph staleness means
	// "regenerating the AG corpus alone produces a line the committed seed lacks",
	// which is a statement about the exact triple. Sharing an ownership key with the
	// paired repo is not evidence AG authored THIS line.
	for _, l := range ntLines(generated) {
		if committedSet[l] || isSeedMarkerLine(l) {
			continue
		}
		if agLines[l] {
			owned = append(owned, l)
		} else {
			external = append(external, l)
		}
	}

	// REMOVED drift — present in the committed seed, missing from generated.
	//
	// Full object identity distinguishes services-authored literals and stable IRIs
	// on shared subject+predicate edges. A narrow literal-replacement fallback keeps
	// the gate fail-closed when an AG-owned literal changes value: the new exact AG
	// line is owned as an addition, and the old line is also owned only when the
	// committed seed contains no current AG value for that edge. An extra services
	// literal coexisting with the current AG line therefore remains external.
	for _, l := range ntLines(committed) {
		if generatedSet[l] || isSeedMarkerLine(l) {
			continue
		}
		if agOwnershipKeys[ntOwnershipKey(l)] || removedOwnedLiteralReplacement(l, committedSet, agLinesBySubjectPredicate) {
			owned = append(owned, l)
		} else {
			external = append(external, l)
		}
	}
	return owned, external
}

// seedMarkerSubjectPrefix is the subject prefix of the self-describing SeedBuild
// marker that seedmeta.AppendMarker stamps onto every generated artifact.
const seedMarkerSubjectPrefix = "<" + seedmeta.NamespaceIRI + "seedBuild/sha256-"

// isSeedMarkerLine reports whether a triple belongs to the seed's self-describing
// SeedBuild marker rather than to the corpus.
//
// The marker names its own sha256 and triple count, so its subject and objects are
// a function of the artifact it is attached to. That makes it structurally
// incomparable across build modes: the awareness-graph repo commits a SELF-ONLY
// seed, while paired-repo CI regenerates a COMBINED one, so the two markers can
// never agree by construction. Marker integrity is verified separately by
// seedmeta; freshness must ignore marker triples in both directions.
func isSeedMarkerLine(line string) bool {
	return strings.HasPrefix(line, seedMarkerSubjectPrefix)
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
// predicate + object ownership term. Minted awareness IRIs keep their compact
// full id. Stable external IRIs and literals keep their complete N-Triples
// identity so repositories cannot claim each other's values merely because they
// share a subject and predicate. Blank-node labels remain serialization-local
// and therefore collapse to the bnode kind.
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

// ntObjectTerm extracts the complete N-Triples object, preserving literals with
// spaces, language tags, and datatype suffixes. Subject and predicate are
// whitespace-free RDF terms, so the object begins after the second token and
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

// ntObjectOwnershipTerm preserves stable RDF object identity. Awareness IRIs
// retain their compact minted id for readable diagnostics; other IRIs and
// literals remain exact. Blank nodes collapse because their labels are scoped to
// one serialization and cannot provide cross-artifact identity.
func ntObjectOwnershipTerm(term string) string {
	if strings.HasPrefix(term, "<https://globular.io/awareness#") {
		trimmed := strings.TrimPrefix(term, "<https://globular.io/awareness#")
		return strings.TrimSuffix(trimmed, ">")
	}
	if strings.HasPrefix(term, "_:") {
		return "bnode"
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

func ntLinesBySubjectPredicate(b []byte) map[string][]string {
	m := map[string][]string{}
	for _, l := range ntLines(b) {
		key := ntSubjectPredicate(l)
		m[key] = append(m[key], l)
	}
	return m
}

// removedOwnedLiteralReplacement recognizes the removed side of a genuine
// AG-owned literal value change without reintroducing literal-kind collisions.
// It applies only when AG owns the subject+predicate and the committed seed does
// not already contain any current exact AG line for that edge.
func removedOwnedLiteralReplacement(line string, committedSet map[string]bool, agLinesBySubjectPredicate map[string][]string) bool {
	if !strings.HasPrefix(ntObjectTerm(line), "\"") {
		return false
	}
	agLines := agLinesBySubjectPredicate[ntSubjectPredicate(line)]
	if len(agLines) == 0 {
		return false
	}
	for _, agLine := range agLines {
		if committedSet[agLine] {
			return false
		}
	}
	return true
}

// generateAgOnlyNT regenerates the seed from the awareness-graph-owned corpus
// alone (this repo's docs/awareness). The resulting statements define what this
// repo owns. On any error it returns nil; callers must then fall back to strict
// comparison so a generation failure cannot silently hide drift.
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

// runSeedFreshness is the `sensei seed-freshness` subcommand. It performs an
// ownership-aware comparison of a committed seed against a freshly generated
// one, exiting non-zero only when this repo's owned triples drift.
func runSeedFreshness(args []string) int {
	fs := flag.NewFlagSet("sensei seed-freshness", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	committedPath := fs.String("committed", "", "path to the committed seed (awareness.nt)")
	generatedPath := fs.String("generated", "", "path to the freshly generated seed")
	agRepo := fs.String("ag-repo", "", "awareness-graph repo root (provides the owned corpus); auto-detect cwd")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei seed-freshness -committed <path> -generated <path> [-ag-repo <path>]

Ownership-aware seed freshness. Fails only when triples OWNED by the
awareness-graph corpus drift; triples authored by the paired services repo are
reported as cross-repo context and never fail this repo's gate.
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
