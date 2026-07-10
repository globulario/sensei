// SPDX-License-Identifier: Apache-2.0

// Importer for RepairPlan YAML files (Phase 2A).
//
// A RepairPlan is the safe, legal route back toward convergence for a class of
// failure: which failure modes / finding classes it addresses, the
// preconditions and verification it requires, its rollback, its blast radius,
// its approval gate, and the patterns/invariants/tests it is bound to. It is
// advisory compiled context — awareness plans, the owner service and workflow
// gate execute. The importer emits ONE RepairPlan node with flat literals plus
// object-link edges to EXISTING nodes (failure modes, authority domains,
// implementation patterns, invariants, tests); it never types those targets.
//
// Schema (composite-key detected by id + class:RepairPlan):
//
//	id:                            globular.repair.<slug>
//	class:                         RepairPlan          (discriminator)
//	label:                         Human-readable title
//	status:                        active | draft | deprecated
//	confidence:                    high | medium | low | unknown
//	blast_radius:                  local|service|node|cluster|security|data_loss|external
//	approval_gate:                 none|review_required|human_approval_required|
//	                               multi_step_approval_required|manual_only
//	repairs_failure_modes:         []class-qualified failure_mode refs
//	repairs_finding_classes:       []string
//	applies_to_authority_domains:  []class-qualified authority_domain refs
//	preconditions:                 []string
//	repair_steps:                  []string (ordered)
//	verification_steps:            []string
//	rollback_steps:                []string
//	uses_implementation_patterns:  []class-qualified implementation_pattern refs
//	must_not_violate_invariants:   []class-qualified invariant refs
//	governs.contracts:             []bare or class-qualified contract refs
//	governs.invariants:            []bare or class-qualified invariant refs
//	governs.failure_modes:         []bare or class-qualified failure_mode refs
//	governs.forbidden_fixes:       []bare or class-qualified forbidden_fix refs
//	expressed_by.files:            []repo-relative source file paths
//	expressed_by.symbols:          []code symbol ids
//	affected_components:           []bare component ids
//	required_tests:                []class-qualified test refs
//	requires_runtime_evidence:     []string (modelled fully by RuntimeEvidence, Phase 2C)
//	produces_outcome_feedback:     string
//	notes:                         free text → rdfs:comment
//
// Empty id is a soft skip.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

type yamlRepairPlan struct {
	ID                        string   `yaml:"id"`
	Class                     string   `yaml:"class"`
	Label                     string   `yaml:"label"`
	Status                    string   `yaml:"status"`
	WhenToUse                 []string `yaml:"when_to_use"`
	Confidence                string   `yaml:"confidence"`
	BlastRadius               string   `yaml:"blast_radius"`
	ApprovalGate              string   `yaml:"approval_gate"`
	CoversPaths               []string `yaml:"covers_paths"`
	RepairsFailureModes       []string `yaml:"repairs_failure_modes"`
	RepairsFindingClasses     []string `yaml:"repairs_finding_classes"`
	AppliesToAuthorityDomains []string `yaml:"applies_to_authority_domains"`
	Preconditions             []string `yaml:"preconditions"`
	RepairSteps               []string `yaml:"repair_steps"`
	VerificationSteps         []string `yaml:"verification_steps"`
	RollbackSteps             []string `yaml:"rollback_steps"`
	UsesImplementationPattern []string `yaml:"uses_implementation_patterns"`
	MustNotViolateInvariants  []string `yaml:"must_not_violate_invariants"`
	Governs                   struct {
		Contracts      []string `yaml:"contracts"`
		Invariants     []string `yaml:"invariants"`
		FailureModes   []string `yaml:"failure_modes"`
		ForbiddenFixes []string `yaml:"forbidden_fixes"`
	} `yaml:"governs"`
	ExpressedBy struct {
		Files   []string `yaml:"files"`
		Symbols []string `yaml:"symbols"`
	} `yaml:"expressed_by"`
	AffectedComponents      []string `yaml:"affected_components"`
	RequiredTests           []string `yaml:"required_tests"`
	RequiresRuntimeEvidence []string `yaml:"requires_runtime_evidence"`
	ProducesOutcomeFeedback string   `yaml:"produces_outcome_feedback"`
	Notes                   string   `yaml:"notes"`
}

