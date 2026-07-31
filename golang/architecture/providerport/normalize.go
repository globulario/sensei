// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import "github.com/globulario/sensei/golang/architecture/synthesis"

// NormalizeCapabilities returns a copy of c with every slice field non-nil.
func NormalizeCapabilities(c Capabilities) Capabilities {
	c.SupportedOperations = normalizeOperations(c.SupportedOperations)
	return c
}

// NormalizeRequest returns a copy of r with its populated payload field
// (if any) normalized via the corresponding golang/architecture/synthesis
// normalizer, so nested slice fields inside an embedded O1 artifact are
// non-nil too.
func NormalizeRequest(r Request) Request {
	if r.InterpretationPayload != nil {
		norm := synthesis.NormalizeSession(*r.InterpretationPayload)
		r.InterpretationPayload = &norm
	}
	if r.PlanningPayload != nil {
		norm := synthesis.NormalizeInterpretation(*r.PlanningPayload)
		r.PlanningPayload = &norm
	}
	if r.GenerationPayload != nil {
		norm := synthesis.NormalizePlan(*r.GenerationPayload)
		r.GenerationPayload = &norm
	}
	if r.EvaluationObservationPayload != nil {
		norm := synthesis.NormalizeAttempt(*r.EvaluationObservationPayload)
		r.EvaluationObservationPayload = &norm
	}
	return r
}

// NormalizeResult returns a copy of r with its populated payload field (if
// any) normalized via the corresponding golang/architecture/synthesis
// normalizer.
func NormalizeResult(r Result) Result {
	if r.InterpretationPayload != nil {
		norm := synthesis.NormalizeInterpretation(*r.InterpretationPayload)
		r.InterpretationPayload = &norm
	}
	if r.PlanningPayload != nil {
		norm := synthesis.NormalizePlan(*r.PlanningPayload)
		r.PlanningPayload = &norm
	}
	if r.GenerationPayload != nil {
		norm := synthesis.NormalizeAttempt(*r.GenerationPayload)
		r.GenerationPayload = &norm
	}
	if r.EvaluationObservationPayload != nil {
		norm := synthesis.NormalizeEvaluation(*r.EvaluationObservationPayload)
		r.EvaluationObservationPayload = &norm
	}
	return r
}

// NormalizeObservationBatch returns a copy of b with every slice field
// non-nil.
func NormalizeObservationBatch(b ObservationBatch) ObservationBatch {
	if b.Observations == nil {
		b.Observations = []Observation{}
	}
	return b
}

// NormalizeReceipt returns a copy of r with every slice field non-nil.
func NormalizeReceipt(r Receipt) Receipt {
	if r.Limitations == nil {
		r.Limitations = []synthesis.Limitation{}
	}
	return r
}

func normalizeOperations(in []Operation) []Operation {
	if in == nil {
		return []Operation{}
	}
	return in
}
