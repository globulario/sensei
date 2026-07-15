// SPDX-License-Identifier: Apache-2.0

package plane

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/rdf"
)

func Assess(ctx Context) (Report, error) {
	doc, err := architecture.NormalizeClaimDocument(ctx.Claims)
	if err != nil {
		return Report{}, err
	}
	ctx.Claims = doc
	if ctx.Evidence != nil {
		if err := maintenance.ValidateEvidenceStateDocument(*ctx.Evidence, &doc.Binding); err != nil {
			return Report{}, err
		}
	}
	if ctx.Dialogue != nil {
		d, err := architecture.NormalizeDialogueDocument(*ctx.Dialogue)
		if err != nil {
			return Report{}, err
		}
		ctx.Dialogue = &d
	}

	facts := map[string]architecture.ClaimFactReceipt{}
	for _, r := range doc.FactReceipts {
		facts[r.Fact.ID] = r
	}
	maint := maintenanceByClaim(ctx.Maintenance)
	assessments := make([]ClaimAssessment, 0, len(doc.Claims))
	for _, claim := range doc.Claims {
		assessment := assessClaim(ctx, claim, facts, maint[claim.ID])
		assessments = append(assessments, assessment)
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedBy:   GeneratedBy,
		ClaimBinding: ClaimBindingReport{
			RepositoryDomain:  doc.Binding.RepositoryDomain,
			Revision:          doc.Binding.Revision,
			RevisionStatus:    doc.Binding.RevisionStatus,
			GraphDigestSHA256: doc.Binding.GraphDigestSHA256,
			GraphDigestStatus: doc.Binding.GraphDigestStatus,
		},
		GraphSnapshot: GraphSnapshotReport{
			Path:         ctx.GraphSnapshotPath,
			DigestSHA256: ctx.GraphDigest,
			DigestStatus: ctx.GraphDigestStatus,
		},
		ClaimAssessments: assessments,
		Limitations:      append([]architecture.Limitation{}, doc.Limitations...),
	}
	report.PropositionGroups = buildGroups(doc.Claims, assessments)
	report = normalizeReport(report)
	return report, nil
}

func assessClaim(ctx Context, claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt, maintEval *maintenance.ClaimEvaluation) ClaimAssessment {
	bases := []BasisAssessment{}
	reasons := []Reason{{Code: "plane.truth_layer.separate_axis", Detail: "architectural plane is assessed independently from system truth layer"}}
	reasons = append(reasons, graphDigestReason(ctx)...)
	var state string
	switch claim.ArchitecturalPlane {
	case architecture.PlaneObserved:
		bases, reasons, state = assessObserved(ctx, claim, facts, bases, reasons)
	case architecture.PlaneEnforced:
		bases, reasons, state = assessEnforced(ctx, claim, facts, bases, reasons)
	case architecture.PlaneIntended:
		bases, reasons, state = assessIntended(ctx, claim, bases, reasons)
	case architecture.PlaneHistorical:
		bases, reasons, state = assessHistorical(ctx, claim, facts, bases, reasons)
	case architecture.PlaneDesired:
		bases, reasons, state = assessDesired(ctx, claim, facts, bases, reasons)
	default:
		reasons = append(reasons, Reason{Code: "plane.unknown", Detail: "unknown architectural plane"})
		state = StateUnknown
	}
	if claim.Freshness == architecture.StatusStale || claim.Freshness == "stale" || claim.EpistemicStatus == architecture.StatusStale {
		state = StateStale
		reasons = append(reasons, Reason{Code: "plane.claim.stale", Detail: "claim truth-maintenance freshness is stale"})
	}
	open, answers, dialogueReasons := dialogueContext(ctx.Dialogue, claim.ID)
	reasons = append(reasons, dialogueReasons...)
	maintenanceReasons := []Reason{}
	if maintEval != nil {
		for _, r := range maintEval.Reasons {
			maintenanceReasons = append(maintenanceReasons, Reason{Code: r.Code, Detail: r.Detail})
		}
	}
	return ClaimAssessment{
		ClaimID:            claim.ID,
		PropositionKey:     PropositionKey(claim),
		DeclaredPlane:      claim.ArchitecturalPlane,
		AssertionOrigin:    claim.AssertionOrigin,
		EpistemicStatus:    claim.EpistemicStatus,
		PromotionStatus:    claim.PromotionStatus,
		Freshness:          claim.Freshness,
		PlaneState:         state,
		Bases:              bases,
		Reasons:            reasons,
		OpenQuestions:      open,
		ArchitectAnswers:   answers,
		MaintenanceReasons: maintenanceReasons,
	}
}

