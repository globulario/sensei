// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/rdf"
)

func importArchitectureEvidenceProbes(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	doc, err := probe.UnmarshalDocumentYAML(data, nil)
	if err != nil {
		return fmt.Errorf("validate architecture_evidence_probes: %w", err)
	}
	for _, p := range doc.Probes {
		emitEvidenceProbe(e, path, doc, p)
	}
	return nil
}

func emitEvidenceProbe(e *rdf.Emitter, path string, doc probe.ProbeDocument, p probe.EvidenceProbe) {
	subj := rdf.MintIRI(rdf.ClassEvidenceProbe, p.ID)
	e.Typed(subj, rdf.ClassEvidenceProbe)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(p.Label, p.ID)))
	emitOptLit(e, subj, rdf.PropComment, p.Description)
	e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(p.Status))
	e.Triple(subj, rdf.IRI(rdf.PropProbeForQuestion), rdf.MintIRI(rdf.ClassOpenQuestion, p.QuestionID))
	for _, id := range p.ClosureBlockerIDs {
		e.Triple(subj, rdf.IRI(rdf.PropAddressesClosureBlocker), rdf.Lit(id))
	}
	for _, id := range p.ClaimIDs {
		e.Triple(subj, rdf.IRI(rdf.PropTargetsClaim), rdf.MintIRI(rdf.ClassArchitectureClaim, id))
	}
	for _, ref := range p.NodeRefs {
		if iri, ok := claimReferenceIRI(ref); ok {
			e.Triple(subj, rdf.IRI(rdf.PropTargetsNode), iri)
		}
	}
	e.Triple(subj, rdf.IRI(rdf.PropProbeTemplateID), rdf.Lit(p.TemplateID))
	e.Triple(subj, rdf.IRI(rdf.PropProbeTemplateVersion), rdf.Lit(p.TemplateVersion))
	e.Triple(subj, rdf.IRI(rdf.PropProbeKind), rdf.Lit(p.ProbeKind))
	e.Triple(subj, rdf.IRI(rdf.PropHasEvidenceLane), rdf.Lit(p.EvidenceLane))
	e.Triple(subj, rdf.IRI(rdf.PropEvidenceRole), rdf.Lit(p.EvidenceRole))
	if p.TargetEvidenceID != "" {
		e.Triple(subj, rdf.IRI(rdf.PropProducesEvidence), evidenceRefIRI(p.TargetEvidenceID))
	}
	for _, id := range p.RuntimeEvidenceIDs {
		e.Triple(subj, rdf.IRI(rdf.PropUsesRuntimeEvidenceProfile), rdf.MintIRI(rdf.ClassRuntimeEvidence, id))
	}
	for _, id := range p.ProofObligationIDs {
		e.Triple(subj, rdf.IRI(rdf.PropDischargesProofObligation), rdf.MintIRI(rdf.ClassProofObligation, id))
	}
	for _, id := range p.ProofSlotIDs {
		e.Triple(subj, rdf.IRI(rdf.PropDischargesProofSlot), rdf.MintIRI(rdf.ClassProofSlot, id))
	}
	for _, id := range p.RepairPlanIDs {
		e.Triple(subj, rdf.IRI(rdf.PropProbeForRepairPlan), rdf.MintIRI(rdf.ClassRepairPlan, id))
	}
	for _, id := range p.TestIDs {
		e.Triple(subj, rdf.IRI(rdf.PropProbeForTest), rdf.MintIRI(rdf.ClassTest, id))
	}
	emitOptLit(e, subj, rdf.PropObservedFromService, p.OwnerService)
	for _, path := range p.ObservationPaths {
		e.Triple(subj, rdf.IRI(rdf.PropObservedViaPath), rdf.Lit(path))
	}
	emitOptLit(e, subj, rdf.PropHasFreshnessWindow, p.FreshnessWindow)
	emitOptLit(e, subj, rdf.PropHasTrustLevel, p.TrustLevel)
	if p.MustComeFromOwnerPath {
		e.Triple(subj, rdf.IRI(rdf.PropMustComeFromOwnerPath), rdf.Lit(strconv.FormatBool(p.MustComeFromOwnerPath)))
	}
	if p.CannotPromoteToPassWhenStale {
		e.Triple(subj, rdf.IRI(rdf.PropCannotPromoteToPassWhenStale), rdf.Lit(strconv.FormatBool(p.CannotPromoteToPassWhenStale)))
	}
	e.Triple(subj, rdf.IRI(rdf.PropSafetyClass), rdf.Lit(p.SafetyClass))
	e.Triple(subj, rdf.IRI(rdf.PropRequiresApprovalGate), rdf.Lit(p.ApprovalGate))
	e.Triple(subj, rdf.IRI(rdf.PropAutomaticExecutionAllowed), rdf.Lit(strconv.FormatBool(p.AutomaticExecutionAllowed)))
	for _, pre := range p.Preconditions {
		e.Triple(subj, rdf.IRI(rdf.PropRequiresPrecondition), rdf.Lit(pre))
	}
	for i, st := range p.Steps {
		step := fmt.Sprintf("%03d|%s|%s|%s|%s", i+1, st.Kind, st.Target, st.SourceRef, strings.ReplaceAll(st.Description, "\n", " "))
		if st.Command != "" {
			step += "|command:" + st.Command
		}
		e.Triple(subj, rdf.IRI(rdf.PropHasProbeStep), rdf.Lit(step))
	}
	for _, kind := range p.ExpectedArtifactKinds {
		e.Triple(subj, rdf.IRI(rdf.PropExpectedArtifactKind), rdf.Lit(kind))
	}
	e.Triple(subj, rdf.IRI(rdf.PropSourceClosureAssessmentDigest), rdf.Lit(doc.SourceClosureAssessmentDigestSHA256))
	e.Triple(subj, rdf.IRI(rdf.PropSourceDialogueDigest), rdf.Lit(doc.SourceDialogueDigestSHA256))
	e.Triple(subj, rdf.IRI(rdf.PropSourceClaimDocumentDigest), rdf.Lit(doc.SourceClaimDocumentDigestSHA256))
	emitProbeBinding(e, subj, doc.Binding)
	e.Triple(subj, rdf.IRI(rdf.PropSourceKind), rdf.Lit("generated_candidate"))
	e.Triple(subj, rdf.IRI(rdf.PropSourcePath), rdf.Lit(e.NormPath(path)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	if doc.Binding.RepositoryDomain != "" {
		e.Triple(subj, rdf.IRI(rdf.PropRepo), rdf.Lit(doc.Binding.RepositoryDomain))
	}
	e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(rdf.DomainRepo))
}

func emitProbeBinding(e *rdf.Emitter, subj string, binding architecture.ClaimDocumentBinding) {
	emitOptLit(e, subj, rdf.PropValidForCommit, binding.Revision)
	emitOptLit(e, subj, rdf.PropValidForGraphDigest, binding.GraphDigestSHA256)
}
