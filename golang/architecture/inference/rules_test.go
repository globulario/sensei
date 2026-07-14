// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestDefaultRegistryHasStableSortedRuleIDs(t *testing.T) {
	reg, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(reg.RuleIDs(), "\n")
	want := strings.Join([]string{
		"rule.component_dependency_crossing.v1",
		"rule.exported_api_tested_behavior.v1",
		"rule.interface_implementation_surface.v1",
		"rule.observed_guard_behavior.v1",
		"rule.observed_writer_set.v1",
		"rule.rule_signaling_test_expectation.v1",
		"rule.shared_entrypoint_behavior_path.v1",
		"rule.tested_failure_boundary.v1",
		"rule.tested_monotonic_state.v1",
	}, "\n")
	if got != want {
		t.Fatalf("rule ids:\n%s\nwant:\n%s", got, want)
	}
}

func TestRegistryRejectsDuplicateRuleID(t *testing.T) {
	_, err := NewRegistry([]Rule{stubRule{id: "rule.x.v1", version: "v1"}, stubRule{id: "rule.x.v1", version: "v1"}})
	if err == nil {
		t.Fatal("expected duplicate rule rejection")
	}
}

func TestRegistryRejectsUnversionedRuleID(t *testing.T) {
	_, err := NewRegistry([]Rule{stubRule{id: "rule.x", version: ""}})
	if err == nil {
		t.Fatal("expected unversioned rule rejection")
	}
}

func TestRegistrySelectsRequestedRulesDeterministically(t *testing.T) {
	reg, _ := DefaultRegistry()
	rules, err := reg.Select([]string{"rule.rule_signaling_test_expectation.v1", "rule.observed_guard_behavior.v1", "rule.observed_guard_behavior.v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[0].Descriptor().ID; got != "rule.observed_guard_behavior.v1" {
		t.Fatalf("first rule=%s", got)
	}
	if len(rules) != 2 {
		t.Fatalf("len=%d", len(rules))
	}
}

func TestUnknownRuleSelectionFails(t *testing.T) {
	reg, _ := DefaultRegistry()
	if _, err := reg.Select([]string{"rule.missing.v1"}); err == nil {
		t.Fatal("expected unknown rule failure")
	}
}

func TestRuleDescriptorsContainLimitations(t *testing.T) {
	reg, _ := DefaultRegistry()
	for _, d := range reg.Descriptors() {
		if len(d.KnownLimitations) == 0 || d.ConfidencePolicy == "" || !d.HumanReviewRequired {
			t.Fatalf("incomplete descriptor: %#v", d)
		}
	}
}

func TestEngineOutputIsDeterministic(t *testing.T) {
	ctx := supportedContext([]architecture.Fact{
		testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.62),
		testFact("fact.write", "write", "svc.Apply", "writes", "state", 0.44),
	})
	reg, _ := DefaultRegistry()
	rules, _ := reg.Select(nil)
	a, err := NewEngine(rules).Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEngine(rules).Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	aj := renderApps(t, a)
	bj := renderApps(t, b)
	if !bytes.Equal(aj, bj) {
		t.Fatalf("nondeterministic apps:\n%s\n---\n%s", aj, bj)
	}
}

