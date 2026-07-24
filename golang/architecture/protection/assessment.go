// SPDX-License-Identifier: AGPL-3.0-only

package protection

// Availability distinguishes WHY a protection Assessment carries no positive
// verdict, so a caller can never conflate "genuinely not protected" with
// "we don't know" (contract §5 correction: unavailable/degraded protection
// must never silently collapse to a boolean false).
type Availability string

const (
	// AvailabilityObserved: derivation ran; CoverageStatus and Protected are
	// real, typed answers (even if CoverageStatus is itself DEGRADED/EMPTY).
	AvailabilityObserved Availability = "observed"
	// AvailabilityUnbound: no exact repository context was configured to
	// assess against (e.g. a server not bound to one repository). This is a
	// normal, expected state — distinct from a failure.
	AvailabilityUnbound Availability = "unbound"
	// AvailabilityUnavailable: a repository context WAS configured but
	// derivation failed (an actual error), not merely "unbound."
	AvailabilityUnavailable Availability = "unavailable"
)

// Assessment is a typed protection verdict for a bounded set of files,
// carrying enough structure that a caller (preflight, a future editor
// surface) can distinguish "not protected" from "protection could not be
// assessed" and never silently treat the latter as safe.
type Assessment struct {
	Availability   Availability
	CoverageStatus ProtectionCoverageStatus // meaningful only when Availability == AvailabilityObserved
	Protected      bool
	Reasons        []ProtectionReason
	Gaps           []string
}

// Assess derives repoRoot's effective protection coverage and classifies
// every file in files against it, returning one aggregated Assessment.
// repoRoot=="" or len(files)==0 returns AvailabilityUnbound without touching
// the filesystem; a Derive failure returns AvailabilityUnavailable.
func Assess(repoRoot string, files []string) Assessment {
	if repoRoot == "" || len(files) == 0 {
		return Assessment{Availability: AvailabilityUnbound}
	}
	cov, err := Derive(repoRoot)
	if err != nil {
		return Assessment{Availability: AvailabilityUnavailable}
	}
	a := Assessment{Availability: AvailabilityObserved, CoverageStatus: cov.Status, Gaps: cov.Gaps}
	for _, f := range files {
		fc, ok := ClassifyFile(repoRoot, cov, f)
		if !ok {
			continue
		}
		if fc.Protected {
			a.Protected = true
			a.Reasons = append(a.Reasons, fc.Reasons...)
		}
	}
	return a
}
