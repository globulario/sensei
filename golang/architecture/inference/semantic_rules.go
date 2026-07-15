// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

type ExportedAPITestedBehaviorRule struct{}

func (ExportedAPITestedBehaviorRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.exported_api_tested_behavior.v1", Version: "v1", Title: "Exported API tested behavior",
		Description:       "Relates an exported Go symbol to repository tests that directly call it.",
		RequiredFactKinds: []string{"export", "test_call"}, RequiredPredicates: []string{"exports_symbol", "test_calls_symbol"},
		OutputPlane: architecture.PlaneObserved, OutputPredicate: "has_observed_test_surface",
		ConfidencePolicy: confidencePolicy + "; cap 0.90", HumanReviewRequired: true,
		KnownLimitations: []string{
			"A direct test call proves only that repository tests reach the symbol through the exercised path.",
			"Static test topology does not establish enforcement, compatibility, or a public stability promise.",
		},
	}
}

func (ExportedAPITestedBehaviorRule) Apply(ctx Context) ([]Application, error) {
	exports := factsByPredicate(ctx.Facts, "exports_symbol")
	testsByTarget := factsByObject(ctx.Facts, "test_calls_symbol")
	var applications []Application
	for _, exported := range exports {
		tests := testsByTarget[exported.Subject]
		if len(tests) == 0 {
			continue
		}
		premises := append([]architecture.Fact{exported}, tests...)
		testSymbols := factSubjects(tests)
		status, unknowns := statusForPremises(ctx, premises)
		claim := baseClaim("rule.exported_api_tested_behavior.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: exported.Subject, Predicate: "has_observed_test_surface", Object: strings.Join(testSymbols, ", "),
		}, premises, status, append(unknowns,
			"Whether the test surface is required enforcement remains unknown.",
			"The tests may cover only a subset of inputs and paths.",
		), 0.90)
		claim.AlternativeExplanations = []string{
			"The tests may protect internal regression behavior rather than a public contract.",
			"The exported symbol may be public for implementation reasons without a compatibility promise.",
		}
		applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: exported.Subject, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
	}
	return applications, nil
}

type InterfaceImplementationSurfaceRule struct{}

func (InterfaceImplementationSurfaceRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.interface_implementation_surface.v1", Version: "v1", Title: "Interface implementation surface",
		Description:       "Projects compiler-checked Go interface satisfaction into an observed behavioral-boundary claim.",
		RequiredFactKinds: []string{"interface"}, RequiredPredicates: []string{"implements_interface"},
		OutputPlane: architecture.PlaneObserved, OutputPredicate: "participates_in_interface_boundary",
		ConfidencePolicy: confidencePolicy + "; cap 0.95", HumanReviewRequired: true,
		KnownLimitations: []string{
			"Method-set satisfaction does not prove behavioral substitutability.",
			"No interface intent, stability promise, or runtime usage is inferred.",
		},
	}
}

func (InterfaceImplementationSurfaceRule) Apply(ctx Context) ([]Application, error) {
	var applications []Application
	for _, fact := range factsByPredicate(ctx.Facts, "implements_interface") {
		status, unknowns := statusForPremises(ctx, []architecture.Fact{fact})
		claim := baseClaim("rule.interface_implementation_surface.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: fact.Subject, Predicate: "participates_in_interface_boundary", Object: fact.Object,
		}, []architecture.Fact{fact}, status, append(unknowns,
			"The exact behavioral semantics of the interface remain unknown.",
			"Runtime construction or use of this implementation is not established.",
		), 0.95)
		claim.AlternativeExplanations = []string{"The implementation may satisfy the interface incidentally or only for tests."}
		applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: fact.Subject + "|" + fact.Object, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
	}
	return applications, nil
}

type ComponentDependencyCrossingRule struct{}

func (ComponentDependencyCrossingRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.component_dependency_crossing.v1", Version: "v1", Title: "Component dependency crossing",
		Description:       "Groups exact repository-local imports crossing two reconstructed components.",
		RequiredFactKinds: []string{"component_dependency"}, RequiredPredicates: []string{"component_depends_on_component"},
		OutputPlane: architecture.PlaneObserved, OutputPredicate: "depends_on_component_through_observed_code",
		ConfidencePolicy: confidencePolicy + "; cap 0.95", HumanReviewRequired: true,
		KnownLimitations: []string{
			"An observed dependency does not establish an intended architectural boundary or Contract.",
			"Dynamic loading and non-Go dependency paths are outside this premise set.",
		},
	}
}

