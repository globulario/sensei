// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

import "slices"

func EvaluateClosure(dimensions []DimensionResult, policy CompletionPolicy) ClosureEvaluation {
	normalized := normalizeDimensionResults(dimensions)
	allowedNA := map[Dimension]bool{}
	for _, dim := range policy.PermittedNotApplicable {
		allowedNA[dim] = true
	}
	allowedWaivers := map[Dimension]bool{}
	for _, dim := range policy.AllowedWaiverDimensions {
		allowedWaivers[dim] = true
	}

	required := map[Dimension]bool{}
	for _, dim := range Dimensions {
		required[dim] = true
	}
	for _, dim := range []Dimension{
		DimensionIdentity, DimensionScope, DimensionAuthority, DimensionMutation,
		DimensionEpistemic, DimensionFreshness, DimensionCompletion,
	} {
		allowedNA[dim] = false
	}

	eval := ClosureEvaluation{}
	seen := map[Dimension]bool{}
	for _, result := range normalized {
		seen[result.Dimension] = true
		switch result.Status {
		case DimensionPass:
		case DimensionPassWithException:
			if !allowedWaivers[result.Dimension] || result.ExceptionID == "" {
				eval.BlockingDimensions = append(eval.BlockingDimensions, result)
				eval.ReasonCodes = append(eval.ReasonCodes, "closure.exception.invalid")
				continue
			}
			eval.AppliedExceptions = append(eval.AppliedExceptions, result.ExceptionID)
		case DimensionNotApplicable:
			if !allowedNA[result.Dimension] {
				eval.BlockingDimensions = append(eval.BlockingDimensions, result)
				eval.ReasonCodes = append(eval.ReasonCodes, "closure.dimension.not_applicable_forbidden")
			}
		default:
			eval.BlockingDimensions = append(eval.BlockingDimensions, result)
		}
	}
	for _, dim := range Dimensions {
		if required[dim] && !seen[dim] {
			eval.BlockingDimensions = append(eval.BlockingDimensions, DimensionResult{
				Dimension:   dim,
				Status:      DimensionUnknown,
				ReasonCodes: []string{"closure.dimension.missing"},
			})
		}
	}
	eval.AppliedExceptions = NormalizeSet(eval.AppliedExceptions)
	eval.ReasonCodes = NormalizeSet(eval.ReasonCodes)
	eval.TerminallyClosed = len(eval.BlockingDimensions) == 0
	return eval
}

func normalizeDimensionResults(results []DimensionResult) []DimensionResult {
	out := make([]DimensionResult, 0, len(results))
	for _, result := range results {
		result.ReasonCodes = NormalizeSet(result.ReasonCodes)
		result.ReceiptIDs = NormalizeSet(result.ReceiptIDs)
		out = append(out, result)
	}
	slices.SortFunc(out, func(a, b DimensionResult) int {
		if a.Dimension < b.Dimension {
			return -1
		}
		if a.Dimension > b.Dimension {
			return 1
		}
		return 0
	})
	return out
}
