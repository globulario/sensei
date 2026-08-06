// SPDX-License-Identifier: AGPL-3.0-only

// Package testobligation decides whether a set of required tests actually
// proved anything.
//
// The problem it exists for: `go test` prints "ok" for a package whose tests
// were all skipped. A required test that never ran is unavailable proof, not
// satisfied proof, but an exit code cannot tell the two apart. Anything that
// certifies from the process exit code alone will report success for a run
// that proved nothing — the same laundering as an awareness graph citing a
// test that does not exist, one level up.
//
// So an obligation carries a typed Outcome, and certification is a function of
// those outcomes rather than of any process result.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=architecture.testobligation
// @awareness enforces=globular.awareness_graph:invariant.awareness.missing_evidence_produces_unknown
package testobligation

import "sort"

// Outcome is what was observed about one test anchor. Skipped and Unavailable
// are deliberately distinct: both block certification, but they describe
// different worlds and the evidence trail must survive. Skipped means the test
// was selected and started and then declined to run (a build tag, a missing
// fixture, requireCombinedSeed); Unavailable means no result for it was
// observed at all (never selected, no runner for its language, results absent).
// Collapsing them would discard the reason a proof is missing.
type Outcome int

const (
	// OutcomeUnavailable is the zero value on purpose: an obligation nobody
	// reported on must default to "no observation", never to Pass.
	OutcomeUnavailable Outcome = iota
	OutcomePass
	OutcomeFail
	OutcomeSkipped
)

func (o Outcome) String() string {
	switch o {
	case OutcomePass:
		return "PASS"
	case OutcomeFail:
		return "FAIL"
	case OutcomeSkipped:
		return "SKIPPED"
	default:
		return "UNAVAILABLE"
	}
}

// executed reports whether the outcome came from a test that actually ran to a
// verdict. Only an executed obligation can contribute proof.
func (o Outcome) executed() bool { return o == OutcomePass || o == OutcomeFail }

// Obligation is one required-test anchor and what was observed about it.
type Obligation struct {
	// Anchor is the graph's id for the test, e.g.
	// "golang/server/main_test.go:TestResolve_RejectsUnsupportedClass".
	Anchor string
	// Required distinguishes a proof obligation from advisory coverage. An
	// optional obligation is reported but never blocks — it is visible so a
	// reader can see what was not observed, which is the point of reporting it
	// at all.
	Required bool
	Outcome  Outcome
	// Reason carries why a non-executed obligation did not produce proof: the
	// skip message, or why no result was found. It is the evidence trail and
	// must reach both human and machine output.
	Reason string
}

// Verdict is the aggregate result over a set of obligations.
type Verdict int

const (
	// VerdictIndeterminate is the zero value: an empty or unobserved set has
	// not proved anything, so the default must not be Pass.
	VerdictIndeterminate Verdict = iota
	VerdictPass
	VerdictFail
)

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "PASS"
	case VerdictFail:
		return "FAIL"
	default:
		return "INDETERMINATE"
	}
}

// Certifies reports whether this verdict may be treated as proof. Only Pass
// does. Both Fail and Indeterminate are non-certifying, which is why callers
// should branch on this rather than on `v == VerdictFail`.
func (v Verdict) Certifies() bool { return v == VerdictPass }

// ExitCode maps a verdict to a process exit code. Indeterminate is non-zero:
// "we could not tell" must not be reported to a caller as success, which is
// the entire failure this package exists to prevent.
//
// 3 rather than 2 for Indeterminate, because the CLI already spends 2 on usage
// and connection errors. A caller that cannot tell an unproved change from a
// mistyped flag would have to treat both as fatal, and the distinction is the
// useful part: FAIL means the code is wrong, INDETERMINATE means the evidence
// is missing, and they call for different responses.
func (v Verdict) ExitCode() int {
	switch v {
	case VerdictPass:
		return 0
	case VerdictFail:
		return 1
	default:
		return 3
	}
}

// Report is the machine-readable result of certification.
type Report struct {
	Verdict Verdict
	// Obligations is the full set, sorted by anchor, including optional and
	// passing ones. A report that listed only problems would make a
	// certification impossible to audit.
	Obligations []Obligation
}

// Blocking returns the required obligations that prevented certification:
// failures first, then unexecuted ones. Empty when the verdict certifies.
func (r Report) Blocking() []Obligation {
	var out []Obligation
	for _, o := range r.Obligations {
		if o.Required && (o.Outcome == OutcomeFail || !o.Outcome.executed()) {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Outcome == OutcomeFail) != (out[j].Outcome == OutcomeFail) {
			return out[i].Outcome == OutcomeFail
		}
		return out[i].Anchor < out[j].Anchor
	})
	return out
}

// Certify applies the certification law:
//
//	any required FAIL                          -> FAIL
//	else any required SKIPPED or UNAVAILABLE   -> INDETERMINATE
//	else all required executed and passed      -> PASS
//
// Optional obligations never change the verdict; they are carried into the
// report so their outcome stays visible.
//
// An empty required set yields INDETERMINATE rather than PASS: certifying a
// change against zero obligations is the vacuous-proof case, and calling it
// PASS would let "no required tests" read as "requirements satisfied".
func Certify(obligations []Obligation) Report {
	sorted := make([]Obligation, len(obligations))
	copy(sorted, obligations)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Anchor < sorted[j].Anchor })

	verdict := VerdictPass
	required := 0
	for _, o := range sorted {
		if !o.Required {
			continue
		}
		required++
		switch {
		case o.Outcome == OutcomeFail:
			// A failure outranks an unexecuted obligation: the run produced a
			// definite negative, and reporting that as "could not tell" would
			// understate it.
			return Report{Verdict: VerdictFail, Obligations: sorted}
		case !o.Outcome.executed():
			verdict = VerdictIndeterminate
		}
	}
	if required == 0 {
		verdict = VerdictIndeterminate
	}
	return Report{Verdict: verdict, Obligations: sorted}
}