func assessObserved(ctx Context, claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt, bases []BasisAssessment, reasons []Reason) ([]BasisAssessment, []Reason, string) {
	accepted, stale, unknown, rejected := false, false, false, false
	for _, id := range claim.PremiseFacts {
		r, ok := facts[id]
		if !ok {
			unknown = true
			bases = append(bases, unknownBasis(BasisFact, id, "fact receipt missing"))
			continue
		}
		if observedFactBasis(r.Fact) {
			accepted = true
			bases = append(bases, acceptedBasis(BasisFact, id, r.Fact.Kind, "observed fact basis"))
			reasons = append(reasons, Reason{Code: "plane.observed.fact_basis", Detail: id})
		}
	}
	for _, ref := range claim.SupportingEvidence {
		node, ok := ctx.Graph.nodeByRef(ref)
		if ok && node.Class == "runtime_evidence" {
			ba, rs, st := currentEvidenceBasis(ctx, ref, node, BasisRuntimeEvidence, "plane.observed.runtime_basis")
			bases = append(bases, ba)
			reasons = append(reasons, rs...)
			if st == BasisAccepted {
				accepted = true
			} else if st == BasisStale {
				stale = true
			} else if st == BasisUnknown {
				unknown = true
			}
		}
	}
	for _, ref := range claim.AboutNodes {
		if node, ok := ctx.Graph.nodeByRef(ref); ok && isAuthoredProseClass(node.Class) {
			rejected = true
			bases = append(bases, rejectedBasis(BasisGovernedNode, ref, node.Class, "authored prose cannot justify observed plane"))
			reasons = append(reasons, Reason{Code: "plane.observed.authored_prose_rejected", Detail: ref})
		}
	}
	return bases, reasons, basisState(accepted, stale, unknown, rejected, "plane.observed.missing_basis", &reasons)
}

