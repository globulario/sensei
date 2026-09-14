// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

// The review finding on evaluator.go:330: matching samples before and after the audit do
// not prove the queries BETWEEN them used that generation. The named counterexample is a
// publish-and-rollback, G1 -> G2 -> G1: both brackets read G1 while the queries in between
// were answered by G2, so the evaluator emitted an AVAILABLE result -- in fact a PASS --
// bound to G1 whose findings came from another graph.
//
// rollbackChecker is that TIMELINE. One generation is current at any moment; the publish
// happens before the first query and the rollback after the last, which is exactly what
// leaves the two brackets agreeing. Modelling it any other way -- a switch no observation
// could ever see -- would be testing the impossible rather than the finding.

import (
	"context"
	"testing"
)

type rollbackChecker struct {
	fakeChecker
	current string
	// publishAt/rollbackAfter are expressed in QUERIES, so the switch straddles them the
	// way the reviewer's sequence does.
	rollbackAfter int
	queries       int
	observed      []string
}

func (c *rollbackChecker) GraphGeneration(ctx context.Context) (string, error) {
	c.observed = append(c.observed, c.current)
	return c.current, nil
}

// onQuery advances the timeline: the first query happens after the publish, and the
// rollback lands once the last query is done and before the closing sample.
func (c *rollbackChecker) onQuery() {
	if c.queries == 0 {
		c.current = "G2" // the concurrent publish
	}
	c.queries++
	if c.queries >= c.rollbackAfter {
		c.current = "G1" // the rollback, before the audit takes its closing sample
	}
}

func (c *rollbackChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	c.onQuery()
	return c.fakeChecker.GetFileImpact(ctx, file, domain)
}

func (c *rollbackChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	c.onQuery()
	return c.fakeChecker.CheckFile(ctx, file, content, domain)
}

// THE FINDING. Both brackets read G1, a query in between was answered by G2, and the audit
// must not report a verdict bound to G1.
func TestAnAuditRefusesWhenAQueryWasAnsweredByAnotherGeneration(t *testing.T) {
	c := &rollbackChecker{current: "G1", rollbackAfter: 5}
	res := evaluateWith(t, c)

	// The fixture must actually have produced the bracket the finding depends on;
	// otherwise this would pass for the wrong reason.
	if len(c.observed) < 2 {
		t.Fatalf("the fixture took %d generation samples, expected an opening and a closing one", len(c.observed))
	}
	if first, last := c.observed[0], c.observed[len(c.observed)-1]; first != "G1" || last != "G1" {
		t.Fatalf("the brackets must agree for this to be the finding's case, got first=%s last=%s (all: %v)",
			first, last, c.observed)
	}
	sawOther := false
	for _, g := range c.observed[1 : len(c.observed)-1] {
		if g != "G1" {
			sawOther = true
		}
	}
	if !sawOther {
		t.Fatalf("no intervening query was answered by another generation, so this is not the finding's case: %v", c.observed)
	}

	if res.Availability == AvailabilityAvailable {
		t.Errorf("an available verdict was emitted although the queries ran across %v", c.observed)
	}
	if res.GraphGeneration == "G1" {
		t.Errorf("the result claims to be bound to G1, but its queries ran across %v", c.observed)
	}
	if res.Decision != DecisionCannotVerify {
		t.Errorf("decision = %s, want CANNOT_VERIFY: a verdict combining generations was reported as a verdict", res.Decision)
	}
	found := false
	for _, rc := range res.ReasonCodes {
		if rc == ReasonGraphGenerationSwitched {
			found = true
		}
	}
	if !found {
		t.Errorf("the switch was not named as the reason: %v / %v", res.ReasonCodes, res.Limitations)
	}
}

// THE CONTROL. One generation answered everything, so the audit binds and is accepted.
// Without this the repair could pass above by refusing every audit.
func TestAnAuditIsAcceptedWhenEveryQueryCameFromTheSampledGeneration(t *testing.T) {
	res := evaluateWith(t, &steadyChecker{current: "G1"})
	for _, rc := range res.ReasonCodes {
		if rc == ReasonGraphGenerationSwitched {
			t.Errorf("an audit answered by one generation throughout was refused: %v", res.Limitations)
		}
	}
	if res.GraphGeneration != "G1" {
		t.Errorf("GraphGeneration = %q, want G1: the agreeing case must still bind", res.GraphGeneration)
	}
}