func TestEngineDoesNotMutateInputFacts(t *testing.T) {
	facts := []architecture.Fact{testFact("", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}
	before := facts[0].ID
	_, err := NewEngine([]Rule{ObservedGuardRule{}}).Apply(supportedContext(facts))
	if err != nil {
		t.Fatal(err)
	}
	if facts[0].ID != before {
		t.Fatal("input fact mutated")
	}
}

func TestEngineRejectsUnknownPremiseFact(t *testing.T) {
	_, err := NewEngine([]Rule{badRule{mutate: func(a *Application) { a.PremiseFactIDs = []string{"fact.missing"} }}}).Apply(supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if err == nil {
		t.Fatal("expected unknown premise rejection")
	}
}

func TestEngineRejectsRuleIDMismatch(t *testing.T) {
	_, err := NewEngine([]Rule{badRule{mutate: func(a *Application) { a.RuleID = "rule.other.v1" }}}).Apply(supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if err == nil {
		t.Fatal("expected rule id mismatch")
	}
}

func TestEngineRejectsNonDerivedClaim(t *testing.T) {
	_, err := NewEngine([]Rule{badRule{mutate: func(a *Application) { a.Claim.AssertionOrigin = architecture.OriginAuthored }}}).Apply(supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if err == nil {
		t.Fatal("expected non-derived rejection")
	}
}

func TestEngineRejectsNonCandidateClaim(t *testing.T) {
	_, err := NewEngine([]Rule{badRule{mutate: func(a *Application) { a.Claim.PromotionStatus = "active" }}}).Apply(supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if err == nil {
		t.Fatal("expected non-candidate rejection")
	}
}

func TestEngineRequiresHumanReview(t *testing.T) {
	_, err := NewEngine([]Rule{badRule{mutate: func(a *Application) { a.Claim.HumanReviewRequired = false }}}).Apply(supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if err == nil {
		t.Fatal("expected human-review rejection")
	}
}

func TestConservativeConfidenceUsesMinimumPremiseConfidence(t *testing.T) {
	got := ConservativeConfidence([]architecture.Fact{
		testFact("a", "write", "a", "writes", "state", 0.8),
		testFact("b", "write", "b", "writes", "state", 0.3),
	}, 0.55)
	if got != 0.3 {
		t.Fatalf("confidence=%v", got)
	}
}

func TestFactCountDoesNotBoostConfidence(t *testing.T) {
	got := ConservativeConfidence([]architecture.Fact{
		testFact("a", "write", "a", "writes", "state", 0.4),
		testFact("b", "write", "b", "writes", "state", 0.4),
		// same confidence repeated must not add confidence
		testFact("c", "write", "c", "writes", "state", 0.4),
	}, 0.55)
	if got != 0.4 {
		t.Fatalf("confidence=%v", got)
	}
}

func TestObservedGuardRuleEmitsObservedClaim(t *testing.T) {
	app := singleApp(t, ObservedGuardRule{}, supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.9)}))
	if app.Claim.Statement.Predicate != "refuses_when" || app.Claim.ArchitecturalPlane != architecture.PlaneObserved || app.Claim.Confidence != 0.65 {
		t.Fatalf("bad guard claim: %#v", app.Claim)
	}
}

func TestTransitionGuardRulePreservesTransitionPredicate(t *testing.T) {
	app := singleApp(t, ObservedGuardRule{}, supportedContext([]architecture.Fact{testFact("fact.transition", "transition", "svc.Move", "rejects_transition_when", "bad state", 0.6)}))
	if app.Claim.Statement.Predicate != "rejects_transition_when" {
		t.Fatalf("predicate=%s", app.Claim.Statement.Predicate)
	}
}

func TestObservedGuardRuleDoesNotUseSiblingTestAsPremise(t *testing.T) {
	ctx := supportedContext([]architecture.Fact{
		testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6),
		testFact("fact.test", "assertion", "svc.TestApplyRejectsBad", "asserts_architectural_rule", "apply rejects bad", 0.75),
	})
	app := singleApp(t, ObservedGuardRule{}, ctx)
	if len(app.Claim.PremiseFacts) != 1 || app.Claim.PremiseFacts[0] != "fact.guard" {
		t.Fatalf("guard premises=%v", app.Claim.PremiseFacts)
	}
}

func TestObservedGuardRuleDoesNotInferGlobalInvariant(t *testing.T) {
	app := singleApp(t, ObservedGuardRule{}, supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if app.Claim.AssertionOrigin != architecture.OriginDerived || app.Claim.PromotionStatus != architecture.PromotionCandidate {
		t.Fatalf("claim promoted or authored: %#v", app.Claim)
	}
}

func TestObservedGuardRuleCarriesBypassUnknown(t *testing.T) {
	app := singleApp(t, ObservedGuardRule{}, supportedContext([]architecture.Fact{testFact("fact.guard", "guard", "svc.Apply", "refuses_when", "bad", 0.6)}))
	if !containsText(app.Claim.Unknowns, "Bypass paths") {
		t.Fatalf("unknowns=%v", app.Claim.Unknowns)
	}
}

func TestRuleSignalingTestEmitsTestScopedClaim(t *testing.T) {
	app := singleApp(t, RuleSignalingTestExpectationRule{}, supportedContext([]architecture.Fact{testFact("fact.test", "assertion", "svc.TestRule", "asserts_architectural_rule", "writes are atomic", 0.75)}))
	if app.Claim.Statement.Predicate != "asserts_rule" || app.Claim.Statement.Subject != "svc.TestRule" {
		t.Fatalf("bad test claim: %#v", app.Claim.Statement)
	}
}

func TestRuleSignalingTestClaimUsesEnforcedPlane(t *testing.T) {
	app := singleApp(t, RuleSignalingTestExpectationRule{}, supportedContext([]architecture.Fact{testFact("fact.test", "assertion", "svc.TestRule", "asserts_architectural_rule", "writes are atomic", 0.75)}))
	if app.Claim.ArchitecturalPlane != architecture.PlaneEnforced {
		t.Fatalf("plane=%s", app.Claim.ArchitecturalPlane)
	}
}

func TestOrdinaryBehaviorExampleTestDoesNotMatchRule(t *testing.T) {
	apps, err := NewEngine([]Rule{RuleSignalingTestExpectationRule{}}).Apply(supportedContext([]architecture.Fact{testFact("fact.test", "assertion", "svc.TestExample", "asserts_behavior_example", "adds numbers", 0.35)}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("ordinary test matched: %#v", apps)
	}
}

func TestRuleSignalingTestDoesNotInferProductionGuard(t *testing.T) {
	app := singleApp(t, RuleSignalingTestExpectationRule{}, supportedContext([]architecture.Fact{testFact("fact.test", "assertion", "svc.TestRule", "asserts_architectural_rule", "writes are atomic", 0.75)}))
	if strings.Contains(app.Claim.Statement.Predicate, "refuses") {
		t.Fatalf("test inferred production guard: %#v", app.Claim.Statement)
	}
}

func TestObservedWriterSetRuleGroupsWritersByState(t *testing.T) {
	apps, err := NewEngine([]Rule{ObservedWriterSetRule{}}).Apply(supportedContext([]architecture.Fact{
		testFact("a", "write", "svc.A", "writes", "state", 0.5),
		testFact("b", "write", "svc.B", "writes", "other", 0.5),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Fatalf("apps=%d", len(apps))
	}
}

func TestObservedWriterSetRuleSortsAndDeduplicatesWriters(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{
		testFact("b", "write", "svc.B", "writes", "state", 0.5),
		testFact("a", "write", "svc.A", "writes", "state", 0.5),
		testFact("a2", "authority_observation", "svc.A", "mutates_state", "state", 0.5),
	}))
	if app.Claim.Statement.Object != "svc.A, svc.B" {
		t.Fatalf("writers=%q", app.Claim.Statement.Object)
	}
}

func TestObservedWriterSetRuleAcceptsWriteFacts(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{testFact("a", "write", "svc.A", "writes", "state", 0.5)}))
	if app.Claim.Statement.Subject != "state" {
		t.Fatalf("subject=%s", app.Claim.Statement.Subject)
	}
}

func TestObservedWriterSetRuleAcceptsAuthorityMutationFacts(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{testFact("a", "authority_observation", "svc.A", "mutates_state", "state", 0.5)}))
	if app.Claim.Statement.Subject != "state" {
		t.Fatalf("subject=%s", app.Claim.Statement.Subject)
	}
}

