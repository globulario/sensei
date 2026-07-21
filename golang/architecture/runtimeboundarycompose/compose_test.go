// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundarycompose

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	cs "github.com/globulario/sensei/golang/architecture/controlstate"
	rb "github.com/globulario/sensei/golang/architecture/runtimeboundary"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	tBoundaryIRI = rdf.AwNS + "boundary/boundary.dashboard_read_only_grpc"
	tRepo        = "github.com/globulario/sensei"
	tCaller      = "component.dashboard"
	tCallee      = "component.awareness_graph_service"
	tContract    = "contract.control_snapshot_read"
)

func tID(t *testing.T, lifecycle rb.LifecycleState, current, integrity, assessable bool) (rb.RuntimeBoundaryIdentity, rb.BoundaryClassResolution) {
	t.Helper()
	id, res, err := rb.BuildRuntimeBoundaryIdentity(tBoundaryIRI, []string{rdf.ClassBoundary}, "read_only",
		tRepo, tRepo, "graph-authority-abc", "reg-1", []string{tContract}, nil, tCaller, tCallee,
		"authority.dashboard", lifecycle, assessable, integrity, current)
	if err != nil {
		t.Fatal(err)
	}
	return id, res
}

func tPolicy(t *testing.T, proof rb.RuntimeProof) rb.BoundaryPolicy {
	t.Helper()
	p, err := rb.BuildBoundaryPolicy(rb.BoundaryPolicy{
		PolicyID: "pol-1", BoundaryIRI: tBoundaryIRI,
		PermittedDirections: []rb.CrossingDirection{rb.DirectionInbound},
		AllowedCallers:      []string{tCaller}, AllowedCallees: []string{tCallee},
		RequiredContract: tContract, RuntimeProof: proof, NextActionOwner: "architect",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func tBinding(t *testing.T) rb.RuntimeArchitectureBinding {
	t.Helper()
	b, err := rb.BuildRuntimeArchitectureBinding(rb.RuntimeArchitectureBinding{
		BindingID: "bind-1", BoundaryIRI: tBoundaryIRI, RepositoryIdentity: tRepo,
		RuntimeTarget: closureprotocol.RuntimeTarget{Platform: "linux"},
		MappedCallers: []string{tCaller}, MappedCallees: []string{tCallee},
		AuthorityGrantIdentity: "grant.x",
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tObs(caller string) rb.RuntimeObservation {
	return rb.RuntimeObservation{
		ObservationID: "obs-1", SchemaVersion: rb.ObservationSchema, Direction: rb.DirectionInbound,
		CallerIdentity: caller, CalleeIdentity: tCallee, EndpointOrContractIdentity: tContract,
		InteractionKind: rb.InteractionRead, RuntimeTarget: closureprotocol.RuntimeTarget{Platform: "linux"},
		CollectorID: "collector.x", EvidenceDigestSHA256: "ev1", Availability: rb.SourceAvailable,
		Freshness: rb.FreshnessFresh, IntegrityVerified: true,
	}
}

// assess produces an owner assessment of the requested verdict (via the real owner, never faked).
func assess(t *testing.T, verdict rb.Verdict) rb.RuntimeBoundaryAssessment {
	t.Helper()
	in := rb.AssessmentInput{CollectorAvailable: true}
	switch verdict {
	case rb.VerdictSatisfied:
		id, res := tID(t, rb.LifecycleActive, true, true, true)
		pol, bind := tPolicy(t, rb.ProofRequired), tBinding(t)
		in.Identity, in.IdentityResolution, in.Policy, in.Binding = id, res, &pol, &bind
		in.Observations = []rb.RuntimeObservation{tObs(tCaller)}
	case rb.VerdictViolated:
		id, res := tID(t, rb.LifecycleActive, true, true, true)
		pol, bind := tPolicy(t, rb.ProofRequired), tBinding(t)
		b := bind
		b.MappedCallers = nil // wildcard so the forbidden caller is admitted then policy forbids it
		nb, err := rb.BuildRuntimeArchitectureBinding(b)
		if err != nil {
			t.Fatal(err)
		}
		in.Identity, in.IdentityResolution, in.Policy, in.Binding = id, res, &pol, &nb
		in.Observations = []rb.RuntimeObservation{tObs("component.evil")}
	case rb.VerdictDegraded:
		id, res := tID(t, rb.LifecycleActive, false, true, true) // stale authority → degraded
		pol, bind := tPolicy(t, rb.ProofRequired), tBinding(t)
		in.Identity, in.IdentityResolution, in.Policy, in.Binding = id, res, &pol, &bind
	case rb.VerdictNotApplicable:
		id, res := tID(t, rb.LifecycleActive, true, true, true)
		pol := tPolicy(t, rb.ProofUnsupported)
		in.Identity, in.IdentityResolution, in.Policy = id, res, &pol
	case rb.VerdictInvalid:
		id, res, err := rb.BuildRuntimeBoundaryIdentity(tBoundaryIRI, []string{rdf.ClassComponent}, "",
			tRepo, tRepo, "auth", "", nil, nil, "", "", "", rb.LifecycleActive, true, true, true)
		if err != nil {
			t.Fatal(err)
		}
		in.Identity, in.IdentityResolution = id, res
	default: // unknown / unavailable
		id, res := tID(t, rb.LifecycleActive, true, true, true)
		pol, bind := tPolicy(t, rb.ProofRequired), tBinding(t)
		in.Identity, in.IdentityResolution, in.Policy, in.Binding = id, res, &pol, &bind
		// no observations → required_evidence_absent → unavailable
	}
	a, err := rb.AssessRuntimeBoundary(in)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// buildBoundaryState composes an assessment's runtime dimension into a full controlstate artifact
// state, exercising the exact production path (registry + policy + BuildArtifactState).
func buildBoundaryState(t *testing.T, a rb.RuntimeBoundaryAssessment) cs.ArtifactState {
	t.Helper()
	obs, err := ToDimensionObservation(a)
	if err != nil {
		t.Fatalf("ToDimensionObservation: %v", err)
	}
	reg := cs.DefaultRegistry()
	id, res, err := cs.BuildArtifactIdentity(reg, tBoundaryIRI, []string{rdf.ClassBoundary}, tRepo, tRepo, "graph-auth", nil)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	bundle := cs.ArtifactSourceBundle{
		GraphAuthority: cs.GraphAuthorityObservation{Identity: "graph-auth", Observed: true, Current: true, Integrity: true},
		Contradiction:  cs.ContradictionSource{Owner: "extractor.contradiction", Schema: "contradiction/v1", Identity: "contra.src", Availability: cs.SourceAvailable},
		Dimensions:     map[string]cs.DimensionObservation{DimensionKey: obs},
	}
	st, err := cs.BuildArtifactState(reg, id, res, bundle)
	if err != nil {
		t.Fatalf("BuildArtifactState: %v", err)
	}
	return st
}

func runtimeDim(t *testing.T, st cs.ArtifactState) cs.DimensionAssessment {
	t.Helper()
	for _, d := range st.Dimensions {
		if d.Dimension == DimensionKey {
			return d
		}
	}
	t.Fatal("runtime dimension missing from boundary artifact state")
	return cs.DimensionAssessment{}
}

// Proof 1: every verdict maps to its dimension state verbatim through the real composition.
func TestVerbatim_VerdictToDimensionState(t *testing.T) {
	cases := []struct {
		verdict rb.Verdict
		state   cs.DimensionState
	}{
		{rb.VerdictSatisfied, cs.DimSatisfied},
		{rb.VerdictViolated, cs.DimOpen},
		{rb.VerdictDegraded, cs.DimDegraded},
		{rb.VerdictUnavailable, cs.DimUnknown},
		{rb.VerdictNotApplicable, cs.DimNotApplicable},
		{rb.VerdictInvalid, cs.DimUnknown},
	}
	for _, c := range cases {
		a := assess(t, c.verdict)
		if a.Verdict != c.verdict {
			t.Fatalf("owner produced %s, wanted %s", a.Verdict, c.verdict)
		}
		st := buildBoundaryState(t, a)
		d := runtimeDim(t, st)
		if d.State != c.state {
			t.Fatalf("verdict %s → dimension %s, wanted %s", c.verdict, d.State, c.state)
		}
		if d.Owner != SourceOwner {
			t.Fatalf("runtime dimension owner must be %q, got %q", SourceOwner, d.Owner)
		}
	}
}

// Proof 2: a violation's attention severity is OWNER-supplied (critical), not governed.
func TestOwnerSeverity_ViolationIsCritical(t *testing.T) {
	st := buildBoundaryState(t, assess(t, rb.VerdictViolated))
	var found bool
	for _, at := range st.Attention {
		if at.AttentionClass == cs.AttnRuntimeBoundaryViolated {
			found = true
			if at.Severity != cs.SeverityCritical || at.SeverityBasis != "source_severity" {
				t.Fatalf("runtime violation must carry OWNER critical severity, got %s/%s", at.Severity, at.SeverityBasis)
			}
		}
	}
	if !found {
		t.Fatal("a runtime violation must raise a runtime_boundary_violated attention item")
	}
}

// Proof 3: not_applicable is a typed dimension outcome, never absence-inferred.
func TestNotApplicable_IsTyped(t *testing.T) {
	d := runtimeDim(t, buildBoundaryState(t, assess(t, rb.VerdictNotApplicable)))
	if d.State != cs.DimNotApplicable {
		t.Fatalf("not-runtime-assessable boundary must be a typed not_applicable, got %s", d.State)
	}
}

// Proof 4: re-assessment supersedes (new digest) — the dimension rebinds to the new assessment.
func TestReassessment_Supersedes(t *testing.T) {
	a1 := assess(t, rb.VerdictViolated)
	a2 := assess(t, rb.VerdictSatisfied)
	if a1.Meta.DigestSHA256 == a2.Meta.DigestSHA256 {
		t.Fatal("distinct assessments must have distinct digests")
	}
	o1, _ := ToDimensionObservation(a1)
	o2, _ := ToDimensionObservation(a2)
	if o1.SourceDigest == o2.SourceDigest {
		t.Fatal("the runtime dimension must rebind its source digest to the current assessment")
	}
	if o1.SourceDigest != a1.Meta.DigestSHA256 || o2.SourceDigest != a2.Meta.DigestSHA256 {
		t.Fatal("the dimension source digest must equal the assessment digest verbatim")
	}
}

// Proof: an invalid assessment cannot be composed (fail closed).
func TestInvalidAssessment_Refused(t *testing.T) {
	if _, err := ToDimensionObservation(rb.RuntimeBoundaryAssessment{}); err == nil {
		t.Fatal("composing a malformed assessment must fail")
	}
}

// Proof: determinism — the same assessment always yields the same dimension observation.
func TestDeterminism(t *testing.T) {
	a := assess(t, rb.VerdictViolated)
	o1, _ := ToDimensionObservation(a)
	o2, _ := ToDimensionObservation(a)
	if o1.SourceDigest != o2.SourceDigest || o1.Outcome != o2.Outcome || o1.SourceSeverity != o2.SourceSeverity {
		t.Fatal("composition must be deterministic")
	}
}
