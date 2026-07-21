// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	tBoundaryIRI = rdf.AwNS + "boundary/boundary.dashboard_read_only_grpc"
	tRepo        = "github.com/globulario/sensei"
	tDomain      = "github.com/globulario/sensei"
	tAuthority   = "graph-authority-digest-abc"
	tCaller      = "component.dashboard"
	tCallee      = "component.awareness_graph_service"
	tContract    = "contract.control_snapshot_read"
	tAuthClass   = "class.reader"
)

func tTarget() closureprotocol.RuntimeTarget {
	return closureprotocol.RuntimeTarget{Platform: "linux", DeploymentID: "deploy-1"}
}

func tID(t *testing.T) (RuntimeBoundaryIdentity, BoundaryClassResolution) {
	t.Helper()
	id, res, err := BuildRuntimeBoundaryIdentity(
		tBoundaryIRI, []string{rdf.ClassBoundary}, "read_only", tRepo, tDomain, tAuthority, "registry-1",
		[]string{tContract}, []string{"invariant.dashboard_read_only"}, tCaller, tCallee, "authority.dashboard",
		LifecycleActive, true, true, true)
	if err != nil {
		t.Fatalf("build identity: %v", err)
	}
	return id, res
}

func tPolicy(t *testing.T) BoundaryPolicy {
	t.Helper()
	p, err := BuildBoundaryPolicy(BoundaryPolicy{
		PolicyID: "policy-1", BoundaryIRI: tBoundaryIRI,
		PermittedDirections:     []CrossingDirection{DirectionInbound},
		AllowedInteractionKinds: []InteractionKind{InteractionRead},
		AllowedCallers:          []string{tCaller},
		AllowedCallees:          []string{tCallee},
		AllowedAuthorityClasses: []string{tAuthClass},
		RequiredContract:        tContract,
		RequireAuthContext:      true,
		RuntimeProof:            ProofRequired,
		NextActionOwner:         "architect",
	})
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	return p
}

func tBinding(t *testing.T) RuntimeArchitectureBinding {
	t.Helper()
	b, err := BuildRuntimeArchitectureBinding(RuntimeArchitectureBinding{
		BindingID: "bind-1", BoundaryIRI: tBoundaryIRI, RepositoryIdentity: tRepo, DomainIdentity: tDomain,
		RuntimeTarget: tTarget(), MappedCallers: []string{tCaller}, MappedCallees: []string{tCallee},
		AuthorityGrantIdentity: "grant.binding-authority",
	})
	if err != nil {
		t.Fatalf("build binding: %v", err)
	}
	return b
}

func tObs() RuntimeObservation {
	return RuntimeObservation{
		ObservationID: "obs-1", SchemaVersion: ObservationSchema,
		Direction: DirectionInbound, CallerIdentity: tCaller, CalleeIdentity: tCallee,
		EndpointOrContractIdentity: tContract, InteractionKind: InteractionRead,
		AuthContextPresent: true, AuthorityClass: tAuthClass,
		RuntimeTarget: tTarget(), CollectorID: "collector.grpc-probe", CollectorVersion: "v1",
		EvidenceDigestSHA256: "abc123", Availability: SourceAvailable, Freshness: FreshnessFresh,
		IntegrityVerified: true,
	}
}

func tInput(t *testing.T) AssessmentInput {
	t.Helper()
	id, res := tID(t)
	pol := tPolicy(t)
	bind := tBinding(t)
	return AssessmentInput{
		Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind,
		Observations: []RuntimeObservation{tObs()}, CollectorAvailable: true,
	}
}

func assess(t *testing.T, in AssessmentInput) RuntimeBoundaryAssessment {
	t.Helper()
	a, err := AssessRuntimeBoundary(in)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if verr := ValidateAssessment(a); verr != nil {
		t.Fatalf("assessment failed self-validation: %v", verr)
	}
	return a
}

// ---- happy paths ----------------------------------------------------------

func TestSatisfied_FullConjunction(t *testing.T) {
	a := assess(t, tInput(t))
	if a.Verdict != VerdictSatisfied || a.ResultKind != KindObservedAuthorizedCrossing {
		t.Fatalf("want satisfied/authorized, got %s/%s", a.Verdict, a.ResultKind)
	}
	if a.Meta.Availability != AvailabilityAvailable || a.AdmissibleObservations != 1 {
		t.Fatalf("satisfied must be available with admissible evidence, got %s adm=%d", a.Meta.Availability, a.AdmissibleObservations)
	}
}

func TestViolated_ForbiddenCrossing(t *testing.T) {
	in := tInput(t)
	o := tObs()
	o.CallerIdentity = "component.evil" // not in AllowedCallers → forbidden
	// remap the binding so the crossing is still admitted (bound), then policy forbids it.
	b := tBinding(t)
	b.MappedCallers = nil // wildcard caller mapping so the evil caller is still admitted
	nb, err := BuildRuntimeArchitectureBinding(b)
	if err != nil {
		t.Fatal(err)
	}
	in.Binding = &nb
	in.Observations = []RuntimeObservation{o}
	a := assess(t, in)
	if a.Verdict != VerdictViolated || a.ResultKind != KindObservedForbiddenCrossing {
		t.Fatalf("want violated/forbidden, got %s/%s", a.Verdict, a.ResultKind)
	}
}

