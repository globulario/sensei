// SPDX-License-Identifier: Apache-2.0

package plane

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/rdf"
)

func TestObservedGuardFactJustifiesObservedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, "")
	requireState(t, report, StateJustified)
	requireReason(t, report, "plane.observed.fact_basis")
}

func TestObservedWriterFactJustifiesObservedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.write", "authority_observation", "mutates_state")}, "")
	requireState(t, report, StateJustified)
}

func TestReachabilityFactJustifiesObservedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.reach", "reachability", "entrypoint_reaches_symbol")}, "")
	requireState(t, report, StateJustified)
	requireReason(t, report, "plane.observed.fact_basis")
}

func TestStaticTestTopologyJustifiesObservedButNotEnforced(t *testing.T) {
	observed := assessOne(t, architecture.PlaneObserved, []architecture.Fact{
		fact("fact.export", "export", "exports_symbol"),
		fact("fact.call", "test_call", "test_calls_symbol"),
	}, "")
	requireState(t, observed, StateJustified)

	enforced := assessOne(t, architecture.PlaneEnforced, []architecture.Fact{
		fact("fact.export", "export", "exports_symbol"),
		fact("fact.call", "test_call", "test_calls_symbol"),
	}, "")
	requireState(t, enforced, StateUnderSupported)
	requireReason(t, enforced, "plane.enforced.missing_basis")
}

func TestObservedRuntimeEvidenceRequiresCurrentEvidence(t *testing.T) {
	report := assessWithEvidence(t, architecture.PlaneObserved, []string{"evidence:runtime.ok"}, graphRuntimeEvidenceNT("runtime.ok"), evidenceDoc("runtime.ok", maintenance.EvidenceStatusPass, maintenance.EvidenceFreshnessCurrent))
	requireState(t, report, StateJustified)
	requireReason(t, report, "plane.observed.runtime_basis")
}

func TestIntentDoesNotJustifyObservedPlane(t *testing.T) {
	TestObservedPolicyRejectsAuthoredProseOnly(t)
}

func TestDecisionDoesNotJustifyObservedPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneObserved, []string{"decision:d.active"}, nil, graphNT(t, "decision", "d.active", "active", ""))
	requireState(t, report, StateInvalid)
}

func TestDocumentationClaimAloneDoesNotJustifyObservedPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneObserved, []string{"contract:c.active"}, nil, graphNT(t, "contract", "c.active", "active", ""))
	requireState(t, report, StateInvalid)
}

func TestAssertionFactJustifiesEnforcedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneEnforced, []architecture.Fact{fact("fact.assert", "assertion", "asserts_architectural_rule")}, "")
	requireState(t, report, StateJustified)
	requireReason(t, report, "plane.enforced.test_fact_basis")
}

func TestCIGateFactJustifiesEnforcedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneEnforced, []architecture.Fact{fact("fact.gate", "ci_gate", "rejects")}, "")
	requireState(t, report, StateJustified)
	requireReason(t, report, "plane.enforced.ci_gate_basis")
}

func TestCurrentTestEvidenceJustifiesEnforcedPlane(t *testing.T) {
	report := assessWithEvidence(t, architecture.PlaneEnforced, []string{"evidence:e.test"}, graphEvidenceNT("e.test", true), evidenceDoc("e.test", maintenance.EvidenceStatusPass, maintenance.EvidenceFreshnessCurrent))
	requireState(t, report, StateJustified)
}

func TestTestNodeWithoutPassEvidenceDoesNotJustifyEnforcedPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneEnforced, []string{"test:pkg.Test"}, nil, graphNT(t, "test", "pkg.Test", "", ""))
	requireState(t, report, StateInvalid)
}

func TestSiblingTestProximityDoesNotJustifyEnforcedPlane(t *testing.T) {
	TestTestNodeWithoutPassEvidenceDoesNotJustifyEnforcedPlane(t)
}

func TestSourceGuardAloneDoesNotJustifyEnforcedPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneEnforced, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, "")
	requireState(t, report, StateInvalid)
}

func TestActiveInvariantJustifiesIntendedPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneIntended, []string{"invariant:i.active"}, nil, graphNT(t, "invariant", "i.active", "active", "")), StateJustified)
}

func TestActiveContractJustifiesIntendedPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneIntended, []string{"contract:c.active"}, nil, graphNT(t, "contract", "c.active", "active", "")), StateJustified)
}

func TestActiveDecisionJustifiesIntendedPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneIntended, []string{"decision:d.active"}, nil, graphNT(t, "decision", "d.active", "active", "")), StateJustified)
}

func TestActiveIntentJustifiesIntendedPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneIntended, []string{"intent:i.active"}, nil, graphNT(t, "intent", "i.active", "active", "")), StateJustified)
}

func TestMissingGovernedNodeMakesIntendedUnderSupported(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneIntended, nil, nil, graphNT(t, "test", "pkg.Test", "", ""))
	requireState(t, report, StateUnderSupported)
}

func TestSupersededDecisionDoesNotJustifyIntendedPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneIntended, []string{"decision:d.old"}, nil, graphNT(t, "decision", "d.old", "superseded", ""))
	requireState(t, report, StateInvalid)
}

func TestArchitectAnswerDoesNotJustifyIntendedPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneIntended, []string{"architect_answer:a1"}, nil, "")
	requireState(t, report, StateUnknown)
}

func TestSupersededDecisionJustifiesHistoricalPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneHistorical, []string{"decision:d.old"}, nil, graphNT(t, "decision", "d.old", "superseded", ""))
	requireState(t, report, StateJustified)
}

func TestExplicitHistoricalAnnotationJustifiesHistoricalPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneHistorical, []string{"contract:c.old"}, nil, graphNT(t, "contract", "c.old", "historical", architecture.PlaneHistorical))
	requireState(t, report, StateJustified)
}

func TestHistoricalEvidenceJustifiesHistoricalPlane(t *testing.T) {
	report := assessWithEvidence(t, architecture.PlaneHistorical, []string{"evidence:e.old"}, graphEvidenceNT("e.old", false), evidenceDoc("e.old", maintenance.EvidenceStatusPass, maintenance.EvidenceFreshnessHistorical))
	requireState(t, report, StateJustified)
}

func TestOldTimestampAloneDoesNotJustifyHistoricalPlane(t *testing.T) {
	nt := graphNT(t, "evidence", "e.old", "active", "") + ntLit("evidence", "e.old", rdf.PropLastValidatedAt, "2020-01-01T00:00:00Z")
	report := assessOneWithAbout(t, architecture.PlaneHistorical, []string{"evidence:e.old"}, nil, nt)
	requireState(t, report, StateUnderSupported)
}

func TestActiveIntendedNodeDoesNotJustifyHistoricalPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneHistorical, []string{"intent:i.active"}, nil, graphNT(t, "intent", "i.active", "active", ""))
	requireState(t, report, StateInvalid)
}

func TestExplicitDesiredIntentJustifiesDesiredPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneDesired, []string{"intent:i.want"}, nil, graphNT(t, "intent", "i.want", "active", architecture.PlaneDesired)), StateJustified)
}

func TestExplicitDesiredDecisionJustifiesDesiredPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneDesired, []string{"decision:d.want"}, nil, graphNT(t, "decision", "d.want", "active", architecture.PlaneDesired)), StateJustified)
}

func TestExplicitDesiredContractJustifiesDesiredPlane(t *testing.T) {
	requireState(t, assessOneWithAbout(t, architecture.PlaneDesired, []string{"contract:c.want"}, nil, graphNT(t, "contract", "c.want", "active", architecture.PlaneDesired)), StateJustified)
}

func TestVisionIntentWithoutExplicitPlaneDoesNotJustifyDesired(t *testing.T) {
	TestDesiredPolicyRejectsActiveIntentWithoutExplicitDesired(t)
}

func TestSourceCodeDoesNotJustifyDesiredPlane(t *testing.T) {
	report := assessOne(t, architecture.PlaneDesired, []architecture.Fact{fact("fact.write", "write", "writes")}, "")
	requireState(t, report, StateInvalid)
}

