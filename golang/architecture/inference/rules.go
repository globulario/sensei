// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

type ObservedGuardRule struct{}

func (ObservedGuardRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID:                  "rule.observed_guard_behavior.v1",
		Version:             "v1",
		Title:               "Observed guard behavior",
		Description:         "Projects mechanically observed guard or transition refusal facts into bounded observed claims.",
		RequiredFactKinds:   []string{"guard", "transition"},
		RequiredPredicates:  []string{"refuses_when", "rejects_transition_when"},
		OutputPlane:         architecture.PlaneObserved,
		OutputPredicate:     "refuses_when|rejects_transition_when",
		ConfidencePolicy:    confidencePolicy + "; cap 0.65",
		HumanReviewRequired: true,
		KnownLimitations: []string{
			"This observation proves only the inspected guard branch.",
			"Bypass paths, alternate entry points, reflection, generated code, or runtime mutation may exist outside the premise fact.",
			"Sibling test proximity is not used as proof.",
		},
	}
}

func (ObservedGuardRule) Apply(ctx Context) ([]Application, error) {
	var apps []Application
	for _, f := range ctx.Facts {
		if !isGuardPremise(f) {
			continue
		}
		status, unknowns := statusForPremises(ctx, []architecture.Fact{f})
		c := baseClaim("rule.observed_guard_behavior.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: f.Subject, Predicate: f.Predicate, Object: f.Object,
		}, []architecture.Fact{f}, status, append(unknowns,
			"This observation proves only the inspected guard branch.",
			"Bypass paths, alternate entry points, reflection, generated code, or runtime mutation may exist outside the premise fact.",
		), 0.65)
		apps = append(apps, Application{RuleID: c.InferenceRule, GroupKey: f.ID, PremiseFactIDs: []string{f.ID}, Claim: c})
	}
	return apps, nil
}

func isGuardPremise(f architecture.Fact) bool {
	return (f.Kind == "guard" && f.Predicate == "refuses_when") ||
		(f.Kind == "transition" && f.Predicate == "rejects_transition_when")
}

type RuleSignalingTestExpectationRule struct{}

func (RuleSignalingTestExpectationRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID:                  "rule.rule_signaling_test_expectation.v1",
		Version:             "v1",
		Title:               "Rule-signaling test expectation",
		Description:         "Projects test facts that explicitly assert architectural rules into test-scoped enforced claims.",
		RequiredFactKinds:   []string{"assertion"},
		RequiredPredicates:  []string{"asserts_architectural_rule"},
		OutputPlane:         architecture.PlaneEnforced,
		OutputPredicate:     "asserts_rule",
		ConfidencePolicy:    confidencePolicy + "; cap 0.75",
		HumanReviewRequired: true,
		KnownLimitations: []string{
			"The test proves only its setup, execution path, and assertions.",
			"The test name may overstate the behavior it actually exercises.",
			"No direct relation to a specific production guard is established by sibling-file proximity.",
		},
	}
}

func (RuleSignalingTestExpectationRule) Apply(ctx Context) ([]Application, error) {
	var apps []Application
	for _, f := range ctx.Facts {
		if f.Kind != "assertion" || f.Predicate != "asserts_architectural_rule" {
			continue
		}
		status, unknowns := statusForPremises(ctx, []architecture.Fact{f})
		c := baseClaim("rule.rule_signaling_test_expectation.v1", architecture.PlaneEnforced, architecture.ClaimStatement{
			Subject: f.Subject, Predicate: "asserts_rule", Object: f.Object,
		}, []architecture.Fact{f}, status, append(unknowns,
			"The test proves only its setup, execution path, and assertions.",
			"The test name may overstate the behavior it actually exercises.",
			"No direct relation to a specific production guard is established by sibling-file proximity.",
		), 0.75)
		c.InvalidationConditions = append(c.InvalidationConditions, "The test is skipped, disabled, or no longer executed by the relevant suite.")
		apps = append(apps, Application{RuleID: c.InferenceRule, GroupKey: f.ID, PremiseFactIDs: []string{f.ID}, Claim: c})
	}
	return apps, nil
}

type ObservedWriterSetRule struct{}

func (ObservedWriterSetRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID:                  "rule.observed_writer_set.v1",
		Version:             "v1",
		Title:               "Observed writer set",
		Description:         "Groups mechanically observed mutation facts by state object and records the bounded observed writer set.",
		RequiredFactKinds:   []string{"write", "authority_observation"},
		RequiredPredicates:  []string{"writes", "mutates_state"},
		OutputPlane:         architecture.PlaneObserved,
		OutputPredicate:     "has_observed_writer_set",
		ConfidencePolicy:    confidencePolicy + "; cap 0.55",
		HumanReviewRequired: true,
		KnownLimitations: []string{
			"Observed mutation does not prove architectural ownership.",
			"A single observed writer does not prove sole authority.",
			"Multiple observed writers do not by themselves prove an authority violation.",
		},
	}
}

func (ObservedWriterSetRule) Apply(ctx Context) ([]Application, error) {
	groups := map[string][]architecture.Fact{}
	for _, f := range ctx.Facts {
		if !isWriterPremise(f) || strings.TrimSpace(f.Object) == "" || strings.TrimSpace(f.Subject) == "" {
			continue
		}
		groups[f.Object] = append(groups[f.Object], f)
	}
	var keys []string
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var apps []Application
	for _, object := range keys {
		facts := groups[object]
		writersMap := map[string]bool{}
		for _, f := range facts {
			writersMap[f.Subject] = true
		}
		writers := sortedKeys(writersMap)
		status, unknowns := statusForPremises(ctx, facts)
		c := baseClaim("rule.observed_writer_set.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: object, Predicate: "has_observed_writer_set", Object: strings.Join(writers, ", "),
		}, facts, status, append(unknowns,
			"The observed writer set is bounded to the inspected source set.",
			"Observed mutation does not prove architectural ownership.",
			"A single observed writer does not prove sole authority.",
			"Multiple observed writers do not by themselves prove an authority violation.",
			"Dynamic, generated, reflective, shell, runtime, or external writers may remain unobserved.",
		), 0.55)
		if len(writers) == 1 {
			c.AlternativeExplanations = []string{
				"The writer may be the owner.",
				"The writer may be a delegate of an unseen owner.",
				"Other writers may exist outside the inspected source set.",
			}
		} else {
			c.AlternativeExplanations = []string{
				"The writers may represent delegated mutation.",
				"The writers may represent a migration path.",
				"The writers may represent intentional shared authority.",
				"One or more writers may violate the intended ownership model.",
			}
		}
		c.InvalidationConditions = []string{
			"A premise fact disappears or changes.",
			"A new writer fact appears for the same state.",
			"A source digest changes.",
			"The repository revision changes.",
			"The inference-rule version changes.",
		}
		apps = append(apps, Application{RuleID: c.InferenceRule, GroupKey: object, PremiseFactIDs: c.PremiseFacts, Claim: c})
	}
	return apps, nil
}

func isWriterPremise(f architecture.Fact) bool {
	return (f.Kind == "write" && f.Predicate == "writes") ||
		(f.Kind == "authority_observation" && f.Predicate == "mutates_state")
}
