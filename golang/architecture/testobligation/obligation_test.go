// SPDX-License-Identifier: AGPL-3.0-only

package testobligation

import "testing"

func req(anchor string, o Outcome, reason string) Obligation {
	return Obligation{Anchor: anchor, Required: true, Outcome: o, Reason: reason}
}

// 1. required test passes -> certifies.
func TestCertify_RequiredPassCertifies(t *testing.T) {
	r := Certify([]Obligation{req("a_test.go:TestA", OutcomePass, "")})
	if r.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", r.Verdict)
	}
	if !r.Verdict.Certifies() || r.Verdict.ExitCode() != 0 {
		t.Fatalf("PASS must certify with exit 0, got certifies=%v exit=%d", r.Verdict.Certifies(), r.Verdict.ExitCode())
	}
	if len(r.Blocking()) != 0 {
		t.Fatalf("nothing should block, got %+v", r.Blocking())
	}
}

// 2. required test fails -> FAIL.
func TestCertify_RequiredFailFails(t *testing.T) {
	r := Certify([]Obligation{req("a_test.go:TestA", OutcomeFail, "")})
	if r.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", r.Verdict)
	}
	if r.Verdict.Certifies() || r.Verdict.ExitCode() == 0 {
		t.Fatal("FAIL must not certify and must exit non-zero")
	}
}

// 3. THE CASE THIS PACKAGE EXISTS FOR: the test skipped, so `go test` exited 0
// and printed ok, but nothing was proved. Certification must not follow the
// process result.
func TestCertify_RequiredSkipIsIndeterminateNotPass(t *testing.T) {
	r := Certify([]Obligation{req("a_test.go:TestA", OutcomeSkipped, "combined-seed golden")})
	if r.Verdict != VerdictIndeterminate {
		t.Fatalf("verdict = %s, want INDETERMINATE", r.Verdict)
	}
	if r.Verdict.Certifies() {
		t.Fatal("a skipped required obligation must never certify")
	}
	if got := r.Verdict.ExitCode(); got == 0 {
		t.Fatalf("exit code = %d, want non-zero: exit 0 would re-launder the skip as success", got)
	}
}

// 4. required test unavailable -> INDETERMINATE.
func TestCertify_RequiredUnavailableIsIndeterminate(t *testing.T) {
	r := Certify([]Obligation{req("a_test.go:TestA", OutcomeUnavailable, "no result")})
	if r.Verdict != VerdictIndeterminate {
		t.Fatalf("verdict = %s, want INDETERMINATE", r.Verdict)
	}
	if r.Verdict.ExitCode() == 0 {
		t.Fatal("unavailable required proof must exit non-zero")
	}
}

// 5. optional obligations are visible but never block.
func TestCertify_OptionalSkipIsVisibleAndNonBlocking(t *testing.T) {
	r := Certify([]Obligation{
		req("a_test.go:TestA", OutcomePass, ""),
		{Anchor: "b_test.go:TestB", Required: false, Outcome: OutcomeSkipped, Reason: "no fixture"},
	})
	if r.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want PASS: an optional skip must not block", r.Verdict)
	}
	if len(r.Obligations) != 2 {
		t.Fatalf("optional obligation must stay in the report, got %d entries", len(r.Obligations))
	}
	if len(r.Blocking()) != 0 {
		t.Fatalf("optional obligation must not appear as blocking, got %+v", r.Blocking())
	}
}

// 6. a passing majority never rescues a required skip.
func TestCertify_MixedPassAndRequiredSkipNeverCertifies(t *testing.T) {
	r := Certify([]Obligation{
		req("a_test.go:TestA", OutcomePass, ""),
		req("b_test.go:TestB", OutcomePass, ""),
		req("c_test.go:TestC", OutcomeSkipped, "requireCombinedSeed"),
	})
	if r.Verdict.Certifies() {
		t.Fatalf("verdict = %s: two passes must not outvote one unexecuted obligation", r.Verdict)
	}
	blocking := r.Blocking()
	if len(blocking) != 1 || blocking[0].Anchor != "c_test.go:TestC" {
		t.Fatalf("blocking should name exactly the skipped obligation, got %+v", blocking)
	}
}

