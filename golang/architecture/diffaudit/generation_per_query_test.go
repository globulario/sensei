// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

// Provenance for the diff audit, after the re-review finding on evaluator.go:167.
//
// The earlier version of this file exercised ADJACENT SAMPLING -- observe the generation just
// after each query -- and those witnesses are gone because the mechanism is gone. The reviewer
// was right on both counts: ImpactResponse already carries the generation that produced it, so
// taking a separate reading described a different moment; and a blank reading was being treated
// as harmless absence while the verdict stayed AVAILABLE.
//
// The model now has three states, and the third is the point:
//
//	known generation      the response said which graph produced it
//	mismatch              two contributing queries name different graphs
//	unverifiable          a contributing query could not say at all
//
// An audit must not become authoritative from the third. "No disagreement observed" and
// "identity proven" are different facts.
//
// DOMAIN OF THESE CLAIMS: every graph-backed query that contributes to one audit verdict. Not
// an interval around those queries -- the identity is carried on the responses themselves.

import (
	"context"
	"testing"
)

// boundChecker states the generation that produced each of its responses, per query kind.
type boundChecker struct {
	fakeChecker
	impactGens  []string // consumed in order; last value repeats
	checkGens   []string
	impactCalls int
	checkCalls  int
	lastImpact  string
	lastCheck   string
	// omitImpact / omitCheck drop the corresponding reporter entirely, which is how a
	// checker that cannot state provenance behaves.
	omitImpact bool
	omitCheck  bool
}

func next(seq []string, i int) string {
	if len(seq) == 0 {
		return ""
	}
	if i < len(seq) {
		return seq[i]
	}
	return seq[len(seq)-1]
}

func (c *boundChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	c.lastImpact = next(c.impactGens, c.impactCalls)
	c.impactCalls++
	return c.fakeChecker.GetFileImpact(ctx, file, domain)
}

func (c *boundChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	c.lastCheck = next(c.checkGens, c.checkCalls)
	c.checkCalls++
	return c.fakeChecker.CheckFile(ctx, file, content, domain)
}

func (c *boundChecker) LastImpactGeneration() string { return c.lastImpact }
func (c *boundChecker) LastCheckGeneration() string  { return c.lastCheck }
func (c *boundChecker) GraphGeneration(ctx context.Context) (string, error) {
	return "G1", nil // the brackets agree throughout, so only per-response identity can differ
}

// 1. THE CONTROL. Every contributing response names the same graph, so the audit binds and is
// accepted. Without this the rule below could pass by refusing everything.
func TestAnAuditBindsWhenEveryResponseNamesTheSameGeneration(t *testing.T) {
	c := &boundChecker{impactGens: []string{"G1"}, checkGens: []string{"G1"}}
	res := evaluateWith(t, c)
	if c.impactCalls == 0 || c.checkCalls == 0 {
		t.Fatalf("the fixture made %d impact and %d rule queries; it must make both", c.impactCalls, c.checkCalls)
	}
	for _, rc := range res.ReasonCodes {
		if rc == ReasonGraphGenerationSwitched || rc == ReasonGraphUnavailable {
			t.Errorf("an audit whose every response named G1 was refused: %v", res.Limitations)
		}
	}
	if res.GraphGeneration != "G1" {
		t.Errorf("GraphGeneration = %q, want G1", res.GraphGeneration)
	}
}

// 2. A RESPONSE FROM ANOTHER GENERATION refuses the verdict, per query kind, with the brackets
// agreeing throughout so only the response's own identity can reveal it. Two subtests, so
// neither binding rests on the other's evidence.
func TestAnAuditRefusesAResponseFromAnotherGeneration(t *testing.T) {
	t.Run("impact", func(t *testing.T) {
		c := &boundChecker{impactGens: []string{"G2"}, checkGens: []string{"G1"}}
		assertRefusedForGeneration(t, evaluateWith(t, c))
	})
	t.Run("rule evaluation", func(t *testing.T) {
		c := &boundChecker{impactGens: []string{"G1"}, checkGens: []string{"G2"}}
		assertRefusedForGeneration(t, evaluateWith(t, c))
	})
}

func assertRefusedForGeneration(t *testing.T, res *AuditResult) {
	t.Helper()
	if res.Availability == AvailabilityAvailable {
		t.Errorf("a verdict combining generations was reported as available: %v", res.Limitations)
	}
	if res.Decision != DecisionCannotVerify {
		t.Errorf("decision = %s, want CANNOT_VERIFY", res.Decision)
	}
	if res.GraphGeneration != "" {
		t.Errorf("the result bound itself to %q although its responses named different graphs", res.GraphGeneration)
	}
}

