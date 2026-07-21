// SPDX-License-Identifier: AGPL-3.0-only

// Package rigor is the Phase 9 polish (issue #93) proportional-rigor classifier: it answers "what
// proof obligations does a proposed CHANGE owe" — never "what is repository truth". It classifies
// GOVERNED SURFACES (stable named entities), then binds changed files to surfaces through explicit
// ownership records; it never assigns a rigor class to a raw filename.
//
// Laws (all fail-closed):
//   - Effective rigor over a change is the STRICTEST class among all touched governed surfaces.
//   - A file that matches no governed surface is UNCLASSIFIED and forces Class A (the strictest).
//   - An optional DECLARED class can only RAISE strictness; it can never downgrade contact with an
//     A/B-owned surface (the changed-file set, the ownership map, and the declared class must agree).
//   - Class D means lighter SEMANTIC proof only — every class, including D, keeps the repository-
//     integrity obligations (ownership, determinism, licensing, generated-artifact, build).
//
// The package is pure: stdlib + yaml only. It reads no filesystem and enforces nothing — it returns
// the obligations that APPLY; the existing guards/CI enforce them. The CLI supplies file bytes.
package rigor

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Class is the CLOSED proportional-rigor vocabulary, strictest→lightest. The zero value is
// ClassUnknown, which always resolves to the strictest applicable class (fail closed).
type Class string

const (
	ClassUnknown Class = ""  // zero value: unclassified → fails closed to Class A
	ClassA       Class = "A" // semantic owner, authority, certification, identity
	ClassB       Class = "B" // evidence ingestion, admission, binding
	ClassC       Class = "C" // projection, transport, rendering
	ClassD       Class = "D" // cosmetic / explanatory local UI
)

// rank orders classes by strictness (0 = strictest). ClassUnknown ranks as strict as A so an
// unclassified surface can never be treated as lighter than the strictest applicable class.
func rank(c Class) int {
	switch c {
	case ClassA, ClassUnknown:
		return 0
	case ClassB:
		return 1
	case ClassC:
		return 2
	case ClassD:
		return 3
	}
	return 0 // any off-vocabulary value fails closed to strictest
}

func validClass(c Class) bool {
	switch c {
	case ClassA, ClassB, ClassC, ClassD:
		return true
	}
	return false
}

// resolveUnknown maps the unclassified/zero class to the strictest applicable class.
func resolveUnknown(c Class) Class {
	if !validClass(c) {
		return ClassA
	}
	return c
}

// stricter returns the stricter of two classes (lower rank wins; unknown → A).
func stricter(a, b Class) Class {
	if rank(a) <= rank(b) {
		return resolveUnknown(a)
	}
	return resolveUnknown(b)
}

// Surface is one stable governed surface: a named architectural entity, its rigor class, and the
// package/path prefixes it owns. Classification keys on the SURFACE; files inherit its class.
type Surface struct {
	ID       string   `yaml:"id"`
	Class    Class    `yaml:"class"`
	Packages []string `yaml:"packages"`
}

// Manifest is the explicit, reviewable source of truth for surface→class.
type Manifest struct {
	Surfaces []Surface `yaml:"surfaces"`
}

// ParseManifest parses and validates the manifest bytes. Every surface must have an id, a valid
// class, and at least one owned package prefix; ids and package prefixes must be unique so a file
// can never map ambiguously.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("rigor manifest parse: %w", err)
	}
	seenID := map[string]bool{}
	seenPkg := map[string]string{}
	for _, s := range m.Surfaces {
		if strings.TrimSpace(s.ID) == "" {
			return Manifest{}, fmt.Errorf("rigor surface missing id")
		}
		if !validClass(s.Class) {
			return Manifest{}, fmt.Errorf("rigor surface %q has off-vocabulary class %q", s.ID, s.Class)
		}
		if len(s.Packages) == 0 {
			return Manifest{}, fmt.Errorf("rigor surface %q owns no packages (a surface must bind to code)", s.ID)
		}
		if seenID[s.ID] {
			return Manifest{}, fmt.Errorf("duplicate rigor surface id %q", s.ID)
		}
		seenID[s.ID] = true
		for _, p := range s.Packages {
			if p == "" || p != strings.TrimSpace(p) {
				return Manifest{}, fmt.Errorf("rigor surface %q has an empty/padded package prefix", s.ID)
			}
			if other, ok := seenPkg[p]; ok {
				return Manifest{}, fmt.Errorf("package prefix %q is claimed by two surfaces (%q and %q)", p, other, s.ID)
			}
			seenPkg[p] = s.ID
		}
	}
	return m, nil
}