// 7. the reason a proof is missing must survive into the report.
func TestCertify_SkipReasonSurvives(t *testing.T) {
	const reason = "combined-seed golden: standalone seed omits Globular/services content"
	r := Certify([]Obligation{req("a_test.go:TestA", OutcomeSkipped, reason)})
	if got := r.Blocking()[0].Reason; got != reason {
		t.Fatalf("reason = %q, want it preserved verbatim", got)
	}
}

// A definite failure outranks an unexecuted obligation: reporting FAIL as
// "could not tell" would understate a known negative.
func TestCertify_FailOutranksSkip(t *testing.T) {
	r := Certify([]Obligation{
		req("a_test.go:TestA", OutcomeSkipped, "skipped"),
		req("b_test.go:TestB", OutcomeFail, ""),
	})
	if r.Verdict != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL", r.Verdict)
	}
}

// Zero required obligations is vacuous proof, not satisfied proof.
func TestCertify_NoRequiredObligationsIsIndeterminate(t *testing.T) {
	if v := Certify(nil).Verdict; v != VerdictIndeterminate {
		t.Fatalf("empty set verdict = %s, want INDETERMINATE", v)
	}
	r := Certify([]Obligation{{Anchor: "a_test.go:TestA", Required: false, Outcome: OutcomePass}})
	if r.Verdict != VerdictIndeterminate {
		t.Fatalf("optional-only verdict = %s, want INDETERMINATE", r.Verdict)
	}
}

// The zero values must be the honest ones: an obligation nobody reported on,
// and a report nobody filled in, must not read as proof.
func TestZeroValuesAreNotProof(t *testing.T) {
	var o Obligation
	if o.Outcome != OutcomeUnavailable {
		t.Fatalf("zero Outcome = %s, want UNAVAILABLE", o.Outcome)
	}
	var v Verdict
	if v.Certifies() {
		t.Fatal("zero Verdict must not certify")
	}
}

// The exit codes are a CLI contract that CI branches on, so they are pinned
// rather than merely documented. 3 in particular must not drift or collapse
// into a generic failure bucket: FAIL means the code is wrong, INDETERMINATE
// means the evidence is missing, and they call for different responses.
func TestVerdictExitCodesAreStable(t *testing.T) {
	for _, tc := range []struct {
		verdict Verdict
		want    int
	}{
		{VerdictPass, 0},
		{VerdictFail, 1},
		{VerdictIndeterminate, 3},
	} {
		if got := tc.verdict.ExitCode(); got != tc.want {
			t.Errorf("%s exit code = %d, want %d", tc.verdict, got, tc.want)
		}
	}
}

// An unrecognized verdict must fail closed. A future constant added without
// updating ExitCode/Certifies must not silently become a passing exit — that
// would let a state nobody has reasoned about certify a change.
func TestUnknownVerdictFailsClosed(t *testing.T) {
	unknown := Verdict(99)
	if unknown.Certifies() {
		t.Fatal("an unknown verdict must never certify")
	}
	if got := unknown.ExitCode(); got == 0 {
		t.Fatalf("unknown verdict exit code = %d, want non-zero", got)
	}
	if unknown.String() != "INDETERMINATE" {
		t.Fatalf("unknown verdict renders as %q; it must not read as PASS or FAIL", unknown.String())
	}
}

// Same fail-closed requirement one level down: an unrecognized outcome must not
// count as executed proof.
func TestUnknownOutcomeIsNotProof(t *testing.T) {
	unknown := Outcome(99)
	if unknown.executed() {
		t.Fatal("an unknown outcome must not count as executed")
	}
	if Certify([]Obligation{{Anchor: "a_test.go:TestA", Required: true, Outcome: unknown}}).Verdict.Certifies() {
		t.Fatal("an unknown outcome must not certify")
	}
}
