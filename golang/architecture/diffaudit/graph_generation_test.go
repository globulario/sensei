// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

// Law 5 of the graph-identity front (docs/architecture/oxygraph_usage.md):
//
//	Every graph query used by that run must prove it belongs to the pinned
//	identity. A silent generation switch is forbidden.
//
// The evaluator already enforced half of this and the comment says so: graphCommit
// must be consistent across every file, because "a divergence means the snapshot
// shifted mid-audit". What it binds, though, is the RULE SNAPSHOT COMMIT, and on
// this installation that commit belongs to the services repository — the graph
// server runs with -home-domain github.com/globulario/services. So two different
// Sensei graph generations built from the same rule snapshot carry the SAME
// graph_commit, and switching between them mid-audit was invisible.
//
// Three live stores on this machine answer healthily with 237,049 / 142,739 /
// 35,268 triples. A switch between them is not hypothetical.
//
// The generation is therefore a SECOND identity, recorded beside the commit and
// never merged into it: one names the bytes that answered, the other the snapshot
// that produced the rules, and pouring either into the other makes one measured
// fact carry a different claim.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// generationReportingChecker is a fakeChecker that also reports which generation
// answered. Each call returns the next value in sequence, so a test can make the
// graph switch underneath one audit.
type generationReportingChecker struct {
	fakeChecker
	generations []string
	err         error
	calls       int
}

func (c *generationReportingChecker) GraphGeneration(ctx context.Context) (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	if len(c.generations) == 0 {
		return "", nil
	}
	if c.calls-1 < len(c.generations) {
		return c.generations[c.calls-1], nil
	}
	return c.generations[len(c.generations)-1], nil
}

// unreportingChecker implements SingleFileChecker and nothing more, which is the
// pre-law-5 shape: it can answer about files and cannot say which graph answered.
type unreportingChecker struct{}

func (unreportingChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	return nil, nil
}

func (unreportingChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, string, error) {
	return nil, nil, nil, "cleancommit", nil
}

func evaluateWith(t *testing.T, checker SingleFileChecker) *AuditResult {
	t.Helper()
	parsed, err := ParseDiff(sampleValidDiff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}
	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	return res
}

func TestAnAuditRecordsWhichGenerationAnsweredIt(t *testing.T) {
	res := evaluateWith(t, &generationReportingChecker{generations: []string{"c0b660fc42a5"}})
	if res.GraphGeneration != "c0b660fc42a5" {
		t.Errorf("the generation that answered did not reach the result: %q", res.GraphGeneration)
	}
	// The two identities stay separate. The commit is the rule snapshot; the
	// generation is the bytes. A result carrying one in the other's field would
	// look bound while proving nothing about the graph that answered.
	if res.GraphCommit != "cleancommit" {
		t.Errorf("graph_commit was disturbed: %q", res.GraphCommit)
	}
	if res.Availability != AvailabilityAvailable || res.Decision != DecisionPass {
		t.Errorf("a fully bound audit was degraded: %s / %s %v", res.Availability, res.Decision, res.Limitations)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The law's own words: a silent generation switch is forbidden.
func TestAGenerationSwitchDuringTheAuditRefusesTheResult(t *testing.T) {
	res := evaluateWith(t, &generationReportingChecker{generations: []string{"c0b660fc42a5", "230a74f68fed"}})
	if res.Availability != AvailabilityCannotVerify {
		t.Errorf("a generation switch left availability %s", res.Availability)
	}
	if res.Decision == DecisionPass {
		t.Error("a generation switch produced a passing audit")
	}
	if !hasReason(res.ReasonCodes, ReasonGraphGenerationSwitched) {
		t.Errorf("no graph_generation_switched reason code: %v", res.ReasonCodes)
	}
	// Both values must be named. "They disagree" without them is not actionable,
	// and the switch is distinct from the graph merely being unavailable.
	joined := strings.Join(res.Limitations, " | ")
	for _, want := range []string{"c0b660fc42a5", "230a74f68fed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the limitation does not name %s: %s", want, joined)
		}
	}
}

// Unobservable is not agreement. This is law 13's rule in another currency and the
// same one markerAgreement follows: an identity that cannot be checked is not a
// matching one.
func TestAnAuditThatCannotObserveItsGenerationIsNotAvailable(t *testing.T) {
	// A checker that cannot report at all — it does not implement the interface.
	res := evaluateWith(t, &unreportingChecker{})
	if res.Availability == AvailabilityAvailable {
		t.Error("an audit that could not observe which generation answered was reported as available")
	}
	if res.GraphGeneration != "" {
		t.Errorf("a generation was invented: %q", res.GraphGeneration)
	}
	if !hasReason(res.ReasonCodes, ReasonGraphUnavailable) {
		t.Errorf("no reason code explains the degradation: %v", res.ReasonCodes)
	}

	// A checker that reports an error, and one that reports an empty string, are
	// both "no identity" and must not differ in verdict.
	for name, c := range map[string]SingleFileChecker{
		"reporting an error": &generationReportingChecker{err: errors.New("store unreachable")},
		"reporting nothing":  &generationReportingChecker{generations: []string{""}},
	} {
		res := evaluateWith(t, c)
		if res.Availability == AvailabilityAvailable {
			t.Errorf("%s: reported as available", name)
		}
	}
}

// The structural lock, mirroring the one graph_commit already has: a result that
// claims availability must be bound to the generation that produced it, so a future
// caller cannot construct an "available" verdict anchored to nothing.
func TestAnAvailableResultMustBeBoundToAGeneration(t *testing.T) {
	res := evaluateWith(t, &generationReportingChecker{generations: []string{"c0b660fc42a5"}})
	res.GraphGeneration = ""
	digest, err := res.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	res.Digest = digest
	if err := res.Validate(); err == nil {
		t.Fatal("an available result with no generation identity validated")
	} else if !strings.Contains(err.Error(), "generation") {
		t.Errorf("the refusal does not name the missing generation: %v", err)
	}
}

// The generation participates in the result's identity. Two audits that differ only
// by which graph answered are not the same record — that is what #134 could not
// replay, because nothing recorded which generation had answered.
func TestTheGenerationParticipatesInTheResultDigest(t *testing.T) {
	a := evaluateWith(t, &generationReportingChecker{generations: []string{"c0b660fc42a5"}})
	b := evaluateWith(t, &generationReportingChecker{generations: []string{"230a74f68fed"}})
	if a.Digest == b.Digest {
		t.Error("two audits answered by different generations produced the same digest")
	}
}
