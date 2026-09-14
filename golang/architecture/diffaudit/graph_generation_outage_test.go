// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

// P2-2, preserved from blind review of #361 at cmd/awareness-mcp/main.go:2534:
//
//	"GraphGeneration returns (string, error) directly from Metadata RPC rather than suppressing
//	 errors from an unreachable/silent service as claimed in its docstring. When the awareness
//	 service is down [...] GraphGeneration returns ('', err) instead of ('', nil), causing
//	 EvaluateDiff to fail with an outage error instead of treating the missing generation
//	 identity as unverifiable (cannot_verify)."
//
// The first half HOLDS as a contract mismatch: the docstring promises "", nil and the code
// returns "", err.
//
// The SECOND HALF DOES NOT HOLD, and it is the half that names a defect. EvaluateDiff does not
// fail on a generation error -- it degrades to cannot_verify with ReasonGraphUnavailable, the
// same availability and reason code as a reporter that returned nothing. These witnesses pin
// that, so the classification rests on executed behaviour rather than on reading.
//
// They also pin what the suggested repair would DESTROY. observeGeneration's own comment says
// the distinction is deliberate -- "a reporting FAILURE and the absence of a reporter are
// different facts about different things, and only one of them names something an operator can
// fix" -- and the error text is what carries that. Returning "", nil would collapse "the store
// is unreachable, here is why" into "no identity", which is the direction every law on this
// front forbids: evidence must not evaporate when structured truth narrows.

import (
	"errors"
	"strings"
	"testing"
)

// P2-2-A. AN RPC ERROR IS NOT A FAILURE. The audit completes, and its verdict is cannot_verify.
func TestAGenerationRPCErrorDegradesTheAuditRatherThanFailingIt(t *testing.T) {
	res := evaluateWith(t, &generationReportingChecker{err: errors.New("store unreachable")})

	if res == nil {
		t.Fatal("EvaluateDiff produced no result at all on a generation RPC error")
	}
	if res.Availability != AvailabilityCannotVerify {
		t.Errorf("Availability = %q, want %q", res.Availability, AvailabilityCannotVerify)
	}
	if !hasReason(res.ReasonCodes, ReasonGraphUnavailable) {
		t.Errorf("reason codes %v do not include %q", res.ReasonCodes, ReasonGraphUnavailable)
	}
	if hasReason(res.ReasonCodes, ReasonGraphGenerationSwitched) {
		t.Errorf("an unobservable generation was reported as a SWITCH, which is a different fact: %v",
			res.ReasonCodes)
	}
}

// P2-2-B. THE ERROR TEXT SURVIVES, which is the whole reason the error is returned rather than
// swallowed. An operator reading this audit must be able to tell "the store is unreachable" from
// "the checker cannot report generations at all"; only the first names something they can fix.
//
// This is the witness that the suggested repair -- return "", nil -- must not pass.
func TestAGenerationRPCErrorKeepsTheReasonAnOperatorCanAct(t *testing.T) {
	const because = "store unreachable: connection refused"
	res := evaluateWith(t, &generationReportingChecker{err: errors.New(because)})

	joined := strings.Join(res.Limitations, "\n")
	if !strings.Contains(joined, because) {
		t.Fatalf("the limitation does not carry the RPC failure, so an outage is indistinguishable "+
			"from a checker that reports nothing:\n%s", joined)
	}
	// And it must say the generation could not be OBSERVED -- a reporting failure -- rather than
	// that it was not observABLE, which is the absence case.
	if !strings.Contains(joined, "could not be observed") {
		t.Errorf("the limitation does not present this as a reporting failure:\n%s", joined)
	}
}

// P2-2-C. THE TWO ABSENCES REMAIN DISTINGUISHABLE while agreeing on the verdict. Equal
// availability, equal reason code, different explanation -- the shape observeGeneration's comment
// prescribes. Collapsing them is what the reviewer's repair direction would do.
func TestAReportingFailureAndAnAbsentIdentityAgreeOnTheVerdictAndDifferInTheExplanation(t *testing.T) {
	failed := evaluateWith(t, &generationReportingChecker{err: errors.New("store unreachable")})
	absent := evaluateWith(t, &generationReportingChecker{generations: []string{""}})

	if failed.Availability != absent.Availability {
		t.Errorf("verdicts differ: %q vs %q; both are 'no identity' and a verdict that differed "+
			"would be describing the messenger rather than the graph",
			failed.Availability, absent.Availability)
	}
	if !hasReason(failed.ReasonCodes, ReasonGraphUnavailable) ||
		!hasReason(absent.ReasonCodes, ReasonGraphUnavailable) {
		t.Errorf("reason codes differ: %v vs %v", failed.ReasonCodes, absent.ReasonCodes)
	}
	f, a := strings.Join(failed.Limitations, "\n"), strings.Join(absent.Limitations, "\n")
	if f == a {
		t.Fatal("a reporting FAILURE and an ABSENT identity produced identical explanations, so " +
			"the one an operator can act on is no longer distinguishable")
	}
}

// P2-2-D. And the healthy path is unaffected, or the three above would pass against a build that
// had stopped observing generations at all.
func TestAReachableGenerationStillProducesAnAvailableAudit(t *testing.T) {
	res := evaluateWith(t, &generationReportingChecker{generations: []string{"c0b660fc42a5"}})
	if res.Availability != AvailabilityAvailable {
		t.Fatalf("Availability = %q on a healthy generation, want %q", res.Availability, AvailabilityAvailable)
	}
	if res.GraphGeneration != "c0b660fc42a5" {
		t.Errorf("GraphGeneration = %q, want the generation that answered", res.GraphGeneration)
	}
}