// 3. THE THIRD STATE, and the correction the reviewer asked for. A response that states NO
// generation makes the audit unverifiable. This inverts the earlier witness here, which
// treated a blank identity as harmless absence and let the verdict stay AVAILABLE labelled G1
// -- conflating "no disagreement observed" with "identity proven".
func TestAResponseThatNamesNoGenerationMakesTheAuditUnverifiable(t *testing.T) {
	c := &boundChecker{impactGens: []string{""}, checkGens: []string{"G1"}}
	res := evaluateWith(t, c)
	if res.Availability == AvailabilityAvailable {
		t.Errorf("an audit with an unverifiable contributing query stayed available: %v", res.Limitations)
	}
	if res.GraphGeneration != "" {
		t.Errorf("the result claims generation %q although one response could not name its own", res.GraphGeneration)
	}
	found := false
	for _, l := range res.Limitations {
		if contains(l, "cannot be bound to a graph generation") {
			found = true
		}
	}
	if !found {
		t.Errorf("the unverifiable query is not named in the limitations: %v", res.Limitations)
	}
}

// 4. A checker that cannot report a kind of provenance at all is unverifiable for it, rather
// than silently trusted. This is the live shape at this revision for rule evaluation.
func TestACheckerThatCannotStateProvenanceIsUnverifiable(t *testing.T) {
	c := &noCheckProvenanceChecker{}
	c.impactGens = []string{"G1"}
	res := evaluateWith(t, c)
	if res.Availability == AvailabilityAvailable {
		t.Errorf("an audit whose rule evaluations cannot be bound stayed available: %v", res.Limitations)
	}
	if res.Decision != DecisionCannotVerify {
		t.Errorf("decision = %s, want CANNOT_VERIFY", res.Decision)
	}
}

// noCheckProvenanceChecker implements ImpactGenerationReporter and NOT CheckGenerationReporter.
// inner is a FIELD, not an embedded type: embedding fakeChecker would PROMOTE both
// reporters and this fixture would silently implement the very interface it exists to
// lack. A first version embedded it and passed for a mismatch instead of for absence.
type noCheckProvenanceChecker struct {
	inner       fakeChecker
	impactGens  []string
	impactCalls int
	lastImpact  string
}

func (c *noCheckProvenanceChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	c.lastImpact = next(c.impactGens, c.impactCalls)
	c.impactCalls++
	return c.inner.GetFileImpact(ctx, file, domain)
}
func (c *noCheckProvenanceChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	return c.inner.CheckFile(ctx, file, content, domain)
}
func (c *noCheckProvenanceChecker) LastImpactGeneration() string { return c.lastImpact }
func (c *noCheckProvenanceChecker) GraphGeneration(ctx context.Context) (string, error) {
	return "G1", nil
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// 5. The MIRROR of case 4: a checker that cannot state IMPACT provenance is unverifiable for
// it. Two cases because a single one left the other fallback unproven -- a mutant trusting a
// checker with no impact reporter survived the rule-evaluation witness.
func TestACheckerThatCannotStateImpactProvenanceIsUnverifiable(t *testing.T) {
	c := &noImpactProvenanceChecker{}
	c.checkGens = []string{"G1"}
	res := evaluateWith(t, c)
	if res.Availability == AvailabilityAvailable {
		t.Errorf("an audit whose impact queries cannot be bound stayed available: %v", res.Limitations)
	}
	if res.Decision != DecisionCannotVerify {
		t.Errorf("decision = %s, want CANNOT_VERIFY", res.Decision)
	}
	named := false
	for _, l := range res.Limitations {
		if contains(l, "impact query") {
			named = true
		}
	}
	if !named {
		t.Errorf("the unbound impact query is not named: %v", res.Limitations)
	}
}

// noImpactProvenanceChecker implements CheckGenerationReporter and NOT ImpactGenerationReporter.
// inner is a FIELD, not an embedded type: embedding fakeChecker would PROMOTE both
// reporters and this fixture would silently implement the very interface it exists to
// lack. A first version embedded it and passed for a mismatch instead of for absence.
type noImpactProvenanceChecker struct {
	inner      fakeChecker
	checkGens  []string
	checkCalls int
	lastCheck  string
}

func (c *noImpactProvenanceChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	c.lastCheck = next(c.checkGens, c.checkCalls)
	c.checkCalls++
	return c.inner.CheckFile(ctx, file, content, domain)
}
func (c *noImpactProvenanceChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	return c.inner.GetFileImpact(ctx, file, domain)
}
func (c *noImpactProvenanceChecker) LastCheckGeneration() string { return c.lastCheck }
func (c *noImpactProvenanceChecker) GraphGeneration(ctx context.Context) (string, error) {
	return "G1", nil
}

// Compile-time proof that each fixture lacks the interface it is meant to lack. Without this
// a later edit could re-embed fakeChecker and both witnesses would quietly start passing for
// a mismatch instead of for absence.
func TestTheMissingReporterFixturesReallyLackTheirInterfaces(t *testing.T) {
	if _, ok := interface{}(&noCheckProvenanceChecker{}).(CheckGenerationReporter); ok {
		t.Error("noCheckProvenanceChecker implements CheckGenerationReporter, so it does not model a checker missing it")
	}
	if _, ok := interface{}(&noImpactProvenanceChecker{}).(ImpactGenerationReporter); ok {
		t.Error("noImpactProvenanceChecker implements ImpactGenerationReporter, so it does not model a checker missing it")
	}
}