func TestObservedWriterSetRuleRejectsPersistsViaAsStateOwnership(t *testing.T) {
	apps, err := NewEngine([]Rule{ObservedWriterSetRule{}}).Apply(supportedContext([]architecture.Fact{testFact("a", "write", "svc.A", "persists_via", "Save", 0.5)}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("persists_via inferred writer set: %#v", apps)
	}
}

func TestSingleObservedWriterDoesNotInferSoleAuthority(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{testFact("a", "write", "svc.A", "writes", "state", 0.5)}))
	forbidden := app.Claim.Statement.Predicate + " " + app.Claim.Statement.Object
	if strings.Contains(forbidden, "sole_authority") || strings.Contains(forbidden, "only_writer") || strings.Contains(forbidden, "is_authoritative_for") {
		t.Fatalf("sole authority inferred: %#v", app.Claim)
	}
}

func TestMultipleObservedWritersDoNotAutoConflict(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{
		testFact("a", "write", "svc.A", "writes", "state", 0.5),
		testFact("b", "write", "svc.B", "writes", "state", 0.5),
	}))
	if app.Claim.EpistemicStatus == architecture.StatusContested || len(app.Claim.ConflictsWith) > 0 {
		t.Fatalf("auto conflict inferred: %#v", app.Claim)
	}
}

func TestMultipleObservedWritersCarryAlternativeExplanations(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{
		testFact("a", "write", "svc.A", "writes", "state", 0.5),
		testFact("b", "write", "svc.B", "writes", "state", 0.5),
	}))
	if !containsText(app.Claim.AlternativeExplanations, "delegated mutation") {
		t.Fatalf("alternatives=%v", app.Claim.AlternativeExplanations)
	}
}

