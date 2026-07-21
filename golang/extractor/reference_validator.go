// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=extractor
// @awareness file_role=cross_reference_validator
// @awareness implements=globular.awareness_graph:intent.awareness.cited_anchors_must_be_defined

package extractor

// Cross-reference validator. The importers in this package have a
// deliberate two-step pattern for classes that can be both referenced
// from other anchors (e.g. invariant.forbidden_fixes) AND defined in
// their own schema (forbidden_fixes.yaml). The referencing importer
// calls ensureNode to mint a typed-stub IRI so the edge has a valid
// target; the defining importer then enriches that same IRI with a
// label, prose, authoredIn, etc.
//
// The pattern only works when both sides mint the SAME IRI. If they
// don't (typo in the cite, missing definition, prefix mismatch like
// `forbidden.X` vs `X`), the citing importer produces a typed node
// that no schema ever enriched — a "dangling reference". Until this
// validator existed, the only signal of a dangling reference was a
// missing label at briefing time, which is silent.
//
// What this catches:
//   - An invariant cites a forbidden_fix ID that no forbidden_fixes.yaml
//     defines (the round-3 case for `hot_deploy_local_binary_as_break_glass`
//     and `bypass_cycle_with_direct_storage_write`, which sat
//     cited-but-undefined for months).
//   - A required_test ID is cited from invariants/failure_modes but
//     no required_tests.yaml defines it.
//   - Naming-style drift (definitions use bare IDs, citers prefix with
//     `forbidden.` — the two mint different IRIs and the citation is
//     effectively dangling).
//
// What this does NOT catch:
//   - Semantic correctness of a cite (e.g. citing the wrong forbidden
//     fix for an invariant). That's the invariant author's call.
//   - References to types that DON'T use the ensureNode-from-citer
//     pattern (e.g. FailureMode, where the comment explicitly notes
//     "missing FM is a drift-query concern"). Extending coverage is
//     possible but each class needs its own definer-marker decision.

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// ReferenceError describes one dangling-reference finding.
type ReferenceError struct {
	// Class IRI of the dangling node (e.g. AwNS+"ForbiddenFix").
	Class string
	// Subject IRI of the dangling node, with surrounding < > stripped.
	Subject string
	// ID portion (the part after the class prefix). Useful for grepping
	// the citing YAML.
	ID string
}

func (e ReferenceError) Error() string {
	return fmt.Sprintf("dangling %s reference: %s (no aw:authoredIn — no schema defines this anchor)", classBaseName(e.Class), e.ID)
}

// referencePolicy holds the per-class rules: which type IRI to scan for,
// and which predicate IRI distinguishes "defined" from "referenced-only".
type referencePolicy struct {
	classIRI         string
	definerPredicate string
	trackMentions    bool
	// bridgeClassIRI, when set, treats a subject of classIRI as "defined" if a
	// subject of bridgeClassIRI with the same normalized ID exists — even when
	// the subject itself lacks definerPredicate. Used for Test: a required_test
	// citation mints a ClassTest node, while the code scanner emits the actual
	// test function as a ClassTestSymbol (with aw:authoredIn). A cited test that
	// points at a REAL, scanned test is therefore proven to exist by the scan
	// and must not be flagged dangling — a redundant required_tests.yaml entry
	// is not required. Typos / nonexistent tests still dangle (no matching
	// ClassTestSymbol). Empty = no bridge.
	bridgeClassIRI string
}

// normalizeTestID canonicalizes a test-reference ID so the two separator
// conventions compare equal: required_tests.yaml citations use '::'
// (file_test.go::TestX) while the code scanner's ClassTestSymbol emission uses
// a single ':' (file_test.go:TestX). A file path or Go test name never contains
// '::', so collapsing it to ':' is loss-free for this domain.
func normalizeTestID(id string) string {
	return strings.ReplaceAll(id, "::", ":")
}

