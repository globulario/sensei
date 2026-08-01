// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import "github.com/globulario/sensei/golang/architecture/synthesis"

// NormalizeEvaluationPolicy returns a copy of p with every slice field
// defaulted to a non-nil empty slice, so two semantically identical
// policies always encode identically before digesting and no slice field
// marshals to JSON null where the schema requires an array. Evaluators and
// FailureClassRecommendations are caller-authored order (the design doc
// calls the former "ordered evaluator specifications") and are not
// reordered here.
func NormalizeEvaluationPolicy(p EvaluationPolicy) EvaluationPolicy {
	if p.Evaluators == nil {
		p.Evaluators = []EvaluatorSpec{}
	}
	if p.RequiredCheckIDs == nil {
		p.RequiredCheckIDs = []string{}
	}
	if p.FailureClassRecommendations == nil {
		p.FailureClassRecommendations = []FailureClassRecommendation{}
	}
	return p
}

// NormalizeEvaluatorDescriptor returns a copy of d with every slice field
// defaulted to a non-nil empty slice.
func NormalizeEvaluatorDescriptor(d EvaluatorDescriptor) EvaluatorDescriptor {
	if d.SupportedCheckIDs == nil {
		d.SupportedCheckIDs = []string{}
	}
	if d.RequiredCapabilities == nil {
		d.RequiredCapabilities = []string{}
	}
	if d.Limitations == nil {
		d.Limitations = []synthesis.Limitation{}
	}
	return d
}

// NormalizeEvaluationInput returns a copy of i with its slice field
// defaulted to a non-nil empty slice.
func NormalizeEvaluationInput(i EvaluationInput) EvaluationInput {
	if i.RequiredProofObligationDigests == nil {
		i.RequiredProofObligationDigests = []string{}
	}
	return i
}

// NormalizeEvaluatorResult returns a copy of r with every slice field
// defaulted to a non-nil empty slice. Checks/EvidenceReferences/
// ClassifiedFailureReasons/Limitations are composition-owned order
// (checkpoint 5); this checkpoint does not reorder them.
func NormalizeEvaluatorResult(r EvaluatorResult) EvaluatorResult {
	if r.Checks == nil {
		r.Checks = []synthesis.CheckObservation{}
	}
	if r.EvidenceReferences == nil {
		r.EvidenceReferences = []EvidenceReference{}
	}
	if r.ClassifiedFailureReasons == nil {
		r.ClassifiedFailureReasons = []string{}
	}
	if r.Limitations == nil {
		r.Limitations = []synthesis.Limitation{}
	}
	return r
}

// NormalizeEvaluationReceipt returns a copy of r with its slice field
// defaulted to a non-nil empty slice. It deliberately does NOT sort
// EvaluatorResultBindings into ascending EvaluatorID order -- canonical
// ordering here is a validation requirement (ValidateEvaluationReceipt
// rejects an out-of-order or duplicate-EvaluatorID document outright), not
// a normalization side effect that would silently launder a wrongly
// ordered document into a passing digest.
func NormalizeEvaluationReceipt(r EvaluationReceipt) EvaluationReceipt {
	if r.EvaluatorResultBindings == nil {
		r.EvaluatorResultBindings = []EvaluatorResultBinding{}
	}
	return r
}
