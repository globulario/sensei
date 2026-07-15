// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import "testing"

func TestEvaluateClosureCompleted(t *testing.T) {
	eval := EvaluateClosure(allPassDimensions(), CompletionPolicy{
		PolicyID: "completion.architectural_closure.v1",
		AllowedWaiverDimensions: []Dimension{DimensionProof},
	})
	if !eval.TerminallyClosed {
		t.Fatalf("expected terminally closed, got blockers: %#v", eval.BlockingDimensions)
	}
}

func TestEvaluateClosureBlockedByAuthority(t *testing.T) {
	dims := allPassDimensions()
	dims[3].Status = DimensionBlocked
	eval := EvaluateClosure(dims, CompletionPolicy{PolicyID: "completion.architectural_closure.v1"})
	if eval.TerminallyClosed {
		t.Fatal("expected authority blocker")
	}
}

func TestEvaluateClosureAllowsExceptionWhenPolicyPermits(t *testing.T) {
	dims := allPassDimensions()
	dims[7].Status = DimensionPassWithException
	dims[7].ExceptionID = "waiver.proof.slot-1"
	eval := EvaluateClosure(dims, CompletionPolicy{
		PolicyID: "completion.architectural_closure.v1",
		AllowedWaiverDimensions: []Dimension{DimensionProof},
	})
	if !eval.TerminallyClosed {
		t.Fatalf("expected closure with permitted exception, got %#v", eval.BlockingDimensions)
	}
}

func allPassDimensions() []DimensionResult {
	return []DimensionResult{
		{Dimension: DimensionIdentity, Status: DimensionPass},
		{Dimension: DimensionScope, Status: DimensionPass},
		{Dimension: DimensionDirection, Status: DimensionPass},
		{Dimension: DimensionAuthority, Status: DimensionPass},
		{Dimension: DimensionMutation, Status: DimensionPass},
		{Dimension: DimensionProtection, Status: DimensionPass},
		{Dimension: DimensionEpistemic, Status: DimensionPass},
		{Dimension: DimensionProof, Status: DimensionPass},
		{Dimension: DimensionFreshness, Status: DimensionPass},
		{Dimension: DimensionCompletion, Status: DimensionPass},
	}
}