// ---- issue #88 adversarial proofs -----------------------------------------

// Proof 1: traffic alone cannot create/authorize a boundary — without a binding, observations are
// refused and never satisfy.
func TestProof_TrafficAloneCannotAuthorize(t *testing.T) {
	in := tInput(t)
	in.Binding = nil
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("traffic without a binding must never satisfy a boundary")
	}
	if a.RefusedObservations != 1 || !contains(a.RefusalReasons, "no_binding") {
		t.Fatalf("unbound observation must be refused (no_binding), got refused=%d reasons=%v", a.RefusedObservations, a.RefusalReasons)
	}
}

// Proof 2: missing telemetry never becomes compliant.
func TestProof_MissingTelemetryNeverCompliant(t *testing.T) {
	in := tInput(t)
	in.Observations = nil
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("missing telemetry must never be satisfied")
	}
	if a.ResultKind != KindRequiredEvidenceAbsent || a.Meta.Availability != AvailabilityUnavailable {
		t.Fatalf("required proof with no evidence → required_evidence_absent/unavailable, got %s/%s", a.ResultKind, a.Meta.Availability)
	}
}

// Proof 3: stale graph authority cannot yield satisfied.
func TestProof_StaleAuthorityNeverSatisfied(t *testing.T) {
	in := tInput(t)
	id, res := tID(t)
	// rebuild identity with authority not current
	id2, res2, err := BuildRuntimeBoundaryIdentity(id.BoundaryIRI, id.ObservedClasses, id.BoundaryKind, id.RepositoryIdentity, id.DomainIdentity, id.GraphAuthorityIdentity, id.RegistryIdentity, id.ProtectedContracts, id.ProtectedInvariants, id.SourceIdentity, id.DestinationIdentity, id.OwningAuthority, id.Lifecycle, id.RuntimeAssessable, id.IntegrityVerified, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	in.Identity, in.IdentityResolution = id2, res2
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("stale graph authority must never satisfy")
	}
	if a.ResultKind != KindCrossingStaleAuthority || a.Verdict != VerdictDegraded {
		t.Fatalf("want crossing_stale_authority/degraded, got %s/%s", a.ResultKind, a.Verdict)
	}
}

// Proof 5: an authorized crossing against the wrong contract does not satisfy.
func TestProof_WrongContractDoesNotSatisfy(t *testing.T) {
	in := tInput(t)
	o := tObs()
	o.EndpointOrContractIdentity = "contract.some_other"
	in.Observations = []RuntimeObservation{o}
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("authorized crossing against the wrong contract must not satisfy")
	}
	if a.ResultKind != KindEvidenceOutOfScope {
		t.Fatalf("want evidence_out_of_scope, got %s", a.ResultKind)
	}
}

// Proof 6: cross-repository/domain evidence is refused.
func TestProof_CrossDomainEvidenceRefused(t *testing.T) {
	in := tInput(t)
	b := tBinding(t)
	b.RepositoryIdentity = "github.com/other/repo" // out of scope
	nb, err := BuildRuntimeArchitectureBinding(b)
	if err != nil {
		t.Fatal(err)
	}
	in.Binding = &nb
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("cross-repository binding must never satisfy")
	}
	if !contains(a.RefusalReasons, "out_of_scope") {
		t.Fatalf("cross-repository evidence must be refused (out_of_scope), got %v", a.RefusalReasons)
	}
}

// Proof 7: ambiguous runtime identity remains unknown (refused, never guessed).
func TestProof_AmbiguousIdentityRefused(t *testing.T) {
	in := tInput(t)
	o := tObs()
	o.CalleeIdentity = "" // unresolved
	in.Observations = []RuntimeObservation{o}
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("ambiguous identity must never satisfy")
	}
	if !contains(a.RefusalReasons, "ambiguous_identity") {
		t.Fatalf("ambiguous identity must be refused, got %v", a.RefusalReasons)
	}
}

// Proof 8: truncated evidence cannot produce complete compliance.
func TestProof_TruncatedEvidenceNotComplete(t *testing.T) {
	in := tInput(t)
	o := tObs()
	o.Truncated = true
	in.Observations = []RuntimeObservation{o}
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("truncated evidence must not produce satisfied")
	}
	if a.ResultKind != KindEvidenceTruncated || a.Verdict != VerdictDegraded {
		t.Fatalf("want evidence_truncated/degraded, got %s/%s", a.ResultKind, a.Verdict)
	}
}

