// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/extractor/importgraph"
)

// boundaryHubThreshold is the minimum number of distinct consumers a component
// needs before it is flagged as a dependency-hub (stability) boundary. Kept
// conservative so ordinary utility packages don't flood the candidate queue.
const boundaryHubThreshold = 3

// boundary_extract.go infers conservative Boundary CANDIDATES from the import
// graph — the "where architecture can be crossed" layer that bootstrap otherwise
// left unimplemented. It emits status: candidate only; nothing is promoted.
//
// Two grounded, deterministic signals (no LLM, no key):
//
//  1. internal/ visibility boundary — a package under a Go `internal/` directory
//     is a COMPILER-ENFORCED boundary: importable only within its parent
//     subtree. That is language truth, not a guess, and it exists in every Go
//     repo that uses internal/.
//  2. contract-exposure boundary — a component that exposes a contract consumed
//     by other components is an API seam. Available when the import classifier
//     populated exposes_contracts.
//
// Both are conservative: a boundary is emitted only from an observed structural
// fact, never from a name or a hunch. assertion: inferred throughout.

type boundaryCandidate struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	Kind             string   `yaml:"kind"`
	Status           string   `yaml:"status"`
	Assertion        string   `yaml:"assertion"`
	Description      string   `yaml:"description"`
	Separates        []string `yaml:"separates,omitempty"`
	ExposesContracts []string `yaml:"exposes_contracts,omitempty"`
	Forbids          []string `yaml:"forbids,omitempty"`
	SourceFiles      []string `yaml:"source_files,omitempty"`
}

type boundaryCandidateDoc struct {
	Boundaries []boundaryCandidate `yaml:"boundaries"`
}

// extractBoundaryCandidates infers boundary candidates from already-scanned
// import-graph components. Passing the components in (rather than re-scanning)
// keeps it cheap and uses the same classifier config bootstrap already loaded.
func extractBoundaryCandidates(comps []importgraph.Component) []boundaryCandidate {
	// Reverse dependency map: component id -> the components that depend on it.
	consumers := map[string][]string{}
	for _, c := range comps {
		for _, dep := range c.DependsOn {
			consumers[dep] = append(consumers[dep], c.ID)
		}
	}

	seen := map[string]bool{}
	var out []boundaryCandidate
	add := func(b boundaryCandidate) {
		if b.ID == "" || seen[b.ID] {
			return
		}
		seen[b.ID] = true
		out = append(out, b)
	}

	for _, c := range comps {
		// 1) internal/ visibility boundary — compiler-enforced.
		if parent, ok := internalParent(c.SourceFiles); ok {
			desc, forbid := "", ""
			if parent == "" { // root-level internal/: module-private
				desc = "Go internal/ package: module-private (importable anywhere in this module, never by another module); a compiler-enforced visibility boundary between the module's public API and its internals."
				forbid = "import " + c.ID + " from outside this module"
			} else {
				desc = "Go internal/ package: importable only within " + parent + "/; a compiler-enforced visibility boundary."
				forbid = "import " + c.ID + " from outside " + parent + "/"
			}
			add(boundaryCandidate{
				ID:          "boundary.visibility." + boundarySlug(c.ID),
				Name:        c.Name + " internal boundary",
				Kind:        "visibility",
				Status:      "candidate",
				Assertion:   "inferred",
				Description: desc,
				Separates:   []string{c.ID},
				Forbids:     []string{forbid},
				SourceFiles: c.SourceFiles,
			})
		}
		// 2) contract-exposure boundary — an API seam.
		if len(c.ExposesContracts) > 0 {
			sep := append([]string{c.ID}, dedupSorted(consumers[c.ID])...)
			add(boundaryCandidate{
				ID:               "boundary.api." + boundarySlug(c.ID),
				Name:             c.Name + " API boundary",
				Kind:             "api",
				Status:           "candidate",
				Assertion:        "inferred",
				Description:      c.Name + " exposes contracts consumed across component lines.",
				Separates:        sep,
				ExposesContracts: dedupSorted(c.ExposesContracts),
				SourceFiles:      c.SourceFiles,
			})
		}
		// 3) dependency-hub boundary — a component many others depend on is a
		// stability seam: a change to it ripples across all consumers. Only the
		// contract-exposure signal is stronger; this catches the shared-kernel
		// case in repos without internal/ or a classifier. Conservative threshold.
		if cons := dedupSorted(consumers[c.ID]); len(cons) >= boundaryHubThreshold {
			add(boundaryCandidate{
				ID:          "boundary.hub." + boundarySlug(c.ID),
				Name:        c.Name + " dependency hub",
				Kind:        "stability",
				Status:      "candidate",
				Assertion:   "inferred",
				Description: c.Name + " is depended on by " + strconv.Itoa(len(cons)) + " components; a change here ripples across all of them (stability boundary).",
				Separates:   append([]string{c.ID}, cons...),
				SourceFiles: c.SourceFiles,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// internalParent reports whether any source file is under a Go `internal/`
// directory, and returns the visibility parent — the path of the directory that
// CONTAINS `internal/`. For a root-level `internal/` the parent is "" (the
// module root: module-private). ok is false when no file is under an internal/
// tree.
func internalParent(sourceFiles []string) (parent string, ok bool) {
	for _, f := range sourceFiles {
		parts := strings.Split(filepath.ToSlash(f), "/")
		for i, p := range parts {
			if p == "internal" {
				return strings.Join(parts[:i], "/"), true // "" when i == 0
			}
		}
	}
	return "", false
}

func boundarySlug(componentID string) string {
	s := strings.TrimPrefix(componentID, "component.")
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteRune('.')
		}
	}
	return strings.Trim(b.String(), ".")
}

func dedupSorted(in []string) []string {
	m := map[string]bool{}
	for _, s := range in {
		if s != "" {
			m[s] = true
		}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