func TestObservedWriterSetConfidenceUsesWeakestPremise(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{
		testFact("a", "write", "svc.A", "writes", "state", 0.5),
		testFact("b", "write", "svc.B", "writes", "state", 0.2),
	}))
	if app.Claim.Confidence != 0.2 {
		t.Fatalf("confidence=%v", app.Claim.Confidence)
	}
}

func TestMissingGraphDigestProducesUnknownClaim(t *testing.T) {
	ctx := supportedContext([]architecture.Fact{testFact("a", "write", "svc.A", "writes", "state", 0.5)})
	ctx.Binding.GraphDigestSHA256 = ""
	ctx.Binding.GraphDigestStatus = architecture.GraphDigestNotRequested
	app := singleApp(t, ObservedWriterSetRule{}, ctx)
	if app.Claim.EpistemicStatus != architecture.StatusUnknown {
		t.Fatalf("status=%s", app.Claim.EpistemicStatus)
	}
}

func TestResolvedBindingCanProduceSupportedClaim(t *testing.T) {
	app := singleApp(t, ObservedWriterSetRule{}, supportedContext([]architecture.Fact{testFact("a", "write", "svc.A", "writes", "state", 0.5)}))
	if app.Claim.EpistemicStatus != architecture.StatusSupported {
		t.Fatalf("status=%s", app.Claim.EpistemicStatus)
	}
}

func TestInferenceNeverEmitsStaleWithoutPriorState(t *testing.T) {
	app := singleApp(t, ObservedGuardRule{}, supportedContext([]architecture.Fact{testFact("a", "guard", "svc.A", "refuses_when", "bad", 0.5)}))
	if app.Claim.EpistemicStatus == architecture.StatusStale {
		t.Fatal("emitted stale")
	}
}

func TestGraphDigestDoesNotChangeClaimID(t *testing.T) {
	facts := []architecture.Fact{testFact("a", "guard", "svc.A", "refuses_when", "bad", 0.5)}
	a := singleApp(t, ObservedGuardRule{}, supportedContext(facts))
	ctx := supportedContext(facts)
	ctx.Binding.GraphDigestSHA256 = strings.Repeat("b", 64)
	b := singleApp(t, ObservedGuardRule{}, ctx)
	if a.Claim.ID != b.Claim.ID {
		t.Fatalf("graph digest changed id: %s != %s", a.Claim.ID, b.Claim.ID)
	}
}

func TestBuildClaimDocumentIncludesOnlyUsedFactReceipts(t *testing.T) {
	used := testFact("used", "guard", "svc.A", "refuses_when", "bad", 0.5)
	unused := testFact("unused", "write", "svc.B", "writes", "state", 0.5)
	ctx := supportedContext([]architecture.Fact{used, unused})
	app := singleApp(t, ObservedGuardRule{}, ctx)
	doc, err := BuildClaimDocument(ctx, []Application{app})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.FactReceipts) != 1 || doc.FactReceipts[0].Fact.ID != "used" {
		t.Fatalf("receipts=%#v", doc.FactReceipts)
	}
}

func TestBuildClaimDocumentRejectsNilPremiseProvenanceAsSupported(t *testing.T) {
	f := testFact("used", "guard", "svc.A", "refuses_when", "bad", 0.5)
	f.Provenance = nil
	ctx := supportedContext([]architecture.Fact{f})
	app := singleApp(t, ObservedGuardRule{}, ctx)
	doc, err := BuildClaimDocument(ctx, []Application{app})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Claims[0].EpistemicStatus != architecture.StatusUnknown {
		t.Fatalf("status=%s", doc.Claims[0].EpistemicStatus)
	}
}

func TestCanonicalClaimYAMLIsDeterministic(t *testing.T) {
	ctx := supportedContext([]architecture.Fact{testFact("used", "guard", "svc.A", "refuses_when", "bad", 0.5)})
	app := singleApp(t, ObservedGuardRule{}, ctx)
	doc, err := BuildClaimDocument(ctx, []Application{app})
	if err != nil {
		t.Fatal(err)
	}
	a, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || !bytes.Contains(a, []byte("architecture_claims:")) {
		t.Fatalf("bad yaml:\n%s", a)
	}
}

