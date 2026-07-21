// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

// dimensionPolicy is one reviewed per-class dimension rule. The satisfied/open/unknown rules are
// applied by assessDimension over a TYPED owner observation — a graph edge is candidate evidence
// only; the owner's typed Satisfied/Blocker signal makes it admissible, never edge presence.
type dimensionPolicy struct {
	Dimension  string
	Label      string
	Required   bool
	Owner      string // typed source owner for this dimension
	NextAction string // next-action owner
}

// assessmentPolicy is a reviewed per-class artifact-assessment policy.
type assessmentPolicy struct {
	ID         string
	ClassIRI   string
	Dimensions []dimensionPolicy
}

// assessmentPolicies returns the immutable v1 policies for the four assessable classes. Each
// dimension names the typed source owner that makes its evidence admissible.
func assessmentPolicies() map[string]assessmentPolicy {
	d := func(dim, label string, required bool, owner, next string) dimensionPolicy {
		return dimensionPolicy{Dimension: dim, Label: label, Required: required, Owner: owner, NextAction: next}
	}
	return map[string]assessmentPolicy{
		"contract.v1": {ID: "contract.v1", ClassIRI: "https://globular.io/awareness#Contract", Dimensions: []dimensionPolicy{
			d("definition", "Definition", true, "closure.definition", "architect"),
			d("ownership", "Ownership", true, "closure.ownership", "architect"),
			d("scope", "Scope", true, "closure.scope", "architect"),
			d("realization", "Realization", true, "closure.realization", "architect"),
			d("enforcement", "Enforcement", true, "closure.enforcement", "architect"),
			d("verification", "Verification", true, "closure.verification", "architect"),
			d("evidence", "Evidence", true, "closure.evidence", "architect"),
			d("contradiction", "Contradiction", true, "extractor.contradiction", "architect"),
		}},
		"invariant.v1": {ID: "invariant.v1", ClassIRI: "https://globular.io/awareness#Invariant", Dimensions: []dimensionPolicy{
			d("definition", "Definition", true, "closure.definition", "architect"),
			d("scope", "Scope", true, "closure.scope", "architect"),
			d("authority", "Authority", true, "closure.authority", "architect"),
			d("enforcement", "Enforcement", true, "closure.enforcement", "architect"),
			d("verification", "Verification", true, "closure.verification", "architect"),
			d("evidence", "Evidence", true, "closure.evidence", "architect"),
			d("contradiction", "Contradiction", true, "extractor.contradiction", "architect"),
		}},
		"component.v1": {ID: "component.v1", ClassIRI: "https://globular.io/awareness#Component", Dimensions: []dimensionPolicy{
			d("definition", "Definition", true, "closure.definition", "architect"),
			d("ownership", "Ownership", true, "closure.ownership", "architect"),
			d("architectural_boundary", "Architectural boundary", true, "closure.boundary", "architect"),
			d("contract_surface", "Contract surface", true, "closure.contract_surface", "architect"),
			d("realization", "Realization", true, "closure.realization", "architect"),
			d("evidence", "Evidence", true, "closure.evidence", "architect"),
			d("contradiction", "Contradiction", true, "extractor.contradiction", "architect"),
		}},
		"boundary.v1": {ID: "boundary.v1", ClassIRI: "https://globular.io/awareness#Boundary", Dimensions: []dimensionPolicy{
			d("definition", "Definition", true, "closure.definition", "architect"),
			d("ownership", "Ownership", true, "closure.ownership", "architect"),
			d("protected_contract_or_invariant", "Protected contract/invariant", true, "closure.protected", "architect"),
			d("enforcement", "Enforcement", true, "closure.enforcement", "architect"),
			d("verification", "Verification", true, "closure.verification", "architect"),
			d("evidence", "Evidence", true, "closure.evidence", "architect"),
			d("contradiction", "Contradiction", true, "extractor.contradiction", "architect"),
		}},
	}
}