// Proof 9: conflicting evidence is preserved and never first-row-wins.
func TestProof_ConflictingEvidencePreserved(t *testing.T) {
	in := tInput(t)
	sat := tObs() // authorized (auth present)
	sat.ObservationID = "obs-sat"
	forb := tObs() // same crossing key, but no auth context → policy forbids
	forb.ObservationID = "obs-forb"
	forb.AuthContextPresent = false
	in.Observations = []RuntimeObservation{sat, forb}
	a := assess(t, in)
	if a.Verdict == VerdictSatisfied {
		t.Fatal("conflicting evidence must not first-row-win into satisfied")
	}
	if a.ResultKind != KindContradictoryObservations || a.Verdict != VerdictDegraded {
		t.Fatalf("want contradictory_observations/degraded, got %s/%s", a.ResultKind, a.Verdict)
	}
	if len(a.Conflicts) != 2 || !contains(a.Conflicts, "obs-sat") || !contains(a.Conflicts, "obs-forb") {
		t.Fatalf("both conflicting observations must be preserved, got %v", a.Conflicts)
	}
}

// Proof 10: collector outage is distinct from no-violation observed.
func TestProof_CollectorOutageDistinct(t *testing.T) {
	in := tInput(t)
	in.Observations = nil
	in.CollectorAvailable = false
	a := assess(t, in)
	if a.ResultKind != KindCollectorUnavailable {
		t.Fatalf("collector outage must be collector_unavailable, got %s", a.ResultKind)
	}
	// distinct from the collector-up-no-evidence case
	in.CollectorAvailable = true
	b := assess(t, in)
	if b.ResultKind == a.ResultKind {
		t.Fatal("collector outage must be distinct from required_evidence_absent")
	}
	if b.ResultKind != KindRequiredEvidenceAbsent {
		t.Fatalf("collector up + no evidence (required) → required_evidence_absent, got %s", b.ResultKind)
	}
}

// Proof 14: assessment is a pure function — same input → identical digest; input not mutated.
func TestProof_PureNoMutation(t *testing.T) {
	in := tInput(t)
	before, _ := digestOf(in.Observations[0])
	a1 := assess(t, in)
	a2 := assess(t, in)
	if a1.Meta.DigestSHA256 != a2.Meta.DigestSHA256 {
		t.Fatal("assessment is not deterministic")
	}
	after, _ := digestOf(in.Observations[0])
	if before != after {
		t.Fatal("assessment mutated its input observation")
	}
}

// not_applicable requires explicit policy — absence of policy is unknown, never not_applicable.
func TestProof_PolicyAbsenceIsUnknownNotNA(t *testing.T) {
	in := tInput(t)
	in.Policy = nil
	a := assess(t, in)
	if a.ResultKind != KindPolicyAbsent || a.Verdict != VerdictUnknown {
		t.Fatalf("absent policy → policy_absent/unknown, got %s/%s", a.ResultKind, a.Verdict)
	}
	// only an explicit unsupported policy yields not_applicable
	pol := tPolicy(t)
	pol.RuntimeProof = ProofUnsupported
	np, err := BuildBoundaryPolicy(pol)
	if err != nil {
		t.Fatal(err)
	}
	in.Policy = &np
	b := assess(t, in)
	if b.Verdict != VerdictNotApplicable || b.ResultKind != KindBoundaryNotAssessable {
		t.Fatalf("explicit unsupported proof → not_applicable, got %s/%s", b.Verdict, b.ResultKind)
	}
}

// A revoked boundary stays visible but is never satisfied.
func TestProof_RevokedBoundaryNotApplicable(t *testing.T) {
	in := tInput(t)
	id, res, err := BuildRuntimeBoundaryIdentity(tBoundaryIRI, []string{rdf.ClassBoundary}, "read_only", tRepo, tDomain, tAuthority, "registry-1", []string{tContract}, nil, tCaller, tCallee, "authority.dashboard", LifecycleRevoked, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	in.Identity, in.IdentityResolution = id, res
	a := assess(t, in)
	if a.Verdict != VerdictNotApplicable || a.ResultKind != KindBoundaryRevoked {
		t.Fatalf("revoked boundary → not_applicable/boundary_revoked, got %s/%s", a.Verdict, a.ResultKind)
	}
}

// A non-boundary node is invalid input, never assessed.
func TestProof_NonBoundaryIsInvalid(t *testing.T) {
	in := tInput(t)
	id, res, err := BuildRuntimeBoundaryIdentity(tBoundaryIRI, []string{rdf.ClassComponent}, "", tRepo, tDomain, tAuthority, "", nil, nil, "", "", "", LifecycleActive, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	in.Identity, in.IdentityResolution = id, res
	a := assess(t, in)
	if a.Verdict != VerdictInvalid || a.ResultKind != KindIdentityUnresolved {
		t.Fatalf("non-boundary → invalid/identity_unresolved, got %s/%s", a.Verdict, a.ResultKind)
	}
}

func contains(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