// matchingSurfaces returns EVERY governed surface whose package prefix owns a changed file. When
// prefixes overlap (a broad and a narrow surface both match), all matches are returned so the caller
// can take the STRICTEST — a narrower but weaker surface can therefore never trick classification
// into a lighter class than a broader stricter surface that also owns the file.
func (m Manifest) matchingSurfaces(file string) []Surface {
	file = strings.TrimSpace(file)
	var out []Surface
	for _, s := range m.Surfaces {
		for _, p := range s.Packages {
			if file == p || strings.HasPrefix(file, strings.TrimSuffix(p, "/")+"/") {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// ObligationKind is the CLOSED proof-obligation vocabulary a class may owe. The first five are
// repository-integrity gates owed by EVERY class (including D); the rest are the graded semantic
// proofs. They name obligations the existing guards/CI already enforce — this package never
// re-implements those checks.
type ObligationKind string

const (
	ObOwnershipGuard     ObligationKind = "ownership_guard"
	ObDeterminism        ObligationKind = "determinism"
	ObLicensing          ObligationKind = "licensing"
	ObGeneratedArtifact  ObligationKind = "generated_artifact"
	ObBuild              ObligationKind = "build"
	ObUIRenderTest       ObligationKind = "ui_render_test"
	ObTransportRoundTrip ObligationKind = "transport_roundtrip"
	ObEvidenceIntegrity  ObligationKind = "evidence_receipt_integrity"
	ObOwnerVerdictProof  ObligationKind = "owner_verdict_proof"
	ObCertificationProof ObligationKind = "certification_proof"
)

// integrityGates are owed by every class — Class D lightens SEMANTIC proof, never repository integrity.
var integrityGates = []ObligationKind{ObOwnershipGuard, ObDeterminism, ObLicensing, ObGeneratedArtifact, ObBuild}

// ObligationsFor returns the obligations a class owes: the integrity gates (always) plus the graded
// semantic proofs for that class. An unclassified class fails closed to Class A's obligations.
func ObligationsFor(c Class) []ObligationKind {
	out := append([]ObligationKind(nil), integrityGates...)
	switch resolveUnknown(c) {
	case ClassA:
		out = append(out, ObTransportRoundTrip, ObEvidenceIntegrity, ObOwnerVerdictProof, ObCertificationProof)
	case ClassB:
		out = append(out, ObTransportRoundTrip, ObEvidenceIntegrity)
	case ClassC:
		out = append(out, ObTransportRoundTrip)
	case ClassD:
		out = append(out, ObUIRenderTest)
	}
	return out
}

// Decision is the resolved rigor outcome for a change.
type Decision struct {
	Effective       Class
	Declared        Class
	DeclaredHonored bool // false when a weaker declared class was ignored (never downgrades)
	TouchedSurfaces []string
	Unclassified    []string // changed files owned by no surface — each forces Class A
	Obligations     []ObligationKind
	Reasons         []string
}

// classLabel is the human name for a class (presentation only; the Class value is the identity).
func classLabel(c Class) string {
	switch resolveUnknown(c) {
	case ClassA:
		return "A — semantic owner / authority / certification / identity"
	case ClassB:
		return "B — evidence ingestion / admission / binding"
	case ClassC:
		return "C — projection / transport / rendering"
	case ClassD:
		return "D — cosmetic / explanatory local UI"
	}
	return "A — (fail closed)"
}

// FormatDecision renders a Decision as a deterministic, human-readable change-review report. It is
// presentation only — the Effective class and Obligations are the authority.
func FormatDecision(d Decision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Proportional rigor: Class %s\n", classLabel(d.Effective))
	if len(d.TouchedSurfaces) > 0 {
		fmt.Fprintf(&b, "Touched surfaces: %s\n", strings.Join(d.TouchedSurfaces, ", "))
	}
	if len(d.Unclassified) > 0 {
		fmt.Fprintf(&b, "Unclassified (forced to Class A): %s\n", strings.Join(d.Unclassified, ", "))
	}
	if d.Declared != "" {
		honored := "honored"
		if !d.DeclaredHonored {
			honored = "IGNORED (a declaration can never downgrade)"
		}
		fmt.Fprintf(&b, "Declared class %s: %s\n", string(d.Declared), honored)
	}
	b.WriteString("Proof obligations:\n")
	for _, o := range d.Obligations {
		integrity := ""
		if isIntegrityGate(o) {
			integrity = "  [repository integrity — owed by every class, including D]"
		}
		fmt.Fprintf(&b, "  - %s%s\n", string(o), integrity)
	}
	for _, r := range d.Reasons {
		fmt.Fprintf(&b, "note: %s\n", r)
	}
	return b.String()
}

func isIntegrityGate(o ObligationKind) bool {
	for _, g := range integrityGates {
		if g == o {
			return true
		}
	}
	return false
}

// ClassifyChange resolves the effective rigor class for a set of changed files and an optional
// declared class. Effective = strictest touched surface; an unclassified file forces Class A; the
// declared class can only raise strictness (a weaker declaration is recorded but ignored).
func ClassifyChange(m Manifest, changedFiles []string, declared Class) Decision {
	d := Decision{Declared: declared}
	var effective Class
	set := false
	touched := map[string]Class{}
	anyFile := false
	// accumulate takes the STRICTEST of the classes touched so far (seeded by the first).
	accumulate := func(c Class) {
		if !set {
			effective, set = resolveUnknown(c), true
			return
		}
		effective = stricter(effective, c)
	}
	for _, f := range changedFiles {
		if strings.TrimSpace(f) == "" {
			continue
		}
		anyFile = true
		ms := m.matchingSurfaces(f)
		if len(ms) == 0 {
			d.Unclassified = append(d.Unclassified, f)
			accumulate(ClassA) // unclassified fails closed to A
			continue
		}
		// A file owes the STRICTEST of every surface that owns it — overlapping prefixes can only
		// add strictness, never remove it (no weaker-surface trick).
		fileClass := ms[0].Class
		for _, s := range ms {
			touched[s.ID] = s.Class
			fileClass = stricter(fileClass, s.Class)
		}
		accumulate(fileClass)
	}
	if !anyFile {
		// No changed files → nothing to owe beyond the strictest default (fail closed).
		d.Effective = ClassA
		d.Reasons = append(d.Reasons, "no changed files supplied — defaulting to the strictest class")
		d.Obligations = ObligationsFor(d.Effective)
		return d
	}
	// A declared class can only RAISE strictness. A weaker declaration is ignored (never downgrades
	// contact with an A/B surface); this is where refinement 9 is enforced structurally.
	computed := resolveUnknown(effective)
	d.Effective = computed
	if validClass(declared) {
		if rank(declared) < rank(computed) {
			d.Effective = declared
			d.DeclaredHonored = true
			d.Reasons = append(d.Reasons, "declared class "+string(declared)+" raised rigor above the computed "+string(computed))
		} else if rank(declared) > rank(computed) {
			d.DeclaredHonored = false
			d.Reasons = append(d.Reasons, "declared class "+string(declared)+" is weaker than the computed "+string(computed)+" — ignored (a declaration can never downgrade)")
		} else {
			d.DeclaredHonored = true
		}
	}
	for id := range touched {
		d.TouchedSurfaces = append(d.TouchedSurfaces, id)
	}
	sort.Strings(d.TouchedSurfaces)
	sort.Strings(d.Unclassified)
	if len(d.Unclassified) > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("%d changed file(s) match no governed surface — forced to Class A", len(d.Unclassified)))
	}
	d.Obligations = ObligationsFor(d.Effective)
	return d
}
