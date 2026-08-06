// SPDX-License-Identifier: AGPL-3.0-only

package testobligation

import (
	"strings"
	"testing"
)

const mod = "github.com/globulario/sensei"

// Verbatim `go test -json` output captured from this repository, not a
// simplified fixture. The framing lines matter: an earlier hand-written
// version of this fixture omitted the "=== RUN" and "--- SKIP" events, which
// hid a bug where the recorded reason was the "--- SKIP: Name (0.00s)" line
// instead of the test's actual skip message. The package-level "pass" event
// (Test empty) is what makes the process exit 0 despite nothing running.
const skippedRun = `
{"Time":"2026-08-05T23:39:30.134606672-04:00","Action":"run","Package":"github.com/globulario/sensei/golang/server","Test":"TestPreflightCriticalSignalsLeadTheActionList"}
{"Time":"2026-08-05T23:39:30.134612292-04:00","Action":"output","Package":"github.com/globulario/sensei/golang/server","Test":"TestPreflightCriticalSignalsLeadTheActionList","Output":"=== RUN   TestPreflightCriticalSignalsLeadTheActionList\n"}
{"Time":"2026-08-05T23:39:30.136576697-04:00","Action":"output","Package":"github.com/globulario/sensei/golang/server","Test":"TestPreflightCriticalSignalsLeadTheActionList","Output":"    preflight_signal_quality_test.go:81: combined-seed golden: standalone seed omits Globular/services content\n"}
{"Time":"2026-08-05T23:39:30.13659992-04:00","Action":"output","Package":"github.com/globulario/sensei/golang/server","Test":"TestPreflightCriticalSignalsLeadTheActionList","Output":"--- SKIP: TestPreflightCriticalSignalsLeadTheActionList (0.00s)\n"}
{"Time":"2026-08-05T23:39:30.136609969-04:00","Action":"skip","Package":"github.com/globulario/sensei/golang/server","Test":"TestPreflightCriticalSignalsLeadTheActionList","Elapsed":0}
{"Time":"2026-08-05T23:39:30.136620000-04:00","Action":"pass","Package":"github.com/globulario/sensei/golang/server","Elapsed":0.012}
`