func TestTestDoesNotJustifyDesiredPlane(t *testing.T) {
	c := claim("claim.one", architecture.PlaneDesired, []string{"fact.assert"}, nil, nil)
	report := assessDoc(t, claimDoc(t, []architecture.Claim{c}, []architecture.Fact{fact("fact.assert", "assertion", "asserts_architectural_rule")}), "")
	requireState(t, report, StateUnderSupported)
}

func TestRuntimeObservationDoesNotJustifyDesiredPlane(t *testing.T) {
	report := assessWithEvidence(t, architecture.PlaneDesired, []string{"evidence:runtime.ok"}, graphRuntimeEvidenceNT("runtime.ok"), evidenceDoc("runtime.ok", maintenance.EvidenceStatusPass, maintenance.EvidenceFreshnessCurrent))
	requireState(t, report, StateUnderSupported)
}

func TestArchitectDesiredDirectionAnswerDoesNotJustifyDesiredPlane(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneDesired, []string{"architect_answer:a1"}, nil, "")
	requireState(t, report, StateUnknown)
}

func TestPlaneAssessmentIsDeterministic(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, "")
	a, err := MarshalCanonicalReportYAML(report)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalCanonicalReportYAML(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("assessment render is not deterministic")
	}
}

func TestPlaneAssessmentDoesNotMutateClaim(t *testing.T) {
	doc := claimDoc(t, []architecture.Claim{claim("claim.one", architecture.PlaneObserved, []string{"fact.guard"}, nil, nil)}, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")})
	before := doc.Claims[0]
	if _, err := Assess(Context{Claims: doc, GraphDigestStatus: architecture.GraphDigestNotRequested}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, doc.Claims[0]) {
		t.Fatal("assessment mutated claim")
	}
}

func TestSupportedClaimCanHaveInvalidPlane(t *testing.T) {
	c := claim("claim.one", architecture.PlaneDesired, []string{"fact.write"}, nil, nil)
	c.EpistemicStatus = architecture.StatusSupported
	c.InvalidationConditions = []string{"source fact changes"}
	report := assessDoc(t, claimDoc(t, []architecture.Claim{c}, []architecture.Fact{fact("fact.write", "write", "writes")}), "")
	requireState(t, report, StateInvalid)
}

func TestUnknownClaimCanHaveJustifiedPlane(t *testing.T) {
	c := claim("claim.one", architecture.PlaneObserved, []string{"fact.guard"}, nil, nil)
	c.EpistemicStatus = architecture.StatusUnknown
	c.Unknowns = []string{"bounded observation"}
	report := assessDoc(t, claimDoc(t, []architecture.Claim{c}, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}), "")
	requireState(t, report, StateJustified)
}

func TestStaleClaimProducesStalePlaneAssessmentWhenBasisStale(t *testing.T) {
	c := claim("claim.one", architecture.PlaneObserved, []string{"fact.guard"}, nil, nil)
	c.Freshness = "stale"
	report := assessDoc(t, claimDoc(t, []architecture.Claim{c}, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}), "")
	requireState(t, report, StateStale)
}

func TestMissingGraphSnapshotProducesUnknownIntendedBasis(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneIntended, []string{"invariant:i.active"}, nil, "")
	requireState(t, report, StateUnknown)
}

func TestObservedOnlyAssessmentDoesNotRequireGraphSnapshot(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, "")
	requireState(t, report, StateJustified)
}

func TestAssessmentGroupsSamePropositionAcrossPlanes(t *testing.T) {
	c1 := claim("claim.observed", architecture.PlaneObserved, []string{"fact.guard"}, nil, nil)
	c2 := claim("claim.enforced", architecture.PlaneEnforced, []string{"fact.assert"}, nil, nil)
	doc := claimDoc(t, []architecture.Claim{c1, c2}, []architecture.Fact{fact("fact.guard", "guard", "refuses_when"), fact("fact.assert", "assertion", "asserts_architectural_rule")})
	report := assessDoc(t, doc, "")
	if len(report.PropositionGroups) != 1 {
		t.Fatalf("groups=%d", len(report.PropositionGroups))
	}
}