func importRepairPlan(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var p yamlRepairPlan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if p.ID == "" {
		return nil
	}

	subj := rdf.MintIRI(rdf.ClassRepairPlan, p.ID)
	e.Typed(subj, rdf.ClassRepairPlan)

	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(p.Label, p.ID)))
	emitOptLit(e, subj, rdf.PropStatus, p.Status)
	emitOptLit(e, subj, rdf.PropHasConfidence, p.Confidence)
	emitOptLit(e, subj, rdf.PropHasBlastRadius, p.BlastRadius)
	emitOptLit(e, subj, rdf.PropRequiresApprovalGate, p.ApprovalGate)
	emitOptLit(e, subj, rdf.PropProducesOutcomeFeedback, p.ProducesOutcomeFeedback)
	if notes := strings.TrimSpace(p.Notes); notes != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(notes))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	// Ordered / scalar literal lists.
	emitOptLits(e, subj, rdf.PropActivationTrigger, p.WhenToUse)
	emitOptLits(e, subj, rdf.PropCoversPath, p.CoversPaths)
	emitOptLits(e, subj, rdf.PropRepairsFindingClass, p.RepairsFindingClasses)
	emitOptLits(e, subj, rdf.PropRequiresPrecondition, p.Preconditions)
	emitOptLits(e, subj, rdf.PropHasRepairStep, p.RepairSteps)
	emitOptLits(e, subj, rdf.PropRequiresVerification, p.VerificationSteps)
	emitOptLits(e, subj, rdf.PropHasRollbackStep, p.RollbackSteps)
	emitOptLits(e, subj, rdf.PropRequiresRuntimeEvidence, p.RequiresRuntimeEvidence)

	// Object-link edges to existing nodes — resolved from class-qualified refs.
	// Unresolvable refs are skipped (they cannot be real graph nodes).
	emitRefOrBareEdges(e, subj, rdf.PropRepairsFailureMode, rdf.ClassFailureMode, append(append([]string{}, p.RepairsFailureModes...), p.Governs.FailureModes...))
	emitRefEdges(e, subj, rdf.PropAppliesToAuthorityDomain, p.AppliesToAuthorityDomains)
	emitRefEdges(e, subj, rdf.PropUsesImplementationPattern, p.UsesImplementationPattern)
	emitRefOrBareEdges(e, subj, rdf.PropMustNotViolateInvariant, rdf.ClassInvariant, append(append([]string{}, p.MustNotViolateInvariants...), p.Governs.Invariants...))
	emitRefOrBareEdges(e, subj, rdf.PropGovernedByContract, rdf.ClassContract, p.Governs.Contracts)
	emitRefOrBareEdges(e, subj, rdf.PropForbids, rdf.ClassForbiddenFix, p.Governs.ForbiddenFixes)
	emitBareEdges(e, subj, rdf.PropAffectsComponent, rdf.ClassComponent, p.AffectedComponents)
	emitRefEdges(e, subj, rdf.PropRequiresTest, p.RequiredTests)
	for _, f := range p.ExpressedBy.Files {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		fileSubj := rdf.MintIRI(rdf.ClassSourceFile, f)
		e.Typed(fileSubj, rdf.ClassSourceFile)
		e.Triple(subj, rdf.IRI(rdf.PropExpressedBy), fileSubj)
		e.Triple(fileSubj, rdf.IRI(rdf.PropImplements), subj)
	}
	emitSpineAnchors(e, subj, nil, p.ExpressedBy.Symbols)

	return nil
}

// emitRefEdges resolves each class-qualified ref to its node IRI and emits an
// object edge from subj via prop. Linking is not authoring: the target is not
// typed here.
func emitRefEdges(e *rdf.Emitter, subj, prop string, refs []string) {
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if iri, ok := knowledgeRefToIRI(ref); ok {
			e.Triple(subj, rdf.IRI(prop), iri)
		}
	}
}

func emitRefOrBareEdges(e *rdf.Emitter, subj, prop, classIRI string, refs []string) {
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if iri, ok := knowledgeRefToIRI(ref); ok {
			e.Triple(subj, rdf.IRI(prop), iri)
			continue
		}
		e.Triple(subj, rdf.IRI(prop), rdf.MintIRI(classIRI, ref))
	}
}