func assessEnforced(ctx Context, claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt, bases []BasisAssessment, reasons []Reason) ([]BasisAssessment, []Reason, string) {
	accepted, stale, unknown, rejected := false, false, false, false
	for _, id := range claim.PremiseFacts {
		r, ok := facts[id]
		if !ok {
			unknown = true
			bases = append(bases, unknownBasis(BasisFact, id, "fact receipt missing"))
			continue
		}
		switch {
		case r.Fact.Kind == "assertion" && r.Fact.Predicate == "asserts_architectural_rule":
			accepted = true
			bases = append(bases, acceptedBasis(BasisFact, id, r.Fact.Kind, "assertion fact basis"))
			reasons = append(reasons, Reason{Code: "plane.enforced.test_fact_basis", Detail: id})
		case r.Fact.Kind == "ci_gate":
			accepted = true
			bases = append(bases, acceptedBasis(BasisGate, id, r.Fact.Kind, "CI gate fact basis"))
			reasons = append(reasons, Reason{Code: "plane.enforced.ci_gate_basis", Detail: id})
		case sourceObservationFactBasis(r.Fact):
			rejected = true
			bases = append(bases, rejectedBasis(BasisFact, id, r.Fact.Kind, "source observation alone cannot justify enforced plane"))
			reasons = append(reasons, Reason{Code: "plane.enforced.missing_basis", Detail: "source guard alone is not enforcement"})
		}
	}
	for _, ref := range claim.SupportingEvidence {
		node, ok := ctx.Graph.nodeByRef(ref)
		if !ok || node.Class != "evidence" {
			continue
		}
		ba, rs, st := currentEvidenceBasis(ctx, ref, node, BasisEvidence, "plane.enforced.current_evidence_basis")
		if len(node.ProducedByTests) == 0 && node.AssertionMethod != "enforcement" {
			st = BasisRejected
			ba.State = BasisRejected
			ba.Basis.Detail = "Evidence is not test/gate enforcement evidence"
			rs = []Reason{{Code: "plane.enforced.missing_basis", Detail: ref + " is not classified as enforcement evidence"}}
		}
		bases = append(bases, ba)
		reasons = append(reasons, rs...)
		if st == BasisAccepted {
			accepted = true
		} else if st == BasisStale {
			stale = true
		} else if st == BasisUnknown {
			unknown = true
		} else if st == BasisRejected {
			rejected = true
		}
	}
	for _, ref := range claim.AboutNodes {
		if node, ok := ctx.Graph.nodeByRef(ref); ok && node.Class == "test" {
			rejected = true
			bases = append(bases, rejectedBasis(BasisTest, ref, node.Class, "Test node existence is test-name-only evidence"))
			reasons = append(reasons, Reason{Code: "plane.enforced.test_name_only_rejected", Detail: ref})
		}
	}
	if len(claim.SupportingEvidence) > 0 && ctx.Evidence == nil {
		unknown = true
		reasons = append(reasons, Reason{Code: "plane.enforced.missing_evidence_snapshot", Detail: "evidence-state snapshot is required for current enforcement Evidence"})
	}
	return bases, reasons, basisState(accepted, stale, unknown, rejected, "plane.enforced.missing_basis", &reasons)
}

func assessIntended(ctx Context, claim architecture.Claim, bases []BasisAssessment, reasons []Reason) ([]BasisAssessment, []Reason, string) {
	accepted, stale, unknown, rejected := false, false, false, false
	for _, node := range referencedNodes(ctx.Graph, claim) {
		if !intendedClass(node.Class) {
			continue
		}
		ref := node.Class + ":" + node.ID
		switch {
		case isHistoricalNode(node):
			rejected = true
			bases = append(bases, rejectedBasis(BasisGovernedNode, ref, node.Class, "historical node cannot justify intended plane"))
			reasons = append(reasons, Reason{Code: "plane.intended.historical_node_rejected", Detail: ref})
		case isCurrentStatus(node.Status):
			accepted = true
			bases = append(bases, acceptedBasis(BasisGovernedNode, ref, node.Class, "active governed "+node.Class))
			reasons = append(reasons, Reason{Code: intendedReasonCode(node.Class), Detail: ref})
		case node.Status == "":
			unknown = true
			bases = append(bases, unknownBasis(BasisGovernedNode, ref, "governed node status is unknown"))
			reasons = append(reasons, Reason{Code: "plane.graph.node_status_unknown", Detail: ref})
		default:
			unknown = true
			bases = append(bases, unknownBasis(BasisGovernedNode, ref, "governed node status is not current"))
		}
	}
	if len(referencedGovernedRefs(claim)) > 0 && len(referencedNodes(ctx.Graph, claim)) == 0 {
		unknown = true
		reasons = append(reasons, Reason{Code: "plane.intended.missing_governed_node", Detail: "referenced governed node is absent from graph snapshot"})
	}
	if requiresGraph(claim) && len(ctx.Graph.Nodes) == 0 {
		unknown = true
		reasons = append(reasons, Reason{Code: "plane.graph.node_missing", Detail: "graph snapshot unavailable for intended basis"})
	}
	return bases, reasons, basisState(accepted, stale, unknown, rejected, "plane.intended.missing_basis", &reasons)
}

