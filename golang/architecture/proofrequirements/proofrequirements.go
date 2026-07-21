// SPDX-License-Identifier: AGPL-3.0-only

// Package proofrequirements is the deterministic, reusable core of Sensei's
// proof-obligation and proof-plan generation, lifted out of the cmd/awg CLI so
// the `sensei extract-proof-obligations` / `proof-plan` commands and the Phase 7
// result pipeline drive one implementation.
package proofrequirements

import "github.com/globulario/sensei/golang/architecture/factextract"

// authoritySurfaceCandidate keeps the moved code's original identifier bound to
// the shared authority-surface type that now lives in factextract.
type authoritySurfaceCandidate = factextract.AuthoritySurface

// Exported facade — aliases onto the package's internal types and thin wrappers
// over its internal functions, so the generation logic keeps its original
// identifiers and behavior byte-for-byte.

// ObligationsDoc is a generated proof-obligations document.
type ObligationsDoc = proofObligationsDoc

// Obligation is one generated proof obligation.
type Obligation = generatedProofObligation

// Slot is one generated proof slot.
type Slot = generatedProofSlot

// Template is a proof template for an authority surface.
type Template = proofTemplate

// AuthoritySurface is the shared authority-surface candidate type.
type AuthoritySurface = factextract.AuthoritySurface

// BuildObligations generates proof obligations from authority surfaces.
func BuildObligations(surfaces []AuthoritySurface) ObligationsDoc {
	return buildProofObligations(surfaces)
}

// TemplateForAuthoritySurface maps one authority surface to its proof template.
func TemplateForAuthoritySurface(surface AuthoritySurface) Template {
	return templateForAuthoritySurface(surface)
}

// LoadAuthoritySurfaces loads authority surfaces from a candidate YAML path.
func LoadAuthoritySurfaces(path string) ([]AuthoritySurface, error) {
	return loadAuthoritySurfaces(path)
}

// RenderObligations renders a proof-obligations document.
func RenderObligations(doc ObligationsDoc) ([]byte, error) {
	return renderProofObligations(doc)
}

// RenderObligationSummary renders the human summary line for an obligations doc.
func RenderObligationSummary(doc ObligationsDoc, target string, check bool) string {
	return renderProofObligationSummary(doc, target, check)
}
