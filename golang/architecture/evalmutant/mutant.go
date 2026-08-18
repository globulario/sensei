// SPDX-License-Identifier: AGPL-3.0-only

// Package evalmutant builds deterministic repositories that each carry exactly
// one bounded architectural defect, for evaluating what an investigator can and
// cannot recover.
//
// It is the mutant suite issue #131 section 4 requires. Every defect class it
// enumerates is represented here; none is invented, because a mutant suite is
// a measuring instrument and an instrument that measures something nobody asked
// for reports a number nobody can use.
//
// # Why each mutant carries a witness
//
// The failure mode of a mutant suite is not a mutant that is too hard to find.
// It is a mutant that does not actually contain its defect: the evaluator
// "fails to detect" nothing at all, or "detects" a defect that was never
// introduced, and the score is meaningless in a way no amount of careful
// reporting reveals. That is the green-test-that-is-itself-the-bug shape.
//
// So every Mutant ships a Witness: an independent predicate over the
// materialized tree that answers "is this defect present?" without consulting
// the builder that produced it. The suite's own tests then require, for every
// defect, that the witness finds it in the mutant and does NOT find it in the
// clean baseline. A mutant that cannot demonstrate its own defect is not
// evidence about an evaluator; it is noise wearing a label.
//
// # What this package does not do
//
// It does not evaluate. It has no opinion on whether an investigator SHOULD
// detect any given defect, and it records no expected verdict, because
// "detectable" depends on evidence the evaluator is given and is exactly the
// thing under measurement. This package's whole contract is: here is a tree,
// here is precisely what is wrong with it, and here is an independent way to
// confirm that.
package evalmutant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Defect names one bounded architectural fault. The set is closed and matches
// issue #131 section 4 exactly.
type Defect string

const (
	// DefectAuthoritySplit: two owners independently compute the same
	// decision, so the system has two answers and no way to choose.
	DefectAuthoritySplit Defect = "authority_split"
	// DefectForbiddenDependencyDirection: a lower layer imports an upper one,
	// inverting the intended dependency direction.
	DefectForbiddenDependencyDirection Defect = "forbidden_dependency_direction"
	// DefectStaleDocumentation: prose states a rule the code contradicts.
	DefectStaleDocumentation Defect = "stale_documentation"
	// DefectImplementationRepetition: the same small routine appears three
	// times. Repetition is a maintenance fact, NOT an architectural law, and an
	// investigator that promotes it to one has fabricated structure.
	DefectImplementationRepetition Defect = "implementation_repetition"
	// DefectMissingEvidenceProvider: a claim cites evidence that does not exist.
	DefectMissingEvidenceProvider Defect = "missing_evidence_provider"
	// DefectMisleadingCommitMessage: the message describes work the diff does
	// not contain.
	DefectMisleadingCommitMessage Defect = "misleading_commit_message"
	// DefectGeneratedFileContamination: a generated file is edited by hand, so
	// the next regeneration silently discards the edit.
	DefectGeneratedFileContamination Defect = "generated_file_contamination"
	// DefectCrossDomainLeakage: one domain reaches directly into another's
	// internals rather than through its published surface.
	DefectCrossDomainLeakage Defect = "cross_domain_symbol_leakage"
	// DefectIncompleteProofDischarge: an obligation is marked discharged while
	// its proof is absent or does not cover it.
	DefectIncompleteProofDischarge Defect = "incomplete_proof_discharge"
)

// Defects lists every defect class, in stable order.
func Defects() []Defect {
	return []Defect{
		DefectAuthoritySplit,
		DefectForbiddenDependencyDirection,
		DefectStaleDocumentation,
		DefectImplementationRepetition,
		DefectMissingEvidenceProvider,
		DefectMisleadingCommitMessage,
		DefectGeneratedFileContamination,
		DefectCrossDomainLeakage,
		DefectIncompleteProofDischarge,
	}
}

// Mutant is one repository carrying exactly one defect.
type Mutant struct {
	Defect Defect
	// Statement is what is wrong, in the terms an architect would use. It is
	// the reference answer a report compares against — deliberately prose, so
	// a reader can judge whether a detection actually matched the defect or
	// merely landed near it.
	Statement string
	// Files is the complete tree: baseline plus this defect's changes.
	Files map[string]string
	// CommitMessage is what the mutating commit claims to do. It matters only
	// for the misleading-commit-message defect, but every mutant carries one so
	// history-based evidence is uniform across the suite.
	CommitMessage string
	// TouchedPaths are the paths this defect changed relative to the baseline,
	// sorted. A grader that needs to know where the defect lives should read
	// this rather than diffing, so the answer is stated rather than derived.
	TouchedPaths []string
}

// Witness reports whether a defect is present in a materialized tree.
//
// It deliberately re-reads the tree rather than inspecting the Mutant it came
// from: a witness that trusted the builder would confirm the builder's
// intention, not the tree's content, and the two diverge exactly when a build
// bug is what needs catching.
type Witness func(root string) (present bool, detail string, err error)

// Build returns the mutant for one defect. The bytes are deterministic: the
// same defect always produces the same tree, so a suite committed today and run
// in six months measures the same thing.
func Build(d Defect) (Mutant, error) {
	files := baselineFiles()
	m := Mutant{Defect: d, Files: files}
	mutate, ok := mutators[d]
	if !ok {
		return Mutant{}, fmt.Errorf("evalmutant: unknown defect %q", d)
	}
	before := snapshot(files)
	m.Statement, m.CommitMessage = mutate(files)
	m.TouchedPaths = changedPaths(before, files)
	if len(m.TouchedPaths) == 0 {
		// A mutator that changed nothing would yield a "mutant" identical to
		// the baseline, which any evaluator passes for free.
		return Mutant{}, fmt.Errorf("evalmutant: %s changed no file; it would be indistinguishable from the baseline", d)
	}
	return m, nil
}

// Baseline is the clean control tree. It must contain no defect, and the
// suite's tests assert that against every witness — a baseline that already
// carried a defect would make detection look free.
func Baseline() Mutant {
	files := baselineFiles()
	return Mutant{
		Defect:        "",
		Statement:     "clean control: no defect introduced",
		Files:         files,
		CommitMessage: "baseline: the control tree",
	}
}

// Materialize writes a tree to disk. It refuses a non-empty directory rather
// than merging into whatever was there, since a mutant blended with unrelated
// content is no longer the bounded defect it claims to be.
func Materialize(root string, m Mutant) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
			return mkErr
		}
	} else if len(entries) > 0 {
		return fmt.Errorf("evalmutant: %s is not empty; a mutant must be materialized into a clean directory or it is no longer a bounded defect", root)
	}
	paths := make([]string, 0, len(m.Files))
	for p := range m.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(m.Files[p]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// WitnessFor returns the independent presence check for a defect.
func WitnessFor(d Defect) (Witness, error) {
	w, ok := witnesses[d]
	if !ok {
		return nil, fmt.Errorf("evalmutant: no witness for defect %q", d)
	}
	return w, nil
}

func snapshot(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for k, v := range files {
		out[k] = v
	}
	return out
}

func changedPaths(before, after map[string]string) []string {
	var out []string
	for p, v := range after {
		if prior, existed := before[p]; !existed || prior != v {
			out = append(out, p)
		}
	}
	for p := range before {
		if _, still := after[p]; !still {
			out = append(out, p+" (removed)")
		}
	}
	sort.Strings(out)
	return out
}

// readFile is the witnesses' only view of a tree.
func readFile(root, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