func assessHistorical(ctx Context, claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt, bases []BasisAssessment, reasons []Reason) ([]BasisAssessment, []Reason, string) {
	accepted, stale, unknown, rejected := false, false, false, false
	for _, id := range claim.PremiseFacts {
		if r, ok := facts[id]; ok && r.Fact.Kind == "historical_removal" {
			accepted = true
			bases = append(bases, acceptedBasis(BasisHistoricalRecord, id, "historical_removal", "historical removal fact"))
			reasons = append(reasons, Reason{Code: "plane.historical.removal_fact_basis", Detail: id})
		}
	}
	for _, node := range referencedNodes(ctx.Graph, claim) {
		ref := node.Class + ":" + node.ID
		switch {
		case node.ArchitecturalPlane == architecture.PlaneHistorical:
			accepted = true
			bases = append(bases, acceptedBasis(BasisExplicitPlaneAnnotation, ref, node.Class, "explicit historical annotation"))
			reasons = append(reasons, Reason{Code: "plane.historical.explicit_annotation_basis", Detail: ref})
		case isHistoricalNode(node):
			accepted = true
			bases = append(bases, acceptedBasis(BasisGovernedNode, ref, node.Class, "superseded or historical governed node"))
			reasons = append(reasons, Reason{Code: "plane.historical.superseded_node_basis", Detail: ref})
		case intendedClass(node.Class):
			rejected = true
			bases = append(bases, rejectedBasis(BasisGovernedNode, ref, node.Class, "active intended node cannot justify historical plane"))
			reasons = append(reasons, Reason{Code: "plane.historical.active_node_rejected", Detail: ref})
		}
		if node.Class == "evidence" && node.Freshness == maintenance.EvidenceFreshnessHistorical {
			accepted = true
			reasons = append(reasons, Reason{Code: "plane.historical.historical_evidence_basis", Detail: ref})
		}
	}
	for _, ref := range claim.SupportingEvidence {
		if ev, ok := evidenceState(ctx, ref); ok && ev.Freshness == maintenance.EvidenceFreshnessHistorical {
			accepted = true
			bases = append(bases, acceptedBasis(BasisEvidence, ref, "evidence", "historical evidence freshness"))
			reasons = append(reasons, Reason{Code: "plane.historical.historical_evidence_basis", Detail: ref})
		}
	}
	return bases, reasons, basisState(accepted, stale, unknown, rejected, "plane.historical.missing_basis", &reasons)
}

func assessDesired(ctx Context, claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt, bases []BasisAssessment, reasons []Reason) ([]BasisAssessment, []Reason, string) {
	accepted, stale, unknown, rejected := false, false, false, false
	for _, id := range claim.PremiseFacts {
		if r, ok := facts[id]; ok && observedFactBasis(r.Fact) {
			rejected = true
			bases = append(bases, rejectedBasis(BasisFact, id, r.Fact.Kind, "source facts cannot justify desired plane"))
			reasons = append(reasons, Reason{Code: "plane.desired.source_inference_rejected", Detail: id})
		}
	}
	for _, node := range referencedNodes(ctx.Graph, claim) {
		ref := node.Class + ":" + node.ID
		if !desiredClass(node.Class) {
			continue
		}
		switch {
		case node.ArchitecturalPlane != architecture.PlaneDesired:
			rejected = true
			bases = append(bases, rejectedBasis(BasisGovernedNode, ref, node.Class, "desired plane requires explicit desired annotation"))
			if node.Class == "intent" {
				reasons = append(reasons, Reason{Code: "plane.desired.implicit_intent_rejected", Detail: ref})
			}
			reasons = append(reasons, Reason{Code: "plane.desired.missing_explicit_annotation", Detail: ref})
		case isHistoricalNode(node):
			rejected = true
			bases = append(bases, rejectedBasis(BasisExplicitPlaneAnnotation, ref, node.Class, "desired node is historical or superseded"))
		case isCurrentStatus(node.Status):
			accepted = true
			bases = append(bases, acceptedBasis(BasisExplicitPlaneAnnotation, ref, node.Class, "explicit desired annotation"))
			reasons = append(reasons, Reason{Code: desiredReasonCode(node.Class), Detail: ref})
		case node.Status == "":
			unknown = true
			reasons = append(reasons, Reason{Code: "plane.graph.node_status_unknown", Detail: ref})
		default:
			unknown = true
		}
	}
	if len(referencedGovernedRefs(claim)) > 0 && len(referencedNodes(ctx.Graph, claim)) == 0 {
		unknown = true
		reasons = append(reasons, Reason{Code: "plane.graph.node_missing", Detail: "referenced desired basis node is absent from graph snapshot"})
	}
	return bases, reasons, basisState(accepted, stale, unknown, rejected, "plane.desired.missing_explicit_annotation", &reasons)
}