// steadyChecker answers every query, and every generation observation, from one graph.
type steadyChecker struct {
	fakeChecker
	current string
}

func (c *steadyChecker) GraphGeneration(ctx context.Context) (string, error) { return c.current, nil }

// phasedChecker makes the graph disagree around ONE KIND of query only.
//
// It exists because a fixture that advanced the timeline on both kinds left each binding
// individually unproven: a mutant removing the impact binding survived on the rule
// binding's evidence, and vice versa. The observation the evaluator takes is the one
// immediately AFTER a query, so lastKind names the query that has just run and is cleared
// by each observation -- which leaves both brackets reading G1 and only the targeted
// kind's observations reading G2.
type phasedChecker struct {
	fakeChecker
	during   string // "impact" or "rule"
	lastKind string
	observed []string
	queries  map[string]int
}

func newPhasedChecker(during string) *phasedChecker {
	return &phasedChecker{during: during, queries: map[string]int{}}
}

func (c *phasedChecker) GraphGeneration(ctx context.Context) (string, error) {
	gen := "G1"
	if c.lastKind == c.during {
		gen = "G2"
	}
	c.lastKind = ""
	c.observed = append(c.observed, gen)
	return gen, nil
}

func (c *phasedChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	c.lastKind, c.queries["impact"] = "impact", c.queries["impact"]+1
	return c.fakeChecker.GetFileImpact(ctx, file, domain)
}

func (c *phasedChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	c.lastKind, c.queries["rule"] = "rule", c.queries["rule"]+1
	return c.fakeChecker.CheckFile(ctx, file, content, domain)
}

// Each kind of graph-backed query is bound on its own. Run as two cases so neither
// binding can rest on the other's evidence.
func TestAnAuditBindsEachKindOfQueryIndependently(t *testing.T) {
	for _, kind := range []string{"impact", "rule"} {
		t.Run(kind, func(t *testing.T) {
			c := newPhasedChecker(kind)
			res := evaluateWith(t, c)
			if c.queries[kind] == 0 {
				t.Fatalf("the audit made no %s query, so this proves nothing about binding it", kind)
			}
			if first, last := c.observed[0], c.observed[len(c.observed)-1]; first != "G1" || last != "G1" {
				t.Fatalf("the brackets must agree for this to be the finding's case: first=%s last=%s %v", first, last, c.observed)
			}
			if res.Availability == AvailabilityAvailable {
				t.Errorf("a %s query answered by another generation did not refuse the verdict: %v", kind, c.observed)
			}
			if res.GraphGeneration == "G1" {
				t.Errorf("the result bound itself to G1 although a %s query ran under G2: %v", kind, c.observed)
			}
		})
	}
}

// A blank observation is ABSENCE, not disagreement. The brackets carry the identity; a
// per-query observation the reporter could not supply must not be recorded, or every audit
// against a reporter that sometimes answers "" would refuse for a switch that never
// happened.
func TestABlankPerQueryObservationIsNotADisagreement(t *testing.T) {
	c := &blankBetweenChecker{}
	res := evaluateWith(t, c)
	if c.blanks == 0 {
		t.Fatalf("the fixture produced no blank observation, so it proves nothing")
	}
	for _, rc := range res.ReasonCodes {
		if rc == ReasonGraphGenerationSwitched {
			t.Errorf("a blank per-query observation was read as a generation switch: %v", res.Limitations)
		}
	}
	if res.GraphGeneration != "G1" {
		t.Errorf("GraphGeneration = %q, want G1: the brackets agreed and nothing contradicted them", res.GraphGeneration)
	}
}

// blankBetweenChecker reports G1 for the opening and closing brackets and nothing for the
// observation that follows a query -- the shape of a reporter that cannot always answer.
type blankBetweenChecker struct {
	fakeChecker
	justQueried bool
	queries     int
	blanks      int
}

func (c *blankBetweenChecker) GraphGeneration(ctx context.Context) (string, error) {
	if c.justQueried {
		c.justQueried = false
		c.blanks++
		return "", nil
	}
	return "G1", nil
}

func (c *blankBetweenChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	c.justQueried, c.queries = true, c.queries+1
	return c.fakeChecker.GetFileImpact(ctx, file, domain)
}

func (c *blankBetweenChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	c.justQueried, c.queries = true, c.queries+1
	return c.fakeChecker.CheckFile(ctx, file, content, domain)
}