// 8. The outcome is taken from the structured Action field, not from console
// wording, and the package-level pass does not leak onto the test.
func TestParseGoTestJSON_SkipIsStructuralNotTextual(t *testing.T) {
	res, err := ParseGoTestJSON(strings.NewReader(skippedRun), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	got, ok := res["golang/server:TestPreflightCriticalSignalsLeadTheActionList"]
	if !ok {
		t.Fatalf("test not found in results: %v", res)
	}
	if got.outcome != OutcomeSkipped {
		t.Fatalf("outcome = %s, want SKIPPED", got.outcome)
	}
	if !strings.Contains(got.reason, "combined-seed golden") {
		t.Fatalf("skip reason not captured, got %q", got.reason)
	}
	if strings.Contains(got.reason, "preflight_signal_quality_test.go:81") {
		t.Fatalf("file:line prefix should be trimmed from the reason, got %q", got.reason)
	}
}

// End-to-end over the exact situation on this repository: `go test` exits 0
// and prints ok, and certification must still refuse.
func TestGoRunThatOnlySkipsDoesNotCertify(t *testing.T) {
	res, err := ParseGoTestJSON(strings.NewReader(skippedRun), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	obligations := ResolveGoObligations(
		[]string{"golang/server/preflight_signal_quality_test.go:TestPreflightCriticalSignalsLeadTheActionList"},
		res,
		RunnerProvenCoverage(res, "test-runner"),
	)
	report := Certify(obligations)
	if report.Verdict != VerdictIndeterminate {
		t.Fatalf("verdict = %s, want INDETERMINATE", report.Verdict)
	}
	if report.Verdict.ExitCode() == 0 {
		t.Fatal("must exit non-zero even though `go test` exited 0")
	}
	if !strings.Contains(report.Blocking()[0].Reason, "combined-seed") {
		t.Fatalf("reason lost: %+v", report.Blocking()[0])
	}
}

func TestParseGoTestJSON_PassAndFail(t *testing.T) {
	stream := `
{"Action":"pass","Package":"github.com/globulario/sensei/golang/server","Test":"TestA"}
{"Action":"fail","Package":"github.com/globulario/sensei/cmd/awg","Test":"TestB"}
`
	res, err := ParseGoTestJSON(strings.NewReader(stream), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	if res["golang/server:TestA"].outcome != OutcomePass {
		t.Fatalf("TestA = %s, want PASS", res["golang/server:TestA"].outcome)
	}
	if res["cmd/awg:TestB"].outcome != OutcomeFail {
		t.Fatalf("TestB = %s, want FAIL", res["cmd/awg:TestB"].outcome)
	}
}

// A skipped subtest must not make its parent skipped — the parent's own
// terminal event already states what the whole test did.
func TestParseGoTestJSON_SubtestsFoldIntoParent(t *testing.T) {
	stream := `
{"Action":"skip","Package":"github.com/globulario/sensei/golang/server","Test":"TestA/sub_case"}
{"Action":"pass","Package":"github.com/globulario/sensei/golang/server","Test":"TestA"}
`
	res, err := ParseGoTestJSON(strings.NewReader(stream), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	if got := res["golang/server:TestA"].outcome; got != OutcomePass {
		t.Fatalf("parent outcome = %s, want PASS", got)
	}
	if len(res) != 1 {
		t.Fatalf("subtest should not be recorded separately, got %v", res)
	}
}

// An anchor nobody reported on is Unavailable with a stated reason, never
// dropped and never assumed passing.
func TestResolveGoObligations_UnobservedAnchorIsUnavailable(t *testing.T) {
	obs := ResolveGoObligations([]string{"golang/server/main_test.go:TestMissing"}, map[string]GoTestResult{}, DiscoveryCoverage{})
	if obs[0].Outcome != OutcomeUnavailable {
		t.Fatalf("outcome = %s, want UNAVAILABLE", obs[0].Outcome)
	}
	if obs[0].Reason == "" {
		t.Fatal("an unavailable obligation must state why")
	}
}

// A Go run cannot speak for a TypeScript/Python/Rust anchor. Silence from the
// wrong runner is not evidence, so it blocks certification.
func TestResolveGoObligations_ForeignLanguageAnchorIsUnavailableAndBlocks(t *testing.T) {
	anchors := []string{
		"typescript/client.spec.ts:SpecTitle_locate_uses_config",
		"python/test_client.py:test_locate_uses_config",
	}
	obs := ResolveGoObligations(anchors, map[string]GoTestResult{}, DiscoveryCoverage{})
	for _, o := range obs {
		if o.Outcome != OutcomeUnavailable {
			t.Fatalf("%s outcome = %s, want UNAVAILABLE", o.Anchor, o.Outcome)
		}
		if !strings.Contains(o.Reason, "language") {
			t.Fatalf("%s reason should name the language gap, got %q", o.Anchor, o.Reason)
		}
	}
	if Certify(obs).Verdict.Certifies() {
		t.Fatal("foreign-language obligations must block certification")
	}
}

// The file component of an anchor cannot be recovered from a package-level
// stream, so matching is by directory: a test that moved between files in the
// same package still counts as observed.
func TestAnchorKey_MatchesByPackageDirectory(t *testing.T) {
	key, ok := AnchorKey("golang/server/main_test.go:TestBriefing_UnavailableWhenStoreNil")
	if !ok {
		t.Fatal("expected a usable key")
	}
	if key != "golang/server:TestBriefing_UnavailableWhenStoreNil" {
		t.Fatalf("key = %q", key)
	}
	moved, _ := AnchorKey("golang/server/briefing_test.go:TestBriefing_UnavailableWhenStoreNil")
	if moved != key {
		t.Fatalf("same test in a sibling file should share a key: %q vs %q", moved, key)
	}
}

func TestAnchorKey_DoubleColonAndSemanticIDs(t *testing.T) {
	if key, ok := AnchorKey("golang/server/main_test.go::TestA"); !ok || key != "golang/server:TestA" {
		t.Fatalf("double-colon anchor not normalized: %q ok=%v", key, ok)
	}
	if _, ok := AnchorKey("awareness/debugsession"); ok {
		t.Fatal("a semantic id has no file:test shape and must not yield a key")
	}
}

func TestIsGoAnchor(t *testing.T) {
	for _, tc := range []struct {
		anchor string
		want   bool
	}{
		{"golang/server/main_test.go:TestA", true},
		{"typescript/client.spec.ts:Spec", false},
		{"python/test_client.py:test_a", false},
		{"awareness/debugsession", false},
	} {
		if got := IsGoAnchor(tc.anchor); got != tc.want {
			t.Errorf("IsGoAnchor(%q) = %v, want %v", tc.anchor, got, tc.want)
		}
	}
}

// A server that has not yet decoded ids hands back the wire form. The escaped
// slashes would make path.Dir see one segment, so every anchor keyed to "." and
// reported "no result" for a test that actually ran — a verdict that is right
// for the wrong reason, which is precisely the quiet inaccuracy this tool
// exists to catch. Matching must survive either spelling.
func TestEncodedAnchorStillMatchesAndKeepsTheRealReason(t *testing.T) {
	const encoded = "golang%2Fserver%2Fpreflight_signal_quality_test.go:TestPreflightCriticalSignalsLeadTheActionList"

	key, ok := AnchorKey(encoded)
	if !ok || key != "golang/server:TestPreflightCriticalSignalsLeadTheActionList" {
		t.Fatalf("encoded anchor key = %q ok=%v", key, ok)
	}
	if !IsGoAnchor(encoded) {
		t.Fatal("encoded Go anchor must still be recognized as Go")
	}

	res, err := ParseGoTestJSON(strings.NewReader(skippedRun), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	obs := ResolveGoObligations([]string{encoded}, res, CoverageFromRun(res, true))
	if obs[0].Outcome != OutcomeSkipped {
		t.Fatalf("outcome = %s, want SKIPPED — not a bogus UNAVAILABLE", obs[0].Outcome)
	}
	if !strings.Contains(obs[0].Reason, "combined-seed golden") {
		t.Fatalf("real skip reason lost, got %q", obs[0].Reason)
	}
}

// The recorded reason must be the test's own skip message, never test2json's
// "--- SKIP: Name (0.00s)" framing — that framing only restates the outcome
// and would leave the report unable to say WHY a proof is missing.
func TestParseGoTestJSON_ReasonIsTheSkipMessageNotTheFraming(t *testing.T) {
	res, err := ParseGoTestJSON(strings.NewReader(skippedRun), mod)
	if err != nil {
		t.Fatalf("ParseGoTestJSON: %v", err)
	}
	reason := res["golang/server:TestPreflightCriticalSignalsLeadTheActionList"].reason
	if reason != "combined-seed golden: standalone seed omits Globular/services content" {
		t.Fatalf("reason = %q, want the t.Skip message verbatim", reason)
	}
	for _, framing := range []string{"--- SKIP", "=== RUN", "(0.00s)"} {
		if strings.Contains(reason, framing) {
			t.Fatalf("reason contains test2json framing %q: %q", framing, reason)
		}
	}
}

// Case 1 — discovery unavailable. The exact test cannot be found AND the Go
// discovery surface was not loaded (or not declared exhaustive). "I could not
// inspect the evidence" must not be reported as "the evidence is gone": that
// is the conflation that produced 193 findings where 1 was real.
func TestGap_DiscoveryUnavailableIsUnavailableNotMissing(t *testing.T) {
	anchor := "golang/server/main_test.go:TestNotInRun"

	t.Run("no results supplied at all", func(t *testing.T) {
		obs := ResolveGoObligations([]string{anchor}, map[string]GoTestResult{}, DiscoveryCoverage{})
		if obs[0].Outcome != OutcomeUnavailable {
			t.Fatalf("outcome = %s, want UNAVAILABLE", obs[0].Outcome)
		}
	})

	t.Run("run supplied but not declared complete", func(t *testing.T) {
		res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}
		obs := ResolveGoObligations([]string{anchor}, res, CoverageFromRun(res, false))
		if obs[0].Outcome != OutcomeUnavailable {
			t.Fatalf("outcome = %s, want UNAVAILABLE: a filtered run cannot prove absence", obs[0].Outcome)
		}
		if !strings.Contains(obs[0].Reason, "not declared complete") {
			t.Fatalf("reason should name the incompleteness, got %q", obs[0].Reason)
		}
	})

	t.Run("complete run that never covered this package", func(t *testing.T) {
		res := map[string]GoTestResult{"cmd/awg:TestElsewhere": {outcome: OutcomePass}}
		obs := ResolveGoObligations([]string{anchor}, res, RunnerProvenCoverage(res, "test-runner"))
		if obs[0].Outcome != OutcomeUnavailable {
			t.Fatalf("outcome = %s, want UNAVAILABLE: this package was never inspected", obs[0].Outcome)
		}
		if !strings.Contains(obs[0].Reason, "not part of the supplied run") {
			t.Fatalf("reason should name the uncovered package, got %q", obs[0].Reason)
		}
	})
}

// Case 2 — the test moved. Discovery is complete over the anchor's package and
// the anchored function is not there, while a same-named test exists elsewhere.
// The obligation names an exact proof anchor: a test in another package is a
// replacement CANDIDATE, and must not silently satisfy the old claim.
func TestGap_MovedTestIsMissingImplementationWithNonAuthoritativeHint(t *testing.T) {
	res := map[string]GoTestResult{
		"golang/server:TestSomethingElse":                {outcome: OutcomePass},
		"golang/architecture/tasksession:TestBriefingXYZ": {outcome: OutcomePass},
	}
	obs := ResolveGoObligations(
		[]string{"golang/server/briefing_test.go:TestBriefingXYZ"},
		res,
		RunnerProvenCoverage(res, "test-runner"),
	)

	if obs[0].Outcome != OutcomeMissingImplementation {
		t.Fatalf("outcome = %s, want MISSING_IMPLEMENTATION", obs[0].Outcome)
	}
	if obs[0].CandidateHint != "golang/architecture/tasksession:TestBriefingXYZ" {
		t.Fatalf("candidate hint = %q, want the relocated test", obs[0].CandidateHint)
	}
	// The hint is a lead, not proof: it must not rescue the verdict.
	if Certify(obs).Verdict.Certifies() {
		t.Fatal("a relocated test must not satisfy an obligation naming a different anchor")
	}
}

// MISSING_IMPLEMENTATION blocks like every other unexecuted state, and stays
// distinguishable from UNAVAILABLE in the report.
func TestMissingImplementationBlocksAndStaysDistinct(t *testing.T) {
	res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}
	obs := ResolveGoObligations([]string{"golang/server/main_test.go:TestGone"}, res, RunnerProvenCoverage(res, "test-runner"))
	report := Certify(obs)

	if report.Verdict != VerdictIndeterminate {
		t.Fatalf("verdict = %s, want INDETERMINATE", report.Verdict)
	}
	blocking := report.Blocking()
	if len(blocking) != 1 || blocking[0].Outcome != OutcomeMissingImplementation {
		t.Fatalf("blocking = %+v, want one MISSING_IMPLEMENTATION", blocking)
	}
	if OutcomeMissingImplementation.String() == OutcomeUnavailable.String() {
		t.Fatal("MISSING_IMPLEMENTATION and UNAVAILABLE must not render identically")
	}
}

// Declared coverage, not path inference, decides the verdict: the SAME absent
// anchor flips between the two states purely on what the caller declared.
func TestVerdictFollowsDeclaredCoverageNotPathInference(t *testing.T) {
	anchor := "golang/server/main_test.go:TestGone"
	res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}

	incomplete := ResolveGoObligations([]string{anchor}, res, CoverageFromRun(res, false))[0]
	complete := ResolveGoObligations([]string{anchor}, res, RunnerProvenCoverage(res, "test-runner"))[0]

	if incomplete.Outcome != OutcomeUnavailable {
		t.Fatalf("incomplete run = %s, want UNAVAILABLE", incomplete.Outcome)
	}
	if complete.Outcome != OutcomeMissingImplementation {
		t.Fatalf("complete run = %s, want MISSING_IMPLEMENTATION", complete.Outcome)
	}
}

// THE AUTHORITY BOUNDARY. A completeness claim from whoever typed the command
// is unverifiable — a -run-filtered run is indistinguishable from an exhaustive
// one in the stream — so it must not convert "we did not observe the test" into
// "the test does not exist". Without this, one flag rebuilds the exact
// conflation this package was written to remove.
func TestCallerAttestedCompletenessCannotAccuse(t *testing.T) {
	anchor := "golang/server/main_test.go:TestGone"
	res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}

	attested := CoverageFromRun(res, true) // what the CLI flag produces
	if attested.Provenance != CoverageCallerAttested {
		t.Fatalf("provenance = %s, want caller-attested", attested.Provenance)
	}
	obs := ResolveGoObligations([]string{anchor}, res, attested)
	if obs[0].Outcome != OutcomeUnavailable {
		t.Fatalf("outcome = %s, want UNAVAILABLE: a caller assertion may not accuse", obs[0].Outcome)
	}
	if !strings.Contains(obs[0].Reason, "caller-attested") {
		t.Fatalf("reason must name whose claim this rests on, got %q", obs[0].Reason)
	}

	// Only a trusted producer converts the same input into an accusation.
	proven := ResolveGoObligations([]string{anchor}, res, RunnerProvenCoverage(res, "trusted-runner"))
	if proven[0].Outcome != OutcomeMissingImplementation {
		t.Fatalf("runner-proven outcome = %s, want MISSING_IMPLEMENTATION", proven[0].Outcome)
	}
}

// Fail closed on an unrecognized provenance: a value added later without
// deciding its authority must not inherit the right to accuse.
func TestUnknownProvenanceCannotAccuse(t *testing.T) {
	res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}
	coverage := CoverageFromRun(res, true)
	coverage.Provenance = CoverageProvenance(99)

	obs := ResolveGoObligations([]string{"golang/server/main_test.go:TestGone"}, res, coverage)
	if obs[0].Outcome != OutcomeUnavailable {
		t.Fatalf("outcome = %s, want UNAVAILABLE for an unknown provenance", obs[0].Outcome)
	}
	if CoverageProvenance(99).String() == "runner-proven" {
		t.Fatal("an unknown provenance must not render as the trusted one")
	}
}

// The undeclared zero value carries no authority either.
func TestUndeclaredCoverageCannotAccuse(t *testing.T) {
	res := map[string]GoTestResult{"golang/server:TestOther": {outcome: OutcomePass}}
	obs := ResolveGoObligations([]string{"golang/server/main_test.go:TestGone"}, res, DiscoveryCoverage{})
	if obs[0].Outcome != OutcomeUnavailable {
		t.Fatalf("outcome = %s, want UNAVAILABLE", obs[0].Outcome)
	}
}