func (ComponentDependencyCrossingRule) Apply(ctx Context) ([]Application, error) {
	groups := map[string][]architecture.Fact{}
	for _, fact := range factsByPredicate(ctx.Facts, "component_depends_on_component") {
		groups[fact.Subject+"\x00"+fact.Object] = append(groups[fact.Subject+"\x00"+fact.Object], fact)
	}
	var applications []Application
	for _, key := range sortedFactGroupKeys(groups) {
		facts := groups[key]
		status, unknowns := statusForPremises(ctx, facts)
		claim := baseClaim("rule.component_dependency_crossing.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: facts[0].Subject, Predicate: "depends_on_component_through_observed_code", Object: facts[0].Object,
		}, facts, status, append(unknowns,
			"Whether the crossing is intended, stable, or contract-governed remains unknown.",
		), 0.95)
		claim.AlternativeExplanations = []string{
			"The dependency may be an intentional internal implementation detail.",
			"The dependency may indicate a missing or violated boundary.",
		}
		applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: key, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
	}
	return applications, nil
}

type SharedEntrypointBehaviorPathRule struct{}

func (SharedEntrypointBehaviorPathRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.shared_entrypoint_behavior_path.v1", Version: "v1", Title: "Shared entrypoint behavior path",
		Description:       "Finds internal symbols reached by two or more exported repository entrypoints through the static call graph.",
		RequiredFactKinds: []string{"reachability"}, RequiredPredicates: []string{"entrypoint_reaches_symbol"},
		OutputPlane: architecture.PlaneObserved, OutputPredicate: "is_shared_implementation_path_for",
		ConfidencePolicy: confidencePolicy + "; cap 0.85", HumanReviewRequired: true,
		KnownLimitations: []string{
			"Shared static reachability does not prove behavioral equivalence or a requirement to remain equivalent.",
			"Interface dispatch, reflection, function values, generated code, and runtime routing may be absent.",
		},
	}
}

func (SharedEntrypointBehaviorPathRule) Apply(ctx Context) ([]Application, error) {
	groups := factsByObject(ctx.Facts, "entrypoint_reaches_symbol")
	var applications []Application
	for _, target := range sortedFactGroupKeys(groups) {
		facts := uniqueFactsBySubject(groups[target])
		entrypoints := factSubjects(facts)
		if len(entrypoints) < 2 {
			continue
		}
		status, unknowns := statusForPremises(ctx, facts)
		claim := baseClaim("rule.shared_entrypoint_behavior_path.v1", architecture.PlaneObserved, architecture.ClaimStatement{
			Subject: target, Predicate: "is_shared_implementation_path_for", Object: strings.Join(entrypoints, ", "),
		}, facts, status, append(unknowns,
			"Whether these entrypoints are intended to remain behaviorally equivalent remains unknown.",
		), 0.85)
		claim.AlternativeExplanations = []string{
			"The entrypoints may share plumbing while intentionally differing before or after this path.",
			"The shared path may be an incidental implementation detail.",
		}
		applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: target, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
	}
	return applications, nil
}

type TestedFailureBoundaryRule struct{}

func (TestedFailureBoundaryRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.tested_failure_boundary.v1", Version: "v1", Title: "Test-backed failure boundary",
		Description:       "Relates a failure-signaling test expectation to the exact production symbol it directly calls.",
		RequiredFactKinds: []string{"assertion", "test_call"}, RequiredPredicates: []string{"asserts_behavior_example", "asserts_architectural_rule", "test_calls_symbol"},
		OutputPlane: architecture.PlaneEnforced, OutputPredicate: "has_tested_failure_boundary",
		ConfidencePolicy: confidencePolicy + "; cap 0.75", HumanReviewRequired: true,
		KnownLimitations: []string{
			"The test-name expectation may overstate the assertion body.",
			"A tested failure path does not establish that the failure occurs in production.",
		},
	}
}

func (TestedFailureBoundaryRule) Apply(ctx Context) ([]Application, error) {
	assertions := failureAssertionsByTest(ctx.Facts)
	var applications []Application
	for _, call := range factsByPredicate(ctx.Facts, "test_calls_symbol") {
		assertion, ok := assertions[call.Subject]
		if !ok {
			continue
		}
		premises := []architecture.Fact{assertion, call}
		status, unknowns := statusForPremises(ctx, premises)
		claim := baseClaim("rule.tested_failure_boundary.v1", architecture.PlaneEnforced, architecture.ClaimStatement{
			Subject: call.Object, Predicate: "has_tested_failure_boundary", Object: assertion.Object,
		}, premises, status, append(unknowns,
			"The runtime occurrence and frequency of this failure mode remain unknown.",
		), 0.75)
		claim.AlternativeExplanations = []string{"The test may exercise defensive behavior that is unreachable in normal production operation."}
		applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: call.Object + "|" + call.Subject, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
	}
	return applications, nil
}