func observedFactBasis(f architecture.Fact) bool {
	switch f.Kind {
	case "guard":
		return f.Predicate == "refuses_when"
	case "transition":
		return f.Predicate == "rejects_transition_when"
	case "write":
		return f.Predicate == "writes"
	case "read":
		return f.Predicate == "reads"
	case "authority_observation":
		return f.Predicate == "mutates_state" || f.Predicate == "exposes_route" || f.Predicate == "controls_lifecycle"
	case "schema_constraint", "persistence":
		return true
	case "export":
		return f.Predicate == "exports_symbol"
	case "test_call":
		return f.Predicate == "test_calls_symbol"
	case "reachability":
		return f.Predicate == "entrypoint_reaches_symbol"
	case "interface":
		return f.Predicate == "implements_interface"
	case "component_dependency":
		return f.Predicate == "component_depends_on_component"
	default:
		return false
	}
}

func sourceObservationFactBasis(f architecture.Fact) bool {
	switch f.Kind {
	case "guard":
		return f.Predicate == "refuses_when"
	case "transition":
		return f.Predicate == "rejects_transition_when"
	case "write":
		return f.Predicate == "writes"
	case "read":
		return f.Predicate == "reads"
	case "authority_observation":
		return f.Predicate == "mutates_state" || f.Predicate == "exposes_route" || f.Predicate == "controls_lifecycle"
	case "schema_constraint", "persistence":
		return true
	default:
		return false
	}
}

func currentEvidenceBasis(ctx Context, ref string, node GovernedNode, kind, code string) (BasisAssessment, []Reason, string) {
	if ctx.Evidence == nil {
		return unknownBasis(kind, ref, "evidence-state snapshot missing"), []Reason{{Code: "plane.enforced.missing_evidence_snapshot", Detail: ref}}, BasisUnknown
	}
	ev, ok := evidenceState(ctx, ref)
	if !ok {
		return unknownBasis(kind, ref, "evidence missing from evidence-state snapshot"), []Reason{{Code: "plane.graph.node_missing", Detail: ref}}, BasisUnknown
	}
	b := Basis{Kind: kind, ID: ref, Class: node.Class, Status: ev.Status, Freshness: ev.Freshness, SourcePath: node.SourcePath}
	if maintenance.EvidenceIsActive(ev, ctx.Evidence.Binding, ctx.Claims.Binding) {
		b.Detail = "current active evidence"
		return BasisAssessment{Basis: b, State: BasisAccepted}, []Reason{{Code: code, Detail: ref}}, BasisAccepted
	}
	if ev.Freshness == maintenance.EvidenceFreshnessStale || ev.Status == maintenance.EvidenceStatusStale {
		b.Detail = "stale evidence"
		return BasisAssessment{Basis: b, State: BasisStale}, []Reason{{Code: "plane.evidence.stale", Detail: ref}}, BasisStale
	}
	b.Detail = "evidence current state unknown or inactive"
	return BasisAssessment{Basis: b, State: BasisUnknown}, []Reason{{Code: "plane.evidence.unknown", Detail: ref}}, BasisUnknown
}