// defaultPolicies returns the classes whose dangling references are
// checked by ValidateReferences. Order is significant only for the
// determinism of error output — sorted alphabetically by class.
//
// Currently covered:
//   - ForbiddenFix: defined by importForbiddenFixes (emits aw:authoredIn);
//     cited via ensureNode from invariants.yaml and incident_patterns.yaml.
//   - Test: defined by importRequiredTests (emits aw:authoredIn); cited
//     via ensureNode from invariants.yaml and failure_modes.yaml.
//   - Contract: defined by the contract importers (emits aw:authoredIn) and
//     referenced from failure-mode / realization links. Unlike ForbiddenFix and
//     Test, contract references must NOT mint typed stubs, so validation tracks
//     any mentioned Contract IRI rather than relying on rdf:type from the citer.
//
// FailureMode is intentionally excluded — the importer for invariants
// notes that missing FMs are tracked by a separate drift query, and the
// invariants importer does NOT call ensureNode for them.
func defaultPolicies() []referencePolicy {
	return []referencePolicy{
		{classIRI: rdf.ClassForbiddenFix, definerPredicate: rdf.PropAuthoredIn},
		{classIRI: rdf.ClassTest, definerPredicate: rdf.PropAuthoredIn, bridgeClassIRI: rdf.ClassTestSymbol},
		{classIRI: rdf.ClassContract, definerPredicate: rdf.PropAuthoredIn, trackMentions: true},
	}
}

// ValidateReferences scans an N-Triples stream and returns one error
// per dangling reference. A dangling reference is a subject that has
// rdf:type = <class> but no <definerPredicate> triple, for each class
// in defaultPolicies.
//
// The function reads the stream linearly and is safe to call on the
// same buffer yaml2nt is about to write — it does not modify the input.
func ValidateReferences(r io.Reader) ([]ReferenceError, error) {
	return validateReferencesWithPolicies(r, defaultPolicies())
}

// FilterAllowed splits errs into (new, known) using the allowlist of
// "class\tid" baseline entries. allowed is the set of currently-known
// dangling references — entries in errs that match are returned as
// "known" (existing debt, not CI-blocking); the rest are "new"
// (regressions added since the baseline). The ratchet pattern: CI
// fails only on "new" entries, and the baseline file is updated
// deliberately when a dangling reference is intentionally added (which
// should be rare and discussed).
//
// The allowlist key is "Class\tID" — both fields exactly as the
// ReferenceError carries them. Tab separator avoids collisions with
// the dot/underscore characters that appear inside class IRIs and IDs.
func FilterAllowed(errs []ReferenceError, allowed map[string]bool) (new, known []ReferenceError) {
	for _, e := range errs {
		key := e.Class + "\t" + e.ID
		if allowed[key] {
			known = append(known, e)
		} else {
			new = append(new, e)
		}
	}
	return new, known
}

// LoadAllowedRefs reads a baseline allowlist file. Each non-empty,
// non-comment line is one "Class<tab>ID" entry — the same format
// SerializeAllowedRefs writes. Lines starting with '#' are comments.
//
// Format is intentionally flat and append-friendly so the baseline
// file diffs cleanly when a new entry is approved.
func LoadAllowedRefs(r io.Reader) (map[string]bool, error) {
	out := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("allowed-refs line %d: expected 'Class<tab>ID', got %q", lineNo, line)
		}
		out[parts[0]+"\t"+parts[1]] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("allowed-refs scan: %w", err)
	}
	return out, nil
}

