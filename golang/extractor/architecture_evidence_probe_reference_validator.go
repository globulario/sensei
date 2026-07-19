// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/rdf"
)

var probeBlockerIDRE = regexp.MustCompile(`^blocker\.(structural|authority|contract|behavioral|evidence|contradiction|direction|agent)\.[a-f0-9]{12}$`)
var probeSHA256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ArchitectureEvidenceProbeReferenceError struct {
	ProbeID string
	Reason  string
}

type probeNTNode struct {
	class string
	props map[string][]string
}

func (e ArchitectureEvidenceProbeReferenceError) Error() string {
	if e.ProbeID == "" {
		return "architecture evidence probe reference error: " + e.Reason
	}
	return fmt.Sprintf("architecture evidence probe %s: %s", e.ProbeID, e.Reason)
}

func ValidateArchitectureEvidenceProbeReferences(r io.Reader) ([]ArchitectureEvidenceProbeReferenceError, error) {
	nodes := map[string]*probeNTNode{}
	defined := map[string]bool{}
	definedAuthoredEvidence := map[string]bool{}
	claimSupport := map[string]map[string]bool{}
	claimRefute := map[string]map[string]bool{}

	get := func(iri string) *probeNTNode {
		st := nodes[iri]
		if st == nil {
			st = &probeNTNode{props: map[string][]string{}}
			nodes[iri] = st
		}
		return st
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".") {
			continue
		}
		toks := tokenize(strings.TrimSpace(strings.TrimSuffix(line, ".")))
		if len(toks) != 3 {
			continue
		}
		subjIRI := stripAngleBrackets(toks[0])
		pred := stripAngleBrackets(toks[1])
		obj := toks[2]
		objIRI := stripAngleBrackets(obj)
		if pred == rdf.PropType {
			defined[subjIRI] = true
			if objIRI == rdf.ClassEvidenceProbe {
				get(subjIRI).class = "evidence_probe"
			}
			continue
		}
		if pred == rdf.PropAuthoredIn && matchesClassSubject(subjIRI, rdf.ClassEvidence) {
			definedAuthoredEvidence[subjIRI] = true
		}
		if pred == rdf.PropSupportedByEvidence && matchesClassSubject(subjIRI, rdf.ClassArchitectureClaim) {
			if claimSupport[subjIRI] == nil {
				claimSupport[subjIRI] = map[string]bool{}
			}
			claimSupport[subjIRI][objIRI] = true
		}
		if pred == rdf.PropRefutedByEvidence && matchesClassSubject(subjIRI, rdf.ClassArchitectureClaim) {
			if claimRefute[subjIRI] == nil {
				claimRefute[subjIRI] = map[string]bool{}
			}
			claimRefute[subjIRI][objIRI] = true
		}
		if matchesClassSubject(subjIRI, rdf.ClassEvidenceProbe) {
			get(subjIRI).props[pred] = append(get(subjIRI).props[pred], obj)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	var errs []ArchitectureEvidenceProbeReferenceError
	for iri, st := range nodes {
		if st.class != "evidence_probe" {
			continue
		}
		id := "evidence_probe:" + extractIDFromIRI(iri, rdf.ClassEvidenceProbe)
		requireProbeProp(&errs, id, st, rdf.PropLabel, "label")
		requireProbeProp(&errs, id, st, rdf.PropStatus, "status")
		requireProbeProp(&errs, id, st, rdf.PropProbeForQuestion, "probeForQuestion")
		requireProbeProp(&errs, id, st, rdf.PropProbeTemplateID, "probeTemplateId")
		requireProbeProp(&errs, id, st, rdf.PropProbeTemplateVersion, "probeTemplateVersion")
		requireProbeProp(&errs, id, st, rdf.PropProbeKind, "probeKind")
		requireProbeProp(&errs, id, st, rdf.PropHasEvidenceLane, "hasEvidenceLane")
		requireProbeProp(&errs, id, st, rdf.PropEvidenceRole, "evidenceRole")
		requireProbeProp(&errs, id, st, rdf.PropSafetyClass, "safetyClass")
		requireProbeProp(&errs, id, st, rdf.PropRequiresApprovalGate, "requiresApprovalGate")
		requireProbeProp(&errs, id, st, rdf.PropAutomaticExecutionAllowed, "automaticExecutionAllowed")
		requireProbeProp(&errs, id, st, rdf.PropSourceClosureAssessmentDigest, "sourceClosureAssessmentDigest")
		requireProbeProp(&errs, id, st, rdf.PropSourceDialogueDigest, "sourceDialogueDigest")
		requireProbeProp(&errs, id, st, rdf.PropSourceClaimDocumentDigest, "sourceClaimDocumentDigest")
		requireProbeProp(&errs, id, st, rdf.PropSourceKind, "sourceKind")
		if !hasLiteral(st.props[rdf.PropSourceKind], "generated_candidate") {
			errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "sourceKind must be generated_candidate"})
		}
		if len(st.props[rdf.PropProbeForQuestion]) != 1 {
			errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "probe must answer exactly one question"})
		}
		for _, obj := range st.props[rdf.PropProbeForQuestion] {
			qIRI := stripAngleBrackets(obj)
			if !defined[qIRI] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "question target is not defined: " + extractIDFromIRI(qIRI, rdf.ClassOpenQuestion)})
			}
		}
		for _, obj := range st.props[rdf.PropTargetsClaim] {
			claimIRI := stripAngleBrackets(obj)
			if !defined[claimIRI] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "claim target is not defined: " + extractIDFromIRI(claimIRI, rdf.ClassArchitectureClaim)})
			}
		}
		for _, obj := range st.props[rdf.PropTargetsNode] {
			nodeIRI := stripAngleBrackets(obj)
			if !defined[nodeIRI] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "node target is not defined: " + nodeIRI})
			}
		}
		for _, obj := range st.props[rdf.PropProducesEvidence] {
			evIRI := stripAngleBrackets(obj)
			if !definedAuthoredEvidence[evIRI] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "target Evidence is not authored: " + extractIDFromIRI(evIRI, rdf.ClassEvidence)})
			}
		}
		for _, obj := range st.props[rdf.PropUsesRuntimeEvidenceProfile] {
			if !defined[stripAngleBrackets(obj)] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "RuntimeEvidence target is not defined"})
			}
		}
		for _, obj := range st.props[rdf.PropDischargesProofObligation] {
			if !defined[stripAngleBrackets(obj)] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "ProofObligation target is not defined"})
			}
		}
		for _, obj := range st.props[rdf.PropDischargesProofSlot] {
			if !defined[stripAngleBrackets(obj)] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "ProofSlot target is not defined"})
			}
		}
		for _, obj := range st.props[rdf.PropProbeForRepairPlan] {
			if !defined[stripAngleBrackets(obj)] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "RepairPlan target is not defined"})
			}
		}
		for _, obj := range st.props[rdf.PropProbeForTest] {
			if !defined[stripAngleBrackets(obj)] {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "Test target is not defined"})
			}
		}
		for _, obj := range st.props[rdf.PropAddressesClosureBlocker] {
			if !probeBlockerIDRE.MatchString(unquoteNTLiteral(obj)) {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "closure blocker ID is malformed"})
			}
		}
		for _, prop := range []string{rdf.PropSourceClosureAssessmentDigest, rdf.PropSourceDialogueDigest, rdf.PropSourceClaimDocumentDigest} {
			for _, obj := range st.props[prop] {
				if !probeSHA256RE.MatchString(unquoteNTLiteral(obj)) {
					errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "source artifact digest must be lowercase SHA-256"})
				}
			}
		}
		safety := firstLiteral(st.props[rdf.PropSafetyClass])
		gate := firstLiteral(st.props[rdf.PropRequiresApprovalGate])
		if probe.WeakerApprovalForTest(safety, gate) {
			errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "approval gate is weaker than safety policy"})
		}
		if firstLiteral(st.props[rdf.PropAutomaticExecutionAllowed]) == "true" && !probe.AutomaticAllowedForTest(safety) {
			errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "automatic execution forbidden for safety class"})
		}
		role := firstLiteral(st.props[rdf.PropEvidenceRole])
		if (role == probe.RoleSupporting || role == probe.RoleRefuting) && len(st.props[rdf.PropProducesEvidence]) > 0 {
			evIRI := stripAngleBrackets(st.props[rdf.PropProducesEvidence][0])
			ok := false
			for _, obj := range st.props[rdf.PropTargetsClaim] {
				claimIRI := stripAngleBrackets(obj)
				if role == probe.RoleSupporting && claimSupport[claimIRI][evIRI] {
					ok = true
				}
				if role == probe.RoleRefuting && claimRefute[claimIRI][evIRI] {
					ok = true
				}
			}
			if !ok {
				errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "target Evidence is not linked to target claim in declared role"})
			}
		}
		if len(st.props[rdf.PropProducesEvidence])+len(st.props[rdf.PropUsesRuntimeEvidenceProfile])+len(st.props[rdf.PropDischargesProofObligation])+len(st.props[rdf.PropDischargesProofSlot])+len(st.props[rdf.PropProbeForRepairPlan])+len(st.props[rdf.PropProbeForTest]) == 0 {
			errs = append(errs, ArchitectureEvidenceProbeReferenceError{id, "probe must carry at least one evidence or proof target"})
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].ProbeID != errs[j].ProbeID {
			return errs[i].ProbeID < errs[j].ProbeID
		}
		return errs[i].Reason < errs[j].Reason
	})
	return errs, nil
}

func requireProbeProp(errs *[]ArchitectureEvidenceProbeReferenceError, id string, st *probeNTNode, prop, label string) {
	if len(st.props[prop]) == 0 {
		*errs = append(*errs, ArchitectureEvidenceProbeReferenceError{id, "missing required property " + label})
	}
}

func firstLiteral(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return unquoteNTLiteral(values[0])
}