type TestedMonotonicStateRule struct{}

func (TestedMonotonicStateRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID: "rule.tested_monotonic_state.v1", Version: "v1", Title: "Test-backed monotonic state behavior",
		Description:       "Requires an observed increment, a direct test call, and an explicit monotonic test expectation for the same production symbol.",
		RequiredFactKinds: []string{"generation_check", "assertion", "test_call"}, RequiredPredicates: []string{"increments_generation", "asserts_architectural_rule", "test_calls_symbol"},
		OutputPlane: architecture.PlaneEnforced, OutputPredicate: "has_tested_monotonic_transition",
		ConfidencePolicy: confidencePolicy + "; cap 0.75", HumanReviewRequired: true,
		KnownLimitations: []string{
			"One increment and one test do not prove that every write path is monotonic.",
			"The claim is bounded to the exact production symbol and test expectation.",
		},
	}
}

func (TestedMonotonicStateRule) Apply(ctx Context) ([]Application, error) {
	assertions := monotonicAssertionsByTest(ctx.Facts)
	calls := factsByObject(ctx.Facts, "test_calls_symbol")
	var applications []Application
	for _, increment := range factsByPredicate(ctx.Facts, "increments_generation") {
		for _, call := range calls[increment.Subject] {
			assertion, ok := assertions[call.Subject]
			if !ok {
				continue
			}
			premises := []architecture.Fact{increment, call, assertion}
			status, unknowns := statusForPremises(ctx, premises)
			claim := baseClaim("rule.tested_monotonic_state.v1", architecture.PlaneEnforced, architecture.ClaimStatement{
				Subject: increment.Subject, Predicate: "has_tested_monotonic_transition", Object: increment.Object,
			}, premises, status, append(unknowns,
				"Whether every writer preserves monotonicity remains unknown.",
			), 0.75)
			claim.AlternativeExplanations = []string{"Other write paths may reset or replace the state outside the tested transition."}
			applications = append(applications, Application{RuleID: claim.InferenceRule, GroupKey: increment.Subject + "|" + increment.Object + "|" + call.Subject, PremiseFactIDs: claim.PremiseFacts, Claim: claim})
		}
	}
	return applications, nil
}

func factsByPredicate(facts []architecture.Fact, predicate string) []architecture.Fact {
	var out []architecture.Fact
	for _, fact := range facts {
		if fact.Predicate == predicate {
			out = append(out, fact)
		}
	}
	return out
}

func factsByObject(facts []architecture.Fact, predicate string) map[string][]architecture.Fact {
	out := map[string][]architecture.Fact{}
	for _, fact := range facts {
		if fact.Predicate == predicate && fact.Object != "" {
			out[fact.Object] = append(out[fact.Object], fact)
		}
	}
	return out
}

func factSubjects(facts []architecture.Fact) []string {
	values := map[string]bool{}
	for _, fact := range facts {
		values[fact.Subject] = true
	}
	return sortedKeys(values)
}

func uniqueFactsBySubject(facts []architecture.Fact) []architecture.Fact {
	bySubject := map[string]architecture.Fact{}
	for _, fact := range facts {
		if existing, ok := bySubject[fact.Subject]; !ok || fact.ID < existing.ID {
			bySubject[fact.Subject] = fact
		}
	}
	var out []architecture.Fact
	for _, subject := range sortedMapKeys(bySubject) {
		out = append(out, bySubject[subject])
	}
	return out
}

func sortedFactGroupKeys[V any](groups map[string]V) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func failureAssertionsByTest(facts []architecture.Fact) map[string]architecture.Fact {
	return assertionsByTest(facts, []string{"panic", "recover", "error", "fail", "reject", "invalid"})
}

func monotonicAssertionsByTest(facts []architecture.Fact) map[string]architecture.Fact {
	return assertionsByTest(facts, []string{"monotonic", "never decrease", "only increase", "must increase", "cannot reset", "must not reset", "once written"})
}

func assertionsByTest(facts []architecture.Fact, signals []string) map[string]architecture.Fact {
	out := map[string]architecture.Fact{}
	for _, fact := range facts {
		if fact.Kind != "assertion" || (fact.Predicate != "asserts_behavior_example" && fact.Predicate != "asserts_architectural_rule") {
			continue
		}
		text := strings.ToLower(fact.Object)
		matched := false
		for _, signal := range signals {
			if strings.Contains(text, signal) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if existing, ok := out[fact.Subject]; !ok || fact.ID < existing.ID {
			out[fact.Subject] = fact
		}
	}
	return out
}