func basisState(accepted, stale, unknown, rejected bool, missingCode string, reasons *[]Reason) string {
	switch {
	case rejected:
		return StateInvalid
	case stale:
		return StateStale
	case accepted:
		return StateJustified
	case unknown:
		return StateUnknown
	default:
		*reasons = append(*reasons, Reason{Code: missingCode})
		return StateUnderSupported
	}
}

func acceptedBasis(kind, id, class, detail string) BasisAssessment {
	return BasisAssessment{State: BasisAccepted, Basis: Basis{Kind: kind, ID: id, Class: class, Status: BasisAccepted, Detail: detail}}
}

func rejectedBasis(kind, id, class, detail string) BasisAssessment {
	return BasisAssessment{State: BasisRejected, Basis: Basis{Kind: kind, ID: id, Class: class, Status: BasisRejected, Detail: detail}}
}

func unknownBasis(kind, id, detail string) BasisAssessment {
	return BasisAssessment{State: BasisUnknown, Basis: Basis{Kind: kind, ID: id, Status: BasisUnknown, Detail: detail}}
}

func (g GraphIndex) nodeByRef(ref string) (GovernedNode, bool) {
	class, id, ok := architecture.ParseClassQualifiedReference(ref)
	if !ok {
		return GovernedNode{}, false
	}
	var iri string
	switch class {
	case "invariant":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassInvariant, id), "<>")
	case "contract":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassContract, id), "<>")
	case "decision":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassDecision, id), "<>")
	case "intent":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassIntent, id), "<>")
	case "evidence":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassEvidence, id), "<>")
		if n, ok := g.Nodes[iri]; ok {
			return n, true
		}
		iri = strings.Trim(rdf.MintIRI(rdf.ClassRuntimeEvidence, id), "<>")
	case "test":
		iri = strings.Trim(rdf.MintIRI(rdf.ClassTest, id), "<>")
	default:
		return GovernedNode{}, false
	}
	n, ok := g.Nodes[iri]
	return n, ok
}

func referencedNodes(g GraphIndex, claim architecture.Claim) []GovernedNode {
	refs := referencedGovernedRefs(claim)
	var out []GovernedNode
	for _, ref := range refs {
		if n, ok := g.nodeByRef(ref); ok {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Class+":"+out[i].ID < out[j].Class+":"+out[j].ID })
	return out
}

func referencedGovernedRefs(claim architecture.Claim) []string {
	var refs []string
	refs = append(refs, claim.AboutNodes...)
	refs = append(refs, claim.SupportingEvidence...)
	return sortedUnique(refs)
}

func intendedClass(class string) bool {
	return class == "invariant" || class == "contract" || class == "decision" || class == "intent"
}

func desiredClass(class string) bool {
	return class == "contract" || class == "decision" || class == "intent"
}

func isAuthoredProseClass(class string) bool { return intendedClass(class) }

func isHistoricalNode(n GovernedNode) bool {
	return n.ArchitecturalPlane == architecture.PlaneHistorical || isHistoricalStatus(n.Status) || len(n.SupersededBy) > 0
}

func intendedReasonCode(class string) string {
	switch class {
	case "invariant":
		return "plane.intended.active_invariant_basis"
	case "contract":
		return "plane.intended.active_contract_basis"
	case "decision":
		return "plane.intended.active_decision_basis"
	case "intent":
		return "plane.intended.active_intent_basis"
	default:
		return "plane.intended.missing_basis"
	}
}

func desiredReasonCode(class string) string {
	switch class {
	case "contract":
		return "plane.desired.explicit_contract_basis"
	case "decision":
		return "plane.desired.explicit_decision_basis"
	case "intent":
		return "plane.desired.explicit_intent_basis"
	default:
		return "plane.desired.missing_explicit_annotation"
	}
}

func requiresGraph(claim architecture.Claim) bool {
	return claim.ArchitecturalPlane == architecture.PlaneIntended || claim.ArchitecturalPlane == architecture.PlaneHistorical || claim.ArchitecturalPlane == architecture.PlaneDesired
}

