// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// CoverageProfile selects which evidence lanes a task must satisfy to certify.
type CoverageProfile string

const (
	// CoverageStaticTest is the default: static and test evidence certify a
	// task without any runtime evidence. The runtime lane is recorded
	// not_applicable, never uncertifiable, so a platform-neutral repository can
	// reach a valid evidence state.
	CoverageStaticTest CoverageProfile = "static_test"
	// CoverageStaticTestRuntime is the opt-in escalation: in addition to static
	// and test evidence it requires owner-path runtime evidence.
	CoverageStaticTestRuntime CoverageProfile = "static_test_runtime"
)

// DefaultCoverageProfile is used when a request leaves the coverage unset.
const DefaultCoverageProfile = CoverageStaticTest

// RequiresRuntime reports whether the coverage profile demands runtime evidence.
func (c CoverageProfile) RequiresRuntime() bool { return c == CoverageStaticTestRuntime }

// NormalizeCoverageProfile maps an empty or unrecognized value to the default.
func NormalizeCoverageProfile(s string) CoverageProfile {
	switch CoverageProfile(s) {
	case CoverageStaticTest, CoverageStaticTestRuntime:
		return CoverageProfile(s)
	default:
		return DefaultCoverageProfile
	}
}

// EvidenceItem pairs a receipt with the profile that governs it.
type EvidenceItem struct {
	Profile Profile
	Receipt Receipt
}

// CoverageRequest carries the shared proof context for a set of evidence items.
type CoverageRequest struct {
	Coverage       CoverageProfile
	ExpectedResult ResultBinding
	RuntimeTarget  *RuntimeTarget
	Now            time.Time
}

// Lanes holds the per-lane evidence status.
type Lanes struct {
	Static  LaneStatus `json:"static" yaml:"static"`
	Test    LaneStatus `json:"test" yaml:"test"`
	Runtime LaneStatus `json:"runtime" yaml:"runtime"`
}