func TestPublicAPIRuleRequiresExportAndTest(t *testing.T) {
	exported := testFact("export", "export", "api.Serve", "exports_symbol", "function", 0.98)
	apps, err := NewEngine([]Rule{ExportedAPITestedBehaviorRule{}}).Apply(supportedContext([]architecture.Fact{exported}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("export without direct test produced claim: %#v", apps)
	}
	call := testFact("call", "test_call", "api.TestServe", "test_calls_symbol", "api.Serve", 0.96)
	app := singleApp(t, ExportedAPITestedBehaviorRule{}, supportedContext([]architecture.Fact{call, exported}))
	if len(app.Claim.PremiseFacts) != 2 || app.Claim.Statement.Predicate != "has_enforced_behavioral_surface" {
		t.Fatalf("public API claim lacks exact premises: %#v", app.Claim)
	}
}

func TestPublicAPIRuleDoesNotInferCompatibility(t *testing.T) {
	app := singleApp(t, ExportedAPITestedBehaviorRule{}, supportedContext([]architecture.Fact{
		testFact("export", "export", "api.Serve", "exports_symbol", "function", 0.98),
		testFact("call", "test_call", "api.TestServe", "test_calls_symbol", "api.Serve", 0.96),
	}))
	statement := app.Claim.Statement.Subject + " " + app.Claim.Statement.Predicate + " " + app.Claim.Statement.Object
	if strings.Contains(strings.ToLower(statement), "compatib") {
		t.Fatalf("compatibility commitment inferred: %#v", app.Claim.Statement)
	}
	if !containsText(app.Claim.Unknowns, "compatibility guarantee") {
		t.Fatalf("compatibility uncertainty missing: %v", app.Claim.Unknowns)
	}
}

func TestInterfaceRuleDoesNotInventSemantics(t *testing.T) {
	app := singleApp(t, InterfaceImplementationSurfaceRule{}, supportedContext([]architecture.Fact{
		testFact("interface", "interface", "api.writer", "implements_interface", "api.Writer", 0.98),
	}))
	if app.Claim.Statement.Predicate != "participates_in_interface_boundary" || app.Claim.Statement.Object != "api.Writer" {
		t.Fatalf("interface semantics invented: %#v", app.Claim.Statement)
	}
	if !containsText(app.Claim.Unknowns, "exact behavioral semantics") {
		t.Fatalf("semantic uncertainty missing: %v", app.Claim.Unknowns)
	}
}

func TestComponentCrossingRuleDoesNotCreateContract(t *testing.T) {
	app := singleApp(t, ComponentDependencyCrossingRule{}, supportedContext([]architecture.Fact{
		testFact("dependency", "component_dependency", "component.api", "component_depends_on_component", "component.internal", 0.98),
	}))
	if strings.Contains(strings.ToLower(app.Claim.Statement.Predicate), "contract") || strings.Contains(strings.ToLower(app.Claim.Statement.Object), "contract") {
		t.Fatalf("component dependency became Contract: %#v", app.Claim.Statement)
	}
	if app.Claim.ArchitecturalPlane != architecture.PlaneObserved {
		t.Fatalf("component dependency plane=%s", app.Claim.ArchitecturalPlane)
	}
}

func TestSharedEntrypointRuleRequiresCallPath(t *testing.T) {
	one := testFact("one", "reachability", "api.ServeHTTP", "entrypoint_reaches_symbol", "api.resolve", 0.9)
	apps, err := NewEngine([]Rule{SharedEntrypointBehaviorPathRule{}}).Apply(supportedContext([]architecture.Fact{one}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("one entrypoint became shared path: %#v", apps)
	}
	two := testFact("two", "reachability", "api.HandleContext", "entrypoint_reaches_symbol", "api.resolve", 0.9)
	app := singleApp(t, SharedEntrypointBehaviorPathRule{}, supportedContext([]architecture.Fact{two, one}))
	if len(app.Claim.PremiseFacts) != 2 || app.Claim.Statement.Object != "api.HandleContext, api.ServeHTTP" {
		t.Fatalf("shared path claim=%#v", app.Claim)
	}
}

func TestFailureBoundaryRuleUsesExactTestExpectation(t *testing.T) {
	assertion := testFact("assertion", "assertion", "api.TestMustRecoverPanic", "asserts_architectural_rule", "Must recover panic", 0.75)
	wrongCall := testFact("wrong-call", "test_call", "api.TestOther", "test_calls_symbol", "api.Serve", 0.96)
	apps, err := NewEngine([]Rule{TestedFailureBoundaryRule{}}).Apply(supportedContext([]architecture.Fact{assertion, wrongCall}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("unrelated test expectation used as premise: %#v", apps)
	}
	call := testFact("call", "test_call", "api.TestMustRecoverPanic", "test_calls_symbol", "api.Serve", 0.96)
	app := singleApp(t, TestedFailureBoundaryRule{}, supportedContext([]architecture.Fact{assertion, call}))
	if app.Claim.Statement.Subject != "api.Serve" || len(app.Claim.PremiseFacts) != 2 {
		t.Fatalf("failure boundary lacks exact production/test receipts: %#v", app.Claim)
	}
}

func TestMonotonicRuleRequiresMultipleExplicitPremises(t *testing.T) {
	increment := testFact("increment", "generation_check", "state.Advance", "increments_generation", "generation", 0.65)
	call := testFact("call", "test_call", "state.TestGenerationMustIncrease", "test_calls_symbol", "state.Advance", 0.96)
	assertion := testFact("assertion", "assertion", "state.TestGenerationMustIncrease", "asserts_architectural_rule", "Generation must increase", 0.75)
	for _, facts := range [][]architecture.Fact{{increment}, {increment, call}, {increment, assertion}} {
		apps, err := NewEngine([]Rule{TestedMonotonicStateRule{}}).Apply(supportedContext(facts))
		if err != nil {
			t.Fatal(err)
		}
		if len(apps) != 0 {
			t.Fatalf("partial monotonic premises produced claim: %#v", apps)
		}
	}
	app := singleApp(t, TestedMonotonicStateRule{}, supportedContext([]architecture.Fact{assertion, increment, call}))
	if len(app.Claim.PremiseFacts) != 3 || app.Claim.Statement.Predicate != "has_tested_monotonic_transition" {
		t.Fatalf("monotonic claim=%#v", app.Claim)
	}
}

func TestNewRulesProduceStableIDs(t *testing.T) {
	facts := []architecture.Fact{
		testFact("export", "export", "api.Serve", "exports_symbol", "function", 0.98),
		testFact("call", "test_call", "api.TestServe", "test_calls_symbol", "api.Serve", 0.96),
	}
	first := singleApp(t, ExportedAPITestedBehaviorRule{}, supportedContext(facts))
	second := singleApp(t, ExportedAPITestedBehaviorRule{}, supportedContext([]architecture.Fact{facts[1], facts[0]}))
	if first.Claim.ID != second.Claim.ID {
		t.Fatalf("stable claim id changed with premise order: %s != %s", first.Claim.ID, second.Claim.ID)
	}
}

type stubRule struct {
	id      string
	version string
}

func (s stubRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: s.id, Version: s.version, HumanReviewRequired: true}
}
func (s stubRule) Apply(Context) ([]Application, error) { return nil, nil }

type badRule struct {
	mutate func(*Application)
}

func (badRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "rule.bad.v1", Version: "v1", HumanReviewRequired: true}
}
func (b badRule) Apply(ctx Context) ([]Application, error) {
	apps, err := ObservedGuardRule{}.Apply(ctx)
	if err != nil || len(apps) == 0 {
		return apps, err
	}
	b.mutate(&apps[0])
	return apps, nil
}

func testFact(id, kind, subject, predicate, object string, confidence float64) architecture.Fact {
	return architecture.Fact{
		ID:        id,
		Kind:      kind,
		Subject:   subject,
		Predicate: predicate,
		Object:    object,
		Scope: architecture.Scope{
			Repository: "github.com/example/project",
			Files:      []string{"state.go"},
			Symbols:    []string{subject},
		},
		Evidence:   architecture.Evidence{SourceFile: "state.go", LineStart: 1, LineEnd: 2},
		Confidence: confidence,
		Extractor:  "test_extractor",
		Provenance: &architecture.Provenance{
			RepositoryDomain:       "github.com/example/project",
			RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision:               "abc123",
			RevisionStatus:         architecture.RevisionResolved,
			SourceDigest:           "digest",
			SourceDigestStatus:     architecture.SourceDigestResolved,
			SourceKind:             "source_file",
		},
	}
}

func supportedContext(facts []architecture.Fact) Context {
	return Context{
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: strings.Repeat("a", 64),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Facts: facts,
	}
}

func singleApp(t *testing.T, rule Rule, ctx Context) Application {
	t.Helper()
	apps, err := NewEngine([]Rule{rule}).Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps=%d: %#v", len(apps), apps)
	}
	return apps[0]
}

func renderApps(t *testing.T, apps []Application) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, app := range apps {
		b.WriteString(app.RuleID)
		b.WriteByte('|')
		b.WriteString(app.GroupKey)
		b.WriteByte('|')
		b.WriteString(app.Claim.ID)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func containsText(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}