func evidenceState(ctx Context, ref string) (maintenance.EvidenceState, bool) {
	if ctx.Evidence == nil {
		return maintenance.EvidenceState{}, false
	}
	ev, ok := ctx.Evidence.ByID()[strings.TrimPrefix(ref, "evidence:")]
	return ev, ok
}

func graphDigestReason(ctx Context) []Reason {
	switch ctx.GraphDigestStatus {
	case architecture.GraphDigestResolved:
		if ctx.GraphDigestVerified {
			return []Reason{{Code: "plane.graph.digest_current"}}
		}
		return []Reason{{Code: "plane.graph.digest_mismatch"}}
	case architecture.GraphDigestUnavailable, architecture.GraphDigestNotRequested:
		return []Reason{{Code: "plane.graph.digest_unavailable", Detail: ctx.GraphDigestStatus}}
	default:
		return nil
	}
}

func maintenanceByClaim(report *maintenance.Report) map[string]*maintenance.ClaimEvaluation {
	out := map[string]*maintenance.ClaimEvaluation{}
	if report == nil {
		return out
	}
	for i := range report.ClaimEvaluations {
		ev := &report.ClaimEvaluations[i]
		out[ev.ClaimID] = ev
	}
	return out
}

func dialogueContext(doc *architecture.DialogueDocument, claimID string) ([]string, []string, []Reason) {
	if doc == nil {
		return nil, nil, nil
	}
	var questions, answers []string
	for _, q := range doc.OpenQuestions {
		for _, cid := range q.BlocksClaims {
			if cid == claimID {
				questions = append(questions, q.ID)
				for _, aid := range q.ResolvedByAnswers {
					answers = append(answers, aid)
				}
			}
		}
	}
	for _, a := range doc.Answers {
		for _, qid := range a.AnswersQuestions {
			if containsString(questions, qid) {
				answers = append(answers, a.ID)
			}
		}
	}
	if len(questions)+len(answers) == 0 {
		return nil, nil, nil
	}
	return sortedUnique(questions), sortedUnique(answers), []Reason{{Code: "plane.dialogue.non_probative", Detail: "dialogue context is reported but never used as plane basis"}}
}

func buildGroups(claims []architecture.Claim, assessments []ClaimAssessment) []PropositionGroup {
	byKey := map[string]*PropositionGroup{}
	assessmentByID := map[string]string{}
	for _, a := range assessments {
		assessmentByID[a.ClaimID] = a.PlaneState
	}
	for _, c := range claims {
		key := PropositionKey(c)
		g := byKey[key]
		if g == nil {
			g = &PropositionGroup{
				PropositionKey:    key,
				Subject:           c.Statement.Subject,
				Predicate:         c.Statement.Predicate,
				Object:            c.Statement.Object,
				ClaimsByPlane:     map[string][]string{},
				AssessmentByClaim: map[string]string{},
			}
			byKey[key] = g
		}
		g.ClaimsByPlane[c.ArchitecturalPlane] = append(g.ClaimsByPlane[c.ArchitecturalPlane], c.ID)
		g.AssessmentByClaim[c.ID] = assessmentByID[c.ID]
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PropositionGroup, 0, len(keys))
	for _, key := range keys {
		g := *byKey[key]
		for plane, ids := range g.ClaimsByPlane {
			g.ClaimsByPlane[plane] = sortedUnique(ids)
		}
		for _, plane := range PlaneOrder {
			if len(g.ClaimsByPlane[plane]) > 0 {
				g.PresentPlanes = append(g.PresentPlanes, plane)
			} else {
				g.MissingPlanes = append(g.MissingPlanes, plane)
			}
		}
		out = append(out, g)
	}
	return out
}

func containsString(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

func debugClaim(c architecture.Claim) string {
	return fmt.Sprintf("%s %s %s", c.Statement.Subject, c.Statement.Predicate, c.Statement.Object)
}