// SerializeAllowedRefs writes errs as a baseline allowlist file. The
// output is sorted (ValidateReferences already sorts) so two runs
// produce identical files, which keeps diffs honest.
func SerializeAllowedRefs(w io.Writer, errs []ReferenceError) error {
	header := "# Generated by yaml2nt -dump-allowed-refs.\n" +
		"# Each line is one currently-known dangling-reference baseline entry.\n" +
		"# Format: <ClassIRI>\\t<ID>\n" +
		"# To add a new entry: define the cited anchor in the appropriate YAML\n" +
		"# (forbidden_fixes.yaml, required_tests.yaml, etc.) rather than expanding\n" +
		"# this baseline. Expanding the baseline is appropriate only when a\n" +
		"# reference is intentionally cite-only (e.g. an external standard).\n"
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	for _, e := range errs {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", e.Class, e.ID); err != nil {
			return err
		}
	}
	return nil
}

// validateReferencesWithPolicies is the testable core. It accepts an
// arbitrary policy list so tests can pin individual classes without
// dragging in the full default set.
func validateReferencesWithPolicies(r io.Reader, policies []referencePolicy) ([]ReferenceError, error) {
	// For each tracked class, accumulate two sets:
	//   subjects: every subject that has rdf:type = <class>
	//   defined:  every subject that has the definer predicate
	// At the end, defined is a subset of subjects in the well-formed
	// case; anything in subjects-but-not-defined is a dangling reference.
	type classState struct {
		subjects map[string]bool
		defined  map[string]bool
	}
	state := make(map[string]*classState, len(policies))
	mentionTracked := make([]referencePolicy, 0, len(policies))
	// predicateToClasses is keyed by predicate IRI. Multiple policies
	// can share the same definer (e.g. both ForbiddenFix and Test use
	// aw:authoredIn), so the value is a slice — every matching class
	// marks the subject as "defined" for itself. The intersection with
	// each class's "subjects" set produces correct per-class verdicts.
	predicateToClasses := make(map[string][]string, len(policies))
	// Bridge support: a subject of classIRI is also "defined" if a subject of
	// bridgeClassIRI with the same normalized ID exists. policyBridge maps a
	// policy's classIRI to its bridge class; bridgeIDs collects the normalized
	// IDs seen for each bridge class during the scan.
	policyBridge := make(map[string]string, len(policies))
	bridgeClassSet := make(map[string]bool, len(policies))
	bridgeIDs := make(map[string]map[string]bool, len(policies))
	for _, p := range policies {
		state[p.classIRI] = &classState{
			subjects: map[string]bool{},
			defined:  map[string]bool{},
		}
		predicateToClasses[p.definerPredicate] = append(predicateToClasses[p.definerPredicate], p.classIRI)
		if p.trackMentions {
			mentionTracked = append(mentionTracked, p)
		}
		if p.bridgeClassIRI != "" {
			policyBridge[p.classIRI] = p.bridgeClassIRI
			bridgeClassSet[p.bridgeClassIRI] = true
			if bridgeIDs[p.bridgeClassIRI] == nil {
				bridgeIDs[p.bridgeClassIRI] = map[string]bool{}
			}
		}
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	typePredicate := rdf.IRI(rdf.PropType)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasSuffix(line, ".") {
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(line, "."))
		toks := tokenize(body)
		if len(toks) != 3 {
			continue
		}
		subj, pred, obj := toks[0], toks[1], toks[2]

		for _, p := range mentionTracked {
			if matchesClassSubject(stripAngleBrackets(subj), p.classIRI) {
				state[p.classIRI].subjects[subj] = true
			}
			if strings.HasPrefix(obj, "<") && matchesClassSubject(stripAngleBrackets(obj), p.classIRI) {
				state[p.classIRI].subjects[obj] = true
			}
		}

		// rdf:type triples populate the "subjects" set when the object
		// matches a tracked class IRI. ensureNode and importForbiddenFixes
		// both emit this triple, so we only need to track distinct
		// subjects (which the map does for free).
		if pred == typePredicate {
			classIRI := stripAngleBrackets(obj)
			if cs, ok := state[classIRI]; ok {
				cs.subjects[subj] = true
			}
			// Record bridge-class subjects (e.g. ClassTestSymbol) by normalized
			// ID so a cited node of the bridged class can resolve against them.
			if bridgeClassSet[classIRI] {
				id := normalizeTestID(extractIDFromIRI(stripAngleBrackets(subj), classIRI))
				bridgeIDs[classIRI][id] = true
			}
			continue
		}

		// Definer-predicate triples populate the "defined" set. One
		// predicate may definitively-mark multiple classes (e.g.
		// aw:authoredIn is the definer for both ForbiddenFix and Test),
		// so we mark the subject for every class that uses this
		// predicate. The per-class intersection with "subjects" yields
		// the right verdict.
		predBare := stripAngleBrackets(pred)
		if classes, ok := predicateToClasses[predBare]; ok {
			for _, c := range classes {
				state[c].defined[subj] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	var errs []ReferenceError
	for classIRI, cs := range state {
		bridge := policyBridge[classIRI]
		for subj := range cs.subjects {
			if cs.defined[subj] {
				continue
			}
			id := extractIDFromIRI(stripAngleBrackets(subj), classIRI)
			// A cited node whose normalized ID matches a scanned bridge-class
			// subject (e.g. a required_test pointing at a real ClassTestSymbol)
			// is proven to exist by the scan — not dangling.
			if bridge != "" && bridgeIDs[bridge][normalizeTestID(id)] {
				continue
			}
			errs = append(errs, ReferenceError{
				Class:   classIRI,
				Subject: stripAngleBrackets(subj),
				ID:      id,
			})
		}
	}

	// Deterministic order: sort by class then by ID. Callers (yaml2nt,
	// tests) rely on stable diff output.
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Class != errs[j].Class {
			return errs[i].Class < errs[j].Class
		}
		return errs[i].ID < errs[j].ID
	})
	return errs, nil
}

func matchesClassSubject(iri, classIRI string) bool {
	hashIdx := strings.LastIndex(classIRI, "#")
	if hashIdx < 0 {
		return false
	}
	classBase := classIRI[hashIdx+1:]
	if classBase == "" {
		return false
	}
	prefix := classIRI[:hashIdx+1] + lowerFirst(classBase) + "/"
	return strings.HasPrefix(iri, prefix)
}

func stripAngleBrackets(s string) string {
	if len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>' {
		return s[1 : len(s)-1]
	}
	return s
}