func TestAssessmentDoesNotGroupDifferentPredicates(t *testing.T) {
	c1 := claim("claim.one", architecture.PlaneObserved, []string{"fact.guard"}, nil, nil)
	c2 := claim("claim.two", architecture.PlaneObserved, []string{"fact.write"}, nil, nil)
	c2.Statement.Predicate = "writes"
	doc := claimDoc(t, []architecture.Claim{c1, c2}, []architecture.Fact{fact("fact.guard", "guard", "refuses_when"), fact("fact.write", "write", "writes")})
	report := assessDoc(t, doc, "")
	if len(report.PropositionGroups) != 2 {
		t.Fatalf("groups=%d", len(report.PropositionGroups))
	}
}

func TestAssessmentDoesNotInferContradiction(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, "")
	for _, r := range report.ClaimAssessments[0].Reasons {
		if strings.Contains(r.Code, "contradiction") {
			t.Fatalf("unexpected contradiction reason: %s", r.Code)
		}
	}
}

func TestAssessmentDoesNotProduceClosureVerdict(t *testing.T) {
	b, err := MarshalCanonicalReportYAML(assessOne(t, architecture.PlaneObserved, []architecture.Fact{fact("fact.guard", "guard", "refuses_when")}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("closure")) || bytes.Contains(b, []byte("verdict")) {
		t.Fatalf("assessment rendered closure language:\n%s", string(b))
	}
}

func assessOne(t *testing.T, planeName string, facts []architecture.Fact, graph string) Report {
	return assessOneWithAbout(t, planeName, []string{"intent:i.current"}, facts, graph)
}

func assessOneWithAbout(t *testing.T, planeName string, about []string, facts []architecture.Fact, graph string) Report {
	if len(facts) == 0 {
		facts = []architecture.Fact{fact("fact.doc", "documentation", "mentions")}
	}
	c := claim("claim.one", planeName, factIDs(facts), nil, about)
	return assessDoc(t, claimDoc(t, []architecture.Claim{c}, facts), graph)
}

func assessWithEvidence(t *testing.T, planeName string, evidence []string, graph string, ev maintenance.EvidenceStateDocument) Report {
	c := claim("claim.one", planeName, nil, evidence, nil)
	doc := claimDoc(t, []architecture.Claim{c}, nil)
	idx, err := ReadGraphIndex(strings.NewReader(graph))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Assess(Context{Claims: doc, Graph: idx, Evidence: &ev, GraphDigestStatus: architecture.GraphDigestNotRequested})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func assessDoc(t *testing.T, doc architecture.ClaimDocument, graph string) Report {
	t.Helper()
	idx := GraphIndex{Nodes: map[string]GovernedNode{}}
	if graph != "" {
		var err error
		idx, err = ReadGraphIndex(strings.NewReader(graph))
		if err != nil {
			t.Fatal(err)
		}
	}
	report, err := Assess(Context{Claims: doc, Graph: idx, GraphDigestStatus: architecture.GraphDigestNotRequested})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func fact(id, kind, predicate string) architecture.Fact {
	return architecture.Fact{
		ID:         id,
		Kind:       kind,
		Subject:    "S",
		Predicate:  predicate,
		Object:     "O",
		Confidence: 0.8,
		Extractor:  "test",
		Provenance: &architecture.Provenance{
			RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			RepositoryDomain:       "github.com/example/repo",
			RevisionStatus:         architecture.RevisionResolved,
			Revision:               "abc123",
			SourceDigestStatus:     architecture.SourceDigestUnavailable,
			SourceKind:             "test",
		},
	}
}

func claim(id, planeName string, premiseFacts, evidence, about []string) architecture.Claim {
	return architecture.Claim{
		ID:                  id,
		Label:               id,
		Statement:           architecture.ClaimStatement{Subject: "S", Predicate: "p", Object: "O"},
		Scope:               architecture.ClaimScope{Repository: "github.com/example/repo", Repo: "github.com/example/repo"},
		ArchitecturalPlane:  planeName,
		AssertionOrigin:     architecture.OriginDerived,
		EpistemicStatus:     architecture.StatusUnknown,
		InferenceRule:       "rule.test",
		PremiseFacts:        premiseFacts,
		SupportingEvidence:  evidence,
		AboutNodes:          about,
		Unknowns:            []string{"test uncertainty"},
		Confidence:          0.5,
		HumanReviewRequired: true,
		PromotionStatus:     architecture.PromotionCandidate,
	}
}

func claimDoc(t *testing.T, claims []architecture.Claim, facts []architecture.Fact) architecture.ClaimDocument {
	t.Helper()
	var receipts []architecture.ClaimFactReceipt
	for _, f := range facts {
		p := *f.Provenance
		receipts = append(receipts, architecture.ClaimFactReceipt{Fact: f, Provenance: p})
	}
	doc, err := architecture.NormalizeClaimDocument(architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/repo",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: "def456",
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		FactReceipts: receipts,
		Claims:       claims,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func factIDs(facts []architecture.Fact) []string {
	var ids []string
	for _, f := range facts {
		ids = append(ids, f.ID)
	}
	return ids
}

func graphNT(t *testing.T, class, id, status, architecturalPlane string) string {
	t.Helper()
	classIRI := classIRI(class)
	subj := rdf.MintIRI(classIRI, id)
	nt := subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(classIRI) + " .\n"
	if status != "" {
		nt += subj + " " + rdf.IRI(rdf.PropStatus) + " " + rdf.Lit(status) + " .\n"
	}
	if architecturalPlane != "" {
		nt += subj + " " + rdf.IRI(rdf.PropArchitecturalPlane) + " " + rdf.Lit(architecturalPlane) + " .\n"
	}
	return nt
}

func graphRuntimeEvidenceNT(id string) string {
	return rdf.MintIRI(rdf.ClassRuntimeEvidence, id) + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassRuntimeEvidence) + " .\n"
}

func graphEvidenceNT(id string, producedByTest bool) string {
	subj := rdf.MintIRI(rdf.ClassEvidence, id)
	nt := subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassEvidence) + " .\n"
	if producedByTest {
		nt += subj + " " + rdf.IRI(rdf.PropProducedByTest) + " " + rdf.MintIRI(rdf.ClassTest, "pkg.Test") + " .\n"
	}
	return nt
}

func ntLit(class, id, prop, lit string) string {
	return rdf.MintIRI(classIRI(class), id) + " " + rdf.IRI(prop) + " " + rdf.Lit(lit) + " .\n"
}

func classIRI(class string) string {
	switch class {
	case "invariant":
		return rdf.ClassInvariant
	case "contract":
		return rdf.ClassContract
	case "decision":
		return rdf.ClassDecision
	case "intent":
		return rdf.ClassIntent
	case "evidence":
		return rdf.ClassEvidence
	case "runtime_evidence":
		return rdf.ClassRuntimeEvidence
	case "test":
		return rdf.ClassTest
	default:
		return rdf.ClassComponent
	}
}

func evidenceDoc(id, status, freshness string) maintenance.EvidenceStateDocument {
	return maintenance.EvidenceStateDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/repo",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: "def456",
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Evidence: []maintenance.EvidenceState{{ID: id, Status: status, Freshness: freshness}},
	}
}

func requireState(t *testing.T, report Report, state string) {
	t.Helper()
	if got := report.ClaimAssessments[0].PlaneState; got != state {
		t.Fatalf("state=%s want %s reasons=%+v bases=%+v", got, state, report.ClaimAssessments[0].Reasons, report.ClaimAssessments[0].Bases)
	}
}

func requireReason(t *testing.T, report Report, code string) {
	t.Helper()
	for _, r := range report.ClaimAssessments[0].Reasons {
		if r.Code == code {
			return
		}
	}
	t.Fatalf("missing reason %s in %+v", code, report.ClaimAssessments[0].Reasons)
}