// CoverageResult is the folded evidence verdict for a task. EvidenceLane feeds
// the certification receipt's evidence lane in Phase 5.
type CoverageResult struct {
	Coverage     CoverageProfile `json:"coverage" yaml:"coverage"`
	Lanes        Lanes           `json:"lanes" yaml:"lanes"`
	EvidenceLane LaneStatus      `json:"evidence_lane" yaml:"evidence_lane"`
	Assessments  []Assessment    `json:"assessments,omitempty" yaml:"assessments,omitempty"`
	Conflicts    []Conflict      `json:"conflicts,omitempty" yaml:"conflicts,omitempty"`
	ReasonCodes  []string        `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
}

// EvaluateCoverage validates each evidence item and folds the per-receipt
// statuses into lane statuses and an overall evidence lane, according to the
// coverage profile. Under static_test the runtime lane is always
// not_applicable. Under static_test_runtime the runtime lane must be satisfied
// by owner-path runtime evidence.
func EvaluateCoverage(req CoverageRequest, items []EvidenceItem) CoverageResult {
	coverage := NormalizeCoverageProfile(string(req.Coverage))
	result := CoverageResult{Coverage: coverage}

	var staticR, testR, runtimeR []Receipt
	for _, item := range items {
		pr := ProofRequest{
			Profile:        item.Profile,
			ExpectedResult: req.ExpectedResult,
			RuntimeTarget:  req.RuntimeTarget,
			Now:            req.Now,
		}
		a := Validate(pr, item.Receipt)
		result.Assessments = append(result.Assessments, a)

		switch item.Receipt.EvidenceKind {
		case closureprotocol.EvidenceRuntime:
			runtimeR = append(runtimeR, item.Receipt)
		case closureprotocol.EvidenceTest:
			testR = append(testR, item.Receipt)
		default:
			// static / artifact / review / authority / hybrid all count as
			// non-runtime structural evidence for the static lane.
			staticR = append(staticR, item.Receipt)
		}
	}

	byID := map[string]Assessment{}
	for _, a := range result.Assessments {
		byID[a.ReceiptID] = a
	}

	staticStatus, staticReasons, staticConflicts := laneStatus(staticR, byID)
	testStatus, testReasons, testConflicts := laneStatus(testR, byID)

	result.Lanes.Static = staticStatus
	result.Lanes.Test = testStatus
	result.ReasonCodes = append(result.ReasonCodes, staticReasons...)
	result.ReasonCodes = append(result.ReasonCodes, testReasons...)
	result.Conflicts = append(result.Conflicts, staticConflicts...)
	result.Conflicts = append(result.Conflicts, testConflicts...)

	applicable := []LaneStatus{staticStatus, testStatus}
	if coverage.RequiresRuntime() {
		runtimeStatus, runtimeReasons, runtimeConflicts := laneStatus(runtimeR, byID)
		result.Lanes.Runtime = runtimeStatus
		result.ReasonCodes = append(result.ReasonCodes, runtimeReasons...)
		result.Conflicts = append(result.Conflicts, runtimeConflicts...)
		applicable = append(applicable, runtimeStatus)
	} else {
		// static_test: runtime never blocks certification for platform-neutral
		// repositories.
		result.Lanes.Runtime = closureprotocol.DimensionNotApplicable
	}

	result.EvidenceLane = combineLanes(applicable)
	result.ReasonCodes = closureprotocol.NormalizeSet(result.ReasonCodes)
	return result
}

// laneStatus folds one lane's receipts into a single status. Missing evidence
// blocks the lane (fail-closed); disagreement conflicts it.
func laneStatus(receipts []Receipt, byID map[string]Assessment) (LaneStatus, []string, []Conflict) {
	if len(receipts) == 0 {
		return closureprotocol.DimensionBlocked, []string{ReasonLaneMissing}, nil
	}
	conflicts := DetectConflicts(receipts)
	if len(conflicts) > 0 {
		return closureprotocol.DimensionConflicted, []string{ReasonOwnerPathConflicted}, conflicts
	}

	var reasons []string
	sawValid := false
	worst := closureprotocol.DimensionPass
	for _, r := range receipts {
		a := byID[r.ReceiptID]
		reasons = append(reasons, a.ReasonCodes...)
		switch a.Status {
		case closureprotocol.ReceiptValid:
			sawValid = true
		case closureprotocol.ReceiptStale:
			worst = worseLane(worst, closureprotocol.DimensionStale)
		case closureprotocol.ReceiptUnknown, closureprotocol.ReceiptSuperseded:
			worst = worseLane(worst, closureprotocol.DimensionUnknown)
		default: // invalid / conflicted / revoked
			worst = worseLane(worst, closureprotocol.DimensionBlocked)
		}
	}
	if worst == closureprotocol.DimensionPass {
		if !sawValid {
			return closureprotocol.DimensionBlocked, append(reasons, ReasonLaneMissing), nil
		}
		return closureprotocol.DimensionPass, nil, nil
	}
	return worst, reasons, nil
}

// laneSeverity ranks lane statuses so the worst dominates. not_applicable is
// excluded by callers.
func laneSeverity(s LaneStatus) int {
	switch s {
	case closureprotocol.DimensionPass:
		return 0
	case closureprotocol.DimensionPassWithException:
		return 1
	case closureprotocol.DimensionUnknown:
		return 2
	case closureprotocol.DimensionStale:
		return 3
	case closureprotocol.DimensionBlocked:
		return 4
	case closureprotocol.DimensionConflicted:
		return 5
	default:
		return 4
	}
}

func worseLane(a, b LaneStatus) LaneStatus {
	if laneSeverity(b) > laneSeverity(a) {
		return b
	}
	return a
}

func combineLanes(statuses []LaneStatus) LaneStatus {
	worst := closureprotocol.DimensionNotApplicable
	seen := false
	for _, s := range statuses {
		if s == closureprotocol.DimensionNotApplicable {
			continue
		}
		if !seen {
			worst, seen = s, true
			continue
		}
		worst = worseLane(worst, s)
	}
	if !seen {
		return closureprotocol.DimensionNotApplicable
	}
	return worst
}