// extractIDFromIRI returns the ID segment from a class-scoped IRI. The
// IRI format is the MintIRI output: <AwNS><lowercaseClass>/<encodedID>.
// We strip the namespace + class prefix and percent-decode the segment
// so the error message reads as the author wrote it.
func extractIDFromIRI(subjIRI, classIRI string) string {
	// classIRI = AwNS + "ForbiddenFix"; the matching subject prefix is
	// AwNS + "forbiddenFix/" — same lowerFirst transform MintIRI uses.
	hashIdx := strings.LastIndex(classIRI, "#")
	if hashIdx < 0 {
		return subjIRI
	}
	classBase := classIRI[hashIdx+1:]
	if classBase == "" {
		return subjIRI
	}
	prefix := classIRI[:hashIdx+1] + lowerFirst(classBase) + "/"
	if id, ok := strings.CutPrefix(subjIRI, prefix); ok {
		return decodeIRIPath(id)
	}
	return subjIRI
}

// classBaseName returns the trailing class segment for use in error
// messages (e.g. AwNS+"ForbiddenFix" → "ForbiddenFix").
func classBaseName(classIRI string) string {
	if i := strings.LastIndex(classIRI, "#"); i >= 0 {
		return classIRI[i+1:]
	}
	return classIRI
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'A' && c <= 'Z' {
		return string(c+32) + s[1:]
	}
	return s
}

// decodeIRIPath inverts rdf.EncodeIRIPath. We re-implement here rather
// than depending on rdf so the package boundary stays clean (rdf has no
// other reverse-encoder).
func decodeIRIPath(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := fromHex(s[i+1])
			lo := fromHex(s[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func fromHex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}
